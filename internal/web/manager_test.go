package web

import (
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"photo-clean/internal/classify/mock"
	"photo-clean/internal/config"
)

func TestManagerTempRootCleanup(t *testing.T) {
	cfg := config.Default()
	cfg.Paths.Jobs = ""
	cfg.Web.MaxJobs = 1
	cfg.Web.JobTTL = config.Duration(time.Minute)
	cfg.Classify.Timeout = config.Duration(time.Second)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr, err := NewManager(cfg, mock.New(), log)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	root := mgr.Root()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("el directorio temporal no existe: %v", err)
	}

	mgr.Close()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("el directorio temporal no se borró: %v", err)
	}
}
