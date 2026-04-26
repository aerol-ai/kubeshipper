package kube

import (
	"strings"
	"testing"
)

// ptrInt is a convenience helper for creating *int literals in tests.
func ptrInt(i int) *int { return &i }

// --- ParseServiceSpec tests (tests 1–20) ---

// Test 1
func TestParseServiceSpec_ValidMinimal(t *testing.T) {
	body := []byte(`{"name":"my-app","image":"nginx:latest","namespace":"default"}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "my-app" {
		t.Errorf("name: got %q, want %q", spec.Name, "my-app")
	}
	if spec.Image != "nginx:latest" {
		t.Errorf("image: got %q, want %q", spec.Image, "nginx:latest")
	}
	if spec.Type != "service" {
		t.Errorf("type default: got %q, want %q", spec.Type, "service")
	}
	if spec.Replicas == nil || *spec.Replicas != 1 {
		t.Errorf("replicas default: want 1, got %v", spec.Replicas)
	}
}

// Test 2
func TestParseServiceSpec_InvalidJSON(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// Test 3
func TestParseServiceSpec_EmptyName(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"","image":"nginx"}`))
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

// Test 4
func TestParseServiceSpec_NameTooLong(t *testing.T) {
	name := strings.Repeat("a", 64)
	_, err := ParseServiceSpec([]byte(`{"name":"` + name + `","image":"nginx"}`))
	if err == nil {
		t.Fatal("expected error for name longer than 63 chars")
	}
}

// Test 5
func TestParseServiceSpec_NameUpperCase(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"MyApp","image":"nginx"}`))
	if err == nil {
		t.Fatal("expected error for uppercase name (DNS-1035 violation)")
	}
}

// Test 6
func TestParseServiceSpec_NameUnderscore(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"my_app","image":"nginx"}`))
	if err == nil {
		t.Fatal("expected error for underscore in name")
	}
}

// Test 7
func TestParseServiceSpec_NameStartDash(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"-myapp","image":"nginx"}`))
	if err == nil {
		t.Fatal("expected error for name starting with dash")
	}
}

// Test 8
func TestParseServiceSpec_NameEndDash(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"myapp-","image":"nginx"}`))
	if err == nil {
		t.Fatal("expected error for name ending with dash")
	}
}

// Test 9
func TestParseServiceSpec_NameValidDashes(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"my-service-v2","image":"nginx"}`))
	if err != nil {
		t.Fatalf("unexpected error for valid dashed name: %v", err)
	}
	if spec.Name != "my-service-v2" {
		t.Errorf("got %q", spec.Name)
	}
}

// Test 10
func TestParseServiceSpec_MissingImage(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"myapp"}`))
	if err == nil {
		t.Fatal("expected error for missing image")
	}
}

// Test 11
func TestParseServiceSpec_PortZero(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","port":0}`))
	if err == nil {
		t.Fatal("expected error for port=0")
	}
}

// Test 12
func TestParseServiceSpec_PortTooHigh(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","port":65536}`))
	if err == nil {
		t.Fatal("expected error for port=65536")
	}
}

// Test 13
func TestParseServiceSpec_ValidPort(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","port":8080}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Port == nil || *spec.Port != 8080 {
		t.Errorf("port: want 8080, got %v", spec.Port)
	}
}

// Test 14
func TestParseServiceSpec_NegativeReplicas(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","replicas":-1}`))
	if err == nil {
		t.Fatal("expected error for negative replicas")
	}
}

// Test 15
func TestParseServiceSpec_DefaultReplicas(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Replicas == nil || *spec.Replicas != 1 {
		t.Errorf("want replicas=1, got %v", spec.Replicas)
	}
}

// Test 16
func TestParseServiceSpec_DefaultType(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Type != "service" {
		t.Errorf("want type=service, got %q", spec.Type)
	}
}

// Test 17
func TestParseServiceSpec_InvalidType(t *testing.T) {
	_, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","type":"worker"}`))
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

// Test 18
func TestParseServiceSpec_TypeJob(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","type":"job"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Type != "job" {
		t.Errorf("want type=job, got %q", spec.Type)
	}
}

// Test 19
func TestParseServiceSpec_TypeCronJob(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","type":"cronjob"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Type != "cronjob" {
		t.Errorf("want type=cronjob, got %q", spec.Type)
	}
}

// Test 20
func TestParseServiceSpec_WithEnv(t *testing.T) {
	body := []byte(`{"name":"myapp","image":"nginx","env":{"KEY":"val","OTHER":"x"}}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Env["KEY"] != "val" {
		t.Errorf("env KEY: got %q", spec.Env["KEY"])
	}
	if spec.Env["OTHER"] != "x" {
		t.Errorf("env OTHER: got %q", spec.Env["OTHER"])
	}
}

// --- ServiceSpec.Merge tests (tests 21–25) ---

// Test 21
func TestServiceSpec_Merge_Image(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx:1.0", Namespace: "default"}
	_ = base.Validate()
	patch := &ServiceSpec{Image: "nginx:2.0"}
	merged := base.Merge(patch)
	if merged.Image != "nginx:2.0" {
		t.Errorf("got %q, want nginx:2.0", merged.Image)
	}
	if merged.Name != "app" {
		t.Errorf("name changed unexpectedly: got %q", merged.Name)
	}
}

// Test 22
func TestServiceSpec_Merge_Env(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx", Env: map[string]string{"A": "1"}}
	_ = base.Validate()
	patch := &ServiceSpec{Env: map[string]string{"B": "2"}}
	merged := base.Merge(patch)
	if merged.Env["B"] != "2" {
		t.Errorf("env B: got %q", merged.Env["B"])
	}
}

// Test 23
func TestServiceSpec_Merge_Port(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx"}
	_ = base.Validate()
	patch := &ServiceSpec{Port: ptrInt(8080)}
	merged := base.Merge(patch)
	if merged.Port == nil || *merged.Port != 8080 {
		t.Errorf("port: got %v", merged.Port)
	}
}

// Test 24
func TestServiceSpec_Merge_Replicas(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx"}
	_ = base.Validate()
	patch := &ServiceSpec{Replicas: ptrInt(3)}
	merged := base.Merge(patch)
	if merged.Replicas == nil || *merged.Replicas != 3 {
		t.Errorf("replicas: got %v", merged.Replicas)
	}
}

// Test 25
func TestServiceSpec_Merge_EmptyPatch(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx:1.0"}
	_ = base.Validate()
	patch := &ServiceSpec{}
	merged := base.Merge(patch)
	if merged.Image != "nginx:1.0" {
		t.Errorf("image changed unexpectedly: %q", merged.Image)
	}
	if merged.Name != "app" {
		t.Errorf("name changed unexpectedly: %q", merged.Name)
	}
}
