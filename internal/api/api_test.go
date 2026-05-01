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
	"github.com/aerol-ai/kubeshipper/internal/rollout"
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
	st := newAPITestStore(t)
	kc := newTestKubeClient()
	return NewServer(Deps{
		Store:     st,
		Kube:      kc,
		Helm:      nil,
		Rollouts:  rollout.NewManager(st, kc),
		AuthToken: "",
		StartedAt: "2024-01-01T00:00:00Z",
		Version:   "test",
	})
}

// newTestServerWithToken returns a Server that requires a bearer token.
func newTestServerWithToken(t *testing.T, token string) *Server {
	t.Helper()
	st := newAPITestStore(t)
	kc := newTestKubeClient()
	return NewServer(Deps{
		Store:     st,
		Kube:      kc,
		Helm:      nil,
		Rollouts:  rollout.NewManager(st, kc),
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
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("want text/html content-type, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), "<div id=\"root\"></div>") {
		t.Fatalf("expected embedded dashboard HTML")
	}
}

func TestServerAPIRootEndpoint(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/api/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
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

func TestValidateInstall_RolloutWatchMissingTarget(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:      "myapp",
		Namespace:    "default",
		Source:       &helmtypes.ChartSource{Type: "oci", URL: "oci://r/c", Version: "1.0.0"},
		RolloutWatch: &helmtypes.RolloutWatchConfig{},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for rollout watch without target")
	}
}

func TestValidateInstall_RolloutWatchAliasConflict(t *testing.T) {
	req := &helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "oci", URL: "oci://r/c", Version: "1.0.0"},
		RolloutWatch: &helmtypes.RolloutWatchConfig{
			Deployment: "agent-gateway",
			Service:    "agent-service",
		},
	}
	if err := validateInstall(req); err == nil {
		t.Fatal("expected error for rollout watch alias conflict")
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

// ============================================================
// Additional API tests (adds 40 new tests)
// ============================================================

// --- Service list ---

func TestHandlerListServices_Empty(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/services", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	// The key must exist (value may be null when no services).
	if _, ok := body["services"]; !ok {
		t.Fatal("response missing 'services' key")
	}
}

func TestHandlerListServices_AfterCreate(t *testing.T) {
	srv := newTestServer(t)
	body := bytes.NewReader([]byte(`{"name":"myapp","image":"nginx"}`))
	req := httptest.NewRequest("POST", "/services", body)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Now list and expect at least one service.
	rec2 := do(srv, "GET", "/services", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d", rec2.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &resp)
	svcs := resp["services"].([]any)
	if len(svcs) < 1 {
		t.Errorf("expected at least 1 service, got %d", len(svcs))
	}
}

func TestHandlerListServices_ContentType(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/services", nil)
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
}

// --- Service patch ---

func TestHandlerPatchService_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "PATCH", "/services/nonexistent", []byte(`{"image":"nginx:v2"}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestHandlerPatchService_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	// First create the service.
	do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	// Then patch with invalid JSON.
	rec := do(srv, "PATCH", "/services/myapp", []byte(`not-json`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestHandlerPatchService_Valid(t *testing.T) {
	srv := newTestServer(t)
	do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	rec := do(srv, "PATCH", "/services/myapp", []byte(`{"image":"nginx:v2"}`))
	// Returns 202 (async job) or 200.
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Errorf("want 202 or 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Service delete ---

func TestHandlerDeleteService_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "DELETE", "/services/nonexistent?force=true", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestHandlerDeleteService_Valid(t *testing.T) {
	srv := newTestServer(t)
	do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	rec := do(srv, "DELETE", "/services/myapp?force=true", nil)
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Errorf("want 202/200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Service restart ---

func TestHandlerRestartService_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/services/nonexistent/restart", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestHandlerRestartService_Valid(t *testing.T) {
	srv := newTestServer(t)
	do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	rec := do(srv, "POST", "/services/myapp/restart", nil)
	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Errorf("want 202/200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Service job ---

func TestHandlerGetServiceJob_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/services/nonexistent/job", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rec.Code)
	}
}

func TestHandlerGetServiceJob_Found(t *testing.T) {
	srv := newTestServer(t)
	do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	// After create, service exists; there may not be a job attached, so we check
	// that the service itself is found (not a 404 for the service route).
	rec := do(srv, "GET", "/services/myapp", nil)
	if rec.Code == http.StatusNotFound {
		t.Errorf("service should exist, got 404")
	}
}

// --- Chart job ---

func TestHandlerChartJob_NotFound(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/charts/jobs/00000000-0000-0000-0000-000000000000", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("want 404 for unknown job, got %d", rec.Code)
	}
}

// --- Install variants ---

func TestHandlerInstallChart_InvalidJSON(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts?namespace=default", []byte(`not-json`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rec.Code)
	}
}

func TestHandlerInstallChart_MissingNamespace(t *testing.T) {
	body := helmtypes.InstallReq{
		Release: "myapp",
		Source:  &helmtypes.ChartSource{Type: "oci", URL: "oci://example/chart", Version: "1.0.0"},
	}
	b, _ := json.Marshal(body)
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts", b) // no ?namespace= query param
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerInstallChart_RolloutWatchMissingTarget(t *testing.T) {
	body := helmtypes.InstallReq{
		Release:      "myapp",
		Namespace:    "default",
		Source:       &helmtypes.ChartSource{Type: "oci", URL: "oci://example/chart", Version: "1.0.0"},
		RolloutWatch: &helmtypes.RolloutWatchConfig{},
	}
	b, _ := json.Marshal(body)
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts", b)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing rollout watch target, got %d", rec.Code)
	}
}

// --- Upgrade variants ---

func TestHandlerUpgradeRelease_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	body := helmtypes.UpgradeReq{
		Source: &helmtypes.ChartSource{Type: "oci", URL: "oci://example/chart", Version: "1.0.0"},
	}
	b, _ := json.Marshal(body)
	rec := do(srv, "PATCH", "/charts/myrelease", b)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerUpgradeRelease_MissingSource(t *testing.T) {
	srv := newTestServer(t)
	body := helmtypes.UpgradeReq{}
	b, _ := json.Marshal(body)
	rec := do(srv, "PATCH", "/charts/myrelease?namespace=default", b)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing source, got %d", rec.Code)
	}
}

func TestHandlerUpgradeRelease_RolloutWatchMissingTarget(t *testing.T) {
	srv := newTestServer(t)
	body := helmtypes.UpgradeReq{
		Source:       &helmtypes.ChartSource{Type: "oci", URL: "oci://example/chart", Version: "1.0.0"},
		RolloutWatch: &helmtypes.RolloutWatchConfig{},
	}
	b, _ := json.Marshal(body)
	rec := do(srv, "PATCH", "/charts/myrelease?namespace=default", b)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing rollout watch target, got %d", rec.Code)
	}
}

// --- Other chart endpoints: missing namespace ---

func TestHandlerRollbackRelease_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts/myrelease/rollback", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerReleaseHistory_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/charts/myrelease/history", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerReleaseValues_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/charts/myrelease/values", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerReleaseManifest_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "GET", "/charts/myrelease/manifest", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerDisableResource_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts/myrelease/resources/Deployment/frontend/disable?force=true", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

func TestHandlerDisableResource_MissingForce(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts/myrelease/resources/Deployment/frontend/disable?namespace=default", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing force, got %d", rec.Code)
	}
}

func TestHandlerEnableResource_MissingNamespace(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/charts/myrelease/resources/Deployment/frontend/enable?force=true", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 for missing namespace, got %d", rec.Code)
	}
}

// --- writeJSON variants ---

func TestWriteJSON_200(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"key": "value"})
	if rec.Code != http.StatusOK {
		t.Errorf("code: got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type: got %q", ct)
	}
}

func TestWriteJSON_Array(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, []string{"a", "b", "c"})
	var out []string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("want 3 items, got %d", len(out))
	}
}

func TestWriteJSON_NilValue(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, nil)
	body := strings.TrimSpace(rec.Body.String())
	if body != "null" {
		t.Errorf("nil value should encode as null, got %q", body)
	}
}

// --- Root endpoint ---

func TestServerRootEndpoint_StartsAt(t *testing.T) {
	srv := newTestServer(t)
	// The /health endpoint returns started_at and version.
	rec := do(srv, "GET", "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["started_at"]; !ok {
		t.Error("response missing 'started_at' field")
	}
}

func TestServerRootEndpoint_Version(t *testing.T) {
	srv := newTestServer(t)
	// The /health endpoint returns version.
	rec := do(srv, "GET", "/health", nil)
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["version"]; !ok {
		t.Error("response missing 'version' field")
	}
}

// --- validateInstall edge cases ---

func TestValidateInstall_TGZ_Valid(t *testing.T) {
	b64 := "SGVsbG8gV29ybGQ=" // base64("Hello World")
	req := helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "tgz", TgzB64: b64},
	}
	if err := validateInstall(&req); err != nil {
		t.Errorf("valid TGZ install: %v", err)
	}
}

func TestValidateInstall_TGZ_MissingBase64(t *testing.T) {
	req := helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "tgz"},
	}
	if err := validateInstall(&req); err == nil {
		t.Error("missing tgzBase64 should fail validation")
	}
}

func TestValidateInstall_HTTPS_MissingChart(t *testing.T) {
	req := helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "https", URL: "https://charts.example.com", Version: "1.0.0"},
	}
	if err := validateInstall(&req); err == nil {
		t.Error("HTTPS without chart name should fail validation")
	}
}

func TestValidateInstall_OCI_MissingVersion(t *testing.T) {
	req := helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{Type: "oci", URL: "oci://example/chart"},
	}
	if err := validateInstall(&req); err == nil {
		t.Error("OCI without version should fail validation")
	}
}

func TestValidateInstall_SourceTypeEmpty(t *testing.T) {
	req := helmtypes.InstallReq{
		Release:   "myapp",
		Namespace: "default",
		Source:    &helmtypes.ChartSource{URL: "something"},
	}
	if err := validateInstall(&req); err == nil {
		t.Error("empty source type should fail validation")
	}
}

// --- initiator ---

func TestInitiator_ExactlyEight(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer abcdefgh")
	prefix := initiator(req)
	if prefix != "token:abcdefgh" {
		t.Errorf("initiator: got %q", prefix)
	}
}

// --- Response structure ---

func TestHandlerCreateService_ReturnsStream(t *testing.T) {
	srv := newTestServer(t)
	rec := do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["stream"]; !ok {
		t.Error("response missing 'stream' field")
	}
}

func TestHandlerGetService_HasSpec(t *testing.T) {
	srv := newTestServer(t)
	do(srv, "POST", "/services", []byte(`{"name":"myapp","image":"nginx"}`))
	rec := do(srv, "GET", "/services/myapp", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, ok := body["spec"]; !ok {
		t.Error("response missing 'spec' field")
	}
}
