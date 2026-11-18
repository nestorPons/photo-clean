// Package decisions implementa el clasificador con la Decisions API de OpenAI.
// Evalúa la imagen y devuelve respuestas tipadas (predicate + choice).
package decisions

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
	// ProviderName identifica al proveedor en informes y logs.
	ProviderName = "decisions"

	defaultBaseURL   = "https://api.openai.com/v1"
	defaultEndpoint  = "/decisions"
	defaultModel     = "gpt-6-luna"
	defaultTimeout   = 60 * time.Second
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
	MaxResponseBytes int64
	UserAgent        string
	ContextPrompt    string
	HasSubjectPrompt string
	SubjectPrompt    string
	SubjectPerson    string
	SubjectAnimal    string
	SubjectBoth      string
	SubjectNone      string
}

// Provider implementa classify.Classifier contra la Decisions API.
type Provider struct {
	baseURL          string
	endpoint         string
	apiKey           string
	model            string
	threshold        float64
	retries          int
	maxImageSide     int
	imageQuality     int
	maxResponse      int64
	userAgent        string
	contextPrompt    string
	hasSubjectPrompt string
	subjectPrompt    string
	subjectPerson    string
	subjectAnimal    string
	subjectBoth      string
	subjectNone      string
	client           *http.Client
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
		opts.ImageQuality = 85
	}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}

	opts.ContextPrompt = strings.TrimSpace(opts.ContextPrompt)
	opts.HasSubjectPrompt = strings.TrimSpace(opts.HasSubjectPrompt)
	opts.SubjectPrompt = strings.TrimSpace(opts.SubjectPrompt)
	opts.SubjectPerson = strings.TrimSpace(opts.SubjectPerson)
	opts.SubjectAnimal = strings.TrimSpace(opts.SubjectAnimal)
	opts.SubjectBoth = strings.TrimSpace(opts.SubjectBoth)
	opts.SubjectNone = strings.TrimSpace(opts.SubjectNone)
	if opts.SubjectPerson == "" {
		opts.SubjectPerson = "A real person is visible anywhere (even small or in the background), or a depicted person is the main subject."
	}
	if opts.SubjectAnimal == "" {
		opts.SubjectAnimal = "A real animal is visible anywhere (even small or in the background), or a realistic figurine, model or statue of an animal is the main subject, and no person is."
	}
	if opts.SubjectBoth == "" {
		opts.SubjectBoth = "Both a person and an animal are visible or depicted as main subjects."
	}
	if opts.SubjectNone == "" {
		opts.SubjectNone = "No real person or animal is visible anywhere; only body parts, stuffed animals, screens, or decorative depictions."
	}
	if len(opts.ContextPrompt) < 5 {
		return nil, errors.New("prompt de contexto de Decisions vacío o demasiado corto")
	}
	if len(opts.HasSubjectPrompt) < 10 {
		return nil, errors.New("prompt has_subject de Decisions vacío o demasiado corto")
	}
	if len(opts.SubjectPrompt) < 10 {
		return nil, errors.New("prompt subject de Decisions vacío o demasiado corto")
	}

	return &Provider{
		baseURL:          strings.TrimRight(opts.BaseURL, "/"),
		endpoint:         opts.Endpoint,
		apiKey:           opts.APIKey,
		model:            opts.Model,
		threshold:        opts.Threshold,
		retries:          opts.Retries,
		maxImageSide:     opts.MaxImageSide,
		imageQuality:     opts.ImageQuality,
		maxResponse:      opts.MaxResponseBytes,
		userAgent:        opts.UserAgent,
		contextPrompt:    opts.ContextPrompt,
		hasSubjectPrompt: opts.HasSubjectPrompt,
		subjectPrompt:    opts.SubjectPrompt,
		subjectPerson:    opts.SubjectPerson,
		subjectAnimal:    opts.SubjectAnimal,
		subjectBoth:      opts.SubjectBoth,
		subjectNone:      opts.SubjectNone,
		client:           &http.Client{Timeout: opts.Timeout},
	}, nil
}

func (p *Provider) Provider() string { return ProviderName }

// URL devuelve el endpoint completo usado en las peticiones.
func (p *Provider) URL() string { return p.baseURL + p.endpoint }

func (p *Provider) Classify(ctx context.Context, img []byte) (classify.Result, error) {
	jpg, _, _, err := imageprep.ToJPEG(img, 0, p.maxImageSide, p.imageQuality)
	if err != nil {
		return classify.Result{}, fmt.Errorf("preparar imagen para decisions: %w", err)
	}

	body, err := p.buildRequest(jpg)
	if err != nil {
		return classify.Result{}, err
	}

	raw, err := httpx.Post(ctx, p.client, httpx.Request{
		URL:              p.URL(),
		ContentType:      "application/json",
		Body:             body,
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
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]string{"type": "input_text", "text": p.contextPrompt},
					map[string]string{"type": "input_image", "image_url": dataURL},
				},
			},
		},
		"questions": []any{
			map[string]any{
				"type":         "predicate",
				"name":         "has_subject",
				"instructions": p.hasSubjectPrompt,
			},
			map[string]any{
				"type":         "choice",
				"name":         "subject",
				"instructions": p.subjectPrompt,
				"choices": []any{
					map[string]string{"value": "person", "description": p.subjectPerson},
					map[string]string{"value": "animal", "description": p.subjectAnimal},
					map[string]string{"value": "both", "description": p.subjectBoth},
					map[string]string{"value": "none", "description": p.subjectNone},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("serializar petición decisions: %w", err)
	}
	return body, nil
}

type answer struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Probability *float64 `json:"probability"`
	Choice      string   `json:"choice"`
	Confidence  float64  `json:"confidence"`
}

type response struct {
	Model   string   `json:"model"`
	Answers []answer `json:"answers"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *Provider) parse(raw []byte) (classify.Result, error) {
	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return classify.Result{}, fmt.Errorf("respuesta decisions inválida: %w", err)
	}
	if resp.Error != nil {
		return classify.Result{}, fmt.Errorf("decisions: %s", resp.Error.Message)
	}
	if len(resp.Answers) == 0 {
		return classify.Result{}, errors.New("respuesta decisions sin answers")
	}

	subject := p.findAnswer(resp.Answers, "has_subject")
	if subject == nil {
		return classify.Result{}, errors.New("respuesta decisions sin la pregunta has_subject")
	}
	if subject.Type == "refusal" {
		return classify.Result{}, fmt.Errorf("decisions rechazó has_subject: %s", subject.Name)
	}
	if subject.Probability == nil {
		return classify.Result{}, errors.New("respuesta decisions sin probability en has_subject")
	}

	score := clamp01(*subject.Probability)
	var labels []string
	if choice := p.findAnswer(resp.Answers, "subject"); choice != nil && choice.Type == "choice" {
		labels = choiceLabels(choice.Choice)
	}

	return classify.Result{
		HasSubject: score >= p.threshold,
		Labels:     labels,
		Score:      score,
		Provider:   ProviderName,
		Model:      resp.Model,
	}, nil
}

func (p *Provider) findAnswer(answers []answer, name string) *answer {
	for i := range answers {
		if answers[i].Name == name {
			return &answers[i]
		}
	}
	return nil
}

func choiceLabels(choice string) []string {
	switch strings.ToLower(choice) {
	case "person":
		return []string{"person"}
	case "animal":
		return []string{"animal"}
	case "both":
		return []string{"person", "animal"}
	default:
		return nil
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
