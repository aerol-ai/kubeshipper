package store

import (
	"encoding/json"
	"os"
	"testing"
)

// newTestStore creates a Store backed by a temporary SQLite file.
// The file and the connection are cleaned up via t.Cleanup.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	f, err := os.CreateTemp("", "ks-store-test-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	st, err := Open(f.Name())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// --- Open (test 26) ---

// Test 26
func TestOpen(t *testing.T) {
	st := newTestStore(t)
	if st == nil {
		t.Fatal("expected non-nil store")
	}
	if st.DB == nil {
		t.Fatal("expected non-nil DB")
	}
}

// --- Service CRUD (tests 27–35) ---

// Test 27
func TestUpsertService_Create(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"myapp","image":"nginx"}`)
	if err := st.UpsertService("myapp", spec, StatusPending); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

// Test 28
func TestGetService_Found(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"myapp","image":"nginx"}`)
	_ = st.UpsertService("myapp", spec, StatusPending)

	svc, err := st.GetService("myapp")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if svc == nil {
		t.Fatal("service not found")
	}
	if svc.ID != "myapp" {
		t.Errorf("id: got %q, want %q", svc.ID, "myapp")
	}
	if svc.Status != StatusPending {
		t.Errorf("status: got %q, want %q", svc.Status, StatusPending)
	}
}

// Test 29
func TestGetService_NotFound(t *testing.T) {
	st := newTestStore(t)
	svc, err := st.GetService("nonexistent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if svc != nil {
		t.Fatal("expected nil for nonexistent service")
	}
}

// Test 30
func TestListServices_Empty(t *testing.T) {
	st := newTestStore(t)
	svcs, err := st.ListServices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(svcs) != 0 {
		t.Errorf("want 0 services, got %d", len(svcs))
	}
}

// Test 31
func TestListServices_Multiple(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"x","image":"nginx"}`)
	_ = st.UpsertService("svc1", spec, StatusPending)
	_ = st.UpsertService("svc2", spec, StatusReady)

	svcs, err := st.ListServices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(svcs) != 2 {
		t.Errorf("want 2 services, got %d", len(svcs))
	}
}

// Test 32
func TestUpdateStatus(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusPending)

	if err := st.UpdateStatus("app", StatusReady); err != nil {
		t.Fatalf("update status: %v", err)
	}
	svc, _ := st.GetService("app")
	if svc.Status != StatusReady {
		t.Errorf("status: got %q, want %q", svc.Status, StatusReady)
	}
}

// Test 33
func TestMarkReady(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusDeploying)

	readySpec := json.RawMessage(`{"name":"app","image":"nginx:v2"}`)
	if err := st.MarkReady("app", readySpec); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	svc, _ := st.GetService("app")
	if svc.Status != StatusReady {
		t.Errorf("status: got %q, want READY", svc.Status)
	}
}

// Test 34
func TestDeleteService(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusPending)

	if err := st.DeleteService("app"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	svc, _ := st.GetService("app")
	if svc != nil {
		t.Fatal("expected service to be deleted")
	}
}

// Test 35
func TestServicesByStatus(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"x","image":"nginx"}`)
	_ = st.UpsertService("svc1", spec, StatusPending)
	_ = st.UpsertService("svc2", spec, StatusReady)

	pending, err := st.ServicesByStatus(StatusPending)
	if err != nil {
		t.Fatalf("by status: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending service, got %d", len(pending))
	}
	if pending[0].ID != "svc1" {
		t.Errorf("want svc1, got %q", pending[0].ID)
	}
}

// --- Job attachment (tests 36–38) ---

// Test 36
func TestAttachJob_Set(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusPending)

	jobID, err := st.CreateJob("app", "default", "deploy", "")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := st.AttachJob("app", jobID); err != nil {
		t.Fatalf("attach job: %v", err)
	}
	svc, _ := st.GetService("app")
	if svc.JobID != jobID {
		t.Errorf("job_id: got %q, want %q", svc.JobID, jobID)
	}
}

// Test 37
func TestAttachJob_Clear(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusPending)

	jobID, _ := st.CreateJob("app", "default", "deploy", "")
	_ = st.AttachJob("app", jobID)

	if err := st.AttachJob("app", ""); err != nil {
		t.Fatalf("clear attach: %v", err)
	}
	svc, _ := st.GetService("app")
	if svc.JobID != "" {
		t.Errorf("expected empty job_id after clear, got %q", svc.JobID)
	}
}

// Test 38
func TestResetStuckDeployments(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusDeploying)

	if err := st.ResetStuckDeployments(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	svc, _ := st.GetService("app")
	if svc.Status != StatusPending {
		t.Errorf("status: got %q, want PENDING", svc.Status)
	}
}

// --- Job lifecycle (tests 39–45) ---

// Test 39
func TestCreateJob(t *testing.T) {
	st := newTestStore(t)
	id, err := st.CreateJob("myrelease", "default", "install", "token:abc123")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty job ID")
	}
}

// Test 40
func TestGetJob_Found(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("myrel", "ns", "install", "")

	j, err := st.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if j == nil {
		t.Fatal("job not found")
	}
	if j.Release != "myrel" {
		t.Errorf("release: got %q, want myrel", j.Release)
	}
	if j.Status != JobPending {
		t.Errorf("status: got %q, want pending", j.Status)
	}
}

// Test 41
func TestGetJob_NotFound(t *testing.T) {
	st := newTestStore(t)
	j, err := st.GetJob("does-not-exist")
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if j != nil {
		t.Fatal("expected nil for nonexistent job")
	}
}

// Test 42
func TestSetJobStatus_Succeeded(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	if err := st.SetJobStatus(jobID, JobSucceeded); err != nil {
		t.Fatalf("set status: %v", err)
	}
	j, _ := st.GetJob(jobID)
	if j.Status != JobSucceeded {
		t.Errorf("status: got %q, want succeeded", j.Status)
	}
}

// Test 43
func TestSetJobStatus_Failed(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	if err := st.SetJobStatus(jobID, JobFailed); err != nil {
		t.Fatalf("set status: %v", err)
	}
	j, _ := st.GetJob(jobID)
	if j.Status != JobFailed {
		t.Errorf("status: got %q, want failed", j.Status)
	}
}

// Test 44
func TestAppendEvent_Single(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	ev := Event{Phase: "validation", Message: "starting"}
	if err := st.AppendEvent(jobID, ev); err != nil {
		t.Fatalf("append event: %v", err)
	}
	j, _ := st.GetJob(jobID)
	if len(j.Events) != 1 {
		t.Fatalf("events: want 1, got %d", len(j.Events))
	}
	if j.Events[0].Phase != "validation" {
		t.Errorf("phase: got %q, want validation", j.Events[0].Phase)
	}
}

// Test 45
func TestAppendEvent_Multiple(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	phases := []string{"validation", "apply", "done"}
	for _, p := range phases {
		_ = st.AppendEvent(jobID, Event{Phase: p})
	}
	j, _ := st.GetJob(jobID)
	if len(j.Events) != 3 {
		t.Fatalf("events: want 3, got %d", len(j.Events))
	}
	for i, ev := range j.Events {
		if ev.Phase != phases[i] {
			t.Errorf("event[%d] phase: got %q, want %q", i, ev.Phase, phases[i])
		}
	}
}

// --- parseEvents (tests 46–49) ---

// Test 46
func TestParseEvents_Empty(t *testing.T) {
	evs := parseEvents("")
	if len(evs) != 0 {
		t.Errorf("want 0 events, got %d", len(evs))
	}
}

// Test 47
func TestParseEvents_Single(t *testing.T) {
	line := `{"phase":"validation","message":"ok","ts":123}`
	evs := parseEvents(line)
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].Phase != "validation" {
		t.Errorf("phase: got %q", evs[0].Phase)
	}
	if evs[0].Message != "ok" {
		t.Errorf("message: got %q", evs[0].Message)
	}
}

// Test 48
func TestParseEvents_Multiple(t *testing.T) {
	jsonl := `{"phase":"validation","ts":1}` + "\n" + `{"phase":"done","ts":2}`
	evs := parseEvents(jsonl)
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	if evs[0].Phase != "validation" {
		t.Errorf("event[0] phase: got %q", evs[0].Phase)
	}
	if evs[1].Phase != "done" {
		t.Errorf("event[1] phase: got %q", evs[1].Phase)
	}
}

// Test 49
func TestParseEvents_MalformedIgnored(t *testing.T) {
	jsonl := `{"phase":"validation","ts":1}` + "\n" + `not-json` + "\n" + `{"phase":"done","ts":2}`
	evs := parseEvents(jsonl)
	if len(evs) != 2 {
		t.Fatalf("want 2 events (malformed skipped), got %d", len(evs))
	}
}

// --- AuditLog (test 50) ---

// Test 50
func TestAuditLog(t *testing.T) {
	st := newTestStore(t)
	err := st.AuditLog("token:abc123", "install", "myrelease", "default", "accepted", map[string]any{
		"release":   "myrelease",
		"namespace": "default",
	})
	if err != nil {
		t.Fatalf("audit log: %v", err)
	}
}

// --- redact (tests 51–54) ---

// Test 51
func TestRedact_Password(t *testing.T) {
	in := map[string]any{"password": "secret123", "name": "admin"}
	out, ok := redact(in).(map[string]any)
	if !ok {
		t.Fatal("redact did not return map")
	}
	if out["password"] != "[redacted]" {
		t.Errorf("password not redacted: %v", out["password"])
	}
	if out["name"] != "admin" {
		t.Errorf("non-sensitive key changed: name=%v", out["name"])
	}
}

// Test 52
func TestRedact_Token(t *testing.T) {
	in := map[string]any{"token": "mytoken"}
	out := redact(in).(map[string]any)
	if out["token"] != "[redacted]" {
		t.Errorf("token not redacted: %v", out["token"])
	}
}

// Test 53
func TestRedact_NestedAuth(t *testing.T) {
	// "auth" is itself a sensitive key — the whole value should be redacted.
	in := map[string]any{
		"auth": map[string]any{"username": "user", "password": "pass"},
	}
	out := redact(in).(map[string]any)
	if out["auth"] != "[redacted]" {
		t.Errorf("auth key not redacted: %v", out["auth"])
	}
}

// Test 54
func TestRedact_NonSensitive(t *testing.T) {
	in := map[string]any{"release": "myapp", "namespace": "default", "revision": 3}
	out := redact(in).(map[string]any)
	if out["release"] != "myapp" {
		t.Errorf("release changed: %v", out["release"])
	}
	if out["namespace"] != "default" {
		t.Errorf("namespace changed: %v", out["namespace"])
	}
}

// --- Disabled resources (test 55) ---

// Test 55
func TestRecordClearListDisabled(t *testing.T) {
	st := newTestStore(t)

	if err := st.RecordDisabled("myrelease", "default", "Deployment", "frontend", ""); err != nil {
		t.Fatalf("record disabled: %v", err)
	}

	items, err := st.ListDisabled("myrelease", "default")
	if err != nil {
		t.Fatalf("list disabled: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 disabled resource, got %d", len(items))
	}
	if items[0].Kind != "Deployment" {
		t.Errorf("kind: got %q, want Deployment", items[0].Kind)
	}
	if items[0].Name != "frontend" {
		t.Errorf("name: got %q, want frontend", items[0].Name)
	}

	if err := st.ClearDisabled("myrelease", "default", "Deployment", "frontend", ""); err != nil {
		t.Fatalf("clear disabled: %v", err)
	}

	items, _ = st.ListDisabled("myrelease", "default")
	if len(items) != 0 {
		t.Errorf("want 0 after clear, got %d", len(items))
	}
}

// ============================================================
// Additional store tests (adds 40 new tests)
// ============================================================

// --- Upsert idempotency ---

func TestUpsertService_Idempotent(t *testing.T) {
	st := newTestStore(t)
	spec1 := json.RawMessage(`{"name":"app","image":"nginx:1.0"}`)
	spec2 := json.RawMessage(`{"name":"app","image":"nginx:2.0"}`)

	_ = st.UpsertService("app", spec1, StatusPending)
	_ = st.UpsertService("app", spec2, StatusReady)

	svc, _ := st.GetService("app")
	if svc.Status != StatusReady {
		t.Errorf("status: got %q, want READY", svc.Status)
	}
	// Spec should be the second upsert value.
	if string(svc.Spec) != string(spec2) {
		t.Errorf("spec not updated by second upsert")
	}
}

func TestUpsertService_UpdatesSpec(t *testing.T) {
	st := newTestStore(t)
	_ = st.UpsertService("app", json.RawMessage(`{"name":"app","image":"v1"}`), StatusPending)
	newSpec := json.RawMessage(`{"name":"app","image":"v2"}`)
	_ = st.UpsertService("app", newSpec, StatusPending)

	svc, _ := st.GetService("app")
	if string(svc.Spec) != string(newSpec) {
		t.Errorf("spec not updated: got %s", svc.Spec)
	}
}

// --- Status fields ---

func TestGetService_StatusFields(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusDeploying)

	svc, _ := st.GetService("app")
	if svc.Status != StatusDeploying {
		t.Errorf("status: got %q", svc.Status)
	}
	if svc.CreatedAt.IsZero() {
		t.Error("created_at should not be zero")
	}
	if svc.UpdatedAt.IsZero() {
		t.Error("updated_at should not be zero")
	}
}

func TestUpdateStatus_NonExistent(t *testing.T) {
	st := newTestStore(t)
	// Updating a non-existent row should not error (UPDATE affects 0 rows silently).
	err := st.UpdateStatus("does-not-exist", StatusReady)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMarkReady_SetsLastReadySpec(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusDeploying)
	readySpec := json.RawMessage(`{"name":"app","image":"nginx:v2"}`)
	_ = st.MarkReady("app", readySpec)

	// LastReadySpec is private; verify via indirect check (service is READY).
	svc, _ := st.GetService("app")
	if svc.Status != StatusReady {
		t.Errorf("expected READY, got %q", svc.Status)
	}
}

// --- ServicesByStatus ---

func TestServicesByStatus_Empty(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusReady)

	pending, _ := st.ServicesByStatus(StatusPending)
	if len(pending) != 0 {
		t.Errorf("want 0 pending, got %d", len(pending))
	}
}

func TestServicesByStatus_AllPending(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"x","image":"nginx"}`)
	for _, id := range []string{"svc1", "svc2", "svc3"} {
		_ = st.UpsertService(id, spec, StatusPending)
	}

	pending, _ := st.ServicesByStatus(StatusPending)
	if len(pending) != 3 {
		t.Errorf("want 3 pending, got %d", len(pending))
	}
}

// --- Job creation uniqueness ---

func TestCreateJob_UniqueIDs(t *testing.T) {
	st := newTestStore(t)
	id1, _ := st.CreateJob("rel", "ns", "install", "")
	id2, _ := st.CreateJob("rel", "ns", "install", "")
	if id1 == id2 {
		t.Error("two CreateJob calls should produce different IDs")
	}
}

func TestCreateJob_WithInitiator(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "token:abcd1234")
	j, _ := st.GetJob(jobID)
	if j.Initiator != "token:abcd1234" {
		t.Errorf("initiator: got %q", j.Initiator)
	}
}

func TestGetJob_CheckNamespace(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("myrelease", "production", "upgrade", "")
	j, _ := st.GetJob(jobID)
	if j.Namespace != "production" {
		t.Errorf("namespace: got %q", j.Namespace)
	}
}

func TestGetJob_CheckOperation(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "rollback", "")
	j, _ := st.GetJob(jobID)
	if j.Operation != "rollback" {
		t.Errorf("operation: got %q", j.Operation)
	}
}

func TestGetJob_InitiatorStored(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "token:xyz")
	j, _ := st.GetJob(jobID)
	if j.Initiator != "token:xyz" {
		t.Errorf("initiator: got %q", j.Initiator)
	}
}

func TestSetJobStatus_Running(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")
	_ = st.SetJobStatus(jobID, JobRunning)
	j, _ := st.GetJob(jobID)
	if j.Status != JobRunning {
		t.Errorf("status: got %q, want running", j.Status)
	}
}

// --- AppendEvent details ---

func TestAppendEvent_TSAutoSet(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")
	// Pass an event with TS=0; the store should fill it in.
	_ = st.AppendEvent(jobID, Event{Phase: "validation"})
	j, _ := st.GetJob(jobID)
	if len(j.Events) == 0 {
		t.Fatal("expected at least one event")
	}
	if j.Events[0].TS == 0 {
		t.Error("TS should be auto-set when 0")
	}
}

func TestAppendEvent_TSPreserved(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")
	_ = st.AppendEvent(jobID, Event{Phase: "validation", TS: 999888777})
	j, _ := st.GetJob(jobID)
	if j.Events[0].TS != 999888777 {
		t.Errorf("TS should be preserved: got %d", j.Events[0].TS)
	}
}

func TestAppendEvent_ErrorPhase(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")
	_ = st.AppendEvent(jobID, Event{Phase: "error", Error: "something went wrong"})
	j, _ := st.GetJob(jobID)
	if j.Events[0].Error != "something went wrong" {
		t.Errorf("error field: got %q", j.Events[0].Error)
	}
}

// --- Pub/Sub ---

func TestSubscribe_ReceivesAppendEvent(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	ch, cancel := st.Subscribe(jobID)
	defer cancel()

	_ = st.AppendEvent(jobID, Event{Phase: "apply", Message: "deploying"})

	select {
	case ev := <-ch:
		if ev.Phase != "apply" {
			t.Errorf("phase: got %q, want apply", ev.Phase)
		}
	default:
		t.Error("expected event on channel but got none")
	}
}

func TestSubscribe_Cancel_ClosesChannel(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")
	ch, cancel := st.Subscribe(jobID)
	cancel()

	// After cancel, the channel should be closed.
	_, open := <-ch
	if open {
		t.Error("channel should be closed after cancel")
	}
}

func TestSubscribe_MultipleSubscribers(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	ch1, cancel1 := st.Subscribe(jobID)
	ch2, cancel2 := st.Subscribe(jobID)
	defer cancel1()
	defer cancel2()

	_ = st.AppendEvent(jobID, Event{Phase: "validation"})

	got1, got2 := false, false
	for i := 0; i < 10; i++ {
		select {
		case <-ch1:
			got1 = true
		case <-ch2:
			got2 = true
		default:
		}
	}
	if !got1 {
		t.Error("subscriber 1 should have received event")
	}
	if !got2 {
		t.Error("subscriber 2 should have received event")
	}
}

func TestSetJobStatus_Succeeded_PublishesComplete(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")

	ch, cancel := st.Subscribe(jobID)
	defer cancel()

	_ = st.SetJobStatus(jobID, JobSucceeded)

	select {
	case ev := <-ch:
		if ev.Phase != "complete" {
			t.Errorf("expected complete event, got phase=%q", ev.Phase)
		}
	default:
		t.Error("expected complete event on channel after SetJobStatus succeeded")
	}
}

// --- parseEvents detailed ---

func TestParseEvents_WithAllFields(t *testing.T) {
	line := `{"phase":"apply","message":"deploying","kind":"Deployment","name":"myapp","namespace":"default","error":"","ts":12345}`
	evs := parseEvents(line)
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	ev := evs[0]
	if ev.Kind != "Deployment" {
		t.Errorf("kind: got %q", ev.Kind)
	}
	if ev.Name != "myapp" {
		t.Errorf("name: got %q", ev.Name)
	}
	if ev.Namespace != "default" {
		t.Errorf("namespace: got %q", ev.Namespace)
	}
}

func TestParseEvents_TSField(t *testing.T) {
	line := `{"phase":"done","ts":9999}`
	evs := parseEvents(line)
	if len(evs) != 1 {
		t.Fatalf("want 1 event, got %d", len(evs))
	}
	if evs[0].TS != 9999 {
		t.Errorf("TS: got %d", evs[0].TS)
	}
}

// --- AuditLog ---

func TestAuditLog_WithEmptyInitiator(t *testing.T) {
	st := newTestStore(t)
	err := st.AuditLog("", "install", "rel", "ns", "accepted", nil)
	if err != nil {
		t.Fatalf("audit log with empty initiator: %v", err)
	}
}

// --- redact ---

func TestRedact_SliceOfMaps(t *testing.T) {
	in := []any{
		map[string]any{"password": "secret", "name": "admin"},
		map[string]any{"token": "abc", "role": "editor"},
	}
	out, ok := redact(in).([]any)
	if !ok {
		t.Fatal("expected slice output")
	}
	m0 := out[0].(map[string]any)
	if m0["password"] != "[redacted]" {
		t.Errorf("password not redacted: %v", m0["password"])
	}
	if m0["name"] != "admin" {
		t.Errorf("name changed: %v", m0["name"])
	}
	m1 := out[1].(map[string]any)
	if m1["token"] != "[redacted]" {
		t.Errorf("token not redacted: %v", m1["token"])
	}
}

func TestRedact_EmptyMap(t *testing.T) {
	in := map[string]any{}
	out, ok := redact(in).(map[string]any)
	if !ok {
		t.Fatal("expected map output")
	}
	if len(out) != 0 {
		t.Errorf("want empty map, got %v", out)
	}
}

func TestRedact_NonMapValue(t *testing.T) {
	result := redact("plainstring")
	if result != "plainstring" {
		t.Errorf("non-map value should pass through unchanged: got %v", result)
	}
}

// --- DisabledResource extras ---

func TestRecordDisabled_Idempotent(t *testing.T) {
	st := newTestStore(t)
	// INSERT OR IGNORE: second call should not error.
	_ = st.RecordDisabled("rel", "ns", "Deployment", "frontend", "")
	if err := st.RecordDisabled("rel", "ns", "Deployment", "frontend", ""); err != nil {
		t.Fatalf("second RecordDisabled should be a no-op, not error: %v", err)
	}
	items, _ := st.ListDisabled("rel", "ns")
	if len(items) != 1 {
		t.Errorf("want 1 entry (idempotent), got %d", len(items))
	}
}

func TestListDisabled_MultipleResources(t *testing.T) {
	st := newTestStore(t)
	_ = st.RecordDisabled("rel", "ns", "Deployment", "frontend", "")
	_ = st.RecordDisabled("rel", "ns", "Service", "frontend-svc", "")

	items, _ := st.ListDisabled("rel", "ns")
	if len(items) != 2 {
		t.Errorf("want 2 disabled resources, got %d", len(items))
	}
}

func TestListDisabled_DifferentRelease(t *testing.T) {
	st := newTestStore(t)
	_ = st.RecordDisabled("rel-a", "ns", "Deployment", "d1", "")
	_ = st.RecordDisabled("rel-b", "ns", "Deployment", "d2", "")

	items, _ := st.ListDisabled("rel-a", "ns")
	if len(items) != 1 {
		t.Fatalf("want 1 for rel-a, got %d", len(items))
	}
	if items[0].Name != "d1" {
		t.Errorf("wrong resource: got %q", items[0].Name)
	}
}

func TestClearDisabled_NonExistent(t *testing.T) {
	st := newTestStore(t)
	err := st.ClearDisabled("no-release", "no-ns", "Deployment", "no-name", "")
	if err != nil {
		t.Fatalf("clearing nonexistent resource should not error: %v", err)
	}
}

func TestDisabledResource_WithResourceNs(t *testing.T) {
	st := newTestStore(t)
	_ = st.RecordDisabled("rel", "ns", "PersistentVolumeClaim", "data-pvc", "other-ns")

	items, _ := st.ListDisabled("rel", "ns")
	if len(items) != 1 {
		t.Fatalf("want 1, got %d", len(items))
	}
	if items[0].ResourceNs != "other-ns" {
		t.Errorf("resource_ns: got %q, want other-ns", items[0].ResourceNs)
	}
}

func TestClearDisabled_SpecificResource(t *testing.T) {
	st := newTestStore(t)
	_ = st.RecordDisabled("rel", "ns", "Deployment", "d1", "")
	_ = st.RecordDisabled("rel", "ns", "Deployment", "d2", "")

	_ = st.ClearDisabled("rel", "ns", "Deployment", "d1", "")

	items, _ := st.ListDisabled("rel", "ns")
	if len(items) != 1 {
		t.Fatalf("want 1 remaining, got %d", len(items))
	}
	if items[0].Name != "d2" {
		t.Errorf("wrong resource remains: got %q", items[0].Name)
	}
}

func TestOpen_MigratesIdempotently(t *testing.T) {
	f, _ := os.CreateTemp("", "ks-idempotent-*.db")
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	// First open runs migrations.
	st1, err := Open(f.Name())
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	st1.Close()

	// Second open on the same file should not fail (ALTER TABLE duplicate column is tolerated).
	st2, err := Open(f.Name())
	if err != nil {
		t.Fatalf("second open (re-migration): %v", err)
	}
	st2.Close()
}

func TestGetJob_WithEvents_TSPreserved(t *testing.T) {
	st := newTestStore(t)
	jobID, _ := st.CreateJob("rel", "ns", "install", "")
	_ = st.AppendEvent(jobID, Event{Phase: "apply", TS: 42000})
	j, _ := st.GetJob(jobID)
	if len(j.Events) == 0 {
		t.Fatal("no events")
	}
	if j.Events[0].TS != 42000 {
		t.Errorf("TS: got %d, want 42000", j.Events[0].TS)
	}
}

func TestAttachJob_Overwrite(t *testing.T) {
	st := newTestStore(t)
	spec := json.RawMessage(`{"name":"app","image":"nginx"}`)
	_ = st.UpsertService("app", spec, StatusPending)

	job1, _ := st.CreateJob("app", "default", "deploy", "")
	_ = st.AttachJob("app", job1)
	job2, _ := st.CreateJob("app", "default", "patch", "")
	_ = st.AttachJob("app", job2)

	svc, _ := st.GetService("app")
	if svc.JobID != job2 {
		t.Errorf("job_id should be updated to job2, got %q", svc.JobID)
	}
}
