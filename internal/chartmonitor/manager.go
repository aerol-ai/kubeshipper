package chartmonitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/ociregistry"
	"github.com/aerol-ai/kubeshipper/internal/store"
)

var ErrConfigNotFound = errors.New("chart release config not found")

type Manager struct {
	store *store.Store
	helm  *helm.Manager
}

type ActionResult struct {
	Applied         bool                      `json:"applied"`
	UpdateAvailable bool                      `json:"update_available"`
	Result          string                    `json:"result"`
	Message         string                    `json:"message,omitempty"`
	State           *store.ChartReleaseConfig `json:"state,omitempty"`
}

type MonitorMutationResult struct {
	Message string                    `json:"message,omitempty"`
	State   *store.ChartReleaseConfig `json:"state,omitempty"`
}

func NewManager(st *store.Store, helmMgr *helm.Manager) *Manager {
	return &Manager{store: st, helm: helmMgr}
}

func (m *Manager) Check(ctx context.Context, release, namespace string) (*ActionResult, error) {
	return m.reconcile(ctx, release, namespace, false, nil)
}

func (m *Manager) Sync(ctx context.Context, release, namespace string, emit func(store.Event)) (*ActionResult, error) {
	return m.reconcile(ctx, release, namespace, true, emit)
}

func (m *Manager) SetEnabled(ctx context.Context, release, namespace string, enabled bool) (*MonitorMutationResult, error) {
	record, err := m.store.GetChartReleaseConfig(release, namespace)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrConfigNotFound
	}
	if record.SourceType != "oci" {
		return nil, fmt.Errorf("chart monitor supports OCI sources only")
	}
	if err := m.store.SetChartReleaseMonitorEnabled(release, namespace, enabled); err != nil {
		return nil, err
	}
	updated, err := m.store.GetChartReleaseConfig(release, namespace)
	if err != nil {
		return nil, err
	}
	message := fmt.Sprintf("chart monitor disabled for %s/%s", namespace, release)
	if enabled {
		message = fmt.Sprintf("chart monitor enabled for %s/%s", namespace, release)
	}
	return &MonitorMutationResult{Message: message, State: updated}, nil
}

func (m *Manager) SyncAll(ctx context.Context) {
	rows, err := m.store.ListChartReleaseConfigs()
	if err != nil {
		return
	}
	for _, row := range rows {
		if !row.MonitorEnabled {
			continue
		}
		_, _ = m.Sync(ctx, row.Release, row.Namespace, nil)
	}
}

func (m *Manager) reconcile(ctx context.Context, release, namespace string, apply bool, emit func(store.Event)) (*ActionResult, error) {
	record, err := m.store.GetChartReleaseConfig(release, namespace)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrConfigNotFound
	}

	source, err := decodeSource(record.SourceJSON)
	if err != nil {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: record.CurrentVersion,
			Result:         "error",
			Error:          "invalid stored chart source metadata",
		})
		return nil, fmt.Errorf("decode chart source: %w", err)
	}

	currentVersion, err := m.currentVersion(ctx, release, namespace, source, record)
	if err != nil {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: record.CurrentVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}

	if source.Type != "oci" {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			Result:         "unsupported",
		})
		updated, _ := m.store.GetChartReleaseConfig(release, namespace)
		return &ActionResult{
			Result:  "unsupported",
			Message: "chart monitor supports OCI sources only",
			State:   updated,
		}, nil
	}
	if !strings.HasPrefix(source.URL, "oci://") {
		err := fmt.Errorf("stored OCI source must start with oci://")
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}

	latestVersion, err := latestOCIVersion(source)
	if err != nil {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}

	if latestVersion == "" {
		err := fmt.Errorf("registry returned no semver tags for %s", source.URL)
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}

	updateAvailable := currentVersion != latestVersion
	if !updateAvailable {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Result:         "up_to_date",
		})
		updated, _ := m.store.GetChartReleaseConfig(release, namespace)
		return &ActionResult{
			Result:  "up_to_date",
			Message: fmt.Sprintf("chart already runs %s", latestVersion),
			State:   updated,
		}, nil
	}

	if !apply {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Result:         "update_available",
		})
		updated, _ := m.store.GetChartReleaseConfig(release, namespace)
		return &ActionResult{
			UpdateAvailable: true,
			Result:          "update_available",
			Message:         fmt.Sprintf("chart update available: %s -> %s", currentVersion, latestVersion),
			State:           updated,
		}, nil
	}

	emitEvent(emit, "validation", fmt.Sprintf("checking OCI chart tags for %s", source.URL))
	emitEvent(emit, "validation", fmt.Sprintf("upgrading chart from %s to %s", currentVersion, latestVersion))

	nextSource := cloneSource(source)
	nextSource.Version = latestVersion
	req := &helm.UpgradeReq{
		Source:         nextSource,
		Atomic:         ptrTrue(),
		Wait:           ptrTrue(),
		ReuseValues:    true,
		TimeoutSeconds: 600,
	}
	if err := m.helm.Upgrade(ctx, release, namespace, req, emit); err != nil {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}

	raw, authConfigured, err := encodeStoredSource(nextSource, record.SourceAuthConfigured)
	if err != nil {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}
	if _, err := m.store.UpsertChartReleaseConfig(release, namespace, raw, nextSource.Type, authConfigured, latestVersion); err != nil {
		_ = m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
			Result:         "error",
			Error:          err.Error(),
		})
		return nil, err
	}
	if err := m.store.RecordChartReleaseCheck(release, namespace, store.ChartReleaseCheck{
		CurrentVersion: latestVersion,
		LatestVersion:  latestVersion,
		Result:         "updated",
		Applied:        true,
	}); err != nil {
		return nil, err
	}

	updated, err := m.store.GetChartReleaseConfig(release, namespace)
	if err != nil {
		return nil, err
	}
	return &ActionResult{
		Applied: true,
		Result:  "updated",
		Message: fmt.Sprintf("chart upgraded from %s to %s", currentVersion, latestVersion),
		State:   updated,
	}, nil
}

func (m *Manager) currentVersion(ctx context.Context, release, namespace string, source *helm.ChartSource, record *store.ChartReleaseConfig) (string, error) {
	if m.helm != nil {
		resp, err := m.helm.Get(ctx, release, namespace)
		if err == nil && resp != nil && resp.Release != nil && resp.Release.ChartVersion != "" {
			return resp.Release.ChartVersion, nil
		}
	}
	if record != nil && record.CurrentVersion != "" {
		return record.CurrentVersion, nil
	}
	if source != nil && source.Version != "" {
		return source.Version, nil
	}
	return "", fmt.Errorf("current chart version is unavailable")
}

func latestOCIVersion(source *helm.ChartSource) (string, error) {
	regClient, err := ociregistry.NewClient()
	if err != nil {
		return "", fmt.Errorf("registry client: %w", err)
	}
	if logout, err := ociregistry.LoginIfConfigured(regClient, source.URL, &ociregistry.Auth{
		Username: chartSourceUsername(source),
		Password: chartSourcePassword(source),
		Token:    chartSourceToken(source),
	}); err != nil {
		return "", fmt.Errorf("oci login: %w", err)
	} else if logout != nil {
		defer logout()
	}
	tags, err := regClient.Tags(strings.TrimPrefix(source.URL, "oci://"))
	if err != nil {
		return "", fmt.Errorf("list OCI tags: %w", err)
	}
	if len(tags) == 0 {
		return "", nil
	}
	return tags[0], nil
}

func decodeSource(raw string) (*helm.ChartSource, error) {
	var source helm.ChartSource
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		return nil, err
	}
	return &source, nil
}

func encodeStoredSource(source *helm.ChartSource, priorAuthConfigured bool) (string, bool, error) {
	sanitized := cloneSource(source)
	authConfigured := priorAuthConfigured || hasAuth(source)
	sanitized.TgzB64 = ""
	body, err := json.Marshal(sanitized)
	if err != nil {
		return "", false, err
	}
	return string(body), authConfigured, nil
}

func cloneSource(source *helm.ChartSource) *helm.ChartSource {
	if source == nil {
		return nil
	}
	cloned := *source
	if source.Auth != nil {
		auth := *source.Auth
		cloned.Auth = &auth
	}
	return &cloned
}

func hasAuth(source *helm.ChartSource) bool {
	if source == nil || source.Auth == nil {
		return false
	}
	return source.Auth.Username != "" || source.Auth.Password != "" || source.Auth.Token != "" || source.Auth.SshKeyPem != ""
}

func chartSourceUsername(source *helm.ChartSource) string {
	if source == nil || source.Auth == nil {
		return ""
	}
	return source.Auth.Username
}

func chartSourcePassword(source *helm.ChartSource) string {
	if source == nil || source.Auth == nil {
		return ""
	}
	return source.Auth.Password
}

func chartSourceToken(source *helm.ChartSource) string {
	if source == nil || source.Auth == nil {
		return ""
	}
	return source.Auth.Token
}

func emitEvent(emit func(store.Event), phase, message string) {
	if emit == nil {
		return
	}
	emit(store.Event{Phase: phase, Message: message, TS: time.Now().UnixMilli()})
}

func ptrTrue() *bool {
	v := true
	return &v
}
