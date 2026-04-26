package helm

import (
	"context"
	"fmt"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm/source"

	"helm.sh/helm/v3/pkg/action"
)

func (m *Manager) Upgrade(ctx context.Context, release, namespace string, req *UpgradeReq, emit EmitFn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.runUpgradeLocked(ctx, release, namespace, req, emit)
}

// runUpgradeLocked is also called by DisableResource/EnableResource which
// already hold the mutex. The lock is taken in the public Upgrade entry.
func (m *Manager) runUpgradeLocked(ctx context.Context, release, namespace string, req *UpgradeReq, emit EmitFn) error {
	emitFn(emit, "validation", "starting upgrade")

	cfg, err := m.actionConfig(namespace)
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("action config: %v", err))
		return err
	}

	emitFn(emit, "validation", "fetching chart")
	ch, err := source.Fetch(toSourceReq(req.Source))
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("fetch chart: %v", err))
		return err
	}

	valuesYAML, _ := valuesToYAML(req.Values)
	values, err := parseValuesYAML(valuesYAML)
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("parse values: %v", err))
		return err
	}

	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = namespace
	upgrade.Atomic = boolDefault(req.Atomic, true)
	upgrade.Wait = boolDefault(req.Wait, true)
	upgrade.Timeout = timeoutOrDefault(req.TimeoutSeconds, 10*time.Minute)
	upgrade.ReuseValues = req.ReuseValues
	upgrade.ResetValues = req.ResetValues

	if pr, err := m.postRendererFor(release, namespace); err != nil {
		emitErrFn(emit, fmt.Sprintf("post-renderer: %v", err))
		return err
	} else if pr != nil {
		upgrade.PostRenderer = pr
	}

	emitFn(emit, "apply", fmt.Sprintf("upgrading %s/%s (chart=%s version=%s)",
		namespace, release, ch.Name(), ch.Metadata.Version))

	rel, err := upgrade.RunWithContext(ctx, release, ch, values)
	if err != nil {
		emitErrFn(emit, fmt.Sprintf("upgrade: %v", err))
		return err
	}

	emitFn(emit, "done", fmt.Sprintf("revision=%d status=%s", rel.Version, rel.Info.Status))
	return nil
}
