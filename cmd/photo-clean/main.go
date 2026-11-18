// Comando photo-clean: servidor web para limpiar ZIP de fotos.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"photo-clean/internal/classifier"
	"photo-clean/internal/config"
	"photo-clean/internal/envfile"
	"photo-clean/internal/web"
)

func main() {
	if err := envfile.Load(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cl, err := classifier.Build(cfg.Classify.Provider, cfg, cfg.Classify.Threshold)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	app, err := web.NewServer(cfg, cl, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer app.Close()

	srv := &http.Server{
		Addr:              cfg.App.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("photo-clean escuchando",
		"addr", cfg.App.Addr,
		"config", cfg.Path,
		"env", cfg.App.Environment,
		"provider", cl.Provider(),
		"max_upload_mb", (cfg.Web.MaxUploadBytes+(1<<20)-1)>>20,
		"max_jobs", cfg.Web.MaxJobs,
	)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("servidor detenido", "err", err)
		os.Exit(1)
	}
}
