package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
)

func fetchOCI(s *Req) (*chart.Chart, error) {
	if s.URL == "" {
		return nil, fmt.Errorf("oci.url required")
	}
	if !strings.HasPrefix(s.URL, "oci://") {
		return nil, fmt.Errorf("oci.url must start with oci://")
	}

	settings := cli.New()
	regClient, err := registry.NewClient(
		registry.ClientOptDebug(false),
		registry.ClientOptCredentialsFile(filepath.Join(os.TempDir(), "kubeshipper-registry-creds.json")),
	)
	if err != nil {
		return nil, fmt.Errorf("registry client: %w", err)
	}

	if s.Auth != nil && s.Auth.Username != "" {
		host := strings.TrimPrefix(s.URL, "oci://")
		host = strings.SplitN(host, "/", 2)[0]
		if err := regClient.Login(host, registry.LoginOptBasicAuth(s.Auth.Username, s.Auth.Password)); err != nil {
			return nil, fmt.Errorf("oci login: %w", err)
		}
		defer func() { _ = regClient.Logout(host) }()
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
