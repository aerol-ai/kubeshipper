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

// ============================================================
// Additional ServiceSpec tests (adds 35 new tests)
// ============================================================

// --- Name edge cases ---

func TestParseServiceSpec_NameSingleChar(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"a","image":"nginx"}`))
	if err != nil {
		t.Fatalf("single char name should be valid: %v", err)
	}
	if spec.Name != "a" {
		t.Errorf("name: got %q", spec.Name)
	}
}

func TestParseServiceSpec_Name63Chars(t *testing.T) {
	name := strings.Repeat("a", 63)
	spec, err := ParseServiceSpec([]byte(`{"name":"` + name + `","image":"nginx"}`))
	if err != nil {
		t.Fatalf("63-char name should be valid: %v", err)
	}
	if spec.Name != name {
		t.Errorf("name mismatch")
	}
}

func TestParseServiceSpec_NameAllDigitsStart(t *testing.T) {
	// DNS-1035 regex allows starting with a digit (pattern: [a-z0-9]...)
	spec, err := ParseServiceSpec([]byte(`{"name":"123abc","image":"nginx"}`))
	if err != nil {
		t.Fatalf("name starting with digit should be valid: %v", err)
	}
	if spec.Name != "123abc" {
		t.Errorf("name: got %q", spec.Name)
	}
}

// --- Port edge cases ---

func TestParseServiceSpec_Port1(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","port":1}`))
	if err != nil {
		t.Fatalf("port=1 should be valid: %v", err)
	}
	if spec.Port == nil || *spec.Port != 1 {
		t.Errorf("port: got %v", spec.Port)
	}
}

func TestParseServiceSpec_Port65535(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","port":65535}`))
	if err != nil {
		t.Fatalf("port=65535 should be valid: %v", err)
	}
	if spec.Port == nil || *spec.Port != 65535 {
		t.Errorf("port: got %v", spec.Port)
	}
}

// --- Replicas edge cases ---

func TestParseServiceSpec_ReplicasZero(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","replicas":0}`))
	if err != nil {
		t.Fatalf("replicas=0 (scale-to-zero) should be valid: %v", err)
	}
	if spec.Replicas == nil || *spec.Replicas != 0 {
		t.Errorf("replicas: got %v", spec.Replicas)
	}
}

func TestParseServiceSpec_ReplicasLarge(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","replicas":1000}`))
	if err != nil {
		t.Fatalf("large replicas should be valid: %v", err)
	}
	if *spec.Replicas != 1000 {
		t.Errorf("replicas: got %d", *spec.Replicas)
	}
}

// --- Optional field parsing ---

func TestParseServiceSpec_WithResources(t *testing.T) {
	body := []byte(`{
		"name":"myapp","image":"nginx",
		"resources":{"requests":{"cpu":"100m","memory":"128Mi"},"limits":{"cpu":"500m"}}
	}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Resources == nil {
		t.Fatal("resources should not be nil")
	}
	if spec.Resources.Requests["cpu"] != "100m" {
		t.Errorf("cpu request: got %q", spec.Resources.Requests["cpu"])
	}
	if spec.Resources.Limits["cpu"] != "500m" {
		t.Errorf("cpu limit: got %q", spec.Resources.Limits["cpu"])
	}
}

func TestParseServiceSpec_WithImagePullSecret(t *testing.T) {
	body := []byte(`{"name":"myapp","image":"nginx","imagePullSecret":"gcr-secret"}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ImagePullSecret != "gcr-secret" {
		t.Errorf("imagePullSecret: got %q", spec.ImagePullSecret)
	}
}

func TestParseServiceSpec_WithHostname(t *testing.T) {
	body := []byte(`{"name":"myapp","image":"nginx","hostname":"myapp.example.com"}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Hostname != "myapp.example.com" {
		t.Errorf("hostname: got %q", spec.Hostname)
	}
}

func TestParseServiceSpec_Public(t *testing.T) {
	body := []byte(`{"name":"myapp","image":"nginx","public":true}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !spec.Public {
		t.Error("public should be true")
	}
}

func TestParseServiceSpec_WithSchedule(t *testing.T) {
	body := []byte(`{"name":"myapp","image":"nginx","type":"cronjob","schedule":"0 * * * *"}`)
	spec, err := ParseServiceSpec(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Schedule != "0 * * * *" {
		t.Errorf("schedule: got %q", spec.Schedule)
	}
}

func TestParseServiceSpec_ImageWithTag(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx:1.19-alpine"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Image != "nginx:1.19-alpine" {
		t.Errorf("image: got %q", spec.Image)
	}
}

func TestParseServiceSpec_ImageWithDigest(t *testing.T) {
	img := "nginx@sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"` + img + `"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Image != img {
		t.Errorf("image: got %q", spec.Image)
	}
}

func TestParseServiceSpec_EmptyBody(t *testing.T) {
	_, err := ParseServiceSpec([]byte(``))
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestParseServiceSpec_TypeServiceExplicit(t *testing.T) {
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","type":"service"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Type != "service" {
		t.Errorf("type: got %q", spec.Type)
	}
}

func TestParseServiceSpec_ReplicasDefaultOnlyWhenNil(t *testing.T) {
	// When replicas is explicitly 0, it should NOT be defaulted to 1.
	spec, err := ParseServiceSpec([]byte(`{"name":"myapp","image":"nginx","replicas":0}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Replicas == nil || *spec.Replicas != 0 {
		t.Errorf("replicas should be 0, got %v", spec.Replicas)
	}
}

// --- Merge edge cases ---

func TestServiceSpec_Merge_Hostname(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx"}
	_ = base.Validate()
	patch := &ServiceSpec{Hostname: "new.example.com"}
	merged := base.Merge(patch)
	if merged.Hostname != "new.example.com" {
		t.Errorf("hostname: got %q", merged.Hostname)
	}
}

func TestServiceSpec_Merge_Namespace(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx", Namespace: "default"}
	_ = base.Validate()
	patch := &ServiceSpec{Namespace: "production"}
	merged := base.Merge(patch)
	if merged.Namespace != "production" {
		t.Errorf("namespace: got %q", merged.Namespace)
	}
}

func TestServiceSpec_Merge_ImagePullSecret(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx"}
	_ = base.Validate()
	patch := &ServiceSpec{ImagePullSecret: "my-secret"}
	merged := base.Merge(patch)
	if merged.ImagePullSecret != "my-secret" {
		t.Errorf("imagePullSecret: got %q", merged.ImagePullSecret)
	}
}

func TestServiceSpec_Merge_Resources(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx"}
	_ = base.Validate()
	patch := &ServiceSpec{Resources: &ResourceRequests{
		Requests: map[string]string{"cpu": "200m"},
	}}
	merged := base.Merge(patch)
	if merged.Resources == nil {
		t.Fatal("resources should not be nil after merge")
	}
	if merged.Resources.Requests["cpu"] != "200m" {
		t.Errorf("cpu: got %q", merged.Resources.Requests["cpu"])
	}
}

func TestServiceSpec_Merge_Type(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx"}
	_ = base.Validate()
	patch := &ServiceSpec{Type: "job"}
	merged := base.Merge(patch)
	if merged.Type != "job" {
		t.Errorf("type: got %q", merged.Type)
	}
}

func TestServiceSpec_Merge_Schedule(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx", Type: "cronjob"}
	_ = base.Validate()
	patch := &ServiceSpec{Schedule: "*/5 * * * *"}
	merged := base.Merge(patch)
	if merged.Schedule != "*/5 * * * *" {
		t.Errorf("schedule: got %q", merged.Schedule)
	}
}

func TestServiceSpec_Merge_NilPortDoesntReset(t *testing.T) {
	port := 8080
	base := &ServiceSpec{Name: "app", Image: "nginx", Port: &port}
	_ = base.Validate()
	patch := &ServiceSpec{Image: "nginx:new"} // Port is nil in patch
	merged := base.Merge(patch)
	if merged.Port == nil || *merged.Port != 8080 {
		t.Errorf("port should not have been reset; got %v", merged.Port)
	}
}

func TestServiceSpec_Merge_NilReplicasDoesntReset(t *testing.T) {
	replicas := 3
	base := &ServiceSpec{Name: "app", Image: "nginx", Replicas: &replicas}
	_ = base.Validate()
	patch := &ServiceSpec{Image: "nginx:new"} // Replicas is nil in patch
	merged := base.Merge(patch)
	if merged.Replicas == nil || *merged.Replicas != 3 {
		t.Errorf("replicas should not have been reset; got %v", merged.Replicas)
	}
}

func TestServiceSpec_Merge_MultipleFields(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx:1.0", Namespace: "default"}
	_ = base.Validate()
	patch := &ServiceSpec{Image: "nginx:2.0", Hostname: "new.host"}
	merged := base.Merge(patch)
	if merged.Image != "nginx:2.0" {
		t.Errorf("image: got %q", merged.Image)
	}
	if merged.Hostname != "new.host" {
		t.Errorf("hostname: got %q", merged.Hostname)
	}
	if merged.Namespace != "default" {
		t.Errorf("namespace changed: got %q", merged.Namespace)
	}
}

func TestServiceSpec_Validate_AfterMerge(t *testing.T) {
	base := &ServiceSpec{Name: "app", Image: "nginx", Namespace: "default"}
	_ = base.Validate()
	patch := &ServiceSpec{Replicas: ptrInt(2)}
	merged := base.Merge(patch)
	if err := merged.Validate(); err != nil {
		t.Fatalf("merged spec should validate: %v", err)
	}
}

// --- ResolveNamespace ---

func TestResolveNamespace_Empty_DefaultsToFirst(t *testing.T) {
	c := &Client{Managed: map[string]bool{"default": true}}
	ns, err := c.ResolveNamespace("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "default" {
		t.Errorf("got %q, want default", ns)
	}
}

func TestResolveNamespace_Managed(t *testing.T) {
	c := &Client{Managed: map[string]bool{"production": true, "staging": true}}
	ns, err := c.ResolveNamespace("production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "production" {
		t.Errorf("got %q, want production", ns)
	}
}

func TestResolveNamespace_Unmanaged_Error(t *testing.T) {
	c := &Client{Managed: map[string]bool{"default": true}}
	_, err := c.ResolveNamespace("forbidden")
	if err == nil {
		t.Fatal("expected error for unmanaged namespace")
	}
}

func TestResolveNamespace_NoManaged_Error(t *testing.T) {
	c := &Client{Managed: map[string]bool{}}
	_, err := c.ResolveNamespace("")
	if err == nil {
		t.Fatal("expected error when no namespaces configured")
	}
}
