package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"photo-clean/internal/classify"
	"photo-clean/internal/config"
	"photo-clean/internal/zipset"
)

// Status es el estado de un trabajo de limpieza.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusReading Status = "reading"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

const previewLimit = 200

// Job representa una subida y su progreso.
type Job struct {
	ID        string
	CreatedAt time.Time
	dir       string

	mu       sync.RWMutex
	status   Status
	total    int
	done     int
	accepted int
	rejected int
	failed   int
	errMsg   string

	acceptedNames []string
	rejectedNames []string
	acceptedMore  int
	rejectedMore  int
}

// Snapshot es una copia segura de Job para plantillas y handlers.
type Snapshot struct {
	ID            string
	CreatedAt     time.Time
	Status        Status
	StatusText    string
	Total         int
	Done          int
	Percent       int
	Accepted      int
	Rejected      int
	Failed        int
	ErrMsg        string
	Finished      bool
	AcceptedNames []string
	RejectedNames []string
	AcceptedMore  int
	RejectedMore  int
}

func (j *Job) snapshot() Snapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()

	percent := 0
	if j.total > 0 {
		percent = j.done * 100 / j.total
		if percent > 100 {
			percent = 100
		}
	}

	return Snapshot{
		ID:            j.ID,
		CreatedAt:     j.CreatedAt,
		Status:        j.status,
		StatusText:    statusText(j.status),
		Total:         j.total,
		Done:          j.done,
		Percent:       percent,
		Accepted:      j.accepted,
		Rejected:      j.rejected,
		Failed:        j.failed,
		ErrMsg:        j.errMsg,
		Finished:      j.status == StatusDone || j.status == StatusFailed,
		AcceptedNames: append([]string(nil), j.acceptedNames...),
		RejectedNames: append([]string(nil), j.rejectedNames...),
		AcceptedMore:  j.acceptedMore,
		RejectedMore:  j.rejectedMore,
	}
}

func (j *Job) setStatus(status Status) {
	j.mu.Lock()
	j.status = status
	j.mu.Unlock()
}

func (j *Job) start(total int) {
	j.mu.Lock()
	j.status = StatusRunning
	j.total = total
	j.done = 0
	j.accepted = 0
	j.rejected = 0
	j.failed = 0
	j.mu.Unlock()
}

func (j *Job) record(verdict int8) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.done++
	switch verdict {
	case 1:
		j.accepted++
	case -1:
		j.rejected++
	default:
		j.failed++
	}
}

func (j *Job) finish(accepted, rejected []string) {
	acceptedPreview, acceptedMore := preview(accepted)
	rejectedPreview, rejectedMore := preview(rejected)

	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = StatusDone
	j.done = j.total
	j.accepted = len(accepted)
	j.rejected = len(rejected)
	j.acceptedNames = acceptedPreview
	j.rejectedNames = rejectedPreview
	j.acceptedMore = acceptedMore
	j.rejectedMore = rejectedMore
}

func (j *Job) fail(message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = StatusFailed
	j.errMsg = message
}

func statusText(status Status) string {
	switch status {
	case StatusQueued:
		return "En cola"
	case StatusReading:
		return "Leyendo ZIP"
	case StatusRunning:
		return "Clasificando"
	case StatusDone:
		return "Listo"
	case StatusFailed:
		return "Error"
	default:
		return string(status)
	}
}

func preview(names []string) ([]string, int) {
	if len(names) <= previewLimit {
		return append([]string(nil), names...), 0
	}
	return append([]string(nil), names[:previewLimit]...), len(names) - previewLimit
}

// Manager orquesta la subida, el procesado y la limpieza de trabajos.
type Manager struct {
	cfg  config.Config
	cl   classify.Classifier
	log  *slog.Logger
	root string

	tempRoot bool
	sem      chan struct{}

	mu   sync.RWMutex
	jobs map[string]*Job

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once

	now func() time.Time
}

// NewManager crea el gestor y prepara el directorio de trabajos.
func NewManager(cfg config.Config, cl classify.Classifier, log *slog.Logger) (*Manager, error) {
	if cl == nil {
		return nil, errors.New("clasificador vacío")
	}
	if log == nil {
		log = slog.Default()
	}

	root := strings.TrimSpace(cfg.Paths.Jobs)
	tempRoot := false
	if root == "" {
		dir, err := os.MkdirTemp("", "photo-clean-*")
		if err != nil {
			return nil, fmt.Errorf("crear directorio temporal: %w", err)
		}
		root = dir
		tempRoot = true
	} else if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("crear directorio de trabajos: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:      cfg,
		cl:       cl,
		log:      log,
		root:     root,
		tempRoot: tempRoot,
		sem:      make(chan struct{}, cfg.Web.MaxJobs),
		jobs:     make(map[string]*Job),
		ctx:      ctx,
		cancel:   cancel,
		now:      time.Now,
	}
	m.purgeStaleDirs()
	return m, nil
}

// Root devuelve el directorio donde se guardan las subidas.
func (m *Manager) Root() string { return m.root }

// Close detiene los trabajos en curso y borra los archivos temporales.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.cancel()
		m.wg.Wait()

		m.mu.Lock()
		dirs := make([]string, 0, len(m.jobs))
		for id, job := range m.jobs {
			dirs = append(dirs, job.dir)
			delete(m.jobs, id)
		}
		m.mu.Unlock()

		for _, dir := range dirs {
			_ = os.RemoveAll(dir)
		}
		if m.tempRoot {
			_ = os.RemoveAll(m.root)
		}
	})
}

// Submit mueve el ZIP al directorio del trabajo y lanza el procesado.
func (m *Manager) Submit(zipPath string) (*Snapshot, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(m.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crear trabajo: %w", err)
	}

	dst := filepath.Join(dir, "entrada.zip")
	if err := moveFile(zipPath, dst); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("guardar zip: %w", err)
	}

	job := &Job{
		ID:        id,
		CreatedAt: m.now(),
		dir:       dir,
		status:    StatusQueued,
	}

	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(job, dst)
	}()

	snap := job.snapshot()
	return &snap, nil
}

// Get devuelve un trabajo por id.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	return job, ok
}

// Cleanup elimina trabajos caducados hasta que se cancele el contexto.
func (m *Manager) Cleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.purge()
		}
	}
}

func (m *Manager) run(job *Job, zipPath string) {
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-m.ctx.Done():
		job.fail("servidor detenido")
		return
	}

	job.setStatus(StatusReading)
	items, skipped, err := zipset.ReadLimit(zipPath, m.cfg.Zip.MaxFileBytes, m.cfg.Zip.MaxFiles, m.cfg.Zip.MaxTotalBytes)
	if err != nil {
		job.fail(err.Error())
		m.log.Warn("trabajo fallido al leer zip", "job", job.ID, "err", err)
		return
	}
	for _, reason := range skipped {
		m.log.Warn("entrada omitida", "job", job.ID, "detalle", reason)
	}

	job.start(len(items))
	verdicts := make([]int8, len(items))

	queue := make(chan int)
	var wg sync.WaitGroup

	workers := m.cfg.Classify.Concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(items) {
		workers = len(items)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range queue {
				verdicts[i] = m.classifyOne(job, items[i])
				job.record(verdicts[i])
			}
		}()
	}

	cancelled := false
	for i := range items {
		select {
		case queue <- i:
		case <-m.ctx.Done():
			cancelled = true
		}
		if cancelled {
			break
		}
	}
	close(queue)
	wg.Wait()

	if cancelled {
		job.fail("servidor detenido")
		return
	}

	accepted := make([]zipset.Item, 0, len(items))
	rejected := make([]zipset.Item, 0, len(items))
	acceptedNames := make([]string, 0, len(items))
	rejectedNames := make([]string, 0, len(items))

	for i, verdict := range verdicts {
		switch verdict {
		case 1:
			accepted = append(accepted, items[i])
			acceptedNames = append(acceptedNames, items[i].Name)
		case -1:
			rejected = append(rejected, items[i])
			rejectedNames = append(rejectedNames, items[i].Name)
		}
	}

	if err := zipset.Write(filepath.Join(job.dir, "aceptadas.zip"), accepted); err != nil {
		job.fail("no se pudo crear el ZIP de conservadas")
		m.log.Error("zip conservadas", "job", job.ID, "err", err)
		return
	}
	if err := zipset.Write(filepath.Join(job.dir, "descartadas.zip"), rejected); err != nil {
		job.fail("no se pudo crear el ZIP de eliminadas")
		m.log.Error("zip eliminadas", "job", job.ID, "err", err)
		return
	}

	job.finish(acceptedNames, rejectedNames)
	m.log.Info("trabajo terminado",
		"job", job.ID,
		"total", len(items),
		"conservadas", len(accepted),
		"eliminadas", len(rejected),
		"errores", len(items)-len(accepted)-len(rejected),
	)
}

func (m *Manager) classifyOne(job *Job, item zipset.Item) int8 {
	timeout := m.cfg.Classify.Timeout.Std()
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()

	result, err := m.cl.Classify(ctx, item.Data)
	if err != nil {
		m.log.Warn("clasificación fallida", "job", job.ID, "imagen", item.Name, "err", err)
		return 0
	}
	if result.HasSubject {
		return 1
	}
	return -1
}

func (m *Manager) purge() {
	cutoff := m.now().Add(-m.cfg.Web.JobTTL.Std())

	m.mu.Lock()
	var dirs []string
	for id, job := range m.jobs {
		snap := job.snapshot()
		if snap.Finished && snap.CreatedAt.Before(cutoff) {
			delete(m.jobs, id)
			dirs = append(dirs, job.dir)
		}
	}
	m.mu.Unlock()

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			m.log.Warn("limpiar trabajo", "dir", dir, "err", err)
		}
	}
}

func (m *Manager) purgeStaleDirs() {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return
	}
	cutoff := m.now().Add(-m.cfg.Web.JobTTL.Std())
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(m.root, entry.Name()))
		}
	}
}

func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generar id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
