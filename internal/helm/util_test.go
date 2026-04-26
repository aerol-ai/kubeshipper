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

// ============================================================
// Additional helm/util tests (20 new tests)
// ============================================================

// --- valuesToYAML edge cases ---

func TestValuesToYAML_MultipleKeys(t *testing.T) {
	m := map[string]any{"alpha": "a", "beta": "b", "gamma": "c"}
	out, err := valuesToYAML(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, k := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, k) {
			t.Errorf("key %q missing from YAML output", k)
		}
	}
}

func TestValuesToYAML_NestedMap(t *testing.T) {
	m := map[string]any{
		"outer": map[string]any{"inner": "value"},
	}
	out, err := valuesToYAML(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "outer") {
		t.Error("outer key missing")
	}
	if !strings.Contains(out, "inner") {
		t.Error("inner key missing")
	}
}

func TestValuesToYAML_BoolValue(t *testing.T) {
	m := map[string]any{"enabled": true, "debug": false}
	out, err := valuesToYAML(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "true") {
		t.Error("boolean true not in output")
	}
}

func TestValuesToYAML_ListValue(t *testing.T) {
	m := map[string]any{"hosts": []any{"a.com", "b.com"}}
	out, err := valuesToYAML(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "a.com") {
		t.Error("list item missing from YAML")
	}
}

func TestValuesToYAML_IntValue(t *testing.T) {
	m := map[string]any{"replicas": 3}
	out, err := valuesToYAML(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "3") {
		t.Error("integer value missing from YAML")
	}
}

func TestValuesToYAML_FloatValue(t *testing.T) {
	m := map[string]any{"ratio": 0.75}
	out, err := valuesToYAML(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "0.75") && !strings.Contains(out, "7.5e") {
		t.Errorf("float value missing from YAML: %q", out)
	}
}

// --- parseValuesYAML edge cases ---

func TestParseValuesYAML_NestedMap(t *testing.T) {
	input := "outer:\n  inner: hello\n"
	m, err := parseValuesYAML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outer, ok := m["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer should be a map, got %T", m["outer"])
	}
	if outer["inner"] != "hello" {
		t.Errorf("inner: got %v", outer["inner"])
	}
}

func TestParseValuesYAML_BoolTrue(t *testing.T) {
	m, err := parseValuesYAML("enabled: true\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["enabled"] != true {
		t.Errorf("enabled: got %v", m["enabled"])
	}
}

func TestParseValuesYAML_BoolFalse(t *testing.T) {
	m, err := parseValuesYAML("enabled: false\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["enabled"] != false {
		t.Errorf("enabled: got %v", m["enabled"])
	}
}

func TestParseValuesYAML_List(t *testing.T) {
	m, err := parseValuesYAML("hosts:\n  - a.com\n  - b.com\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hosts, ok := m["hosts"].([]any)
	if !ok {
		t.Fatalf("hosts should be a slice, got %T", m["hosts"])
	}
	if len(hosts) != 2 {
		t.Errorf("want 2 hosts, got %d", len(hosts))
	}
}

func TestParseValuesYAML_InvalidYAML(t *testing.T) {
	_, err := parseValuesYAML("key: [unterminated")
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseValuesYAML_WithComments(t *testing.T) {
	input := "# comment\nname: myapp\n# another comment\nport: 8080\n"
	m, err := parseValuesYAML(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["name"] != "myapp" {
		t.Errorf("name: got %v", m["name"])
	}
}

func TestParseValuesYAML_FloatValue(t *testing.T) {
	m, err := parseValuesYAML("ratio: 1.5\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["ratio"] != 1.5 {
		t.Errorf("ratio: got %v", m["ratio"])
	}
}

// --- round-trip ---

func TestValuesRoundTrip(t *testing.T) {
	original := map[string]any{
		"image":    "nginx:latest",
		"replicas": float64(3), // JSON round-trips ints as float64 via YAML
	}
	yamlStr, err := valuesToYAML(original)
	if err != nil {
		t.Fatalf("toYAML: %v", err)
	}
	parsed, err := parseValuesYAML(yamlStr)
	if err != nil {
		t.Fatalf("fromYAML: %v", err)
	}
	if parsed["image"] != "nginx:latest" {
		t.Errorf("image after round-trip: got %v", parsed["image"])
	}
}

// --- timeoutOrDefault edge cases ---

func TestTimeoutOrDefault_LargeValue(t *testing.T) {
	d := timeoutOrDefault(3600, 5*time.Minute)
	if d != 3600*time.Second {
		t.Errorf("want 1h, got %v", d)
	}
}

// --- emitErr edge cases ---

func TestEmitErr_NilFn(t *testing.T) {
	// nil fn should not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("panic with nil fn: %v", r)
		}
	}()
	emitErr(nil, "test error")
}

func TestEmitErr_MsgInErrorField(t *testing.T) {
	var captured store.Event
	fn := func(ev store.Event) { captured = ev }
	emitErr(fn, "disk full")
	if captured.Error != "disk full" {
		t.Errorf("error field: got %q, want disk full", captured.Error)
	}
}

// --- emit phase variants ---

func TestEmit_PhaseValidation(t *testing.T) {
	var captured store.Event
	fn := func(ev store.Event) { captured = ev }
	emit(fn, "validation", "checking prerequisites")
	if captured.Phase != "validation" {
		t.Errorf("phase: got %q", captured.Phase)
	}
}

func TestEmit_PhaseDone(t *testing.T) {
	var captured store.Event
	fn := func(ev store.Event) { captured = ev }
	emit(fn, "done", "all resources applied")
	if captured.Phase != "done" {
		t.Errorf("phase: got %q", captured.Phase)
	}
}
