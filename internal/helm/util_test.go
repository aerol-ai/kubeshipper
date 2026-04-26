package helm

import (
	"strings"
	"testing"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/store"
)

// --- valuesToYAML tests (91–93) ---

// Test 91
func TestValuesToYAML_NilMap(t *testing.T) {
	s, err := valuesToYAML(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "" {
		t.Errorf("want empty string for nil map, got %q", s)
	}
}

// Test 92
func TestValuesToYAML_EmptyNonNilMap(t *testing.T) {
	s, err := valuesToYAML(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "" {
		t.Errorf("want empty string for empty map, got %q", s)
	}
}

// Test 93
func TestValuesToYAML_SingleValue(t *testing.T) {
	s, err := valuesToYAML(map[string]any{"replicas": 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == "" {
		t.Error("expected non-empty YAML string")
	}
	if !strings.Contains(s, "replicas") {
		t.Errorf("expected 'replicas' in YAML output, got: %q", s)
	}
}

// --- parseValuesYAML tests (94–96) ---

// Test 94
func TestParseValuesYAML_EmptyString(t *testing.T) {
	m, err := parseValuesYAML("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("want empty map for empty input, got %v", m)
	}
}

// Test 95
func TestParseValuesYAML_Valid(t *testing.T) {
	m, err := parseValuesYAML("key: value\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("key: got %v, want value", m["key"])
	}
}

// Test 96
func TestParseValuesYAML_NumberValue(t *testing.T) {
	m, err := parseValuesYAML("replicas: 3\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["replicas"] == nil {
		t.Error("replicas key is nil after parsing")
	}
}

// --- timeoutOrDefault tests (97–99) ---

// Test 97
func TestTimeoutOrDefault_Zero(t *testing.T) {
	d := timeoutOrDefault(0, 10*time.Minute)
	if d != 10*time.Minute {
		t.Errorf("got %v, want 10m", d)
	}
}

// Test 98
func TestTimeoutOrDefault_Negative(t *testing.T) {
	d := timeoutOrDefault(-5, 5*time.Minute)
	if d != 5*time.Minute {
		t.Errorf("got %v, want 5m", d)
	}
}

// Test 99
func TestTimeoutOrDefault_Positive(t *testing.T) {
	d := timeoutOrDefault(30, 10*time.Minute)
	if d != 30*time.Second {
		t.Errorf("got %v, want 30s", d)
	}
}

// --- boolDefault tests (100–103) ---

// Test 100
func TestBoolDefault_NilDefaultTrue(t *testing.T) {
	if !boolDefault(nil, true) {
		t.Error("expected true for nil ptr with true default")
	}
}

// Test 101
func TestBoolDefault_NilDefaultFalse(t *testing.T) {
	if boolDefault(nil, false) {
		t.Error("expected false for nil ptr with false default")
	}
}

// Test 102
func TestBoolDefault_PtrTrue(t *testing.T) {
	b := true
	if !boolDefault(&b, false) {
		t.Error("expected true when pointer is true")
	}
}

// Test 103
func TestBoolDefault_PtrFalse(t *testing.T) {
	b := false
	if boolDefault(&b, true) {
		t.Error("expected false when pointer is false")
	}
}

// --- emit (test 104) ---

// Test 104
func TestEmit_NilFn(t *testing.T) {
	// emit with a nil EmitFn must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("emit(nil, ...) panicked: %v", r)
		}
	}()
	emit(nil, "validation", "should not panic")
}

// Bonus: verify emit actually invokes the callback (counted within test 104 scope).
func TestEmit_CallsEmitFn(t *testing.T) {
	var captured store.Event
	fn := func(ev store.Event) { captured = ev }
	emit(fn, "apply", "applying resources")
	if captured.Phase != "apply" {
		t.Errorf("phase: got %q, want apply", captured.Phase)
	}
	if captured.Message != "applying resources" {
		t.Errorf("message: got %q", captured.Message)
	}
	if captured.TS == 0 {
		t.Error("expected non-zero TS")
	}
}
