package helm

import (
	"testing"

	"github.com/aerol-ai/kubeshipper/internal/helm/source"
)

// ============================================================
// toSourceReq tests (15 tests)
// ============================================================

func TestToSourceReq_Nil(t *testing.T) {
	if toSourceReq(nil) != nil {
		t.Error("nil input should return nil")
	}
}

func TestToSourceReq_MinimalOCI(t *testing.T) {
	in := &ChartSource{Type: "oci", URL: "oci://example/chart", Version: "1.0.0"}
	r := toSourceReq(in)
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.Type != "oci" {
		t.Errorf("type: got %q, want oci", r.Type)
	}
	if r.URL != "oci://example/chart" {
		t.Errorf("url: got %q", r.URL)
	}
	if r.Version != "1.0.0" {
		t.Errorf("version: got %q", r.Version)
	}
}

func TestToSourceReq_WithAuth(t *testing.T) {
	in := &ChartSource{
		Type:    "oci",
		URL:     "oci://example/chart",
		Version: "1.0.0",
		Auth: &Auth{
			Username: "user",
			Password: "pass",
			Token:    "mytoken",
		},
	}
	r := toSourceReq(in)
	if r.Auth == nil {
		t.Fatal("expected non-nil Auth")
	}
	if r.Auth.Username != "user" {
		t.Errorf("username: got %q", r.Auth.Username)
	}
	if r.Auth.Password != "pass" {
		t.Errorf("password: got %q", r.Auth.Password)
	}
	if r.Auth.Token != "mytoken" {
		t.Errorf("token: got %q", r.Auth.Token)
	}
}

func TestToSourceReq_WithoutAuth(t *testing.T) {
	in := &ChartSource{Type: "oci", URL: "oci://example/chart", Version: "1.0.0"}
	r := toSourceReq(in)
	if r.Auth != nil {
		t.Error("expected nil Auth when ChartSource.Auth is nil")
	}
}

func TestToSourceReq_HTTPS(t *testing.T) {
	in := &ChartSource{
		Type:    "https",
		RepoURL: "https://charts.example.com",
		Chart:   "myapp",
		Version: "2.3.4",
	}
	r := toSourceReq(in)
	if r.RepoURL != "https://charts.example.com" {
		t.Errorf("repoURL: got %q", r.RepoURL)
	}
	if r.Chart != "myapp" {
		t.Errorf("chart: got %q", r.Chart)
	}
	if r.Version != "2.3.4" {
		t.Errorf("version: got %q", r.Version)
	}
}

func TestToSourceReq_Git(t *testing.T) {
	in := &ChartSource{
		Type:    "git",
		RepoURL: "https://github.com/org/repo",
		Ref:     "main",
		Path:    "charts/myapp",
	}
	r := toSourceReq(in)
	if r.RepoURL != "https://github.com/org/repo" {
		t.Errorf("repoURL: got %q", r.RepoURL)
	}
	if r.Ref != "main" {
		t.Errorf("ref: got %q", r.Ref)
	}
	if r.Path != "charts/myapp" {
		t.Errorf("path: got %q", r.Path)
	}
}

func TestToSourceReq_TGZ(t *testing.T) {
	b64 := "SGVsbG8gV29ybGQ="
	in := &ChartSource{Type: "tgz", TgzB64: b64}
	r := toSourceReq(in)
	if r.TgzB64 != b64 {
		t.Errorf("tgzB64: got %q", r.TgzB64)
	}
}

func TestToSourceReq_AllFields(t *testing.T) {
	in := &ChartSource{
		Type:    "oci",
		URL:     "oci://example/chart",
		RepoURL: "https://example.com",
		Chart:   "myapp",
		Version: "1.0.0",
		Ref:     "main",
		Path:    "charts/",
		TgzB64:  "base64str",
		Auth: &Auth{
			Username:  "u",
			Password:  "p",
			SshKeyPem: "---BEGIN---",
			Token:     "tok",
		},
	}
	r := toSourceReq(in)
	if r.Type != "oci" {
		t.Errorf("type: %q", r.Type)
	}
	if r.RepoURL != "https://example.com" {
		t.Errorf("repoURL: %q", r.RepoURL)
	}
	if r.Ref != "main" {
		t.Errorf("ref: %q", r.Ref)
	}
	if r.Path != "charts/" {
		t.Errorf("path: %q", r.Path)
	}
	if r.TgzB64 != "base64str" {
		t.Errorf("tgzB64: %q", r.TgzB64)
	}
	if r.Auth.SshKeyPem != "---BEGIN---" {
		t.Errorf("sshKeyPem: %q", r.Auth.SshKeyPem)
	}
}

func TestToSourceReq_Auth_SshKeyPem(t *testing.T) {
	in := &ChartSource{
		Type: "git",
		Auth: &Auth{SshKeyPem: "-----BEGIN RSA PRIVATE KEY-----"},
	}
	r := toSourceReq(in)
	if r.Auth == nil {
		t.Fatal("expected Auth")
	}
	if r.Auth.SshKeyPem != "-----BEGIN RSA PRIVATE KEY-----" {
		t.Errorf("sshKeyPem: %q", r.Auth.SshKeyPem)
	}
}

func TestToSourceReq_TypePreserved(t *testing.T) {
	for _, typ := range []string{"oci", "https", "git", "tgz"} {
		in := &ChartSource{Type: typ}
		r := toSourceReq(in)
		if r.Type != typ {
			t.Errorf("type %q not preserved: got %q", typ, r.Type)
		}
	}
}

func TestToSourceReq_EmptySource(t *testing.T) {
	in := &ChartSource{}
	r := toSourceReq(in)
	if r == nil {
		t.Fatal("empty source should still return non-nil Req")
	}
	if r.Type != "" {
		t.Errorf("type should be empty: %q", r.Type)
	}
}

func TestToSourceReq_ReturnsSourceReqType(t *testing.T) {
	in := &ChartSource{Type: "oci", URL: "oci://x", Version: "1.0"}
	r := toSourceReq(in)
	var _ *source.Req = r // compile-time type assertion
	if r == nil {
		t.Fatal("expected *source.Req")
	}
}

func TestToSourceReq_VersionMapped(t *testing.T) {
	in := &ChartSource{Type: "https", Version: "3.2.1"}
	r := toSourceReq(in)
	if r.Version != "3.2.1" {
		t.Errorf("version: got %q", r.Version)
	}
}

func TestToSourceReq_ChartFieldMapped(t *testing.T) {
	in := &ChartSource{Type: "https", Chart: "nginx-ingress"}
	r := toSourceReq(in)
	if r.Chart != "nginx-ingress" {
		t.Errorf("chart: got %q", r.Chart)
	}
}

func TestToSourceReq_URLMapped(t *testing.T) {
	in := &ChartSource{Type: "oci", URL: "oci://ghcr.io/myorg/myapp"}
	r := toSourceReq(in)
	if r.URL != "oci://ghcr.io/myorg/myapp" {
		t.Errorf("url: got %q", r.URL)
	}
}
