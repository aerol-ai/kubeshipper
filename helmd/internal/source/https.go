package source

import (
	"fmt"
	"os"
	"path/filepath"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

func fetchHTTPS(s *pb.HTTPSSource) (*chart.Chart, error) {
	if s == nil || s.RepoUrl == "" || s.Chart == "" {
		return nil, fmt.Errorf("https.repo_url and https.chart required")
	}

	settings := cli.New()
	tmp, err := os.MkdirTemp("", "helmd-https-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	// Configure repo entry (anonymous or basic-auth).
	entry := &repo.Entry{
		Name: "tmp-" + s.Chart,
		URL:  s.RepoUrl,
	}
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
		Options: []getter.Option{},
	}
	if s.Auth != nil {
		dl.Options = append(dl.Options, getter.WithBasicAuth(s.Auth.Username, s.Auth.Password))
	}

	chartRef := s.RepoUrl + "/" + s.Chart
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
