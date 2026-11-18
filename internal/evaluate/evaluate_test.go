package evaluate

import (
	"math"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	items := []Prediction{
		{Name: "tp1", Truth: true, Predicted: true},
		{Name: "tp2", Truth: true, Predicted: true},
		{Name: "tn1", Truth: false, Predicted: false},
		{Name: "fp1", Truth: false, Predicted: true},
		{Name: "fn1", Truth: true, Predicted: false},
		{Name: "err", Truth: true, Predicted: false, Error: "timeout"},
	}

	report := Build("test", "dataset", 0.5, time.Now(), items)

	if report.Total != 6 {
		t.Fatalf("Total = %d, quiero 6", report.Total)
	}
	if report.Errors != 1 {
		t.Fatalf("Errors = %d, quiero 1", report.Errors)
	}

	want := Confusion{TP: 2, TN: 1, FP: 1, FN: 2}
	if report.Confusion != want {
		t.Fatalf("Confusion = %+v, quiero %+v", report.Confusion, want)
	}

	if !almostEqual(report.Metrics.Accuracy, 0.5) {
		t.Errorf("Accuracy = %.4f, quiero 0.5", report.Metrics.Accuracy)
	}
	if !almostEqual(report.Metrics.Precision, 0.6667) {
		t.Errorf("Precision = %.4f, quiero 0.6667", report.Metrics.Precision)
	}
	if !almostEqual(report.Metrics.Recall, 0.5) {
		t.Errorf("Recall = %.4f, quiero 0.5", report.Metrics.Recall)
	}
	if !almostEqual(report.Metrics.Specificity, 0.5) {
		t.Errorf("Specificity = %.4f, quiero 0.5", report.Metrics.Specificity)
	}

	if fns := report.FalseNegatives(); len(fns) != 2 {
		t.Errorf("FalseNegatives = %d, quiero 2", len(fns))
	}
}

func TestBuildEmpty(t *testing.T) {
	report := Build("test", "dataset", 0.5, time.Now(), nil)
	if report.Metrics.Recall != 0 || report.Metrics.Precision != 0 || report.Metrics.F1 != 0 {
		t.Fatalf("métricas de conjunto vacío deben ser 0: %+v", report.Metrics)
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.0002
}
