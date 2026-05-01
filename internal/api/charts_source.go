package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aerol-ai/kubeshipper/internal/chartmonitor"
	"github.com/aerol-ai/kubeshipper/internal/helm"
	"github.com/aerol-ai/kubeshipper/internal/store"

	"github.com/go-chi/chi/v5"
)

type saveChartSourceReq struct {
	Source         *helm.ChartSource `json:"source"`
	MonitorEnabled *bool             `json:"monitorEnabled,omitempty"`
}

func (s *Server) saveChartSource(w http.ResponseWriter, r *http.Request) {
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace query param required"})
		return
	}

	var req saveChartSourceReq
	if err := decodeBody(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON", "details": err.Error()})
		return
	}
	if req.Source == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source required"})
		return
	}

	if strings.TrimSpace(req.Source.Version) == "" {
		req.Source.Version = s.currentReleaseChartVersion(r.Context(), release, ns)
	}
	if err := validateStoredOCIChartSource(req.Source); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.MonitorEnabled != nil && *req.MonitorEnabled && req.Source.Type != "oci" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chart monitor supports OCI sources only"})
		return
	}

	record, err := s.persistChartReleaseSource(release, ns, req.Source)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.MonitorEnabled != nil {
		if err := s.deps.Store.SetChartReleaseMonitorEnabled(release, ns, *req.MonitorEnabled); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		record, err = s.deps.Store.GetChartReleaseConfig(release, ns)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source":  buildChartSourceSummary(record),
		"monitor": buildChartMonitorState(record),
	})
}

func (s *Server) checkChartMonitor(w http.ResponseWriter, r *http.Request) {
	if s.deps.ChartMonitor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chart monitor is unavailable"})
		return
	}
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.ChartMonitor.Check(r.Context(), release, ns)
	if err != nil {
		writeJSON(w, chartMonitorStatusCode(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"applied":         out.Applied,
		"updateAvailable": out.UpdateAvailable,
		"result":          out.Result,
		"message":         out.Message,
		"monitor":         buildChartMonitorState(out.State),
	})
}

func (s *Server) syncChartMonitor(w http.ResponseWriter, r *http.Request) {
	if s.deps.ChartMonitor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chart monitor is unavailable"})
		return
	}
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace query param required"})
		return
	}

	jobID, err := s.deps.Store.CreateJob(release, ns, "chart-sync", initiator(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	go s.runJob(jobID, "chart-sync", func(ctx context.Context, emit helm.EmitFn) error {
		out, err := s.deps.ChartMonitor.Sync(ctx, release, ns, emit)
		if err != nil {
			return err
		}
		if out != nil && out.Message != "" {
			emit(store.Event{Phase: "done", Message: out.Message, TS: time.Now().UnixMilli()})
		}
		return nil
	})

	writeJSON(w, http.StatusAccepted, map[string]string{
		"jobId":  jobID,
		"stream": "/api/charts/jobs/" + jobID + "/stream",
		"status": "pending",
	})
}

func (s *Server) enableChartMonitor(w http.ResponseWriter, r *http.Request) {
	s.mutateChartMonitorEnabled(w, r, true)
}

func (s *Server) disableChartMonitor(w http.ResponseWriter, r *http.Request) {
	s.mutateChartMonitorEnabled(w, r, false)
}

func (s *Server) mutateChartMonitorEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if s.deps.ChartMonitor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chart monitor is unavailable"})
		return
	}
	release := chi.URLParam(r, "release")
	ns, ok := mustQuery(r, "namespace")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "namespace query param required"})
		return
	}
	out, err := s.deps.ChartMonitor.SetEnabled(r.Context(), release, ns, enabled)
	if err != nil {
		writeJSON(w, chartMonitorStatusCode(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": out.Message,
		"monitor": buildChartMonitorState(out.State),
	})
}

func (s *Server) hydrateChartReleaseResponse(out *helm.GetResp, release, namespace string) error {
	record, err := s.deps.Store.GetChartReleaseConfig(release, namespace)
	if err != nil || record == nil {
		return err
	}
	out.Source = buildChartSourceSummary(record)
	out.Monitor = buildChartMonitorState(record)
	return nil
}

func (s *Server) persistChartReleaseSource(release, namespace string, source *helm.ChartSource) (*store.ChartReleaseConfig, error) {
	merged, err := s.mergeStoredChartReleaseSource(release, namespace, source)
	if err != nil {
		return nil, err
	}
	raw, authConfigured, err := encodeStoredChartSource(merged)
	if err != nil {
		return nil, err
	}
	return s.deps.Store.UpsertChartReleaseConfig(release, namespace, raw, merged.Type, authConfigured, merged.Version)
}

func (s *Server) syncStoredChartReleaseVersion(ctx context.Context, release, namespace string) error {
	record, err := s.deps.Store.GetChartReleaseConfig(release, namespace)
	if err != nil || record == nil {
		return err
	}
	source, err := decodeStoredChartSource(record.SourceJSON)
	if err != nil {
		return err
	}
	currentVersion := s.currentReleaseChartVersion(ctx, release, namespace)
	if currentVersion == "" {
		return nil
	}
	source.Version = currentVersion
	_, err = s.persistChartReleaseSource(release, namespace, source)
	return err
}

func (s *Server) currentReleaseChartVersion(ctx context.Context, release, namespace string) string {
	if s.deps.Helm == nil {
		return ""
	}
	out, err := s.deps.Helm.Get(ctx, release, namespace)
	if err != nil || out == nil || out.Release == nil {
		return ""
	}
	return out.Release.ChartVersion
}

func buildChartSourceSummary(record *store.ChartReleaseConfig) *helm.ChartSourceSummary {
	if record == nil {
		return nil
	}
	source, err := decodeStoredChartSource(record.SourceJSON)
	if err != nil || source == nil {
		return nil
	}
	return &helm.ChartSourceSummary{
		Type:           source.Type,
		URL:            source.URL,
		RepoURL:        source.RepoURL,
		Chart:          source.Chart,
		Version:        source.Version,
		Ref:            source.Ref,
		Path:           source.Path,
		AuthConfigured: record.SourceAuthConfigured,
	}
}

func buildChartMonitorState(record *store.ChartReleaseConfig) *helm.ChartMonitorState {
	if record == nil {
		return nil
	}
	state := &helm.ChartMonitorState{
		MonitorEnabled: record.MonitorEnabled,
		SourceType:     record.SourceType,
		AuthConfigured: record.SourceAuthConfigured,
		CurrentVersion: record.CurrentVersion,
		LatestVersion:  record.LatestVersion,
		LastResult:     record.LastResult,
		LastError:      record.LastError,
		CheckCount:     record.CheckCount,
		SyncCount:      record.SyncCount,
	}
	if record.LastCheckedAt != nil {
		state.LastCheckedAt = record.LastCheckedAt.Format(time.RFC3339)
	}
	if record.LastSyncedAt != nil {
		state.LastSyncedAt = record.LastSyncedAt.Format(time.RFC3339)
	}
	return state
}

func validateStoredOCIChartSource(source *helm.ChartSource) error {
	if source == nil {
		return fmt.Errorf("source required")
	}
	if source.Type != "oci" {
		return fmt.Errorf("release source attachment currently supports OCI charts only")
	}
	if strings.TrimSpace(source.URL) == "" {
		return fmt.Errorf("oci source requires url")
	}
	if !strings.HasPrefix(source.URL, "oci://") {
		return fmt.Errorf("oci source url must start with oci://")
	}
	if strings.TrimSpace(source.Version) == "" {
		return fmt.Errorf("oci source requires version")
	}
	return nil
}

func encodeStoredChartSource(source *helm.ChartSource) (string, bool, error) {
	cloned := *source
	cloned.Auth = cloneChartAuth(source.Auth)
	authConfigured := chartSourceAuthConfigured(&cloned)
	cloned.TgzB64 = ""
	body, err := json.Marshal(&cloned)
	if err != nil {
		return "", false, err
	}
	return string(body), authConfigured, nil
}

func decodeStoredChartSource(raw string) (*helm.ChartSource, error) {
	var source helm.ChartSource
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		return nil, err
	}
	return &source, nil
}

func (s *Server) mergeStoredChartReleaseSource(release, namespace string, source *helm.ChartSource) (*helm.ChartSource, error) {
	if source == nil {
		return nil, nil
	}
	merged := *source

	record, err := s.deps.Store.GetChartReleaseConfig(release, namespace)
	if err != nil || record == nil || record.SourceJSON == "" {
		if err != nil {
			return nil, err
		}
		merged.Auth = cloneChartAuth(source.Auth)
		return &merged, nil
	}

	prior, err := decodeStoredChartSource(record.SourceJSON)
	if err != nil || prior == nil {
		if err != nil {
			return nil, err
		}
		merged.Auth = cloneChartAuth(source.Auth)
		return &merged, nil
	}

	merged.Auth = mergeChartAuth(prior.Auth, source.Auth)
	return &merged, nil
}

func mergeChartAuth(prior, next *helm.Auth) *helm.Auth {
	if !chartAuthProvided(next) {
		return cloneChartAuth(prior)
	}

	merged := cloneChartAuth(prior)
	if merged == nil {
		merged = &helm.Auth{}
	}

	if next == nil {
		return merged
	}
	if strings.TrimSpace(next.Username) != "" {
		merged.Username = strings.TrimSpace(next.Username)
	}
	if strings.TrimSpace(next.Password) != "" {
		merged.Password = strings.TrimSpace(next.Password)
		merged.Token = ""
	}
	if strings.TrimSpace(next.Token) != "" {
		merged.Token = strings.TrimSpace(next.Token)
		merged.Password = ""
	}
	if strings.TrimSpace(next.SshKeyPem) != "" {
		merged.SshKeyPem = next.SshKeyPem
	}
	if !chartAuthProvided(merged) {
		return nil
	}
	return merged
}

func cloneChartAuth(auth *helm.Auth) *helm.Auth {
	if auth == nil {
		return nil
	}
	cloned := *auth
	return &cloned
}

func chartSourceAuthConfigured(source *helm.ChartSource) bool {
	return source != nil && chartAuthProvided(source.Auth)
}

func chartAuthProvided(auth *helm.Auth) bool {
	if auth == nil {
		return false
	}
	return strings.TrimSpace(auth.Username) != "" ||
		strings.TrimSpace(auth.Password) != "" ||
		strings.TrimSpace(auth.Token) != "" ||
		strings.TrimSpace(auth.SshKeyPem) != ""
}

func chartMonitorStatusCode(err error) int {
	if errors.Is(err, chartmonitor.ErrConfigNotFound) {
		return http.StatusNotFound
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "required"):
		return http.StatusBadRequest
	case strings.Contains(msg, "supports oci"):
		return http.StatusBadRequest
	case strings.Contains(msg, "unavailable"):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
