# App Bypass — "运行中的进程" 区域重设计

**Date:** 2026-05-29
**Status:** Design approved, ready for implementation plan
**Scope:** `desktop/src-tauri/src/app_list.rs` (macOS + Windows enumerators) + `webapp/src/pages/AppBypass.tsx` + dashboard i18n (7 locales)
**Supersedes for this concern:** the "运行中的进程" handling in [`2026-05-27-app-bypass-routes-unification.md`](2026-05-27-app-bypass-routes-unification.md)

## Problem

The App Bypass page lists installed apps (primary) plus a secondary "运行中的进程"
section. That section is meant to be a **supplement**: programs that are *not*
installed apps — standalone binaries, brew tools, `node`, dev servers — that a
user may still want to force direct or force proxy. Two defects break it:

1. **Dedup by `id` is a no-op → duplicates.** The webapp filters running apps
   with `running.filter(r => !installed.some(a => a.id === r.id))`
   (`AppBypass.tsx`). But the two enumerators use different id schemes:
   - macOS `InstalledApp.id` = bundle **path** (`/Applications/QQ.app`, `installed_apps.rs:91`)
   - macOS `RunningApp.id` = bundle **identifier** (`com.tencent.qq`, `app_list.rs:164`)

   Path ≠ identifier, so the filter matches nothing and every running app that
   is *already installed* leaks back into the section as a duplicate.

2. **The supplement can't actually appear (macOS) / is buried in noise (Windows).**
   - macOS `app_list.rs:110-111` skips any process whose bundle path does not end
     in `.app` — so a brew/`node`/standalone CLI binary **never appears**. The
     stated purpose (CLI-tool supplement) is impossible on macOS as written.
   - Windows `app_list.rs:199-234` walks **every** process via `sysinfo` —
     hundreds of system services and daemons, no filtering.
   - Presentation: the section is folded inside an `<Accordion>`, visually
     unlike the flat installed-apps list.

## Goal

The "运行中的进程" section shows **user-launched programs that are not installed
apps**, on both macOS and Windows, presented identically to the installed-apps
list (flat, not folded). macOS gains coverage of non-`.app` binaries; Windows
sheds system-service noise. Both converge on the same filtering rule.

## Filtering Rule (both platforms)

A running process appears in the list iff:

- **(a) Not inside an app/service bundle.** No path segment ends in `.app` or
  `.xpc` (GUI apps are covered by the NSWorkspace pass; `.xpc` services are
  background helpers, never user-routable). macOS only.
- **(b) Not in an OS system directory.** Executable path is not under:
  - macOS: `/System`, `/usr/libexec`, `/usr/sbin`, `/sbin`, `/usr/bin`, `/bin`,
    `/Library/Apple`, `/Library/Developer`, `/private/var/folders`, `/private/tmp`, `/tmp`
  - Windows: `C:\Windows\**` (case-insensitive)
- **(c) Not already an installed app.** Shares no process name with any
  `InstalledApp.processNames` (enforced in the webapp — see "Webapp changes").

The macOS directory list in (b) beyond the obvious system roots was derived
empirically (2026-05-29): a live scan of 822 processes produced 36 standalone
candidates under the minimal rule; adding `/Library/Apple`, `/Library/Developer`
(Xcode/CoreSimulator/CoreDevice daemons), the `.xpc` bundle check, and the
transient `/private/var/folders` + `/tmp` paths reduced it to 23 — all of which
were genuine user-launched programs (node, go, java, python, uv, gopls, adb,
esbuild, CLI tools), zero Apple/XPC/temp noise.

There is **no current-user / owner filter.** Rationale: OS daemons are almost
all root-owned *and* live in system directories, so rule (a) already excludes
them. A user filter would be redundant against that noise while wrongly
excluding legitimately user-relevant programs that run as root (`sudo node`,
`sudo brew services` → nginx/db, privileged-port dev servers). Dropping it also
removes the need to look up process owners on either platform → simpler.

Trade-off accepted: a few third-party root background helpers outside system
dirs (e.g. macOS `/Library/PrivilegedHelperTools/*`) may surface. The exclusion
list is extensible if specific noise proves annoying.

## Rust enumerator changes (`desktop/src-tauri/src/app_list.rs`)

### macOS
Keep the existing NSWorkspace pass (GUI `.app` bundles — gives icons, localized
names, helper grouping). **Add** a second pass:

- Enumerate all PIDs; resolve each executable path via `proc_pid::pidpath`
  (already a dependency, used at `app_list.rs:67`).
- Skip a path inside any `.app` or `.xpc` bundle (any segment ends in `.app` /
  `.xpc`) — `.app`s are covered by the NSWorkspace pass; `.xpc` services are
  background helpers (rule (a)).
- Skip a path under a system directory (rule (b)).
- Emit the remainder as `RunningApp { id: exe_path, label: file_stem,
  process_names: [basename], icon_url: Some("kaitu-icon://exe/<enc>") }`.

Merge the two passes. Standalone binaries without a renderable icon fall back to
the Avatar's first-letter behaviour in the webapp.

### Windows
The existing `enumerate` already walks every process. **Add** rule (a): skip any
process whose exe path is under `C:\Windows` (case-insensitive). No owner lookup.
Everything else unchanged.

### macOS root-process coverage — VERIFIED (2026-05-29)
Empirically confirmed that the **user-level Tauri process can read every
process's exe path, including root-owned ones**, with no special privilege.
Test (ctypes against the same libproc syscall the `proc_pid` crate uses, run as
uid 501):

- `proc_pidpath(1)` (launchd, root) → `/sbin/launchd`; configd, syslogd,
  opendirectoryd, mds all resolved.
- Full `proc_listpids(PROC_ALL_PIDS)` sweep: 822 pids, **820 resolved, all 122
  root-owned resolved.** The only 2 failures were our *own* uid-501 processes
  that exited between list and resolve.

`PROC_PIDPATHINFO` is not a privileged operation (unlike reading process memory
or manipulating a process). So root programs (`sudo node`, privileged-port dev
servers, `sudo brew services`) **will appear** on macOS from the Tauri process —
no need to move enumeration into the root daemon. The only required handling is
to **skip a pid that returns `ESRCH`/`ENOENT`** (benign TOCTOU race — process
died mid-scan), never error the whole enumeration.

## Webapp changes (`webapp/src/pages/AppBypass.tsx` + i18n)

- **Dedup by process name** (rule (b)): compute
  `runningExtra = running.filter(r => r.processNames shares no name with the
  union of every installed app's processNames)`, then apply the search filter.
  Implemented as a `useMemo` over `installed`, `running`, `q`.
- **Un-fold:** remove the `<Accordion>` / `AccordionSummary` / `AccordionDetails`
  / `ExpandMoreIcon`. Render the section as a `<Typography variant="subtitle2">`
  header + `<Stack>` of `AppRow`s, identical to the installed-apps list. Render
  only when `runningExtra.length > 0` (no empty box).
- **Search spans both** installed and running-extra lists.
- **i18n:** relabel `dashboard:appBypass.v2.moreSection` from "更多 — 运行中的进程"
  to "其他运行中的程序" across all 7 locales (zh-CN 其他运行中的程序; zh-TW/zh-HK
  其他執行中的程式; ja その他の実行中のプログラム; en-US/AU/GB "Other running
  programs"). New text added to zh-CN first.

The webapp parts of this (dedup + un-fold + i18n) already exist as an
uncommitted working-tree draft from a prior session; they are correct in
direction and roll into this spec's implementation.

## Data flow

```
listInstalled() ─┐
                 ├─► AppBypass: installedProcNames = ∪ installed[].processNames
listRunning()  ──┘            runningExtra = running where ∩ processNames = ∅, matches search
                              └─► render: [installed list] + [其他运行中的程序 list]
```

The engine match contract is unchanged — overrides still write `processNames`
into `forceProxy`/`forceDirect` in `app-routes.store` exactly as today. This
change only affects *which candidate rows the page offers*.

## Testing

- **Webapp (vitest, `AppBypass.test.tsx`):** a running app sharing a process name
  with an installed app does **not** reappear (dedup by name, not id); a
  standalone binary with no installed counterpart **does** appear under "其他运行
  中的程序"; the installed app renders exactly once; the section is absent when
  the difference set is empty.
- **Rust (`app_list.rs` unit tests):** pure path helpers — `is_macos_system_path`
  / `is_windows_system_path` and `is_inside_bundle` (`.app`/`.xpc`) — with table
  cases. Live process enumeration is not unit-tested.
- **Real verification:** `make dev-macos`, launch `node`/a brew tool, confirm it
  appears once and installed apps are not duplicated; on Windows confirm system
  services disappear. (macOS `proc_pidpath` cross-uid access already verified —
  see "macOS root-process coverage — VERIFIED".)

## Out of scope

- Moving running-process enumeration into the root daemon — **not needed**: root
  coverage works from the user-level Tauri process (verified above).
- Icons for standalone macOS binaries (first-letter Avatar fallback is fine).
- Linux (Tauri `list_running_processes` returns empty; Linux desktop uses the Go
  webui bridge where running apps are already the primary list — no separate
  supplement section applies).
