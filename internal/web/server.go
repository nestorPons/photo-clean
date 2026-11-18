package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	webassets "photo-clean/web"

	"photo-clean/internal/classify"
	"photo-clean/internal/config"
)

const (
	acceptedFile = "aceptadas.zip"
	rejectedFile = "descartadas.zip"

	acceptedDownload = "photo-clean-conservadas.zip"
	rejectedDownload = "photo-clean-eliminadas.zip"
)

// Server sirve la interfaz web y la API de trabajos.
type Server struct {
	cfg    config.Config
	mgr    *Manager
	tmpl   *template.Template
	static http.Handler
	log    *slog.Logger

	cleanupCtx    context.Context
	cleanupCancel context.CancelFunc
}

type pageData struct {
	MaxUploadMB int64
	MaxFiles    int
	Job         *Snapshot
	Error       string
}

// NewServer prepara plantillas, assets y el gestor de trabajos.
func NewServer(cfg config.Config, cl classify.Classifier, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}

	mgr, err := NewManager(cfg, cl, log)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.ParseFS(webassets.Assets, "templates/*.html")
	if err != nil {
		mgr.Close()
		return nil, fmt.Errorf("cargar plantillas: %w", err)
	}

	staticFS, err := fs.Sub(webassets.Assets, "static")
	if err != nil {
		mgr.Close()
		return nil, fmt.Errorf("cargar assets: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg:           cfg,
		mgr:           mgr,
		tmpl:          tmpl,
		static:        http.FileServer(http.FS(staticFS)),
		log:           log,
		cleanupCtx:    ctx,
		cleanupCancel: cancel,
	}
	go mgr.Cleanup(ctx)
	return s, nil
}

// Close detiene la limpieza y los trabajos en curso.
func (s *Server) Close() {
	s.cleanupCancel()
	s.mgr.Close()
}

// Handler devuelve el árbol de rutas con middlewares.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.logRequests(s.security(s.routes())))
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /jobs", s.handleCreateJob)
	mux.HandleFunc("GET /jobs/{id}", s.handleJobPage)
	mux.HandleFunc("GET /jobs/{id}/progress", s.handleProgress)
	mux.HandleFunc("GET /jobs/{id}/aceptadas.zip", s.handleDownload(acceptedFile, acceptedDownload))
	mux.HandleFunc("GET /jobs/{id}/descartadas.zip", s.handleDownload(rejectedFile, rejectedDownload))
	mux.Handle("GET /static/", http.StripPrefix("/static/", s.static))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.renderError(w, r, http.StatusNotFound, "Esa página no existe.")
		return
	}
	s.render(w, http.StatusOK, "index.html", s.baseData())
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.Web.MaxUploadBytes)

	if err := r.ParseMultipartForm(8 << 20); err != nil {
		s.renderError(w, r, http.StatusBadRequest, s.uploadError(err))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, header, err := r.FormFile("zip")
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Selecciona un archivo ZIP.")
		return
	}
	defer file.Close()

	if header.Size <= 0 {
		s.renderError(w, r, http.StatusBadRequest, "El archivo está vacío.")
		return
	}
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		s.renderError(w, r, http.StatusBadRequest, "El archivo debe tener extensión .zip.")
		return
	}

	tmp, err := os.CreateTemp(s.mgr.Root(), "subida-*.zip")
	if err != nil {
		s.log.Error("crear temporal de subida", "err", err)
		s.renderError(w, r, http.StatusInternalServerError, "No se pudo guardar la subida.")
		return
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, file); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		s.renderError(w, r, http.StatusBadRequest, "No se pudo leer el ZIP.")
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		s.renderError(w, r, http.StatusInternalServerError, "No se pudo guardar la subida.")
		return
	}

	if err := checkZip(tmpName); err != nil {
		_ = os.Remove(tmpName)
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	snap, err := s.mgr.Submit(tmpName)
	if err != nil {
		_ = os.Remove(tmpName)
		s.log.Error("crear trabajo", "err", err)
		s.renderError(w, r, http.StatusInternalServerError, "No se pudo iniciar el trabajo.")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		s.render(w, http.StatusOK, "progress.html", s.baseData().withJob(snap))
		return
	}
	http.Redirect(w, r, "/jobs/"+snap.ID, http.StatusSeeOther)
}

func (s *Server) handleJobPage(w http.ResponseWriter, r *http.Request) {
	job, ok := s.lookupJob(r)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "Ese trabajo no existe o ha caducado.")
		return
	}

	snap := job.snapshot()
	data := s.baseData().withJob(&snap)
	if snap.Status == StatusFailed {
		data.Error = snap.ErrMsg
	}
	s.render(w, http.StatusOK, "job.html", data)
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	job, ok := s.lookupJob(r)
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "Ese trabajo no existe o ha caducado.")
		return
	}

	snap := job.snapshot()
	data := s.baseData().withJob(&snap)
	if snap.Status == StatusFailed {
		data.Error = snap.ErrMsg
		s.render(w, http.StatusOK, "error.html", data)
		return
	}
	if snap.Finished {
		s.render(w, http.StatusOK, "result.html", data)
		return
	}
	s.render(w, http.StatusOK, "progress.html", data)
}

func (s *Server) handleDownload(onDisk, downloadName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, ok := s.lookupJob(r)
		if !ok {
			http.Error(w, "trabajo no encontrado", http.StatusNotFound)
			return
		}

		snap := job.snapshot()
		if !snap.Finished || snap.Status == StatusFailed {
			http.Error(w, "el ZIP todavía no está listo", http.StatusConflict)
			return
		}

		path := filepath.Join(job.dir, onDisk)
		if _, err := os.Stat(path); err != nil {
			http.Error(w, "zip no disponible", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, path)
	}
}

func (s *Server) lookupJob(r *http.Request) (*Job, bool) {
	id := r.PathValue("id")
	if !validID(id) {
		return nil, false
	}
	return s.mgr.Get(id)
}

func (s *Server) baseData() pageData {
	return pageData{
		MaxUploadMB: (s.cfg.Web.MaxUploadBytes + (1 << 20) - 1) >> 20,
		MaxFiles:    s.cfg.Zip.MaxFiles,
	}
}

func (d pageData) withJob(snap *Snapshot) pageData {
	d.Job = snap
	return d
}

func (s *Server) uploadError(err error) string {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		mb := (s.cfg.Web.MaxUploadBytes + (1 << 20) - 1) >> 20
		return fmt.Sprintf("El ZIP supera el límite de %d MB.", mb)
	}
	return "No se pudo leer el formulario. Inténtalo de nuevo."
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := s.baseData()
	data.Error = message

	if r.Header.Get("HX-Request") == "true" {
		s.render(w, http.StatusOK, "error.html", data)
		return
	}
	s.render(w, status, "index.html", data)
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data pageData) {
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Error("renderizar plantilla", "template", name, "err", err)
		http.Error(w, "error interno", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func checkZip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("no se pudo abrir el ZIP")
	}
	defer f.Close()

	var sig [4]byte
	if _, err := io.ReadFull(f, sig[:]); err != nil {
		return errors.New("el archivo no es un ZIP válido")
	}
	if !bytes.Equal(sig[:], []byte{'P', 'K', 0x03, 0x04}) &&
		!bytes.Equal(sig[:], []byte{'P', 'K', 0x05, 0x06}) {
		return errors.New("el archivo no es un ZIP válido")
	}
	return nil
}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; "+
				"connect-src 'self'; form-action 'self'; base-uri 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.statusCode(),
			"dur", time.Since(start).String(),
		)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.log.Error("panic", "path", r.URL.Path, "err", recovered)
				http.Error(w, "error interno", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
