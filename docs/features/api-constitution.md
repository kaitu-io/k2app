# API Constitution — Center Service Code Quality

## Meta

| Field | Value |
|-------|-------|
| Status | draft |
| Created | 2026-02-17 |
| Updated | 2026-02-17 |
| Version | 1.0 |

## Summary

Fix failing tests, establish coding conventions ("constitution") for the `api/` Go package, fix all violations, and expand `api/CLAUDE.md` as the single source of truth for conventions.

## Constraints

- **Do NOT change** existing API request/response structures
- All other code is changeable
- Conventions should be practical for this project, not abstract Go best practices

## Acceptance Criteria

- [ ] AC1: `cd api && go test ./...` passes with zero failures
- [ ] AC2: `cd api/cloudprovider && go test ./...` passes with zero failures
- [ ] AC3: `api/CLAUDE.md` contains complete constitution (test tiers, error handling, logging, response patterns, documented exceptions)
- [ ] AC4: No deprecated stdlib usage (`strings.Title`)
- [ ] AC5: Single mock DB helper (`SetupMockDB` only, no duplicate `setupMockDB`)
- [ ] AC6: Test route paths match production paths
- [ ] AC7: All intentional convention deviations are documented with comments

## Decisions (from Scrum)

1. **Delete** `api_admin_plan_integration_test.go` — zero-value assertions
2. **Fix** `testInitConfig()` — skip instead of panic when config missing
3. **Fix** `TestSSHStandaloneProvider_GetOrphanStatus` — assertion mismatch
4. **Document** webhook HTTP status code exception (keep behavior)
5. **Alias** `ListWithData` → `List` (keep for backward compat)
6. **Unify** mock DB to `SetupMockDB` only
7. **Fix** `strings.Title` → ASCII title-case
8. **Fix** test routes `/api/k2v4/` → `/api/`
9. **Document** asynqmon HTML response exception
10. **Fix** slave_api.go logging (remove `[ERROR]`/`[DEBUG]` prefixes, `Tracef` → `Debugf`)
11. **Expand** `api/CLAUDE.md` with full constitution
