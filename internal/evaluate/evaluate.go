// Package evaluate construye la matriz de confusión y las métricas del test.
//
// Convención: positivo = la imagen tiene motivo (persona o animal).
//   - FN: motivo descartado  -> error grave (se pierde una foto válida)
//   - FP: paisaje conservado -> error tolerable
package evaluate

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Prediction es el resultado del clasificador para una muestra.
type Prediction struct {
	Name      string   `json:"name"`
	Truth     bool     `json:"truth"`
	Predicted bool     `json:"predicted"`
	Score     float64  `json:"score"`
	Labels    []string `json:"labels,omitempty"`
	Provider  string   `json:"provider"`
	Model     string   `json:"model,omitempty"`
	LatencyMS int64    `json:"latency_ms"`
	Error     string   `json:"error,omitempty"`
}

// Confusion es la matriz de confusión.
type Confusion struct {
	TP int `json:"tp"`
	TN int `json:"tn"`
	FP int `json:"fp"`
	FN int `json:"fn"`
}

// Metrics son las métricas derivadas.
type Metrics struct {
	Accuracy    float64 `json:"accuracy"`
	Precision   float64 `json:"precision"`
	Recall      float64 `json:"recall"`
	F1          float64 `json:"f1"`
	Specificity float64 `json:"specificity"`
}

// Report es el informe completo de una ejecución.
type Report struct {
	SchemaVersion int          `json:"schema_version"`
	Provider      string       `json:"provider"`
	Dataset       string       `json:"dataset"`
	Threshold     float64      `json:"threshold"`
	StartedAt     time.Time    `json:"started_at"`
	DurationMS    int64        `json:"duration_ms"`
	Total         int          `json:"total"`
	Errors        int          `json:"errors"`
	Confusion     Confusion    `json:"confusion"`
	Metrics       Metrics      `json:"metrics"`
	Items         []Prediction `json:"items"`
}

// Build calcula el informe a partir de las predicciones.
func Build(provider, datasetName string, threshold float64, startedAt time.Time, items []Prediction) Report {
	var c Confusion
	errors := 0

	for _, item := range items {
		if item.Error != "" {
			errors++
		}
		switch {
		case item.Truth && item.Predicted:
			c.TP++
		case !item.Truth && item.Predicted:
			c.FP++
		case item.Truth && !item.Predicted:
			c.FN++
		default:
			c.TN++
		}
	}

	return Report{
		SchemaVersion: 1,
		Provider:      provider,
		Dataset:       datasetName,
		Threshold:     threshold,
		StartedAt:     startedAt.UTC(),
		DurationMS:    time.Since(startedAt).Milliseconds(),
		Total:         len(items),
		Errors:        errors,
		Confusion:     c,
		Metrics:       compute(c),
		Items:         items,
	}
}

func compute(c Confusion) Metrics {
	m := Metrics{
		Accuracy:    ratio(c.TP+c.TN, c.TP+c.TN+c.FP+c.FN),
		Precision:   ratio(c.TP, c.TP+c.FP),
		Recall:      ratio(c.TP, c.TP+c.FN),
		Specificity: ratio(c.TN, c.TN+c.FP),
	}
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}
	return m
}

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return math.Round(float64(num)/float64(den)*10000) / 10000
}

// Summary devuelve un resumen legible para consola.
func (r Report) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Proveedor: %s    Dataset: %s    Umbral: %.2f\n", r.Provider, r.Dataset, r.Threshold)
	fmt.Fprintf(&b, "Muestras: %d (errores de clasificación: %d)    Duración: %.1fs\n",
		r.Total, r.Errors, float64(r.DurationMS)/1000)
	fmt.Fprintf(&b, "TP=%d  TN=%d  FP=%d  FN=%d\n", r.Confusion.TP, r.Confusion.TN, r.Confusion.FP, r.Confusion.FN)
	fmt.Fprintf(&b, "Accuracy=%.3f  Precision=%.3f  Recall=%.3f  F1=%.3f  Specificity=%.3f\n",
		r.Metrics.Accuracy, r.Metrics.Precision, r.Metrics.Recall, r.Metrics.F1, r.Metrics.Specificity)

	return b.String()
}

// FalseNegatives devuelve las muestras con motivo que fueron descartadas.
func (r Report) FalseNegatives() []Prediction {
	var out []Prediction
	for _, item := range r.Items {
		if item.Truth && !item.Predicted {
			out = append(out, item)
		}
	}
	return out
}
