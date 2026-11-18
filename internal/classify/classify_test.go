package classify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeClassifier struct {
	name    string
	result  Result
	err     error
	calls   int
	onClass func()
}

func (f *fakeClassifier) Provider() string { return f.name }

func (f *fakeClassifier) Classify(context.Context, []byte) (Result, error) {
	f.calls++
	if f.onClass != nil {
		f.onClass()
	}
	return f.result, f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestChainFallsBack(t *testing.T) {
	first := &fakeClassifier{name: "first", err: errors.New("caído")}
	second := &fakeClassifier{name: "second", result: Result{HasSubject: true, Score: 0.9}}

	chain := NewChain(testLogger(), 3, first, second)
	res, err := chain.Classify(context.Background(), []byte("img"))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !res.HasSubject || res.Provider != "second" {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("llamadas = %d/%d, quiero 1/1", first.calls, second.calls)
	}
}

func TestChainDisablesFailingProvider(t *testing.T) {
	first := &fakeClassifier{name: "first", err: errors.New("caído")}
	second := &fakeClassifier{name: "second", result: Result{HasSubject: false}}

	chain := NewChain(testLogger(), 3, first, second)
	for i := 0; i < 5; i++ {
		if _, err := chain.Classify(context.Background(), []byte("img")); err != nil {
			t.Fatalf("Classify: %v", err)
		}
	}

	if first.calls != 3 {
		t.Fatalf("primer proveedor llamado %d veces, quiero 3", first.calls)
	}
	if second.calls != 5 {
		t.Fatalf("segundo proveedor llamado %d veces", second.calls)
	}
}

func TestChainAllFail(t *testing.T) {
	first := &fakeClassifier{name: "first", err: errors.New("a")}
	second := &fakeClassifier{name: "second", err: errors.New("b")}

	chain := NewChain(testLogger(), 3, first, second)
	if _, err := chain.Classify(context.Background(), []byte("img")); err == nil {
		t.Fatal("se esperaba error")
	}
}

func TestChainEmpty(t *testing.T) {
	chain := NewChain(testLogger(), 3)
	if _, err := chain.Classify(context.Background(), []byte("img")); err == nil {
		t.Fatal("se esperaba error")
	}
}
