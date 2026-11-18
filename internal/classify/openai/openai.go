// Package openai implementa el clasificador de visión con la API de OpenAI.
package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"photo-clean/internal/classify"
	"photo-clean/internal/httpx"
	"photo-clean/internal/imageprep"
)

const (
	ProviderName     = "openai"
	defaultBaseURL   = "https://api.openai.com/v1"
	defaultEndpoint  = "/chat/completions"
	defaultModel     = "gpt-4o-mini"
	defaultTimeout   = 90 * time.Second
	defaultUserAgent = "photo-clean/0.1"
)

// Options configura el proveedor. Los valores a cero se sustituyen por los
// valores por defecto.
type Options struct {
	BaseURL          string
	Endpoint         string
	APIKey           string
	Model            string
	Threshold        float64
	Timeout          time.Duration
	Retries          int
	MaxImageSide     int
	ImageQuality     int
	ImageDetail      string // low | high | auto
	MaxTokens        int
	MaxResponseBytes int64
	UserAgent        string
	SystemPrompt     string
	UserPrompt       string
}

// Provider implementa classify.Classifier contra OpenAI.
type Provider struct {
	baseURL      string
	endpoint     string
	apiKey       string
	model        string
	threshold    float64
	retries      int
	maxImageSide int
	imageQuality int
	detail       string
	maxTokens    int
	maxResponse  int64
	userAgent    string
	systemPrompt string
	userPrompt   string
	client       *http.Client
}

func New(opts Options) (*Provider, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, errors.New("OPENAI_API_KEY vacía")
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.Endpoint == "" {
		opts.Endpoint = defaultEndpoint
	}
	if opts.Model == "" {
		opts.Model = defaultModel
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.Threshold <= 0 || opts.Threshold > 1 {
		opts.Threshold = 0.5
	}
	if opts.Retries < 1 {
		opts.Retries = 3
	}
	if opts.MaxImageSide <= 0 {
		opts.MaxImageSide = 1536
	}
	if opts.ImageQuality <= 0 || opts.ImageQuality > 100 {
		opts.ImageQuality = 82
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 120
	}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}

	opts.SystemPrompt = strings.TrimSpace(opts.SystemPrompt)
	opts.UserPrompt = strings.TrimSpace(opts.UserPrompt)
	if len(opts.SystemPrompt) < 10 {
		return nil, errors.New("prompt system de OpenAI vacío o demasiado corto")
	}
	if len(opts.UserPrompt) < 10 {
		return nil, errors.New("prompt de usuario de OpenAI vacío o demasiado corto")
	}

	detail := strings.ToLower(strings.TrimSpace(opts.ImageDetail))
	switch detail {
	case "low", "high", "auto":
	default:
		detail = "auto"
	}

	return &Provider{
		baseURL:      strings.TrimRight(opts.BaseURL, "/"),
		endpoint:     opts.Endpoint,
		apiKey:       opts.APIKey,
		model:        opts.Model,
		threshold:    opts.Threshold,
		retries:      opts.Retries,
		maxImageSide: opts.MaxImageSide,
		imageQuality: opts.ImageQuality,
		detail:       detail,
		maxTokens:    opts.MaxTokens,
		maxResponse:  opts.MaxResponseBytes,
		userAgent:    opts.UserAgent,
		systemPrompt: opts.SystemPrompt,
		userPrompt:   opts.UserPrompt,
		client:       &http.Client{Timeout: opts.Timeout},
	}, nil
}

func (p *Provider) Provider() string { return ProviderName }

// URL devuelve el endpoint completo usado en las peticiones.
func (p *Provider) URL() string { return p.baseURL + p.endpoint }

func (p *Provider) Classify(ctx context.Context, img []byte) (classify.Result, error) {
	jpg, _, _, err := imageprep.ToJPEG(img, 0, p.maxImageSide, p.imageQuality)
	if err != nil {
		return classify.Result{}, fmt.Errorf("preparar imagen para openai: %w", err)
	}

	payload, err := p.buildRequest(jpg)
	if err != nil {
		return classify.Result{}, err
	}

	raw, err := httpx.Post(ctx, p.client, httpx.Request{
		URL:              p.URL(),
		ContentType:      "application/json",
		Body:             payload,
		Headers:          map[string]string{"Authorization": "Bearer " + p.apiKey},
		Attempts:         p.retries,
		MaxResponseBytes: p.maxResponse,
		UserAgent:        p.userAgent,
	})
	if err != nil {
		return classify.Result{}, err
	}

	return p.parse(raw)
}

func (p *Provider) buildRequest(jpg []byte) ([]byte, error) {
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)

	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{"role": "system", "content": p.systemPrompt},
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": p.userPrompt},
					{"type": "image_url", "image_url": map[string]string{
						"url":    dataURL,
						"detail": p.detail,
					}},
				},
			},
		},
		"temperature":     0,
		"max_tokens":      p.maxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("serializar petición openai: %w", err)
	}
	return body, nil
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type verdict struct {
	HasSubject bool     `json:"has_subject"`
	Labels     []string `json:"labels"`
	Confidence float64  `json:"confidence"`
}

func (p *Provider) parse(raw []byte) (classify.Result, error) {
	var resp chatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return classify.Result{}, fmt.Errorf("respuesta openai inválida: %w", err)
	}
	if resp.Error != nil {
		return classify.Result{}, fmt.Errorf("openai: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return classify.Result{}, errors.New("respuesta openai sin choices")
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return classify.Result{}, errors.New("respuesta openai vacía")
	}
	content = stripCodeFence(content)

	var v verdict
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return classify.Result{}, fmt.Errorf("json de openai inválido: %w (%s)", err, content)
	}

	score := v.Confidence
	if score <= 0 || score > 1 {
		if v.HasSubject {
			score = 1
		}
	}

	return classify.Result{
		HasSubject: v.HasSubject,
		Labels:     cleanLabels(v.Labels),
		Score:      score,
		Provider:   ProviderName,
		Model:      p.model,
	}, nil
}

func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func cleanLabels(labels []string) []string {
	seen := make(map[string]bool, len(labels))
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.ToLower(strings.TrimSpace(label))
		if label != "person" && label != "animal" {
			continue
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	return out
}
