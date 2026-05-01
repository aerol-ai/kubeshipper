# KubeShipper Test Plan

## Overview

100 unit / integration tests distributed across four packages. All tests run with
`go test ./...` — no running Kubernetes cluster or Helm server is required.

| Package | File | Count |
|---------|------|-------|
| `internal/kube` | `spec_test.go` | 25 |
| `internal/store` | `store_test.go` | 30 |
| `internal/api` | `api_test.go` | 31 |
| `internal/helm` | `util_test.go` | 14 |
| **Total** | | **100** |

---

## Package: `internal/kube` (25 tests)

Tests cover `ServiceSpec` parsing, validation rules, and the `Merge` helper.
No external dependencies — pure Go logic.

| # | Test | Expectation |
|---|------|-------------|
| 1 | `TestParseServiceSpec_ValidMinimal` | Valid JSON → no error; defaults (type=service, replicas=1) are applied |
| 2 | `TestParseServiceSpec_InvalidJSON` | Non-JSON body → error |
| 3 | `TestParseServiceSpec_EmptyName` | `name=""` → error |
| 4 | `TestParseServiceSpec_NameTooLong` | 64-char name → error |
| 5 | `TestParseServiceSpec_NameUpperCase` | `"MyApp"` → error (DNS-1035) |
| 6 | `TestParseServiceSpec_NameUnderscore` | `"my_app"` → error |
| 7 | `TestParseServiceSpec_NameStartDash` | `"-myapp"` → error |
| 8 | `TestParseServiceSpec_NameEndDash` | `"myapp-"` → error |
| 9 | `TestParseServiceSpec_NameValidDashes` | `"my-service-v2"` → ok |
| 10 | `TestParseServiceSpec_MissingImage` | No `image` field → error |
| 11 | `TestParseServiceSpec_PortZero` | `port=0` → error |
| 12 | `TestParseServiceSpec_PortTooHigh` | `port=65536` → error |
| 13 | `TestParseServiceSpec_ValidPort` | `port=8080` → ok |
| 14 | `TestParseServiceSpec_NegativeReplicas` | `replicas=-1` → error |
| 15 | `TestParseServiceSpec_DefaultReplicas` | No replicas field → defaults to 1 |
| 16 | `TestParseServiceSpec_DefaultType` | No type field → defaults to `"service"` |
| 17 | `TestParseServiceSpec_InvalidType` | `type="worker"` → error |
| 18 | `TestParseServiceSpec_TypeJob` | `type="job"` → ok |
| 19 | `TestParseServiceSpec_TypeCronJob` | `type="cronjob"` → ok |
| 20 | `TestParseServiceSpec_WithEnv` | Env map is parsed correctly |
| 21 | `TestServiceSpec_Merge_Image` | Patch with new image updates image only |
| 22 | `TestServiceSpec_Merge_Env` | Patch with new env map replaces env |
| 23 | `TestServiceSpec_Merge_Port` | Patch with port updates port |
| 24 | `TestServiceSpec_Merge_Replicas` | Patch with replicas updates replicas |
| 25 | `TestServiceSpec_Merge_EmptyPatch` | Empty patch leaves original spec unchanged |

---

## Package: `internal/store` (30 tests)

Tests use a real SQLite database in a temporary file. Covers service CRUD, job
lifecycle, pub-sub event streaming helpers, audit log, payload redaction, and
disabled-resource tracking.

| # | Test | Expectation |
|---|------|-------------|
| 26 | `TestOpen` | `store.Open(tmpFile)` succeeds |
| 27 | `TestUpsertService_Create` | Insert new service row → no error |
| 28 | `TestGetService_Found` | After upsert, `GetService` returns the row |
| 29 | `TestGetService_NotFound` | `GetService` for unknown ID returns `nil, nil` |
| 30 | `TestListServices_Empty` | Empty DB → empty slice |
| 31 | `TestListServices_Multiple` | Two services → slice of length 2 |
| 32 | `TestUpdateStatus` | `UpdateStatus` changes the status column |
| 33 | `TestMarkReady` | `MarkReady` sets status=READY and last_ready_spec |
| 34 | `TestDeleteService` | `DeleteService` removes the row |
| 35 | `TestServicesByStatus` | Filters correctly by status |
| 36 | `TestAttachJob_Set` | `AttachJob(svc, jobID)` sets job_id column |
| 37 | `TestAttachJob_Clear` | `AttachJob(svc, "")` clears job_id to NULL |
| 38 | `TestResetStuckDeployments` | DEPLOYING rows become PENDING |
| 39 | `TestCreateJob` | Returns a non-empty ID |
| 40 | `TestGetJob_Found` | After create, `GetJob` returns the row |
| 41 | `TestGetJob_NotFound` | Unknown ID → `nil, nil` |
| 42 | `TestSetJobStatus_Succeeded` | Status updated to `succeeded` |
| 43 | `TestSetJobStatus_Failed` | Status updated to `failed` |
| 44 | `TestAppendEvent_Single` | One event → job has 1 event after read |
| 45 | `TestAppendEvent_Multiple` | Three events → job has 3 events in order |
| 46 | `TestParseEvents_Empty` | Empty string → empty slice |
| 47 | `TestParseEvents_Single` | Single JSONL line → one event |
| 48 | `TestParseEvents_Multiple` | Two JSONL lines → two events |
| 49 | `TestParseEvents_MalformedIgnored` | Bad JSONL line skipped; good lines returned |
| 50 | `TestAuditLog` | `AuditLog` inserts a row without error |
| 51 | `TestRedact_Password` | `password` key becomes `"[redacted]"` |
| 52 | `TestRedact_Token` | `token` key becomes `"[redacted]"` |
| 53 | `TestRedact_NestedAuth` | `auth` key (itself sensitive) becomes `"[redacted]"` |
| 54 | `TestRedact_NonSensitive` | Non-sensitive keys are preserved as-is |
| 55 | `TestRecordClearListDisabled` | Full lifecycle: record → list → clear → empty list |

---

## Package: `internal/api` (31 tests)

Tests use `httptest.NewRecorder` with a server wired to a real in-memory SQLite
store and a fake Kubernetes clientset (`k8s.io/client-go/kubernetes/fake`).
The Helm manager is `nil`; only endpoints that return before reaching Helm are
exercised.

### Auth middleware (5)

| # | Test | Expectation |
|---|------|-------------|
| 56 | `TestAuthMiddleware_NoTokenConfigured` | Requests pass through when `AUTH_TOKEN=""` |
| 57 | `TestAuthMiddleware_MissingHeader` | No Authorization header → 401 |
| 58 | `TestAuthMiddleware_NoBearer` | Header present but not `Bearer …` → 401 |
| 59 | `TestAuthMiddleware_WrongToken` | Wrong token → 403 |
| 60 | `TestAuthMiddleware_CorrectToken` | Correct token → not 401/403 |

### Initiator fingerprint (3)

| # | Test | Expectation |
|---|------|-------------|
| 61 | `TestInitiator_NoHeader` | No Authorization → `""` |
| 62 | `TestInitiator_ShortToken` | Token shorter than 8 chars → `"token:short"` |
| 63 | `TestInitiator_NormalToken` | Normal token → `"token:" + first 8 chars` |

### Public endpoints (2)

| # | Test | Expectation |
|---|------|-------------|
| 64 | `TestServerRootEndpoint` | `GET /` → 200; body has `"name":"kubeshipper"` |
| 65 | `TestServerHealthEndpoint` | `GET /health` → 200; body has `"status":"ok"` |

### `validateInstall` (10)

| # | Test | Expectation |
|---|------|-------------|
| 66 | `TestValidateInstall_EmptyRelease` | error |
| 67 | `TestValidateInstall_EmptyNamespace` | error |
| 68 | `TestValidateInstall_MissingSource` | error |
| 69 | `TestValidateInstall_OCI_MissingURL` | error |
| 70 | `TestValidateInstall_OCI_Valid` | nil |
| 71 | `TestValidateInstall_HTTPS_MissingRepoURL` | error |
| 72 | `TestValidateInstall_HTTPS_Valid` | nil |
| 73 | `TestValidateInstall_Git_MissingRepoURL` | error |
| 74 | `TestValidateInstall_Git_Valid` | nil |
| 75 | `TestValidateInstall_UnknownSourceType` | error |

### Request helpers (5)

| # | Test | Expectation |
|---|------|-------------|
| 76 | `TestMustQuery_Present` | Returns value + `true` |
| 77 | `TestMustQuery_Absent` | Returns `""` + `false` |
| 78 | `TestRequireForce_True` | `?force=true` → `true` |
| 79 | `TestRequireForce_FalseValue` | `?force=false` → `false` |
| 80 | `TestRequireForce_Absent` | No param → `false` |

### `writeJSON` (1)

| # | Test | Expectation |
|---|------|-------------|
| 81 | `TestWriteJSON_SetsContentTypeAndStatus` | Content-Type=application/json; status code matches |

### Service handler integration (6)

| # | Test | Expectation |
|---|------|-------------|
| 82 | `TestHandlerCreateService_InvalidJSON` | `POST /api/services` bad body → 400 |
| 83 | `TestHandlerCreateService_InvalidNamespace` | Unmanaged namespace → 400 |
| 84 | `TestHandlerCreateService_MissingImage` | No image field → 400 |
| 85 | `TestHandlerCreateService_Valid` | Valid spec → 202 with jobId |
| 86 | `TestHandlerGetService_NotFound` | `GET /api/services/nonexistent` → 404 |
| 87 | `TestHandlerGetService_Found` | Create then GET → 200 |

### Chart handler validation (3; helm manager is nil)

| # | Test | Expectation |
|---|------|-------------|
| 88 | `TestHandlerInstallChart_MissingRelease` | `POST /api/charts` no release → 400 |
| 89 | `TestHandlerGetRelease_MissingNamespace` | `GET /api/charts/{r}` no namespace → 400 |
| 90 | `TestHandlerUninstallRelease_MissingForce` | `DELETE /api/charts/{r}?namespace=default` no `force=true` → 400 |

---

## Package: `internal/helm` (14 tests)

Tests cover pure utility functions in `util.go`. No Kubernetes or Helm SDK
connections needed.

| # | Test | Expectation |
|---|------|-------------|
| 91 | `TestValuesToYAML_NilMap` | nil map → `"", nil` |
| 92 | `TestValuesToYAML_EmptyNonNilMap` | `map[string]any{}` → `"", nil` |
| 93 | `TestValuesToYAML_SingleValue` | Single key → non-empty YAML string |
| 94 | `TestParseValuesYAML_EmptyString` | `""` → empty map, nil error |
| 95 | `TestParseValuesYAML_Valid` | Valid YAML → correct Go map |
| 96 | `TestParseValuesYAML_NumberValue` | Numeric YAML value parses without error |
| 97 | `TestTimeoutOrDefault_Zero` | `seconds=0` → uses default duration |
| 98 | `TestTimeoutOrDefault_Negative` | `seconds<0` → uses default duration |
| 99 | `TestTimeoutOrDefault_Positive` | `seconds=30` → 30s |
| 100 | `TestBoolDefault_NilDefaultTrue` | nil ptr, default `true` → `true` |
| 101 | `TestBoolDefault_NilDefaultFalse` | nil ptr, default `false` → `false` |
| 102 | `TestBoolDefault_PtrTrue` | `&true` → `true` |
| 103 | `TestBoolDefault_PtrFalse` | `&false` → `false` |
| 104 | `TestEmit_NilFn` | nil `EmitFn` → no panic |

> Note: tests 101–104 are listed here for reference; the file contains exactly
> 14 test functions matching the table above (tests 91–104).

---

## Running the tests

```sh
# All tests
go test ./...

# Verbose with race detector
go test -race -v ./...

# Single package
go test -v ./internal/kube/...
go test -v ./internal/store/...
go test -v ./internal/api/...
go test -v ./internal/helm/...
```
