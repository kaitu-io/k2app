# Feature: Webapp Broken Feature Removal

## Meta

- Status: implemented
- Version: 1.1
- Created: 2026-02-17
- Updated: 2026-02-17
- Branch: w9f/webapp-stale-cleanup
- Related: [control-types-alignment](./control-types-alignment.md) (deeper type system cleanup)

## Summary

FAQ page has two completely broken features: speed test (`_k2.run('speedtest')`) and one-click network repair (`_k2.run('fix_network')`). Both actions don't exist in the Go daemon — they return 400. This spec removes all broken UI, dead component files, stale i18n keys, and outdated documentation.

Scope is strictly **UI-layer removal** — type system alignment is in the companion spec.

## Changes

### 1. Delete SpeedTest component

**Delete**: `webapp/src/components/SpeedTest.tsx` (entire file, 280 lines)

### 2. Clean FAQ page

**File**: `webapp/src/pages/FAQ.tsx`

- Remove `SpeedTest` import and `<SpeedTest />` render
- Remove fix_network state (`isFixingNetwork`, `fixNetworkResult`), handler (`handleFixNetwork`), and the entire "网络修复工具" Card
- Remove unused imports (`useState`, `CircularProgress`, `Alert`, `Build as FixIcon`)
- Keep remaining 3 cards: security software guide, community feedback, submit ticket

### 3. Remove "Fix Network" button from error notifications

**File**: `webapp/src/components/ConnectionNotification.tsx`

- Remove `showFixNetwork` field from `NotificationConfig`
- Remove `handleFixNetwork` callback (navigates to `/faq`)
- Remove the conditional "Fix Network" `<Button>` block
- Remove `Build as FixIcon` import
- Remove `useNavigate` import (no longer needed)
- Keep the error notification display itself (still useful)

**File**: `webapp/src/components/CollapsibleConnectionSection.tsx`

- Remove `handleFixNetwork` callback
- Remove `isNetworkError()` helper function (only used for showFixNetwork)
- Remove `showNetworkFix` variable and the conditional "Fix Network" `<Button>` block
- Remove `Build as FixIcon` import
- Remove `useNavigate` import (no longer needed)
- Keep the error display bar itself (still useful)

### 4. Clean i18n locale files

Remove `troubleshooting.speedtest` and `troubleshooting.fixNetwork` keys from all 14 files:

| Locale | Files |
|--------|-------|
| zh-CN | `zh-CN.json`, `zh-CN/dashboard.json` |
| zh-TW | `zh-TW.json`, `zh-TW/dashboard.json` |
| zh-HK | `zh-HK.json`, `zh-HK/dashboard.json` |
| en-US | `en-US.json`, `en-US/dashboard.json` |
| en-AU | `en-AU.json`, `en-AU/dashboard.json` |
| en-GB | `en-GB.json`, `en-GB/dashboard.json` |
| ja | `ja.json`, `ja/dashboard.json` |

### 5. Update documentation

**File**: `docs/contracts/webapp-daemon-api.md`
- Remove `action: "speedtest"` section (lines 104-116)
- Remove `action: "get_speedtest_status"` section (lines 117-124)

**File**: `docs/baselines/k2app-baseline.md`
- Remove `speedtest` from daemon capabilities line

**File**: `docs/features/k2app-rewrite.md`
- Remove `speedtest` and `get_speedtest_status` rows from daemon action table

## Files Touched

| Category | Files | Action |
|----------|-------|--------|
| Delete | `SpeedTest.tsx` | Delete entire file |
| Code | `FAQ.tsx` | Remove fix_network + speedtest sections |
| Code | `ConnectionNotification.tsx` | Remove "Fix Network" button |
| Code | `CollapsibleConnectionSection.tsx` | Remove "Fix Network" button |
| i18n | 14 locale files | Remove speedtest + fixNetwork keys |
| Docs | `webapp-daemon-api.md` | Remove speedtest API docs |
| Docs | `k2app-baseline.md` | Remove speedtest capability |
| Docs | `k2app-rewrite.md` | Remove speedtest from action table |
| **Total** | **21 files** | |

## Acceptance Criteria

- [ ] AC1: `SpeedTest.tsx` deleted, no remaining imports
- [ ] AC2: FAQ page renders with 3 cards (security software, community feedback, submit ticket)
- [ ] AC3: `ConnectionNotification` shows error text but no "Fix Network" button
- [ ] AC4: `CollapsibleConnectionSection` shows error bar but no "Fix Network" button
- [ ] AC5: All 14 i18n files have no `troubleshooting.speedtest` or `troubleshooting.fixNetwork` keys
- [ ] AC6: Documentation updated (contracts, baseline, rewrite)
- [ ] AC7: `yarn tsc --noEmit` passes
- [ ] AC8: `yarn test` passes

## Version History

- v1.0 (2026-02-17): Initial spec
- v1.1 (2026-02-17): Narrowed scope to UI-layer only; type system cleanup moved to control-types-alignment spec
