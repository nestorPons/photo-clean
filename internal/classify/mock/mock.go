// Package mock ofrece un clasificador determinista para probar el pipeline
// sin consumir APIs externas.
package mock

import (
	"context"
	"crypto/sha256"

	"photo-clean/internal/classify"
)

const ProviderName = "mock"

// Provider genera un veredicto estable a partir del hash de la imagen.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Provider() string { return ProviderName }

func (p *Provider) Classify(_ context.Context, img []byte) (classify.Result, error) {
	sum := sha256.Sum256(img)

	// ~70% de imágenes con motivo, para obtener una matriz no trivial.
	hasSubject := sum[0]%10 < 7
	score := 0.15
	if hasSubject {
		score = 0.85
	}

	var labels []string
	if hasSubject {
		if sum[1]%2 == 0 {
			labels = []string{"person"}
		} else {
			labels = []string{"animal"}
		}
	}

	return classify.Result{
		HasSubject: hasSubject,
		Labels:     labels,
		Score:      score,
		Provider:   ProviderName,
		Model:      "hash",
	}, nil
}
