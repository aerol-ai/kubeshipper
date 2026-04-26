package helm

import (
	"context"
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"sigs.k8s.io/yaml"
)

func (m *Manager) List(ctx context.Context, namespace string, all bool) ([]ReleaseInfo, error) {
	cfg, err := m.actionConfig(namespace)
	if err != nil {
		return nil, err
	}
	list := action.NewList(cfg)
	list.AllNamespaces = namespace == ""
	list.All = all
	list.SetStateMask()

	rels, err := list.Run()
	if err != nil {
		return nil, err
	}
	out := make([]ReleaseInfo, 0, len(rels))
	for _, r := range rels {
		out = append(out, toReleaseInfo(r))
	}
	return out, nil
}

func (m *Manager) Get(ctx context.Context, release, namespace string) (*GetResp, error) {
	cfg, err := m.actionConfig(namespace)
	if err != nil {
		return nil, err
	}
	get := action.NewGet(cfg)
	rel, err := get.Run(release)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}

	valuesYAML, _ := yaml.Marshal(rel.Config)

	disabled := []DisabledResourceFromStore{}
	rows, _ := m.store.ListDisabled(release, namespace)
	for _, r := range rows {
		disabled = append(disabled, DisabledResourceFromStore{
			Kind: r.Kind, Name: r.Name, Namespace: r.ResourceNs,
		})
	}

	info := toReleaseInfo(rel)
	return &GetResp{
		Release:    &info,
		Manifest:   rel.Manifest,
		ValuesYAML: string(valuesYAML),
		Disabled:   disabled,
	}, nil
}

func (m *Manager) History(ctx context.Context, release, namespace string, max int) ([]HistoryEntry, error) {
	cfg, err := m.actionConfig(namespace)
	if err != nil {
		return nil, err
	}
	hist := action.NewHistory(cfg)
	if max <= 0 {
		max = 20
	}
	hist.Max = max
	rels, err := hist.Run(release)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(rels))
	for _, r := range rels {
		entry := HistoryEntry{
			Revision:    r.Version,
			Status:      string(r.Info.Status),
			UpdatedAt:   r.Info.LastDeployed.Format("2006-01-02T15:04:05Z07:00"),
			Description: r.Info.Description,
		}
		if r.Chart != nil && r.Chart.Metadata != nil {
			entry.Chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
			entry.AppVersion = r.Chart.Metadata.AppVersion
		}
		out = append(out, entry)
	}
	return out, nil
}

func (m *Manager) Rollback(ctx context.Context, release, namespace string, revision int, wait bool, timeoutSec int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := m.actionConfig(namespace)
	if err != nil {
		return 0, err
	}
	rb := action.NewRollback(cfg)
	rb.Version = revision
	rb.Wait = wait
	rb.Timeout = timeoutOrDefault(timeoutSec, 5*60_000_000_000) // fallback to 5min if seconds<=0
	if err := rb.Run(release); err != nil {
		return 0, fmt.Errorf("rollback: %w", err)
	}
	rel, err := action.NewGet(cfg).Run(release)
	if err != nil {
		return 0, err
	}
	return rel.Version, nil
}

func toReleaseInfo(r *release.Release) ReleaseInfo {
	chart := ""
	app := ""
	if r.Chart != nil && r.Chart.Metadata != nil {
		chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
		app = r.Chart.Metadata.AppVersion
	}
	return ReleaseInfo{
		Name:       r.Name,
		Namespace:  r.Namespace,
		Revision:   r.Version,
		Status:     string(r.Info.Status),
		Chart:      chart,
		AppVersion: app,
		UpdatedAt:  r.Info.LastDeployed.Format("2006-01-02T15:04:05Z07:00"),
	}
}
