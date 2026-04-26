package source

import (
	"fmt"
	"os"
	"path/filepath"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

func fetchHTTPS(s *Req) (*chart.Chart, error) {
	if s.RepoURL == "" || s.Chart == "" {
		return nil, fmt.Errorf("https.repoUrl and https.chart required")
	}

	settings := cli.New()
	tmp, err := os.MkdirTemp("", "kubeshipper-https-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	entry := &repo.Entry{Name: "tmp-" + s.Chart, URL: s.RepoURL}
	if s.Auth != nil {
		entry.Username = s.Auth.Username
		entry.Password = s.Auth.Password
	}

	r, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		return nil, fmt.Errorf("repo: %w", err)
	}
	r.CachePath = tmp
	if _, err := r.DownloadIndexFile(); err != nil {
		return nil, fmt.Errorf("download index: %w", err)
	}

	dl := downloader.ChartDownloader{
		Out:     os.Stderr,
		Getters: getter.All(settings),
	}
	if s.Auth != nil {
		dl.Options = append(dl.Options, getter.WithBasicAuth(s.Auth.Username, s.Auth.Password))
	}

	chartRef := s.RepoURL + "/" + s.Chart
	tgz, _, err := dl.DownloadTo(chartRef, s.Version, tmp)
	if err != nil {
		return nil, fmt.Errorf("download chart: %w", err)
	}
	c, err := loader.Load(tgz)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	_ = os.Remove(filepath.Clean(tgz))
	return c, nil
}
