package source

import (
	"encoding/base64"
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

func fetchTgz(s *Req) (*chart.Chart, error) {
	if s.TgzB64 == "" {
		return nil, fmt.Errorf("tgz.tgzBase64 required")
	}
	raw, err := base64.StdEncoding.DecodeString(s.TgzB64)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	tmp, err := os.CreateTemp("", "kubeshipper-upload-*.tgz")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(raw); err != nil {
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
