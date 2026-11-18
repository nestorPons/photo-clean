package decisions

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testContextPrompt = "Inspect this photo and answer the questions."
	testHasPrompt     = "Does the image show at least one real person or animal?"
	testSubjectPrompt = "Which real subject is visible in the image?"
)

func TestClassify(t *testing.T) {
	var payload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/decisions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("Authorization = %q", auth)
		}

		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("payload no es json: %v", err)
		}

		_, _ = w.Write([]byte(`{"model":"gpt-6-luna","answers":[
			{"type":"predicate","name":"has_subject","probability":0.88},
			{"type":"choice","name":"subject","choice":"animal","confidence":0.9}
		]}`))
	}))
	defer srv.Close()

	p, err := New(Options{
		BaseURL: srv.URL, APIKey: "sk-test", Threshold: 0.5,
		ContextPrompt: testContextPrompt, HasSubjectPrompt: testHasPrompt, SubjectPrompt: testSubjectPrompt,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := p.Classify(context.Background(), jpegImage(t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if !res.HasSubject || res.Score != 0.88 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if len(res.Labels) != 1 || res.Labels[0] != "animal" {
		t.Errorf("Labels = %v, quiero [animal]", res.Labels)
	}
	if res.Provider != ProviderName || res.Model != "gpt-6-luna" {
		t.Errorf("provider/model = %s/%s", res.Provider, res.Model)
	}

	if payload["model"] != defaultModel {
		t.Errorf("model enviado = %v", payload["model"])
	}

	input, _ := payload["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input = %v", payload["input"])
	}
	message, _ := input[0].(map[string]any)
	content, _ := message["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("content = %v", content)
	}
	imagePart, _ := content[1].(map[string]any)
	imageURL, _ := imagePart["image_url"].(string)
	if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
		t.Errorf("image_url = %.40q", imageURL)
	}

	questions, _ := payload["questions"].([]any)
	if len(questions) != 2 {
		t.Fatalf("questions = %v", questions)
	}
	predicate, _ := questions[0].(map[string]any)
	if predicate["type"] != "predicate" || predicate["instructions"] != testHasPrompt {
		t.Errorf("pregunta predicate = %v", predicate)
	}
}

func TestParseZeroProbability(t *testing.T) {
	p, err := New(Options{
		APIKey: "sk-test", Threshold: 0.5,
		ContextPrompt: testContextPrompt, HasSubjectPrompt: testHasPrompt, SubjectPrompt: testSubjectPrompt,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := p.parse([]byte(`{"model":"gpt-6-luna","answers":[
		{"type":"predicate","name":"has_subject","probability":0},
		{"type":"choice","name":"subject","choice":"none"}
	]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.HasSubject {
		t.Error("HasSubject = true, quiero false")
	}
	if len(res.Labels) != 0 {
		t.Errorf("Labels = %v, quiero vacío", res.Labels)
	}
}

func TestParseRefusal(t *testing.T) {
	p, err := New(Options{
		APIKey:        "sk-test",
		ContextPrompt: testContextPrompt, HasSubjectPrompt: testHasPrompt, SubjectPrompt: testSubjectPrompt,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.parse([]byte(`{"answers":[{"type":"refusal","name":"has_subject"}]}`)); err == nil {
		t.Fatal("se esperaba error por refusal")
	}
}

func TestParseAPIError(t *testing.T) {
	p, err := New(Options{
		APIKey:        "sk-test",
		ContextPrompt: testContextPrompt, HasSubjectPrompt: testHasPrompt, SubjectPrompt: testSubjectPrompt,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.parse([]byte(`{"error":{"message":"invalid key","type":"auth"}}`)); err == nil {
		t.Fatal("se esperaba error")
	}
	if _, err := p.parse([]byte(`{"answers":[]}`)); err == nil {
		t.Fatal("se esperaba error sin answers")
	}
}

func TestNewValidations(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("se esperaba error sin API key")
	}
	if _, err := New(Options{APIKey: "sk-test"}); err == nil {
		t.Fatal("se esperaba error sin prompts")
	}
}

func jpegImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 480, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 480; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 60, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}
