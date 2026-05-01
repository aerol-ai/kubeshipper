package api

import (
	"testing"

	"github.com/aerol-ai/kubeshipper/internal/helm"
)

func TestEncodeStoredChartSourcePreservesAuth(t *testing.T) {
	raw, authConfigured, err := encodeStoredChartSource(&helm.ChartSource{
		Type:    "oci",
		URL:     "oci://ghcr.io/acme/platform",
		Version: "1.2.3",
		TgzB64:  "ignored",
		Auth: &helm.Auth{
			Username: "octocat",
			Token:    "ghp_token",
		},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !authConfigured {
		t.Fatal("expected authConfigured to be true")
	}

	source, err := decodeStoredChartSource(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if source.Auth == nil {
		t.Fatal("expected stored auth to be preserved")
	}
	if source.Auth.Username != "octocat" {
		t.Fatalf("username: got %q", source.Auth.Username)
	}
	if source.Auth.Token != "ghp_token" {
		t.Fatalf("token: got %q", source.Auth.Token)
	}
	if source.TgzB64 != "" {
		t.Fatalf("tgz should be stripped, got %q", source.TgzB64)
	}
}

func TestPersistChartReleaseSourcePreservesStoredAuth(t *testing.T) {
	st := newAPITestStore(t)
	srv := NewServer(Deps{Store: st})

	if _, err := srv.persistChartReleaseSource("aerol-stack", "default", &helm.ChartSource{
		Type:    "oci",
		URL:     "oci://ghcr.io/penify-dev/aerol-stack",
		Version: "0.1.15",
		Auth: &helm.Auth{
			Username: "octocat",
			Token:    "ghp_old",
		},
	}); err != nil {
		t.Fatalf("persist initial source: %v", err)
	}

	if _, err := srv.persistChartReleaseSource("aerol-stack", "default", &helm.ChartSource{
		Type:    "oci",
		URL:     "oci://ghcr.io/penify-dev/aerol-stack",
		Version: "0.1.16",
	}); err != nil {
		t.Fatalf("persist version-only source: %v", err)
	}

	if _, err := srv.persistChartReleaseSource("aerol-stack", "default", &helm.ChartSource{
		Type:    "oci",
		URL:     "oci://ghcr.io/penify-dev/aerol-stack",
		Version: "0.1.17",
		Auth: &helm.Auth{
			Token: "ghp_new",
		},
	}); err != nil {
		t.Fatalf("persist updated token source: %v", err)
	}

	record, err := st.GetChartReleaseConfig("aerol-stack", "default")
	if err != nil {
		t.Fatalf("get stored config: %v", err)
	}
	source, err := decodeStoredChartSource(record.SourceJSON)
	if err != nil {
		t.Fatalf("decode stored source: %v", err)
	}
	if source.Auth == nil {
		t.Fatal("expected stored auth")
	}
	if source.Auth.Username != "octocat" {
		t.Fatalf("username: got %q", source.Auth.Username)
	}
	if source.Auth.Token != "ghp_new" {
		t.Fatalf("token: got %q", source.Auth.Token)
	}
	if source.Auth.Password != "" {
		t.Fatalf("password should be cleared when token replaces it, got %q", source.Auth.Password)
	}
}
