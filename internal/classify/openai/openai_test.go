package openai

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
	"testing"
)

func TestClassify(t *testing.T) {
	var gotPayload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test" {
			t.Errorf("Authorization = %q", auth)
		}

		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotPayload); err != nil {
			t.Errorf("payload no es json: %v", err)
		}

		content := `{"has_subject":true,"labels":["person","animal","Person"],"confidence":0.93}`
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": content}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := New(Options{BaseURL: srv.URL, APIKey: "sk-test", Threshold: 0.5, SystemPrompt: testSystemPrompt, UserPrompt: testUserPrompt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	res, err := p.Classify(context.Background(), jpegImage(t))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	if !res.HasSubject || res.Score != 0.93 {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if len(res.Labels) != 2 || res.Labels[0] != "person" || res.Labels[1] != "animal" {
		t.Errorf("Labels = %v, quiero [person animal]", res.Labels)
	}

	if gotPayload["model"] != defaultModel {
		t.Errorf("model = %v", gotPayload["model"])
	}
	if _, ok := gotPayload["response_format"]; !ok {
		t.Error("falta response_format en la petición")
	}
}

func TestParseCodeFence(t *testing.T) {
	p, err := New(Options{APIKey: "sk-test", SystemPrompt: testSystemPrompt, UserPrompt: testUserPrompt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	content := "```json\n{\"has_subject\":false,\"labels\":[],\"confidence\":0.7}\n```"
	raw, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	res, err := p.parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.HasSubject {
		t.Error("HasSubject = true, quiero false")
	}
}

func TestParseAPIError(t *testing.T) {
	p, err := New(Options{APIKey: "sk-test", SystemPrompt: testSystemPrompt, UserPrompt: testUserPrompt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.parse([]byte(`{"error":{"message":"invalid key"}}`)); err == nil {
		t.Fatal("se esperaba error")
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("se esperaba error sin API key")
	}
}

const (
	testSystemPrompt = "You classify photos. Answer only with JSON."
	testUserPrompt   = "Does the image show a person or an animal? Return JSON."
)

func jpegImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 40, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.Bytes()
}
