// Package classifier construye el clasificador configurado (decisions,
// openai, chain o mock) a partir de la configuración de la aplicación.
package classifier

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"photo-clean/internal/classify"
	"photo-clean/internal/classify/decisions"
	"photo-clean/internal/classify/mock"
	"photo-clean/internal/classify/openai"
	"photo-clean/internal/config"
)

// Build devuelve el clasificador indicado por name.
func Build(name string, cfg config.Config, threshold float64) (classify.Classifier, error) {
	logger := slog.Default()

	newDecisions := func() (classify.Classifier, error) {
		return decisions.New(decisions.Options{
			BaseURL:          cfg.Decisions.BaseURL,
			Endpoint:         cfg.Decisions.Endpoint,
			APIKey:           cfg.OpenAI.APIKey,
			Model:            cfg.Decisions.Model,
			Threshold:        threshold,
			Timeout:          cfg.Decisions.Timeout.Std(),
			Retries:          cfg.Decisions.Retries,
			MaxImageSide:     cfg.Decisions.MaxImageSide,
			ImageQuality:     cfg.Decisions.ImageQuality,
			MaxResponseBytes: cfg.HTTP.MaxResponseBytes,
			UserAgent:        cfg.HTTP.UserAgent,
			ContextPrompt:    cfg.Prompts.DecisionsContext,
			HasSubjectPrompt: cfg.Prompts.DecisionsHasSubject,
			SubjectPrompt:    cfg.Prompts.DecisionsSubject,
			SubjectPerson:    cfg.Prompts.DecisionsSubjectPerson,
			SubjectAnimal:    cfg.Prompts.DecisionsSubjectAnimal,
			SubjectBoth:      cfg.Prompts.DecisionsSubjectBoth,
			SubjectNone:      cfg.Prompts.DecisionsSubjectNone,
		})
	}
	newOpenAI := func() (classify.Classifier, error) {
		return openai.New(openai.Options{
			BaseURL:          cfg.OpenAI.BaseURL,
			Endpoint:         cfg.OpenAI.Endpoint,
			APIKey:           cfg.OpenAI.APIKey,
			Model:            cfg.OpenAI.Model,
			Threshold:        threshold,
			Timeout:          cfg.OpenAI.Timeout.Std(),
			Retries:          cfg.OpenAI.Retries,
			MaxImageSide:     cfg.OpenAI.MaxImageSide,
			ImageQuality:     cfg.OpenAI.ImageQuality,
			ImageDetail:      cfg.OpenAI.ImageDetail,
			MaxTokens:        cfg.OpenAI.MaxTokens,
			MaxResponseBytes: cfg.HTTP.MaxResponseBytes,
			UserAgent:        cfg.HTTP.UserAgent,
			SystemPrompt:     cfg.Prompts.System,
			UserPrompt:       cfg.Prompts.OpenAI,
		})
	}

	switch strings.ToLower(name) {
	case "decisions":
		return newDecisions()
	case "openai":
		return newOpenAI()
	case "mock":
		return mock.New(), nil
	case "chain":
		var providers []classify.Classifier
		if cfg.OpenAI.APIKey != "" {
			p, err := newDecisions()
			if err != nil {
				return nil, err
			}
			providers = append(providers, p)

			fallback, err := newOpenAI()
			if err != nil {
				return nil, err
			}
			providers = append(providers, fallback)
		}
		if len(providers) == 0 {
			return nil, errors.New("chain requiere OPENAI_API_KEY")
		}
		return classify.NewChain(logger, cfg.Classify.ChainMaxFailures, providers...), nil
	default:
		return nil, fmt.Errorf("proveedor desconocido %q (decisions, openai, chain, mock)", name)
	}
}
