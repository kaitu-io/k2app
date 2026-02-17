# Execution Plan: api-constitution

Feature: api-constitution
Spec: docs/features/api-constitution.md
Created: 2026-02-17

## Tasks

### T1: Fix test infrastructure and delete bad tests
- **AC coverage**: AC1, AC2
- **depends_on**: none
- **files**:
  - `api/testutil_test.go` — fix `testInitConfig` to not panic
  - `api/api_admin_plan_integration_test.go` — DELETE
  - `api/cloudprovider/ssh_standalone_test.go` — fix assertion
- **steps**:
  1. In `testutil_test.go`: wrap `util.SetConfigFile()` in os.Stat check; if file missing, set a flag and skip
  2. Delete `api_admin_plan_integration_test.go`
  3. Read and fix `TestSSHStandaloneProvider_GetOrphanStatus` in cloudprovider
  4. Run `go test ./...` from api/ to verify

### T2: Fix code violations (quick fixes)
- **AC coverage**: AC4, AC6, AC7
- **depends_on**: none
- **files**:
  - `api/util.go` — replace `strings.Title` with ASCII title-case
  - `api/api_strategy_test.go` — fix route prefix `/api/k2v4/` → `/api/`
  - `api/slave_api.go` — remove `[ERROR]`/`[DEBUG]` prefixes, `Tracef` → `Debugf`
  - `api/response.go` — alias `ListWithData` to `List`
  - `api/api_webhook.go` — add convention exception comment, fix import order
  - `api/middleware.go` — add asynqmon HTML exception comment
- **steps**:
  1. Fix each file per decisions
  2. Run `go test ./...` to verify no regressions

### T3: Unify mock DB helpers
- **AC coverage**: AC5
- **depends_on**: T1
- **files**:
  - `api/db_mock_test.go` — migrate tests to use `SetupMockDB`, delete `setupMockDB` function
  - `api/mock_db_test.go` — may need minor adjustments
- **steps**:
  1. Read both files, understand differences
  2. Rewrite `db_mock_test.go` tests to use `SetupMockDB(t).DB` and `SetupMockDB(t).Mock`
  3. Remove `setupMockDB` function
  4. Run tests to verify

### T4: Expand api/CLAUDE.md constitution
- **AC coverage**: AC3
- **depends_on**: T1, T2, T3
- **files**:
  - `api/CLAUDE.md`
- **steps**:
  1. Read current api/CLAUDE.md
  2. Add Test Conventions section (3 tiers, skip guards, SetupMockDB)
  3. Add Error Handling section (Error(), ErrorE(), webhook exception)
  4. Add Logging section (no manual prefixes, Debugf not Tracef)
  5. Add Response Patterns section (HTTP 200 always, asynqmon exception)
  6. Keep concise: rules + code examples

## Test Command

```bash
cd api && go test ./...
```

## Dependency Graph

```
T1 ──┐
     ├── T3 ── T4
T2 ──┘
```

T1 and T2 can run in parallel. T3 depends on T1. T4 depends on all.
