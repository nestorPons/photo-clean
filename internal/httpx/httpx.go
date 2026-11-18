// Package httpx centraliza las peticiones HTTP con reintentos y errores
// tipados por estado.
package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultMaxResponseBytes = 8 << 20

// Request describe un POST.
type Request struct {
	URL              string
	ContentType      string
	Body             []byte
	Headers          map[string]string
	Attempts         int
	MaxResponseBytes int64
	UserAgent        string
}

// StatusError representa una respuesta HTTP >= 400.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, truncate(e.Body, 300))
}

// Retryable indica si merece la pena repetir la petición.
func (e *StatusError) Retryable() bool {
	return e.Status == http.StatusRequestTimeout ||
		e.Status == http.StatusTooManyRequests ||
		e.Status >= 500
}

// Post envía un POST con reintentos exponenciales (500ms, 1s, 2s...).
// Solo reintenta errores de red, timeouts, 429 y 5xx.
func Post(ctx context.Context, client *http.Client, req Request) ([]byte, error) {
	if req.Attempts < 1 {
		req.Attempts = 1
	}
	if req.MaxResponseBytes <= 0 {
		req.MaxResponseBytes = defaultMaxResponseBytes
	}

	var lastErr error
	for attempt := 0; attempt < req.Attempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			if delay > 4*time.Second {
				delay = 4 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		data, err := postOnce(ctx, client, req)
		if err == nil {
			return data, nil
		}
		lastErr = err

		var statusErr *StatusError
		if errors.As(err, &statusErr) && !statusErr.Retryable() {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("tras %d intentos: %w", req.Attempts, lastErr)
}

func postOnce(ctx context.Context, client *http.Client, req Request) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("crear petición: %w", err)
	}
	httpReq.Header.Set("Content-Type", req.ContentType)
	httpReq.Header.Set("Accept", "application/json")
	if req.UserAgent != "" {
		httpReq.Header.Set("User-Agent", req.UserAgent)
	}
	for key, value := range req.Headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("conectar con %s: %w", req.URL, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, req.MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("leer respuesta: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &StatusError{Status: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
