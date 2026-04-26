package source

import (
	"fmt"
	"os"

	pb "kubeshipper/helmd/gen"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

func fetchTgz(s *pb.TgzSource) (*chart.Chart, error) {
	if s == nil || len(s.TgzBytes) == 0 {
		return nil, fmt.Errorf("tgz.tgz_bytes required")
	}

	tmp, err := os.CreateTemp("", "helmd-upload-*.tgz")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(s.TgzBytes); err != nil {
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}

	c, err := loader.Load(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("load tgz: %w", err)
	}
	return c, nil
}
