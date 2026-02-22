# Plan: k2 CLI Redesign v2 — Platform-Aware Error Hints

## Meta

| Field | Value |
|-------|-------|
| Feature | k2-cli-redesign (v2) |
| Spec | docs/features/k2-cli-redesign.md |
| Date | 2026-02-22 |
| Complexity | simple |

## Scope

v1 (nginx-style CLI restructure) is already implemented. This plan covers **v2 only**: TD8 platform-aware error hints when daemon is unreachable from `k2 ctl` commands.

All changes are in the `k2/` submodule (`k2/cmd/k2/`). Single task, no worktrees needed.

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC7: daemon 不可达时打印平台感知错误提示 | `TestDaemonNotRunningHint_ContainsSetup` | T1 |
| AC7: 提示含平台对应 service start 命令 | `TestDaemonNotRunningHint_ContainsPlatformCmd` | T1 |
| AC7: ctl up/down/status 统一使用平台提示 | `TestCtlUp_DaemonUnreachable_ShowsHint`, `TestCtlDown_DaemonUnreachable_ShowsHint`, `TestCtlStatus_DaemonUnreachable_ShowsHint` | T1 |
| AC7: 不含任何自动 start/install 行为 | `TestCtlUp_NoAutoStart` (verify no service calls) | T1 |

## Feature Tasks

### T1: Platform-aware ctl error hints

**Scope**: 当 `k2 ctl up/down/status` 无法连接 daemon 时，替换通用错误 `"Is the daemon running? Start it with: k2"` 为平台感知的 next-step 提示。

**Files** (all in `k2/cmd/k2/`):
- `ctl_hint_darwin.go` — NEW: macOS hint constant
- `ctl_hint_linux.go` — NEW: Linux hint constant
- `ctl_hint_windows.go` — NEW: Windows hint constant
- `ctl.go` — MODIFY: error paths call `daemonNotRunningHint()`
- `ctl_hint_test.go` — NEW: test hint content

**Depends on**: none

**TDD**:

- RED: Write failing tests
  - `TestDaemonNotRunningHint_ContainsSetup` — `daemonNotRunningHint()` 返回值包含 `k2 setup`
  - `TestDaemonNotRunningHint_ContainsPlatformCmd` — 返回值包含当前平台的 service start 命令（darwin: `launchctl`、linux: `systemctl`、windows: `sc start`）
  - `TestCtlUp_DaemonUnreachable_ShowsHint` — mock daemon 不可达，验证 stderr 包含 hint 而非旧的 `"Is the daemon running?"`
  - `TestCtlDown_DaemonUnreachable_ShowsHint` — 同上
  - `TestCtlStatus_DaemonUnreachable_ShowsHint` — 同上
  - `TestCtlUp_NoAutoStart` — 验证 error path 不调用任何 service install/start

- GREEN: Implement platform hint files + wire into ctl.go
  - 3 个 build tag 文件各定义 `platformServiceStartCmd` 常量 + `daemonNotRunningHint() string` 函数
  - `ctl_hint_darwin.go`:
    ```go
    //go:build darwin
    package main

    import "fmt"

    func daemonNotRunningHint() string {
        return fmt.Sprintf(`Error: daemon not running

  First time?     k2 setup <URL>
  Already set up? sudo launchctl start kaitu
`)
    }
    ```
  - `ctl_hint_linux.go`: 同模式，`sudo systemctl start k2`
  - `ctl_hint_windows.go`: 同模式，`sc start k2`
  - `ctl.go`: 3 个函数（`ctlUp`/`ctlDown`/`ctlStatus`）的 `if err != nil` 分支统一改为 `fmt.Fprint(os.Stderr, daemonNotRunningHint())`

- REFACTOR:
  - [SHOULD] 提取 `ctlDo(action string, params map[string]any) (*daemon.Response, error)` 辅助函数，消除 ctlUp/ctlDown/ctlStatus 中重复的 client 创建 + error handling
  - [SHOULD] 错误提示加上 daemon 日志路径提示（如 `Check logs: /var/log/k2/k2.log`）

**Acceptance**:
- `k2 ctl up`（daemon 未运行）→ stderr 包含 "daemon not running" + `k2 setup <URL>` + 平台 service start 命令
- `k2 ctl down` / `k2 ctl status` → 同上
- 无任何自动 service start/install 调用
- `go test ./cmd/k2/... -v` 全部通过（新增 + 旧有）

**Knowledge**: docs/knowledge/task-splitting.md → "Go Submodule: Single Branch for Sequential Dependencies"

## Execution Notes

- 单任务，直接在 k2/ submodule 工作
- Build tag 文件遵循已有模式（参考 `signal_windows.go` 的 `//go:build windows`）
- 测试只能验证当前编译平台的 hint 内容，其他平台通过 CI 覆盖
- hint 文案用英文（CLI 不走 i18n）
