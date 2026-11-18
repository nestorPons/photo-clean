package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadShippedFile(t *testing.T) {
	cfg, err := LoadFile("../../config/config.yaml")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if cfg.Classify.Provider != "decisions" {
		t.Errorf("provider = %q, quiero decisions", cfg.Classify.Provider)
	}
	if cfg.Classify.Threshold != 0.5 {
		t.Errorf("threshold = %v", cfg.Classify.Threshold)
	}
	if cfg.Classify.Timeout.Std().String() != "1m0s" {
		t.Errorf("timeout = %s", cfg.Classify.Timeout.Std())
	}
	if cfg.Decisions.Endpoint != "/decisions" {
		t.Errorf("decisions.endpoint = %q", cfg.Decisions.Endpoint)
	}
	if cfg.Decisions.Model != "gpt-6-luna" {
		t.Errorf("decisions.model = %q", cfg.Decisions.Model)
	}
	if cfg.OpenAI.Endpoint != "/chat/completions" {
		t.Errorf("openai.endpoint = %q", cfg.OpenAI.Endpoint)
	}
	if cfg.Decisions.Timeout.Std() <= 0 || cfg.Decisions.Retries < 1 {
		t.Errorf("decisions.timeout/retries inválidos")
	}
	if cfg.Paths.Report != "out/informe.json" {
		t.Errorf("paths.report = %q", cfg.Paths.Report)
	}
	if cfg.Zip.MaxFiles != 5000 {
		t.Errorf("zip.max_files = %d", cfg.Zip.MaxFiles)
	}
	if cfg.Zip.MaxTotalBytes != 1<<30 {
		t.Errorf("zip.max_total_bytes = %d", cfg.Zip.MaxTotalBytes)
	}
	if cfg.Paths.Jobs != "" {
		t.Errorf("paths.jobs = %q, quiero vacío (temporal)", cfg.Paths.Jobs)
	}
	if cfg.Web.MaxJobs != 2 || cfg.Web.MaxUploadBytes != 300<<20 {
		t.Errorf("web = %+v", cfg.Web)
	}
	if cfg.Prompts.DecisionsHasSubject == "" || cfg.Prompts.OpenAI == "" {
		t.Error("faltan prompts en la configuración")
	}
	if !strings.Contains(cfg.Prompts.OpenAI, "has_subject") {
		t.Error("el prompt de OpenAI debe pedir el JSON esperado")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("CONFIG_FILE", "../../config/config.yaml")
	t.Setenv("CLASSIFIER_PROVIDER", "openai")
	t.Setenv("DECISIONS_MAX_IMAGE_SIDE", "768")
	t.Setenv("OPENAI_MODEL", "gpt-test")
	t.Setenv("WEB_MAX_JOBS", "3")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Classify.Provider != "openai" {
		t.Errorf("provider = %q", cfg.Classify.Provider)
	}
	if cfg.Decisions.MaxImageSide != 768 {
		t.Errorf("decisions.max_image_side = %d", cfg.Decisions.MaxImageSide)
	}
	if cfg.OpenAI.Model != "gpt-test" {
		t.Errorf("openai.model = %q", cfg.OpenAI.Model)
	}
	if cfg.Web.MaxJobs != 3 {
		t.Errorf("web.max_jobs = %d", cfg.Web.MaxJobs)
	}
}

func TestPromptsOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prompts.yaml")
	content := "prompts:\n  openai: \"prompt personalizado para el test\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Prompts.OpenAI != "prompt personalizado para el test" {
		t.Errorf("prompts.openai = %q", cfg.Prompts.OpenAI)
	}
	if cfg.Prompts.System == "" {
		t.Error("los prompts no sobrescritos deben conservar el valor por defecto")
	}
}

func TestLoadFileMissing(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "no-existe.yaml")); err == nil {
		t.Fatal("se esperaba error")
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]string{
		"threshold":   "classify:\n  threshold: 2\n",
		"provider":    "classify:\n  provider: inventado\n",
		"endpoint":    "decisions:\n  endpoint: api/v1\n",
		"concurrency": "classify:\n  concurrency: 0\n",
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".yaml")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := LoadFile(path); err == nil {
				t.Fatal("se esperaba error de validación")
			}
		})
	}
}
