package server

import (
	"fmt"
	"time"

	pb "kubeshipper/helmd/gen"
	"kubeshipper/helmd/internal/source"

	"helm.sh/helm/v3/pkg/action"
)

func (s *Server) Upgrade(req *pb.UpgradeRequest, stream pb.Helmd_UpgradeServer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	emit := newEmitter(stream)
	emit("validation", "starting upgrade")

	cfg, err := s.actionConfig(req.Namespace)
	if err != nil {
		return emitError(stream, fmt.Sprintf("action config: %v", err))
	}

	emit("validation", "fetching chart")
	ch, err := source.Fetch(req.Source)
	if err != nil {
		return emitError(stream, fmt.Sprintf("fetch chart: %v", err))
	}

	values, err := parseValues(req.ValuesYaml)
	if err != nil {
		return emitError(stream, fmt.Sprintf("parse values: %v", err))
	}

	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = req.Namespace
	upgrade.Atomic = req.Atomic
	upgrade.Wait = req.Wait
	upgrade.Timeout = timeout(req.TimeoutSeconds, 10*time.Minute)
	upgrade.ReuseValues = req.ReuseValues
	upgrade.ResetValues = req.ResetValues

	// Inject post-renderer if there are disabled resources for this release.
	pr, err := s.postRendererFor(req.Release, req.Namespace)
	if err != nil {
		return emitError(stream, fmt.Sprintf("post-renderer: %v", err))
	}
	if pr != nil {
		upgrade.PostRenderer = pr
	}

	emit("apply", fmt.Sprintf("upgrading %s/%s to chart=%s version=%s",
		req.Namespace, req.Release, ch.Name(), ch.Metadata.Version))

	rel, err := upgrade.RunWithContext(stream.Context(), req.Release, ch, values)
	if err != nil {
		return emitError(stream, fmt.Sprintf("upgrade: %v", err))
	}

	emit("done", fmt.Sprintf("revision=%d status=%s", rel.Version, rel.Info.Status))
	return nil
}
