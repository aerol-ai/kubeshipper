package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aerol-ai/kubeshipper/internal/ociregistry"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

func fetchOCI(s *Req) (*chart.Chart, error) {
	if s.URL == "" {
		return nil, fmt.Errorf("oci.url required")
	}
	if !strings.HasPrefix(s.URL, "oci://") {
		return nil, fmt.Errorf("oci.url must start with oci://")
	}

	settings := cli.New()
	regClient, err := ociregistry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("registry client: %w", err)
	}

	if logout, err := ociregistry.LoginIfConfigured(regClient, s.URL, &ociregistry.Auth{
		Username: authUsername(s.Auth),
		Password: authPassword(s.Auth),
		Token:    authToken(s.Auth),
	}); err != nil {
		return nil, fmt.Errorf("oci login: %w", err)
	} else if logout != nil {
		defer logout()
	}

	cfg := &action.Configuration{RegistryClient: regClient}
	pull := action.NewPullWithOpts(action.WithConfig(cfg))
	pull.Settings = settings
	pull.Version = s.Version
	pull.DestDir = os.TempDir()
	pull.Untar = false

	if _, err := pull.Run(s.URL); err != nil {
		return nil, fmt.Errorf("oci pull: %w", err)
	}

	parts := strings.Split(strings.TrimPrefix(s.URL, "oci://"), "/")
	chartName := parts[len(parts)-1]
	tgzPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s.tgz", chartName, s.Version))

	c, err := loader.Load(tgzPath)
	if err != nil {
		return nil, fmt.Errorf("load chart %s: %w", tgzPath, err)
	}
	_ = os.Remove(tgzPath)
	return c, nil
}

func authUsername(auth *Auth) string {
	if auth == nil {
		return ""
	}
	return auth.Username
}

func authPassword(auth *Auth) string {
	if auth == nil {
		return ""
	}
	return auth.Password
}

func authToken(auth *Auth) string {
	if auth == nil {
		return ""
	}
	return auth.Token
}
