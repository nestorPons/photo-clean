// Package config carga la configuración desde config/config.yaml y permite
// sobrescribirla con variables de entorno (las claves API solo por entorno).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultPath es la ruta del archivo de configuración versionado.
const DefaultPath = "config/config.yaml"

// Duration permite escribir duraciones legibles en YAML ("60s", "1m30s").
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("duración inválida %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

// Config agrupa toda la configuración no secreta.
type Config struct {
	Path      string          `yaml:"-"`
	App       AppConfig       `yaml:"app"`
	Paths     PathsConfig     `yaml:"paths"`
	Classify  ClassifyConfig  `yaml:"classify"`
	Decisions DecisionsConfig `yaml:"decisions"`
	OpenAI    OpenAIConfig    `yaml:"openai"`
	HTTP      HTTPConfig      `yaml:"http"`
	Zip       ZipConfig       `yaml:"zip"`
	Web       WebConfig       `yaml:"web"`
	Prompts   PromptsConfig   `yaml:"prompts"`
}

type AppConfig struct {
	Addr        string `yaml:"addr"`
	Environment string `yaml:"environment"`
}

type PathsConfig struct {
	Dataset     string `yaml:"dataset"`
	Out         string `yaml:"out"`
	Jobs        string `yaml:"jobs"`
	Report      string `yaml:"report"`
	AcceptedZip string `yaml:"accepted_zip"`
	RejectedZip string `yaml:"rejected_zip"`
}

type ClassifyConfig struct {
	Provider         string   `yaml:"provider"`
	Threshold        float64  `yaml:"threshold"`
	Concurrency      int      `yaml:"concurrency"`
	Timeout          Duration `yaml:"timeout"`
	MinRecall        float64  `yaml:"min_recall"`
	ChainMaxFailures int      `yaml:"chain_max_failures"`
}

type DecisionsConfig struct {
	BaseURL      string   `yaml:"base_url"`
	Endpoint     string   `yaml:"endpoint"`
	Model        string   `yaml:"model"`
	Timeout      Duration `yaml:"timeout"`
	Retries      int      `yaml:"retries"`
	MaxImageSide int      `yaml:"max_image_side"`
	ImageQuality int      `yaml:"image_quality"`
}

type OpenAIConfig struct {
	APIKey       string   `yaml:"-"` // solo por entorno
	BaseURL      string   `yaml:"base_url"`
	Endpoint     string   `yaml:"endpoint"`
	Model        string   `yaml:"model"`
	Timeout      Duration `yaml:"timeout"`
	Retries      int      `yaml:"retries"`
	MaxImageSide int      `yaml:"max_image_side"`
	ImageQuality int      `yaml:"image_quality"`
	ImageDetail  string   `yaml:"image_detail"`
	MaxTokens    int      `yaml:"max_tokens"`
}

type HTTPConfig struct {
	UserAgent        string `yaml:"user_agent"`
	MaxResponseBytes int64  `yaml:"max_response_bytes"`
}

type ZipConfig struct {
	MaxFileBytes  int64 `yaml:"max_file_bytes"`
	MaxFiles      int   `yaml:"max_files"`
	MaxTotalBytes int64 `yaml:"max_total_bytes"`
}

// WebConfig controla la subida y el ciclo de vida de los trabajos web.
type WebConfig struct {
	MaxUploadBytes int64    `yaml:"max_upload_bytes"`
	MaxJobs        int      `yaml:"max_jobs"`
	JobTTL         Duration `yaml:"job_ttl"`
}

// PromptsConfig permite ajustar las instrucciones de clasificación sin
// recompilar.
type PromptsConfig struct {
	System                 string `yaml:"system"`
	DecisionsContext       string `yaml:"decisions_context"`
	DecisionsHasSubject    string `yaml:"decisions_has_subject"`
	DecisionsSubject       string `yaml:"decisions_subject"`
	DecisionsSubjectPerson string `yaml:"decisions_subject_person"`
	DecisionsSubjectAnimal string `yaml:"decisions_subject_animal"`
	DecisionsSubjectBoth   string `yaml:"decisions_subject_both"`
	DecisionsSubjectNone   string `yaml:"decisions_subject_none"`
	OpenAI                 string `yaml:"openai"`
}

// Default devuelve la configuración de referencia (mismos valores que
// config/config.yaml) para que un archivo parcial siga siendo válido.
func Default() Config {
	return Config{
		App: AppConfig{Addr: ":8080", Environment: "development"},
		Paths: PathsConfig{
			Dataset:     "dataset",
			Out:         "out",
			Jobs:        "",
			Report:      "out/informe.json",
			AcceptedZip: "out/aceptadas.zip",
			RejectedZip: "out/descartadas.zip",
		},
		Classify: ClassifyConfig{
			Provider:         "decisions",
			Threshold:        0.5,
			Concurrency:      4,
			Timeout:          Duration(60 * time.Second),
			MinRecall:        0.95,
			ChainMaxFailures: 3,
		},
		Decisions: DecisionsConfig{
			BaseURL:      "https://api.openai.com/v1",
			Endpoint:     "/decisions",
			Model:        "gpt-6-luna",
			Timeout:      Duration(60 * time.Second),
			Retries:      3,
			MaxImageSide: 1536,
			ImageQuality: 85,
		},
		OpenAI: OpenAIConfig{
			BaseURL:      "https://api.openai.com/v1",
			Endpoint:     "/chat/completions",
			Model:        "gpt-4o-mini",
			Timeout:      Duration(90 * time.Second),
			Retries:      3,
			MaxImageSide: 1536,
			ImageQuality: 82,
			ImageDetail:  "auto",
			MaxTokens:    120,
		},
		HTTP: HTTPConfig{UserAgent: "photo-clean/0.1", MaxResponseBytes: 8 << 20},
		Zip:  ZipConfig{MaxFileBytes: 25 << 20, MaxFiles: 5000, MaxTotalBytes: 1 << 30},
		Web:  WebConfig{MaxUploadBytes: 300 << 20, MaxJobs: 2, JobTTL: Duration(time.Hour)},
		Prompts: PromptsConfig{
			System:           "You classify photos. Answer only with JSON, never with prose.",
			DecisionsContext: "Inspect this photo and answer the questions about what it contains. Look at the whole frame, including the background and any depicted subjects.",
			DecisionsHasSubject: "Does the photo contain a person or an animal? " +
				"Answer yes if you can see a real person or animal anywhere in the photo, even if small, distant, blurred, partially hidden or in the background; " +
				"a recognizable face or full head is enough for a person, and a whole animal or a recognizable head is enough for an animal. " +
				"Also answer yes for a depicted person or animal only when the depiction is the main subject: a portrait painting, a realistic figurine, model or statue " +
				"of a person or animal, a person wearing a costume, or a printed photograph shown inside the photo. " +
				"Answer no when only a hand, arm, leg, foot, neck, chin or torso is visible, or the person is cut off by the frame or hidden behind a counter or display " +
				"so that no face or full head is visible (for example, a head cut off at the top edge so only the mouth, chin or neck is visible); when the subjects are stuffed animals, plush toys, teddy bears, dolls or mannequins; when they appear only on a TV, " +
				"monitor, phone or projector screen; or when the depiction is only decorative (statues or figures on a clock, building or monument); " +
				"a hand or arm holding a glass, cutlery or napkin at the edge of a close-up of food; or a figure that is part of a model vehicle, toy or diorama " +
				"(for example, a pilot figure inside a model airplane). " +
				"If the photo is mainly a close-up of food or an object and only a hand or arm enters the frame, answer no.",
			DecisionsSubject:       "Which subject is present in the photo?",
			DecisionsSubjectPerson: "A real person is visible anywhere (even small or in the background), or a depicted person is the main subject.",
			DecisionsSubjectAnimal: "A real animal is visible anywhere (even small or in the background), or a realistic figurine, model or statue of an animal is the main subject, and no person is.",
			DecisionsSubjectBoth:   "Both a person and an animal are visible or depicted as main subjects.",
			DecisionsSubjectNone:   "No real person or animal is visible anywhere; only body parts, stuffed animals, screens, or decorative depictions.",
			OpenAI: `You are an image triage assistant.
Decide if the photo contains a person or an animal.
Answer yes if a real person or animal is visible anywhere in the photo, even if small, distant, blurred, partially
hidden or in the background; a recognizable face or full head is enough for a person, and a whole animal or a
recognizable head is enough for an animal.
Also count a depicted person or animal only when it is the main subject: a portrait painting, a realistic figurine,
model or statue of a person or animal, a person in costume, or a printed photograph shown inside the photo.
Answer no when only a hand, arm, leg, foot, neck, chin or torso is visible; when the person is cut off by the frame
or hidden behind a counter or display without a face or full head; for stuffed animals, plush toys, teddy bears,
dolls or mannequins; for people or animals that appear only on a TV, monitor or phone screen; and for decorative
statues or figures on a clock, building or monument.
If the photo is mainly a close-up of food or an object and only a hand or arm enters the frame, answer no.
Return ONLY valid JSON with this shape:
{"has_subject": true|false, "labels": ["person"|"animal"], "confidence": 0.0-1.0}`,
		},
	}
}

// Load lee CONFIG_FILE (o config/config.yaml) y aplica los overrides de entorno.
func Load() (Config, error) {
	path := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if path == "" {
		path = DefaultPath
	}

	cfg, err := LoadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// LoadFile lee un archivo concreto sin aplicar variables de entorno.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("leer configuración %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsear configuración %s: %w", path, err)
	}
	cfg.Path = path

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Validate comprueba rangos y rutas obligatorias.
func (c Config) Validate() error {
	if strings.TrimSpace(c.App.Addr) == "" {
		return errors.New("app.addr vacío")
	}

	switch c.Classify.Provider {
	case "decisions", "openai", "chain", "mock":
	default:
		return fmt.Errorf("classify.provider desconocido %q (decisions|openai|chain|mock)", c.Classify.Provider)
	}
	if c.Classify.Threshold <= 0 || c.Classify.Threshold > 1 {
		return errors.New("classify.threshold debe estar entre 0 y 1")
	}
	if c.Classify.Concurrency < 1 || c.Classify.Concurrency > 32 {
		return errors.New("classify.concurrency debe estar entre 1 y 32")
	}
	if c.Classify.Timeout.Std() <= 0 {
		return errors.New("classify.timeout debe ser mayor que 0")
	}
	if c.Classify.MinRecall < 0 || c.Classify.MinRecall > 1 {
		return errors.New("classify.min_recall debe estar entre 0 y 1")
	}
	if c.Classify.ChainMaxFailures < 1 {
		return errors.New("classify.chain_max_failures debe ser >= 1")
	}

	if err := validateEndpoint("decisions", c.Decisions.BaseURL, c.Decisions.Endpoint); err != nil {
		return err
	}
	if c.Decisions.Timeout.Std() <= 0 || c.Decisions.Retries < 1 {
		return errors.New("decisions.timeout y decisions.retries deben ser > 0")
	}
	if c.Decisions.MaxImageSide < 256 || c.Decisions.ImageQuality < 1 || c.Decisions.ImageQuality > 100 {
		return errors.New("decisions.max_image_side / image_quality inválidos")
	}

	if err := validateEndpoint("openai", c.OpenAI.BaseURL, c.OpenAI.Endpoint); err != nil {
		return err
	}
	if c.OpenAI.Timeout.Std() <= 0 || c.OpenAI.Retries < 1 {
		return errors.New("openai.timeout y openai.retries deben ser > 0")
	}
	if c.OpenAI.MaxImageSide < 256 || c.OpenAI.ImageQuality < 1 || c.OpenAI.ImageQuality > 100 {
		return errors.New("openai.max_image_side / image_quality inválidos")
	}
	if c.OpenAI.MaxTokens < 32 {
		return errors.New("openai.max_tokens debe ser >= 32")
	}

	if c.HTTP.MaxResponseBytes < 1024 {
		return errors.New("http.max_response_bytes debe ser >= 1024")
	}
	if c.Zip.MaxFileBytes < 1024 || c.Zip.MaxFiles < 1 || c.Zip.MaxTotalBytes < 1<<20 {
		return errors.New("zip.max_file_bytes / max_files / max_total_bytes inválidos")
	}
	if c.Web.MaxUploadBytes < 1<<20 {
		return errors.New("web.max_upload_bytes debe ser >= 1 MiB")
	}
	if c.Web.MaxJobs < 1 || c.Web.MaxJobs > 8 {
		return errors.New("web.max_jobs debe estar entre 1 y 8")
	}
	if c.Web.JobTTL.Std() <= 0 {
		return errors.New("web.job_ttl debe ser mayor que 0")
	}
	if c.Paths.Dataset == "" || c.Paths.Report == "" || c.Paths.AcceptedZip == "" || c.Paths.RejectedZip == "" {
		return errors.New("faltan rutas en paths")
	}

	for name, prompt := range map[string]string{
		"prompts.system":                   c.Prompts.System,
		"prompts.decisions_context":        c.Prompts.DecisionsContext,
		"prompts.decisions_has_subject":    c.Prompts.DecisionsHasSubject,
		"prompts.decisions_subject":        c.Prompts.DecisionsSubject,
		"prompts.decisions_subject_person": c.Prompts.DecisionsSubjectPerson,
		"prompts.decisions_subject_animal": c.Prompts.DecisionsSubjectAnimal,
		"prompts.decisions_subject_both":   c.Prompts.DecisionsSubjectBoth,
		"prompts.decisions_subject_none":   c.Prompts.DecisionsSubjectNone,
		"prompts.openai":                   c.Prompts.OpenAI,
	} {
		if len(strings.TrimSpace(prompt)) < 10 {
			return fmt.Errorf("%s vacío o demasiado corto", name)
		}
	}
	return nil
}

func validateEndpoint(name, baseURL, endpoint string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s.base_url inválida: %q", name, baseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s.base_url debe usar http o https: %q", name, baseURL)
	}
	if !strings.HasPrefix(endpoint, "/") {
		return fmt.Errorf("%s.endpoint debe empezar por /: %q", name, endpoint)
	}
	return nil
}

// applyEnv aplica los overrides documentados en .env.example.
func applyEnv(c *Config) error {
	var err error

	if v, ok := lookup("APP_ADDR"); ok {
		c.App.Addr = v
	}
	if v, ok := lookup("APP_ENV"); ok {
		c.App.Environment = v
	}

	if v, ok := lookup("CLASSIFIER_PROVIDER"); ok {
		c.Classify.Provider = strings.ToLower(v)
	}
	if c.Classify.Threshold, err = envFloat("CLASSIFIER_THRESHOLD", c.Classify.Threshold); err != nil {
		return err
	}
	if c.Classify.Concurrency, err = envInt("CLASSIFY_CONCURRENCY", c.Classify.Concurrency); err != nil {
		return err
	}
	if c.Classify.Timeout, err = envDuration("CLASSIFY_TIMEOUT", c.Classify.Timeout); err != nil {
		return err
	}
	if c.Classify.MinRecall, err = envFloat("CLASSIFIER_MIN_RECALL", c.Classify.MinRecall); err != nil {
		return err
	}

	if v, ok := lookup("OPENAI_API_KEY"); ok {
		c.OpenAI.APIKey = v
	}
	if v, ok := lookup("DECISIONS_BASE_URL"); ok {
		c.Decisions.BaseURL = v
	}
	if v, ok := lookup("DECISIONS_ENDPOINT"); ok {
		c.Decisions.Endpoint = v
	}
	if v, ok := lookup("DECISIONS_MODEL"); ok {
		c.Decisions.Model = v
	}
	if c.Decisions.MaxImageSide, err = envInt("DECISIONS_MAX_IMAGE_SIDE", c.Decisions.MaxImageSide); err != nil {
		return err
	}

	if v, ok := lookup("OPENAI_BASE_URL"); ok {
		c.OpenAI.BaseURL = v
	}
	if v, ok := lookup("OPENAI_ENDPOINT"); ok {
		c.OpenAI.Endpoint = v
	}
	if v, ok := lookup("OPENAI_MODEL"); ok {
		c.OpenAI.Model = v
	}
	if v, ok := lookup("OPENAI_IMAGE_DETAIL"); ok {
		c.OpenAI.ImageDetail = strings.ToLower(v)
	}

	if c.Web.MaxUploadBytes, err = envInt64("WEB_MAX_UPLOAD_BYTES", c.Web.MaxUploadBytes); err != nil {
		return err
	}
	if c.Zip.MaxTotalBytes, err = envInt64("ZIP_MAX_TOTAL_BYTES", c.Zip.MaxTotalBytes); err != nil {
		return err
	}
	if c.Web.MaxJobs, err = envInt("WEB_MAX_JOBS", c.Web.MaxJobs); err != nil {
		return err
	}
	if c.Web.JobTTL, err = envDuration("WEB_JOB_TTL", c.Web.JobTTL); err != nil {
		return err
	}

	return nil
}

func lookup(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return strings.TrimSpace(value), true
}

func envInt(key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envFloat(key string, fallback float64) (float64, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func envDuration(key string, fallback Duration) (Duration, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return Duration(parsed), nil
}
