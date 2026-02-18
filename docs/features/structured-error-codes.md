# Feature: Structured Error Codes

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | structured-error-codes                   |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-18                               |
| Updated   | 2026-02-18                               |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-18 | Initial: k2 engine structured error codes + frontend alignment |

## Overview

k2 engine 当前只输出 error 字符串，前端 bridge 硬编码所有错误为 code 570。前端定义了 20+ 种错误码但从未被 k2 使用。本 feature 在 engine 层增加错误分类，使用 HTTP-aligned 错误码，让前端能根据 code 显示精准的 i18n 错误信息。同时清理前端遗留的 `control-types` 命名。

## Product Requirements

- PR1: 用户连接失败时看到精准的错误描述（网络不可达 vs 认证失败 vs 超时），而非统一的"连接失败" (v1)
- PR2: 错误码体系前后端一致，k2 engine 产出的 code 与前端 i18n 映射 1:1 对应 (v1)
- PR3: 前端类型文件命名清理，移除旧 `control-` 概念 (v1)

## Technical Decisions

### TD1: 错误分类在 engine 层

Wire 层保持现有 80+ 个 error string 不变。Engine 的 `fail()` 调用 `classifyError()` 做字符串匹配 + `net.Error` 类型断言，映射到 HTTP-aligned code。

理由：wire 改动面太大 (80+ sites); engine 离 wire 近、同 repo 维护; JS 端正则太脆弱。

### TD2: HTTP-Aligned 错误码

| Code | 语义 | wire 错误场景 | classifyError 匹配规则 |
|------|------|--------------|----------------------|
| 400 | Bad Config | `parse URL`, `missing auth`, `unsupported scheme`, `missing port` | 字符串含 "parse URL"/"missing auth"/"unsupported scheme"/"missing port" |
| 401 | Auth Rejected | `stream rejected by server` (token 无效/过期) | 字符串含 "stream rejected" |
| 403 | Forbidden | `pin mismatch`, `blocked CA` | 字符串含 "pin mismatch"/"blocked CA" |
| 408 | Timeout | dial/handshake 超时 | `net.Error` 接口 `.Timeout() == true` |
| 502 | TLS/Protocol Error | `uTLS handshake`, `certificate verify`, `QUIC dial` | 字符串含 "uTLS handshake"/"certificate"/"QUIC dial" |
| 503 | Server Unreachable | `TCP dial`, `connection refused`, `network unreachable` | 字符串含 "TCP dial"/"connection refused"/"network unreachable"/"listen UDP" |
| 570 | Fallback | 所有未分类错误 | default case |

分类优先级：先 `net.Error.Timeout()` (408) → 字符串匹配 → fallback 570。

### TD3: API Response 格式变更

error 字段从 string 改为 object：

```
Before: {"state": "stopped", "error": "wire: TCP dial: connection refused"}
After:  {"state": "stopped", "error": {"code": 503, "message": "wire: TCP dial: connection refused"}}
```

`StatusJSON()` 和 daemon `statusInfo()` 同步改。

### TD4: OnError() 接口不变

`EventHandler.OnError(message string)` 保持不变，避免 gomobile 接口变更引发 Swift/Kotlin 修改。Status polling 路径（`StatusJSON()`）已有结构化 error，足够。

### TD5: retrying 字段保留

`retrying: false` 保持不变。k2 engine 无自动重试机制，删除改类型合约无收益。未来如添加 k2 重试，直接填充即可。

### TD6: 前端文件重命名

`control-types.ts` → `vpn-types.ts`。"control" 来自已删除的 Rust 时代，"vpn-types" 涵盖状态+配置+错误类型。

### TD7: 前端错误码与 k2 对齐

删除 k2 不会产出的错误码（100-109 细分, 110-119 服务器, 510-519 VPN 操作），保留与 k2 engine 对齐的码。`getErrorI18nKey()` 映射更新。

## Key Files

### Go (k2 submodule)

| 文件 | 动作 | 说明 |
|------|------|------|
| `k2/engine/error.go` | 新建 | `EngineError` type + `classifyError()` |
| `k2/engine/error_test.go` | 新建 | classifyError 单元测试 |
| `k2/engine/engine.go` | 修改 | `lastError` 从 `string` 改 `*EngineError`; `fail()` 用 classifyError; `StatusJSON()` 输出结构化 error |
| `k2/daemon/daemon.go` | 修改 | `lastError` 从 `string` 改 `*EngineError`; `doUp()` / `statusInfo()` 对齐 |

### Webapp

| 文件 | 动作 | 说明 |
|------|------|------|
| `webapp/src/services/control-types.ts` → `vpn-types.ts` | 重命名+修改 | 错误码对齐 k2, 删除死码, 更新 getErrorI18nKey |
| `webapp/src/services/tauri-k2.ts` | 修改 | transformStatus() 读 raw.error.code |
| `webapp/src/services/capacitor-k2.ts` | 修改 | transformStatus() 读 raw.error.code |
| `webapp/src/services/__tests__/tauri-k2.test.ts` | 修改 | mock 数据格式对齐 |
| `webapp/src/services/__tests__/capacitor-k2.test.ts` | 修改 | mock 数据格式对齐 |
| 所有 import control-types 的文件 (~8个) | 修改 | import path 更新 |
| `webapp/src/i18n/locales/zh-CN/common.json` + 6 locales | 修改 | 错误文案 key 更新 |

## Acceptance Criteria

- AC1: k2 engine `classifyError()` 将 "wire: TCP dial ... connection refused" 分类为 code 503 (v1)
- AC2: k2 engine `classifyError()` 将 "wire: stream rejected by server" 分类为 code 401 (v1)
- AC3: k2 engine `classifyError()` 将 "wire: uTLS handshake: ..." 分类为 code 502 (v1)
- AC4: k2 engine `classifyError()` 将 timeout 错误（net.Error.Timeout()）分类为 code 408 (v1)
- AC5: k2 engine `classifyError()` 将 "wire: parse URL: ..." 分类为 code 400 (v1)
- AC6: k2 engine `classifyError()` 将 "wire: pin mismatch ..." 分类为 code 403 (v1)
- AC7: k2 engine `classifyError()` 将未识别错误分类为 code 570 (v1)
- AC8: k2 engine `StatusJSON()` 输出 `"error": {"code": N, "message": "..."}` 格式 (v1)
- AC9: k2 daemon `statusInfo()` 输出结构化 error object (v1)
- AC10: Tauri bridge `transformStatus()` 从 `raw.error.code` 读取 code，不再硬编码 570 (v1)
- AC11: Capacitor bridge `transformStatus()` 同 AC10 (v1)
- AC12: 前端 `vpn-types.ts` 错误码常量与 k2 engine 对齐 (400/401/403/408/502/503/570) (v1)
- AC13: `control-types.ts` 已重命名为 `vpn-types.ts`，所有 import 更新 (v1)
- AC14: `getErrorI18nKey()` 为每个 k2 错误码返回正确的 i18n key (v1)
- AC15: zh-CN 和其他 6 个 locale 有对应的错误文案 (v1)
- AC16: `OnError(string)` 接口不变，gomobile 兼容 (v1)
- AC17: `cd k2 && go test ./engine/...` 通过 (v1)
- AC18: `cd k2 && go test ./daemon/...` 通过 (v1)
- AC19: `cd webapp && npx vitest run` 通过 (v1)
- AC20: `cd webapp && npx tsc --noEmit` 通过 (v1)

## Testing Strategy

- k2 engine: `error_test.go` 覆盖全部 7 种错误码分类 + timeout 类型断言 + fallback (v1)
- k2 daemon: 现有 daemon 测试更新 mock 格式 (v1)
- webapp bridge: vitest mock 数据格式从 `"error": "string"` 改为 `"error": {code, message}` (v1)
- webapp types: tsc --noEmit 确保 import rename 无遗漏 (v1)

## Deployment & CI/CD

- k2 submodule更新后需重新 `gomobile bind`（mobile 路径） (v1)
- 桌面端 daemon binary 需重新编译 (v1)
- webapp 独立部署，bridge 变更向后兼容（可处理 string 或 object 格式的 error） (v1)

## Impact Analysis

- **Affected modules**: k2/engine, k2/daemon, webapp/services, webapp/types, webapp/i18n
- **Scope**: Moderate — 跨 Go + TypeScript 两层，约 15 文件
- **Breaking change**: daemon API error 格式从 string 改为 object。Bridge 需向后兼容处理旧格式（过渡期）。
- **Supersedes**: 本 feature 完善了 `vpn-error-reconnect` 中 "code: 570 硬编码" 的遗留问题，完善了 `control-types-alignment` 中保留的错误码常量
