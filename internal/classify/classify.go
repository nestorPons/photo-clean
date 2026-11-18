// Package classify define el contrato común de los clasificadores de imagen
// y la cadena de proveedores con fallback.
package classify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// Result es el veredicto de un clasificador para una imagen.
type Result struct {
	HasSubject bool     `json:"has_subject"`
	Labels     []string `json:"labels,omitempty"`
	Score      float64  `json:"score"`
	Provider   string   `json:"provider"`
	Model      string   `json:"model,omitempty"`
}

// Classifier decide si una imagen contiene una persona o un animal.
type Classifier interface {
	Provider() string
	Classify(ctx context.Context, img []byte) (Result, error)
}

// Chain intenta cada proveedor en orden hasta obtener un veredicto.
// Tras maxFailures fallos seguidos desactiva un proveedor para no penalizar
// todo el lote si el servicio está caído. Es segura para uso concurrente.
type Chain struct {
	providers   []Classifier
	log         *slog.Logger
	maxFailures int

	mu       sync.Mutex
	failures map[string]int
	disabled map[string]bool
}

func NewChain(log *slog.Logger, maxFailures int, providers ...Classifier) *Chain {
	if log == nil {
		log = slog.Default()
	}
	if maxFailures < 1 {
		maxFailures = 3
	}
	return &Chain{
		providers:   providers,
		log:         log,
		maxFailures: maxFailures,
		failures:    make(map[string]int),
		disabled:    make(map[string]bool),
	}
}

func (c *Chain) Provider() string {
	names := make([]string, 0, len(c.providers))
	for _, p := range c.providers {
		names = append(names, p.Provider())
	}
	return "chain(" + strings.Join(names, ",") + ")"
}

func (c *Chain) Classify(ctx context.Context, img []byte) (Result, error) {
	if len(c.providers) == 0 {
		return Result{}, errors.New("cadena de clasificadores vacía")
	}

	errs := make([]error, 0, len(c.providers))
	for _, p := range c.providers {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if c.isDisabled(p.Provider()) {
			continue
		}

		res, err := p.Classify(ctx, img)
		if err == nil {
			c.reset(p.Provider())
			if res.Provider == "" {
				res.Provider = p.Provider()
			}
			return res, nil
		}

		c.fail(p.Provider(), err)
		errs = append(errs, fmt.Errorf("%s: %w", p.Provider(), err))
	}

	if len(errs) == 0 {
		return Result{}, errors.New("todos los clasificadores están desactivados")
	}
	return Result{}, errors.Join(errs...)
}

func (c *Chain) isDisabled(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.disabled[name]
}

func (c *Chain) reset(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures[name] > 0 {
		delete(c.failures, name)
	}
}

func (c *Chain) fail(name string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures[name]++
	if c.failures[name] >= c.maxFailures && !c.disabled[name] {
		c.disabled[name] = true
		c.log.Error("proveedor desactivado tras fallos consecutivos", "provider", name, "err", err)
	}
}
