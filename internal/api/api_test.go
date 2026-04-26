package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	helmtypes "github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/kube"
	"github.com/aerol-ai/kubeshipper/internal/store"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// --- test helpers ---

func newAPITestStore(t *testing.T) *store.Store {
	t.Helper()
	f, err := os.CreateTemp("", "ks-api-test-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	st, err := store.Open(f.Name())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// newTestKubeClient creates a kube.Client backed by a fake Kubernetes clientset.
// "default" is the only managed namespace.
func newTestKubeClient() *kube.Client {
	return &kube.Client{
		Managed: map[string]bool{"default": true},
		KC:      kubefake.NewSimpleClientset(),
	}
}

// newTestServer returns a Server with a real SQLite store and fake k8s client.
// deps.Helm is nil — only use for endpoints that return before calling Helm.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(Deps{
		Store:     newAPITestStore(t),
		Kube:      newTestKubeClient(),
		Helm:      nil,
		AuthToken: "",
		StartedAt: "2024-01-01T00:00:00Z",
		Version:   "test",
	})
}

// newTestServerWithToken returns a Server that requires a bearer token.
func newTestServerWithToken(t *testing.T, token string) *Server {
	t.Helper()
	return NewServer(Deps{
		Store:     newAPITestStore(t),
		Kube:      newTestKubeClient(),
		Helm:      nil,
		AuthToken: token,
		StartedAt: "2024-01-01T00:00:00Z",
		Version:   "test",
	})
}

// do issues a request against the server handler and returns the recorder.
func do(srv *Server, method, target string, body []byte) *httptest.ResponseRecorder {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, bodyReader)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// doWithToken issues a request with an Authorization: Bearer header.
func doWithToken(srv *Server, method, target, token string, body []byte) *httptest.ResponseRecorder {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, bodyReader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// --- Auth middleware tests (56–60) ---

// Test 56
func TestAuthMiddleware_NoTokenConfigured(t *testing.T) {
	srv := newTestServer(t) // AuthToken = ""
	rec := do(srv, "GET", "/services", nil)
	if rec.Code == http.StatusUnauthorized {
		t.Error("should pass through when no token is configured")
	}
}

// Test 57
func TestAuthMiddleware_MissingHeader(t *testing.T) {
	srv := newTestServerWithToken(t, "secret123")
	rec := do(srv, "GET", "/services", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

// Test 58
func TestAuthMiddleware_NoBearer(t *testing.T) {
	srv := newTestServerWithToken(t, "secret123")
	req := httptest.NewRequest("GET", "/services", nil)
	req.Header.Set("Authorization", "Token secret123") // wrong scheme
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

// Test 59
func TestAuthMiddleware_WrongToken(t *testing.T) {
	srv := newTestServerWithToken(t, "secret123")
	rec := doWithToken(srv, "GET", "/services", "wrongtoken", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rec.Code)
	}
}

// Test 60
func TestAuthMiddleware_CorrectToken(t *testing.T) {
	srv := newTestServerWithToken(t, "secret123")
	rec := doWithToken(srv, "GET", "/services", "secret123", nil)
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Errorf("expected authenticated response, got %d", rec.Code)
	}
}

// --- Initiator fingerprint tests (61–63) ---

// Test 61
func TestInitiator_NoHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if got := initiator(req); got != "" {
		t.Errorf("want empty initiator, got %q", got)
	}
}

// Test 62
func TestInitiator_ShortToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer abc") // < 8 chars
	got := initiator(req)
	if got != "token:short" {
		t.Errorf("want token:short, got %q", got)
	}
}

// Test 63
func TestInitiator_NormalToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer mytoken123456")
	got := initiator(req)
	if !strings.HasPrefix(got, "token:") {
		t.Errorf("expected token: prefix, got %q", got)
	}
	// should contain first 8 chars of the token
	if got != "token:mytoken1" {
		t.Errorf("want token:mytoken1, got %q", got)
	}
}

// --- Public endpoints (64–65) ---

// Test 64
func TestServerRootEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["name"] != "kubeshipper" {
		t.Errorf("name: got %v", body["name"])
	}
}

// Test 65
func TestServerHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/health", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status: got %v", body["status"])
	}
	if body["version"] != "test" {
		t.Errorf("version: got %v", body["version"])
	}
}

// --- validateInstall tests (66–75) ---

// Test 66
func TestValidateInstall_EmptyRelease(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "oci", URL: "oci://r/c", Version: "1"},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for empty release")
	}
}

// Test 67
func TestValidateInstall_EmptyNamespace(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "",
		Source:    &helmtypes.ChartSource{Type: "oci", URL: "oci://r/c", Version: "1"},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

// Test 68
func TestValidateInstall_MissingSource(t *testing.T) {
	req := &helmtypes.InstallReq{Release: "myapp", Namespace: "default", Source: nil}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for nil source")
	}
}

// Test 69
func TestValidateInstall_OCI_MissingURL(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "oci", URL: "", Version: "1.0"},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for oci missing url")
	}
}

// Test 70
func TestValidateInstall_OCI_Valid(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "oci", URL: "oci://registry/chart", Version: "1.0.0"},
	}
	if err := validateInstall(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test 71
func TestValidateInstall_HTTPS_MissingRepoURL(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "https", RepoURL: "", Chart: "mychart"},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for https missing repoUrl")
	}
}

// Test 72
func TestValidateInstall_HTTPS_Valid(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "https", RepoURL: "https://charts.example.com", Chart: "mychart"},
	}
	if err := validateInstall(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test 73
func TestValidateInstall_Git_MissingRepoURL(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "git", RepoURL: ""},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for git missing repoUrl")
	}
}

// Test 74
func TestValidateInstall_Git_Valid(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "git", RepoURL: "https://github.com/org/repo.git"},
	}
	if err := validateInstall(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Test 75
func TestValidateInstall_UnknownSourceType(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "s3"},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for unknown source type")
	}
}

// --- Request helper tests (76–80) ---

// Test 76
func TestMustQuery_Present(t *testing.T) {
	req := httptest.NewRequest("GET", "/?namespace=default", nil)
	v, ok := mustQuery(req, "namespace")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if v != "default" {
		t.Errorf("value: got %q, want default", v)
	}
}

// Test 77
func TestMustQuery_Absent(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	v, ok := mustQuery(req, "namespace")
	if ok {
		t.Fatal("expected ok=false when key absent")
	}
	if v != "" {
		t.Errorf("value: got %q, want empty", v)
	}
}

// Test 78
func TestRequireForce_True(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/?force=true", nil)
	if !requireForce(req) {
		t.Fatal("expected true for ?force=true")
	}
}

// Test 79
func TestRequireForce_FalseValue(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/?force=false", nil)
	if requireForce(req) {
		t.Fatal("expected false for ?force=false")
	}
}

// Test 80
func TestRequireForce_Absent(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/", nil)
	if requireForce(req) {
		t.Fatal("expected false when force param absent")
	}
}

// --- writeJSON (test 81) ---

// Test 81
func TestWriteJSON_SetsContentTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"a": "b"})
	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
}

// --- Service handler integration tests (82–87) ---

// Test 82
func TestHandlerCreateService_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/services", []byte("not-json"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

// Test 83
func TestHandlerCreateService_InvalidNamespace(t *testing.T) {
	srv := newTestServer(t)
	// "production" is not in the managed namespaces list (only "default" is).
	body := []byte(`{"name":"myapp","image":"nginx","namespace":"production"}`)
	rec := do(srv, "POST", "/services", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for unmanaged namespace, got %d", rec.Code)
	}
}

// Test 84
func TestHandlerCreateService_MissingImage(t *testing.T) {
	srv := newTestServer(t)
	body := []byte(`{"name":"myapp","namespace":"default"}`)
	rec := do(srv, "POST", "/services", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing image, got %d", rec.Code)
	}
}

// Test 85
func TestHandlerCreateService_Valid(t *testing.T) {
	srv := newTestServer(t)
	body := []byte(`{"name":"myapp","image":"nginx:latest","namespace":"default"}`)
	rec := do(srv, "POST", "/services", body)
	if rec.Code != http.StatusAccepted {
		t.Errorf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if resp["jobId"] == "" || resp["jobId"] == nil {
		t.Error("expected non-empty jobId in response")
	}
}

// Test 86
func TestHandlerGetService_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/services/nonexistent", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

// Test 87
func TestHandlerGetService_Found(t *testing.T) {
	srv := newTestServer(t)

	// Create the service first.
	createBody := []byte(`{"name":"myapp","image":"nginx:latest","namespace":"default"}`)
	createRec := do(srv, "POST", "/services", createBody)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create failed: %d %s", createRec.Code, createRec.Body.String())
	}

	// Now fetch it.
	rec := do(srv, "GET", "/services/myapp", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if body["id"] != "myapp" {
		t.Errorf("id: got %v", body["id"])
	}
}

// --- Chart handler validation tests (88–90) ---
// Helm manager is nil; these endpoints return 400 before any Helm call.

// Test 88
func TestHandlerInstallChart_MissingRelease(t *testing.T) {
	srv := newTestServer(t)
	// namespace is present, release is missing.
	body := []byte(`{"namespace":"default","source":{"type":"oci","url":"oci://r/c","version":"1"}}`)
	rec := do(srv, "POST", "/charts", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing release, got %d", rec.Code)
	}
}

// Test 89
func TestHandlerGetRelease_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	// No ?namespace query param → handler returns 400 before reaching Helm.
	rec := do(srv, "GET", "/charts/myrelease", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

// Test 90
func TestHandlerUninstallRelease_MissingForce(t *testing.T) {
	srv := newTestServer(t)
	// namespace is present but ?force=true is absent → 400 before Helm.
	rec := do(srv, "DELETE", "/charts/myrelease?namespace=default", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing force param, got %d", rec.Code)
	}
}
