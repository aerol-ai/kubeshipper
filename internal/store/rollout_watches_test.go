package store

import "testing"

const (
	oldDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	newDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestUpsertRolloutWatch_CreateAndGet(t *testing.T) {
	st := newTestStore(t)
	watch, created, err := st.UpsertRolloutWatch(
		"default",
		"agent-gateway",
		"",
		"ghcr.io/acme/agent:latest",
		"ghcr.io/acme/agent:latest",
		oldDigest,
	)
	if err != nil {
		t.Fatalf("upsert rollout watch: %v", err)
	}
	if !created {
		t.Fatal("expected first upsert to create a rollout watch")
	}
	got, err := st.GetRolloutWatch(watch.ID)
	if err != nil {
		t.Fatalf("get rollout watch: %v", err)
	}
	if got == nil {
		t.Fatal("expected rollout watch to be persisted")
	}
	if got.Namespace != "default" || got.Deployment != "agent-gateway" {
		t.Fatalf("unexpected rollout watch target: %#v", got)
	}
	if got.TrackedImage != "ghcr.io/acme/agent:latest" {
		t.Fatalf("tracked image: got %q", got.TrackedImage)
	}
	if got.CurrentDigest != oldDigest {
		t.Fatalf("current digest: got %q", got.CurrentDigest)
	}
}

func TestRecordRolloutWatchCheck_UpdatesStateAndTimeline(t *testing.T) {
	st := newTestStore(t)
	watch, _, err := st.UpsertRolloutWatch(
		"default",
		"agent-gateway",
		"app",
		"ghcr.io/acme/agent:latest",
		"ghcr.io/acme/agent:latest",
		oldDigest,
	)
	if err != nil {
		t.Fatalf("upsert rollout watch: %v", err)
	}

	err = st.RecordRolloutWatchCheck(watch.ID, RolloutWatchCheck{
		TrackedImage:  "ghcr.io/acme/agent:latest",
		Result:        "updated",
		Message:       "patched deployment image",
		CurrentImage:  "ghcr.io/acme/agent:latest",
		CurrentDigest: oldDigest,
		LatestImage:   "ghcr.io/acme/agent:latest@" + newDigest,
		LatestDigest:  newDigest,
		Applied:       true,
		RecordEvent:   true,
		EventType:     "updated",
	})
	if err != nil {
		t.Fatalf("record rollout watch check: %v", err)
	}

	got, err := st.GetRolloutWatch(watch.ID)
	if err != nil {
		t.Fatalf("get rollout watch: %v", err)
	}
	if got.CheckCount != 1 {
		t.Fatalf("check_count: got %d, want 1", got.CheckCount)
	}
	if got.SyncCount != 1 {
		t.Fatalf("sync_count: got %d, want 1", got.SyncCount)
	}
	if got.LastCheckedAt == nil {
		t.Fatal("expected last_checked_at to be set")
	}
	if got.LastSyncedAt == nil {
		t.Fatal("expected last_synced_at to be set")
	}
	if got.LatestDigest != newDigest {
		t.Fatalf("latest digest: got %q", got.LatestDigest)
	}
	if len(got.Timeline) != 1 {
		t.Fatalf("timeline length: got %d, want 1", len(got.Timeline))
	}
	if got.Timeline[0].Type != "updated" {
		t.Fatalf("timeline event type: got %q", got.Timeline[0].Type)
	}
}
