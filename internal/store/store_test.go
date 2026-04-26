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
