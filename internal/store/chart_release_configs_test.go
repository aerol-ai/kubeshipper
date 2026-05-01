package store

import "testing"

func TestChartReleaseConfigLifecycle(t *testing.T) {
	st := newTestStore(t)

	record, err := st.UpsertChartReleaseConfig(
		"agent",
		"default",
		`{"type":"oci","url":"oci://ghcr.io/acme/agent","version":"1.2.3"}`,
		"oci",
		true,
		"1.2.3",
	)
	if err != nil {
		t.Fatalf("upsert chart release config: %v", err)
	}
	if record == nil {
		t.Fatal("expected record to be returned")
	}
	if record.SourceType != "oci" {
		t.Fatalf("source type: got %q", record.SourceType)
	}
	if !record.SourceAuthConfigured {
		t.Fatal("expected source auth flag to be persisted")
	}

	if err := st.SetChartReleaseMonitorEnabled("agent", "default", true); err != nil {
		t.Fatalf("enable monitor: %v", err)
	}
	if err := st.RecordChartReleaseCheck("agent", "default", ChartReleaseCheck{
		CurrentVersion: "1.2.3",
		LatestVersion:  "1.3.0",
		Result:         "updated",
		Applied:        true,
	}); err != nil {
		t.Fatalf("record chart release check: %v", err)
	}

	got, err := st.GetChartReleaseConfig("agent", "default")
	if err != nil {
		t.Fatalf("get chart release config: %v", err)
	}
	if got == nil {
		t.Fatal("expected stored chart release config")
	}
	if !got.MonitorEnabled {
		t.Fatal("expected monitor to be enabled")
	}
	if got.CurrentVersion != "1.2.3" {
		t.Fatalf("current version: got %q", got.CurrentVersion)
	}
	if got.LatestVersion != "1.3.0" {
		t.Fatalf("latest version: got %q", got.LatestVersion)
	}
	if got.CheckCount != 1 {
		t.Fatalf("check count: got %d", got.CheckCount)
	}
	if got.SyncCount != 1 {
		t.Fatalf("sync count: got %d", got.SyncCount)
	}
	if got.LastCheckedAt == nil {
		t.Fatal("expected last checked timestamp")
	}
	if got.LastSyncedAt == nil {
		t.Fatal("expected last synced timestamp")
	}
}

func TestDeleteChartReleaseConfig(t *testing.T) {
	st := newTestStore(t)

	if _, err := st.UpsertChartReleaseConfig(
		"agent",
		"default",
		`{"type":"oci","url":"oci://ghcr.io/acme/agent","version":"1.2.3"}`,
		"oci",
		false,
		"1.2.3",
	); err != nil {
		t.Fatalf("upsert chart release config: %v", err)
	}

	if err := st.DeleteChartReleaseConfig("agent", "default"); err != nil {
		t.Fatalf("delete chart release config: %v", err)
	}
	got, err := st.GetChartReleaseConfig("agent", "default")
	if err != nil {
		t.Fatalf("get chart release config after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected config to be deleted")
	}
}
