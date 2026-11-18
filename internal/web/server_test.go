package web

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"photo-clean/internal/classify/mock"
	"photo-clean/internal/config"
	"photo-clean/internal/zipset"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	cfg := config.Default()
	cfg.Paths.Jobs = t.TempDir()
	cfg.Classify.Concurrency = 2
	cfg.Classify.Timeout = config.Duration(2 * time.Second)
	cfg.Web.MaxUploadBytes = 5 << 20
	cfg.Web.MaxJobs = 1
	cfg.Web.JobTTL = config.Duration(time.Hour)
	cfg.Zip.MaxFileBytes = 1 << 20
	cfg.Zip.MaxFiles = 100

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := NewServer(cfg, mock.New(), log)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// pickImages devuelve imágenes cuyo hash hace que el proveedor mock las
// conserve o las descarte, para tener un caso determinista.
func pickImages(t *testing.T) (accepted, rejected []zipset.Item) {
	t.Helper()
	for i := 0; i < 500 && (len(accepted) < 2 || len(rejected) < 2); i++ {
		data := []byte(fmt.Sprintf("imagen-mock-%d", i))
		sum := sha256.Sum256(data)
		item := zipset.Item{Name: fmt.Sprintf("foto-%d.jpg", i), Data: data}
		if sum[0]%10 < 7 {
			if len(accepted) < 2 {
				accepted = append(accepted, item)
			}
		} else if len(rejected) < 2 {
			rejected = append(rejected, item)
		}
	}
	if len(accepted) != 2 || len(rejected) != 2 {
		t.Fatal("no se encontraron imágenes para el test")
	}
	return accepted, rejected
}

func buildZip(t *testing.T, items []zipset.Item) []byte {
	t.Helper()
	path := t.TempDir() + "/fotos.zip"
	if err := zipset.Write(path, items); err != nil {
		t.Fatalf("zipset.Write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("leer zip: %v", err)
	}
	return data
}

func multipartBody(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("zip", name)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("escribir parte: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("cerrar multipart: %v", err)
	}
	return &body, w.FormDataContentType()
}

func postZip(t *testing.T, url, name string, data []byte, htmx bool) *http.Response {
	t.Helper()
	body, contentType := multipartBody(t, name, data)
	req, err := http.NewRequest(http.MethodPost, url+"/jobs", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("leer respuesta: %v", err)
	}
	return string(data)
}

func TestUploadFlow(t *testing.T) {
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	accepted, rejected := pickImages(t)
	zipData := buildZip(t, append(append([]zipset.Item{}, accepted...), rejected...))

	resp := postZip(t, httpSrv.URL, "fotos.zip", zipData, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	uploadHTML := readBody(t, resp)

	match := regexp.MustCompile(`/jobs/([a-f0-9]{32})/progress`).FindStringSubmatch(uploadHTML)
	if len(match) != 2 {
		t.Fatalf("no se encontró el id en la respuesta: %s", uploadHTML)
	}
	id := match[1]

	job, ok := srv.mgr.Get(id)
	if !ok {
		t.Fatalf("trabajo %s no encontrado", id)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job.snapshot().Finished {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	snap := job.snapshot()
	if snap.Status != StatusDone {
		t.Fatalf("estado = %s (%s)", snap.Status, snap.ErrMsg)
	}
	if snap.Accepted != 2 || snap.Rejected != 2 || snap.Failed != 0 {
		t.Fatalf("contadores = %+v", snap)
	}

	resp = mustGet(t, httpSrv.URL+"/jobs/"+id+"/progress")
	progressHTML := readBody(t, resp)
	if !strings.Contains(progressHTML, "Descargar conservadas") {
		t.Fatalf("la vista final no tiene descargas: %s", progressHTML)
	}

	acceptedZip := download(t, httpSrv.URL+"/jobs/"+id+"/aceptadas.zip")
	assertZipNames(t, acceptedZip, names(accepted))
	rejectedZip := download(t, httpSrv.URL+"/jobs/"+id+"/descartadas.zip")
	assertZipNames(t, rejectedZip, names(rejected))
}

func TestRejectsInvalidUploads(t *testing.T) {
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp := postZip(t, httpSrv.URL, "malo.zip", []byte("esto no es un zip"), true)
	body := readBody(t, resp)
	if !strings.Contains(body, "no es un ZIP válido") {
		t.Fatalf("respuesta inesperada: %s", body)
	}

	resp = postZip(t, httpSrv.URL, "fotos.txt", []byte("texto"), true)
	body = readBody(t, resp)
	if !strings.Contains(body, "extensión .zip") {
		t.Fatalf("respuesta inesperada: %s", body)
	}
}

func TestProgressShowsWorkingState(t *testing.T) {
	srv := newTestServer(t)

	cases := []struct {
		status Status
		want   string
	}{
		{StatusQueued, "en cola"},
		{StatusReading, "leyendo las imágenes"},
		{StatusRunning, "Clasificando las imágenes"},
	}

	for _, tc := range cases {
		snap := Snapshot{
			ID:         strings.Repeat("a", 32),
			Status:     tc.status,
			StatusText: statusText(tc.status),
			Total:      10,
			Done:       1,
			Percent:    10,
		}

		var buf bytes.Buffer
		if err := srv.tmpl.ExecuteTemplate(&buf, "progress.html", pageData{Job: &snap}); err != nil {
			t.Fatalf("render progreso (%s): %v", tc.status, err)
		}

		html := buf.String()
		if !strings.Contains(strings.ToLower(html), strings.ToLower(tc.want)) {
			t.Fatalf("estado %s: falta %q en el panel:\n%s", tc.status, tc.want, html)
		}
		if !strings.Contains(html, "spinner") {
			t.Fatalf("estado %s: el panel no muestra spinner", tc.status)
		}
		if !strings.Contains(html, `hx-trigger="every 1s"`) {
			t.Fatalf("estado %s: el panel no actualiza solo", tc.status)
		}
	}
}

func TestInvalidJobID(t *testing.T) {
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp := mustGet(t, httpSrv.URL+"/jobs/no-es-un-id/progress")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, quiero 404", resp.StatusCode)
	}
}

func TestIndexAndRedirect(t *testing.T) {
	srv := newTestServer(t)
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp := mustGet(t, httpSrv.URL+"/")
	body := readBody(t, resp)
	if !strings.Contains(body, "photo-clean") || !strings.Contains(body, "Arrastra tu ZIP") {
		t.Fatalf("index inesperado: %s", body)
	}

	accepted, rejected := pickImages(t)
	zipData := buildZip(t, append(append([]zipset.Item{}, accepted...), rejected...))

	bodyBuf, contentType := multipartBody(t, "fotos.zip", zipData)
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/jobs", bodyBuf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, quiero 303", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Location"), "/jobs/") {
		t.Fatalf("location = %q", resp.Header.Get("Location"))
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func download(t *testing.T, url string) []byte {
	t.Helper()
	resp := mustGet(t, url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content-type = %q", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("leer zip: %v", err)
	}
	return data
}

func assertZipNames(t *testing.T, data []byte, want []string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("abrir zip: %v", err)
	}
	got := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		got = append(got, file.Name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("nombres = %v, quiero %v", got, want)
	}
}

func names(items []zipset.Item) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Name
	}
	return out
}
