package ociregistry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/registry"
)

type Auth struct {
	Username string
	Password string
	Token    string
}

func NewClient() (*registry.Client, error) {
	return registry.NewClient(
		registry.ClientOptDebug(false),
		registry.ClientOptCredentialsFile(filepath.Join(os.TempDir(), "kubeshipper-registry-creds.json")),
	)
}

func LoginIfConfigured(regClient *registry.Client, rawURL string, auth *Auth) (func(), error) {
	username, secret, ok, err := BasicAuth(auth)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	host := RegistryHost(rawURL)
	if host == "" {
		return nil, fmt.Errorf("oci url required")
	}
	if err := regClient.Login(host, registry.LoginOptBasicAuth(username, secret)); err != nil {
		return nil, err
	}
	return func() { _ = regClient.Logout(host) }, nil
}

func BasicAuth(auth *Auth) (string, string, bool, error) {
	if auth == nil {
		return "", "", false, nil
	}

	username := strings.TrimSpace(auth.Username)
	password := strings.TrimSpace(auth.Password)
	token := strings.TrimSpace(auth.Token)
	hasSecret := password != "" || token != ""

	if username == "" && !hasSecret {
		return "", "", false, nil
	}
	if username == "" {
		return "", "", false, fmt.Errorf("oci auth.username required when password or token is set")
	}
	if !hasSecret {
		return "", "", false, fmt.Errorf("oci auth.password or auth.token required when username is set")
	}
	if password != "" {
		return username, password, true, nil
	}
	return username, token, true, nil
}

func RegistryHost(rawURL string) string {
	host := strings.TrimSpace(rawURL)
	host = strings.TrimPrefix(host, "oci://")
	if idx := strings.Index(host, "/"); idx >= 0 {
		host = host[:idx]
	}
	return host
}
