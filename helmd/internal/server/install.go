package server

import (
	"context"
	"fmt"
	"time"

	pb "kubeshipper/helmd/gen"
	"kubeshipper/helmd/internal/source"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"sigs.k8s.io/yaml"
)

func (s *Server) Install(req *pb.InstallRequest, stream pb.Helmd_InstallServer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	emit := newEmitter(stream)
	emit("validation", "starting install")

	if req.Release == "" || req.Namespace == "" {
		return emitError(stream, "release and namespace are required")
	}

	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return emitError(stream, fmt.Sprintf("action config: %v", err))
	}

	emit("validation", "fetching chart")
	ch, err := source.Fetch(req.Source)
	if err != nil {
		return emitError(stream, fmt.Sprintf("fetch chart: %v", err))
	}

	if len(req.PrereqSecrets) > 0 {
		emit("prereqs", fmt.Sprintf("provisioning %d prerequisite secret(s)", len(req.PrereqSecrets)))
		if err := s.applyPrereqSecrets(stream.Context(), req.PrereqSecrets); err != nil {
			return emitError(stream, fmt.Sprintf("prereq secrets: %v", err))
		}
	}

	values, err := parseValues(req.ValuesYaml)
	if err != nil {
		return emitError(stream, fmt.Sprintf("parse values: %v", err))
	}

	install := action.NewInstall(cfg)
	install.ReleaseName = req.Release
	install.Namespace = req.Namespace
	install.Atomic = req.Atomic
	install.Wait = req.Wait
	install.CreateNamespace = req.CreateNamespace
	install.Timeout = timeout(req.TimeoutSeconds, 10*time.Minute)

	emit("apply", fmt.Sprintf("installing %s/%s (chart=%s version=%s)",
		req.Namespace, req.Release, ch.Name(), ch.Metadata.Version))

	rel, err := install.RunWithContext(stream.Context(), ch, values)
	if err != nil {
		return emitError(stream, fmt.Sprintf("install: %v", err))
	}

	emit("done", fmt.Sprintf("revision=%d status=%s", rel.Version, rel.Info.Status))
	return nil
}

func parseValues(yamlStr string) (map[string]interface{}, error) {
	if yamlStr == "" {
		return map[string]interface{}{}, nil
	}
	out := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(yamlStr), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func timeout(seconds int64, dflt time.Duration) time.Duration {
	if seconds <= 0 {
		return dflt
	}
	return time.Duration(seconds) * time.Second
}

// chartName is a convenience for getting the chart name from a *chart.Chart.
func chartName(c *chart.Chart) string {
	if c == nil || c.Metadata == nil {
		return ""
	}
	return c.Metadata.Name
}

var _ = context.Background // keep context import
