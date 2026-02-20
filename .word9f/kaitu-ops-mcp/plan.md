# Plan: Kaitu Ops MCP Server

## Meta

| Field | Value |
|-------|-------|
| Feature | kaitu-ops-mcp |
| Spec | docs/features/kaitu-ops-mcp.md |
| Date | 2026-02-20 |
| Complexity | moderate |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: list_nodes X-Access-Key + field filtering + country/name filter | test_list_nodes_filters, test_list_nodes_field_extraction | T3 |
| AC2: exec_on_node SSH exec, stdout/stderr/exitCode | test_exec_basic, test_exec_stderr | T4 |
| AC3: exec_on_node stdout truncation at 10000 chars | test_truncation_at_limit, test_no_truncation_under_limit | T4 |
| AC4: exec_on_node stdout redaction of SECRET patterns | test_redact_node_secret, test_redact_hex_string, test_redact_preserves_normal | T2 |
| AC5: exec_on_node custom timeout | test_exec_timeout | T4 |
| AC6: SSH default key + KAITU_SSH_KEY env + config override | test_ssh_key_resolution_order | T1 |
| AC7: Center API X-Access-Key from config or KAITU_ACCESS_KEY env | test_center_api_auth_header | T1 |
| AC8: Config missing → clear error | test_config_missing_error, test_config_partial_missing | T1 |
| AC9: SSH connection failure → clear error message | test_ssh_connection_refused, test_ssh_auth_failed | T4 |
| AC10: MCP Server stdio discovery | test_server_stdio_init (manual verification) | T5 |
| AC11: Skill architecture identification flow | (content review) | T6 |
| AC12: Skill new k2v5 arch + old k2-slave compat | (content review) | T6 |
| AC13: Skill .env variables documentation | (content review) | T6 |
| AC14: Skill standard ops command table | (content review) | T6 |
| AC15: Skill 7 safety guardrails, positioned as best practice | (content review) | T6 |
| AC16: Skill two script execution modes | (content review) | T6 |
| AC17: Skill references docker/scripts/ with notes | (content review) | T6 |
| AC18: Scripts copied to docker/scripts/ with fixed names | (file check) | T7 |

## Foundation Tasks

### T1: Project Scaffold + Config Module

**Scope**: Initialize `tools/kaitu-ops-mcp/` with package.json, tsconfig.json, and the config module that loads TOML file + env var fallback. This is the foundation all tools depend on.
**Files**:
- `tools/kaitu-ops-mcp/package.json` (create)
- `tools/kaitu-ops-mcp/tsconfig.json` (create)
- `tools/kaitu-ops-mcp/src/config.ts` (create)
- `tools/kaitu-ops-mcp/src/config.test.ts` (create)
- `tools/kaitu-ops-mcp/src/center-api.ts` (create)
- `tools/kaitu-ops-mcp/src/center-api.test.ts` (create)
**Depends on**: none
**TDD**:
- RED: Write failing tests for config resolution
  - `test_config_from_toml_file` — loads center.url, center.access_key, ssh.private_key_path, ssh.user, ssh.port from TOML
  - `test_config_env_overrides_toml` — KAITU_CENTER_URL, KAITU_ACCESS_KEY, KAITU_SSH_KEY, KAITU_SSH_USER, KAITU_SSH_PORT override TOML values
  - `test_config_env_only` — works without TOML file if all env vars set
  - `test_config_missing_error` — no TOML + no env → clear error message listing what's missing
  - `test_config_partial_missing` — TOML has center but no ssh → error lists missing ssh fields
  - `test_ssh_key_resolution_order` — default ~/.ssh/id_rsa → ~/.ssh/id_ed25519 → KAITU_SSH_KEY → config file
  - `test_center_api_auth_header` — CenterApiClient sends `X-Access-Key` header with configured access_key
  - `test_center_api_request_url` — CenterApiClient constructs correct URL from config
- GREEN: Implement config.ts (TOML parsing + env fallback + SSH key discovery) and center-api.ts (HTTP client with X-Access-Key header)
- REFACTOR:
  - [MUST] Export `loadConfig()` and `CenterApiClient` as stable APIs for tool modules
  - [SHOULD] Type validation for config fields (url format, file path existence)
**Acceptance**: Config loads from TOML + env, SSH key resolves with fallback chain, CenterApiClient sends X-Access-Key. Clear errors on missing config.

### T2: Stdout Redaction Module

**Scope**: Standalone redaction module that filters sensitive patterns from stdout before returning to Claude.
**Files**:
- `tools/kaitu-ops-mcp/src/redact.ts` (create)
- `tools/kaitu-ops-mcp/src/redact.test.ts` (create)
**Depends on**: none (pure function, no dependencies)
**TDD**:
- RED: Write failing tests for redaction patterns
  - `test_redact_node_secret` — `K2_NODE_SECRET=abc123...` → `K2_NODE_SECRET=[REDACTED]`
  - `test_redact_secret_equals` — `SECRET=xyz` → `SECRET=[REDACTED]`
  - `test_redact_hex_string_64` — standalone 64-char hex string → `[REDACTED]`
  - `test_redact_preserves_normal` — normal stdout output unchanged
  - `test_redact_multiline` — redacts across multiple lines
  - `test_redact_mixed_content` — redacts secrets but preserves surrounding text
- GREEN: Implement `redactStdout(text: string): string` with regex-based pattern matching
- REFACTOR:
  - [MUST] Export `redactStdout()` as the single public API
  - [SHOULD] Make redaction patterns configurable (for future extension)
**Acceptance**: All SECRET-like patterns are replaced with `[REDACTED]`. Normal output is untouched.

## Feature Tasks

### T3: list_nodes Tool

**Scope**: Implement the `list_nodes` MCP tool that fetches from Center API batch-matrix and filters to return only needed fields.
**Files**:
- `tools/kaitu-ops-mcp/src/tools/list-nodes.ts` (create)
- `tools/kaitu-ops-mcp/src/tools/list-nodes.test.ts` (create)
**Depends on**: [T1]
**TDD**:
- RED: Write failing tests for list_nodes
  - `test_list_nodes_field_extraction` — raw batch-matrix response → filtered to {name, ipv4, ipv6, country, region, tunnels}
  - `test_list_nodes_drops_script_results` — batch script matrix data stripped from response
  - `test_list_nodes_filter_by_country` — country="jp" → only Japanese nodes
  - `test_list_nodes_filter_by_name` — name="jp-01" → only matching node
  - `test_list_nodes_empty_result` — no nodes match filter → empty array
  - `test_list_nodes_tunnel_mapping` — tunnels correctly nested under each node
- GREEN: Implement list-nodes.ts — call CenterApiClient, parse batch-matrix response, filter fields, apply country/name filters
- REFACTOR:
  - [MUST] Export tool definition compatible with `@modelcontextprotocol/sdk` `server.tool()` registration
  - [SHOULD] Type definitions for BatchMatrixResponse and filtered NodeInfo
**Acceptance**: Returns clean node list with tunnels. Filters work. No batch script data leaks through.

### T4: exec_on_node Tool + SSH Helper

**Scope**: Implement the `exec_on_node` MCP tool and the SSH connection helper (including stdin pipe for script transfer).
**Files**:
- `tools/kaitu-ops-mcp/src/ssh.ts` (create)
- `tools/kaitu-ops-mcp/src/ssh.test.ts` (create)
- `tools/kaitu-ops-mcp/src/tools/exec-on-node.ts` (create)
- `tools/kaitu-ops-mcp/src/tools/exec-on-node.test.ts` (create)
**Depends on**: [T1, T2]
**TDD**:
- RED: Write failing tests for SSH helper and exec tool
  - `test_ssh_connect_with_key` — connects to mock SSH server with private key
  - `test_ssh_connection_refused` — unreachable IP → error message "Connection refused to {ip}"
  - `test_ssh_auth_failed` — wrong key → error message "Authentication failed for {ip}"
  - `test_exec_basic` — command returns {stdout, stderr, exitCode}
  - `test_exec_stderr` — command with stderr output captured separately
  - `test_exec_timeout` — command exceeds timeout → exitCode=-1, error in stderr
  - `test_truncation_at_limit` — stdout >10000 chars → truncated=true, output cut
  - `test_no_truncation_under_limit` — stdout <10000 chars → truncated=false, full output
  - `test_exec_redaction_applied` — stdout containing SECRET → redacted before return
  - `test_stdin_pipe_script` — local script content piped via stdin, executed remotely
- GREEN: Implement ssh.ts (connect, exec, stdin pipe) and exec-on-node.ts (tool wrapper with truncation + redaction)
- REFACTOR:
  - [MUST] Export tool definition compatible with `server.tool()` registration
  - [MUST] SSH helper exports `sshExec()` and `sshExecWithStdin()` as stable APIs
  - [SHOULD] Structured error types for connection/auth/timeout failures
**Acceptance**: SSH exec works with key auth. Timeout enforced. Truncation at 10000 chars. Redaction applied. stdin pipe transfers scripts safely.

### T5: MCP Server Entry Point + Integration

**Scope**: Wire everything together into the MCP server entry point. Register both tools. Configure stdio transport. Update `.claude/settings.json` for project-level MCP registration.
**Files**:
- `tools/kaitu-ops-mcp/src/index.ts` (create)
- `.claude/settings.json` (modify — add MCP server entry)
**Depends on**: [T3, T4]
**TDD**:
- RED: Write integration test
  - `test_server_stdio_init` — server starts without error, lists 2 tools (list_nodes, exec_on_node)
  - `test_server_tool_schemas` — both tools have correct input schemas (list_nodes: country?, name?; exec_on_node: ip, command, timeout?)
- GREEN: Implement index.ts — create MCP Server, register list_nodes and exec_on_node tools, configure stdio transport. Update .claude/settings.json with MCP server command.
- REFACTOR:
  - [SHOULD] Add server name and version from package.json
**Acceptance**: `node dist/index.js` starts MCP server. Claude Code discovers and lists 2 tools. .claude/settings.json points to the built server.

### T6: Skill File (Node Ops Safety Guardrails)

**Scope**: Write the `.claude/skills/kaitu-node-ops.md` skill file with complete infrastructure knowledge, dual-architecture identification, standard operations, safety guardrails, and script execution guidance.
**Files**:
- `.claude/skills/kaitu-node-ops.md` (create)
**Depends on**: none (content-only, no code dependency)
**TDD**:
- RED: Content checklist (no automated test — manual review):
  - Architecture identification flow present (docker ps → k2v5 vs k2-slave)
  - New k2v5 architecture: 4 containers, dependency chain, network modes
  - Old k2-slave architecture: container name mapping, differences
  - .env variables: all 11 core variables documented
  - Standard ops: 11 commands listed with container name placeholders
  - Safety guardrails: 7 rules, positioned as "best practice guardrails"
  - Script execution: two modes (direct command vs stdin pipe)
  - Script library: 4 scripts referenced with warnings
- GREEN: Write kaitu-node-ops.md with all sections
- REFACTOR:
  - [SHOULD] Add trigger keywords for skill invocation
**Acceptance**: Skill file covers AC11-AC17. Content is accurate per docker-compose.yml and demo.env.

### T7: Scripts Organization

**Scope**: Copy and rename scripts from `~/Downloads/scripts/` to `docker/scripts/`, fixing file names.
**Files**:
- `docker/scripts/prepare-docker-compose.sh` (create — copy from ~/Downloads)
- `docker/scripts/totally-reinstall-docker.sh` (create — copy from ~/Downloads)
- `docker/scripts/enable-ipv6.sh` (create — copy from ~/Downloads, fix double extension)
- `docker/scripts/simple-docker-pull-restart.sh` (create — copy from ~/Downloads, fix double extension)
**Depends on**: none
**TDD**:
- RED: Verify files exist with correct names (shell check)
  - `test_scripts_exist` — all 4 files exist in docker/scripts/
  - `test_scripts_executable` — all 4 files have executable permission
  - `test_scripts_no_double_extension` — no `.sh.sh` files
- GREEN: Copy files, rename `enable_ipv6.sh.sh` → `enable-ipv6.sh`, `simple-docker-pull-restart.sh.sh` → `simple-docker-pull-restart.sh`
- REFACTOR:
  - [SHOULD] Normalize to kebab-case file names (underscores → hyphens)
**Acceptance**: 4 scripts in docker/scripts/ with correct names, no double extensions.

## Dependency Graph

```
T1 (config + center-api) ───→ T3 (list_nodes) ───→ T5 (entry point + integration)
                          ╲                      ╱
T2 (redaction) ────────────→ T4 (exec + ssh) ──╱

T6 (skill file) ──── independent ────
T7 (scripts)    ──── independent ────
```

**Parallel groups**:
- Group A: T1, T2, T6, T7 (all independent, can run in parallel)
- Group B: T3, T4 (after T1/T2 complete, can run in parallel)
- Group C: T5 (after T3, T4 complete)
