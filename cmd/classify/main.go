// Comando classify: testea el clasificador contra un dataset etiquetado o
// contra un ZIP de imágenes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	classifierfactory "photo-clean/internal/classifier"
	"photo-clean/internal/classify"
	"photo-clean/internal/config"
	"photo-clean/internal/dataset"
	"photo-clean/internal/envfile"
	"photo-clean/internal/evaluate"
	"photo-clean/internal/zipset"
)

var errBelowRecall = errors.New("recall por debajo del objetivo")

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errBelowRecall) {
			fmt.Fprintln(os.Stderr, "test NO superado:", err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type options struct {
	input       string
	provider    string
	out         string
	concurrency int
	limit       int
	threshold   float64
	timeout     time.Duration
	minRecall   float64
	verbose     bool

	zipPath  string
	truth    string
	accepted string
	rejected string

	maxZipFileBytes  int64
	maxZipFiles      int
	maxZipTotalBytes int64
}

func run() error {
	if err := envfile.Load(".env"); err != nil {
		return fmt.Errorf("cargar .env: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	opts, err := parseFlags(cfg)
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if opts.verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	classifier, err := classifierfactory.Build(opts.provider, cfg, opts.threshold)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.zipPath != "" {
		return runZip(ctx, classifier, opts)
	}
	return runDataset(ctx, classifier, opts)
}

func parseFlags(cfg config.Config) (options, error) {
	var opts options

	fs := flag.NewFlagSet("classify", flag.ContinueOnError)
	fs.StringVar(&opts.input, "in", cfg.Paths.Dataset, "directorio del dataset (subcarpetas motivo/ y paisaje/)")
	fs.StringVar(&opts.provider, "provider", cfg.Classify.Provider, "proveedor: decisions | openai | chain | mock")
	fs.StringVar(&opts.out, "out", cfg.Paths.Report, "ruta del informe JSON")
	fs.IntVar(&opts.concurrency, "concurrency", cfg.Classify.Concurrency, "peticiones simultáneas")
	fs.IntVar(&opts.limit, "limit", 0, "máximo de muestras (0 = todas)")
	fs.Float64Var(&opts.threshold, "threshold", cfg.Classify.Threshold, "umbral de probabilidad para considerar que hay motivo")
	fs.DurationVar(&opts.timeout, "timeout", cfg.Classify.Timeout.Std(), "timeout por imagen")
	fs.Float64Var(&opts.minRecall, "min-recall", cfg.Classify.MinRecall, "recall mínimo para dar el test por superado")
	fs.BoolVar(&opts.verbose, "v", false, "log detallado")

	fs.StringVar(&opts.zipPath, "zip", "", "clasificar un ZIP en lugar de un dataset")
	fs.StringVar(&opts.truth, "truth", "paisaje", "etiqueta supuesta para las imágenes del ZIP: motivo | paisaje")
	fs.StringVar(&opts.accepted, "accepted", cfg.Paths.AcceptedZip, "ZIP de salida con las imágenes conservadas")
	fs.StringVar(&opts.rejected, "rejected", cfg.Paths.RejectedZip, "ZIP de salida con las imágenes descartadas")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Uso: go run ./cmd/classify [flags]\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		return options{}, err
	}
	if opts.threshold <= 0 || opts.threshold > 1 {
		return options{}, errors.New("-threshold debe estar entre 0 y 1")
	}
	if opts.concurrency < 1 {
		opts.concurrency = 1
	}
	if opts.truth != "motivo" && opts.truth != "paisaje" {
		return options{}, errors.New("-truth debe ser motivo o paisaje")
	}
	opts.maxZipFileBytes = cfg.Zip.MaxFileBytes
	opts.maxZipFiles = cfg.Zip.MaxFiles
	opts.maxZipTotalBytes = cfg.Zip.MaxTotalBytes
	return opts, nil
}

func runDataset(ctx context.Context, classifier classify.Classifier, opts options) error {
	samples, err := dataset.Load(opts.input)
	if err != nil {
		return err
	}
	if opts.limit > 0 && opts.limit < len(samples) {
		samples = samples[:opts.limit]
	}

	fmt.Printf("Clasificando %d imágenes con %s...\n", len(samples), classifier.Provider())

	startedAt := time.Now()
	items := make([]evaluate.Prediction, len(samples))
	classifyAll(ctx, classifier, opts, samples, items)
	if err := ctx.Err(); err != nil {
		return err
	}

	report := evaluate.Build(classifier.Provider(), opts.input, opts.threshold, startedAt, items)
	if err := writeReport(opts.out, report); err != nil {
		return err
	}

	fmt.Println()
	fmt.Print(report.Summary())
	fmt.Printf("Informe: %s\n", opts.out)
	printFalseNegatives(report)

	if report.Metrics.Recall < opts.minRecall {
		return fmt.Errorf("%w: %.3f < %.3f", errBelowRecall, report.Metrics.Recall, opts.minRecall)
	}
	return nil
}

func runZip(ctx context.Context, classifier classify.Classifier, opts options) error {
	items, skipped, err := zipset.ReadLimit(opts.zipPath, opts.maxZipFileBytes, opts.maxZipFiles, opts.maxZipTotalBytes)
	if err != nil {
		return err
	}

	fmt.Printf("Clasificando %d imágenes de %s con %s (%d omitidas)...\n",
		len(items), opts.zipPath, classifier.Provider(), len(skipped))
	for _, reason := range skipped {
		slog.Warn("entrada omitida", "detalle", reason)
	}

	samples := make([]dataset.Sample, len(items))
	for i, item := range items {
		samples[i] = dataset.Sample{
			Name:  item.Name,
			Label: opts.truth,
			Truth: opts.truth == "motivo",
			Data:  item.Data,
		}
	}

	startedAt := time.Now()
	predictions := make([]evaluate.Prediction, len(samples))
	classifyAll(ctx, classifier, opts, samples, predictions)
	if err := ctx.Err(); err != nil {
		return err
	}

	var accepted, rejected []zipset.Item
	for i, pred := range predictions {
		if pred.Predicted {
			accepted = append(accepted, items[i])
		} else {
			rejected = append(rejected, items[i])
		}
	}

	if err := zipset.Write(opts.accepted, accepted); err != nil {
		return err
	}
	if err := zipset.Write(opts.rejected, rejected); err != nil {
		return err
	}

	report := evaluate.Build(classifier.Provider(), "zip:"+opts.zipPath, opts.threshold, startedAt, predictions)
	if err := writeReport(opts.out, report); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("Conservadas: %d    Descartadas: %d    Errores: %d\n",
		len(accepted), len(rejected), report.Errors)
	fmt.Printf("ZIP conservadas: %s\n", opts.accepted)
	fmt.Printf("ZIP descartadas: %s\n", opts.rejected)
	fmt.Printf("Informe: %s\n", opts.out)

	return nil
}

func classifyAll(ctx context.Context, classifier classify.Classifier, opts options, samples []dataset.Sample, out []evaluate.Prediction) {
	jobs := make(chan int)
	var wg sync.WaitGroup

	workers := opts.concurrency
	if workers > len(samples) {
		workers = len(samples)
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				out[i] = classifyOne(ctx, classifier, opts, samples[i])
				if out[i].Error != "" {
					slog.Warn("fallo de clasificación", "imagen", samples[i].Name, "err", out[i].Error)
				} else if opts.verbose {
					slog.Debug("clasificada", "imagen", samples[i].Name, "motivo", out[i].Predicted, "score", out[i].Score)
				}
			}
		}()
	}

	for i := range samples {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
}

func classifyOne(ctx context.Context, classifier classify.Classifier, opts options, sample dataset.Sample) evaluate.Prediction {
	pred := evaluate.Prediction{Name: sample.Name, Truth: sample.Truth}

	data := sample.Data
	if data == nil {
		var err error
		data, err = os.ReadFile(sample.Path)
		if err != nil {
			pred.Error = fmt.Sprintf("leer imagen: %v", err)
			return pred
		}
	}

	imgCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	start := time.Now()
	res, err := classifier.Classify(imgCtx, data)
	pred.LatencyMS = time.Since(start).Milliseconds()
	pred.Score = res.Score
	pred.Labels = res.Labels
	pred.Provider = res.Provider
	pred.Model = res.Model
	if err != nil {
		pred.Error = err.Error()
		return pred
	}

	pred.Predicted = res.HasSubject
	return pred
}

func writeReport(path string, report evaluate.Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("crear directorio del informe: %w", err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar informe: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("escribir informe: %w", err)
	}
	return nil
}

func printFalseNegatives(report evaluate.Report) {
	fns := report.FalseNegatives()
	if len(fns) == 0 {
		return
	}

	fmt.Println("\nFalsos negativos (motivo descartado) — revisar:")
	limit := len(fns)
	if limit > 10 {
		limit = 10
	}
	for _, item := range fns[:limit] {
		detail := fmt.Sprintf("score=%.2f", item.Score)
		if item.Error != "" {
			detail = "error=" + truncate(item.Error, 120)
		}
		fmt.Printf("  %s  (%s)\n", item.Name, detail)
	}
	if len(fns) > limit {
		fmt.Printf("  ... y %d más (ver informe)\n", len(fns)-limit)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
