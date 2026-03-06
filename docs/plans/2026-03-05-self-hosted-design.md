# Self-Hosted Mode Design

Date: 2026-03-05

Self-hosted mode allows users to connect to their own k2s server without a Cloud API account. Coexists with cloud mode — login promotes cloud nodes to primary, self-hosted demotes to bottom.

## 1. k2s Server Changes

### 1.1 `k2s user` Subcommand

```bash
sudo k2s user add <username>              # Generate token, print full URI
sudo k2s user add <username> --token XXX  # Specify token
sudo k2s user list                        # List all users
sudo k2s user del <username>              # Delete user
sudo k2s user reset <username>            # Regenerate token, print new URI
```

- Reads/writes `{cert_dir}/users` file (default `/etc/k2s/users`). Format: `username:token` per line, `#` comments, blank lines ignored.
- Hot-reload by server — `NewUsersFileValidator()` re-reads file on every auth check, no restart needed.
- `user add` output: complete `k2v5://username:token@host:port?ech=...&pin=...&country=XX#name`
- URI template read from `{cert_dir}/connect-url.txt` — replace udid:token portion with username:token.
- Semantics: username = identifier (was udid), token = password.
- If server not yet running (no connect-url.txt), `user add` prints error: "Run `k2s setup` first".

**Implementation pattern**: New file `k2/cmd/k2s/user.go`. Dispatch via `case "user": cmdUser(args[1:])` in main.go switch (lines 46-68, same pattern as `setup`/`service`).

**users file path resolution**: Read from `config.DefaultServerConfig().CertDir` + `/users` (same as setup.go reads connect-url.txt). No hardcoded `/etc/k2s/`.

**Zero-config auto-setup**: When `k2s setup` runs (zero-config mode, no `auth.users_file` in config), `setupAutoProvision()` should:
1. Generate default admin user via `EnsureAuth()` (existing behavior, produces auth.json)
2. Also write that admin user to `{cert_dir}/users` file
3. Set `cfg.Auth.UsersFile = filepath.Join(cfg.CertDir, "users")` so the validator chain picks it up
4. This ensures `k2s user add` and zero-config mode share the same users file

### 1.2 URI Format Enhancement

Current: `k2v5://udid:token@host:port?ech=...&pin=...&insecure=1`

New: `k2v5://username:token@host:port?ech=...&pin=...&country=XX#name`

- `country`: Auto-detected via IP geolocation API (`ip-api.com/json` — returns `{countryCode: "JP"}`), overridable with `--country XX`. Called once during `setupAutoProvision()`, cached alongside connect-url.txt.
- `#name`: Default = OS hostname (`os.Hostname()`). `user add` inherits server hostname, allows `--name` override.
- Geolocation call added to `logConnectionURL()` alongside existing `ipify.org` probes. Timeout 5s, failure = omit country param (non-blocking).

**wire.go compatibility**: `ParseURL()` (wire.go:110-181) already extracts username as `UDID` and password as `Token` from URL userinfo. No wire.go changes needed — field names are internal, semantics compatible.

### 1.3 `k2s setup` Output Enhancement

```
k2s is running. Share this URL with clients:

  k2v5://admin:a3f8...@1.2.3.4:443?ech=...&pin=...&country=JP#tokyo

Firewall: ensure these ports are open:
  443/tcp    (TLS + WebSocket)
  443/udp    (QUIC)

Add more users:
  sudo k2s user add <username>

Enable port hopping for better stability:
  https://kaitu.io/k2/hop-ports
```

### 1.4 Hop Parameter Format

**Wire protocol uses compact format**: `&hop=50000-50100` (wire.go `parseHopRange()`, line 171-178).

**Webapp tunnel.ts uses split format**: `&hop_port_start=50000&hop_port_end=50100`.

These are two different URL schemes:
- `k2v5://` URIs (Go wire protocol) use `&hop=START-END`
- `k2wss://` URIs (webapp SimpleTunnel) use `&hop_port_start=P1&hop_port_end=P2`

**No conflict for self-hosted**: Self-hosted URIs are `k2v5://` format, passed directly to `buildConnectConfig(uri)` -> `_k2.run('up', config)` -> Go daemon parses with `wire.ParseURL()`. The webapp never parses hop params from k2v5 URIs. No changes needed.

## 2. Webapp Changes

### 2.1 Storage

Single object in platform secure storage (keychain / EncryptedSharedPreferences / localStorage):

- Key: `k2.self_hosted.tunnel`
- Value:

```typescript
interface SelfHostedTunnel {
  uri: string;       // Full k2v5:// URI (contains token)
  name: string;      // Parsed from URI #fragment, or host fallback
  country?: string;  // Parsed from URI &country=
}
```

New store file: `webapp/src/stores/self-hosted.store.ts`

```typescript
interface SelfHostedState {
  tunnel: SelfHostedTunnel | null;
  loaded: boolean;
}

interface SelfHostedActions {
  loadTunnel: () => Promise<void>;
  saveTunnel: (uri: string) => Promise<void>;   // parse + persist
  clearTunnel: () => Promise<void>;
}
```

- `loadTunnel()`: Called from `initializeAllStores()` (after auth store, before vpn store).
- `saveTunnel(uri)`: Validate URI starts with `k2v5://`, parse name/country via URL parsing, persist to `_platform.storage`.
- `clearTunnel()`: Remove from `_platform.storage`, set state to null.
- URI validation: Must start with `k2v5://` and contain `@` (has credentials). Show i18n error otherwise.

**URI parsing for display**: Reuse URL constructor (same approach as `parseSimpleTunnelURL` in tunnel.ts). Extract:
- `hostname` -> display host
- `hash` -> name (strip `#`, decodeURIComponent)
- `searchParams.get('country')` -> country flag
- Token masking for display: show first 4 chars + `***`

### 2.2 Tunnels Page Redesign (`/tunnels`)

Replace the current "Coming Soon" placeholder (Tunnels.tsx). Structure:

```
+--------------------------------------+
| <- Node Management                   |
|                                      |
| [Self-Hosted Node]                   |
| +----------------------------------+ |
| |  Connection URI                  | |
| |  +----------------------------+  | |
| |  | k2v5://...    (input box)  |  | |
| |  +----------------------------+  | |
| |  JP tokyo-server               | |
| |  [ Save ]                       | |
| +----------------------------------+ |
|                                      |
| [Server Deploy Guide]               |
| +----------------------------------+ |
| |  No server? One-click deploy     | |
| |  +----------------------------+  | |
| |  | $ curl -fsSL https://      |  | |
| |  |   kaitu.io/install.sh      |  | |
| |  |   | sudo sh -s k2s         |  | |
| |  | $ sudo k2s setup           |  | |
| |  |                [Copy]      |  | |
| |  +----------------------------+  | |
| |  Full docs ->                    | |
| +----------------------------------+ |
|                                      |
| [Cloud Nodes]                        |
| +----------------------------------+ |
| |  Cloud nodes - login / status    | |
| +----------------------------------+ |
+--------------------------------------+
```

Key behaviors:
- Input box is always visible. Has value = display (token masked). Empty = placeholder.
- Paste/edit URI, click Save. Auto-parse name (#fragment) and country (&country=).
- Clear = empty input box + Save (clears storage).
- No add/edit/delete buttons. No modal. One input, one save.
- "Full docs" links to `kaitu.io/{locale}/k2/server` via `window._platform.openExternal()`.
- Copy button copies: `curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s && sudo k2s setup`
- Limit: one self-hosted tunnel only.
- Save button disabled when input unchanged from stored URI.
- Invalid URI (no `k2v5://` prefix or no `@`) shows inline error text.

**Terminal style**: Match web homepage hero terminal block — dark background, monospace font, colored prompt. Reuse MUI `Paper` with `sx={{ fontFamily: 'monospace', bgcolor: 'grey.900' }}`.

**Cloud section**: Reuse existing logic from current Tunnels.tsx — show login CTA for guests, "using cloud nodes" for authenticated users.

### 2.3 Dashboard Behavior

**No self-hosted tunnel + not logged in**: No change from current.

**Has self-hosted tunnel + not logged in**:
- Replace the current `EmptyState` (login CTA) with a single self-hosted node card showing name + country flag.
- Below the node card: cloud upgrade CTA ("Want more nodes? Log in for free trial").
- Connect button uses self-hosted URI directly.
- "Self-deploy" text link (currently navigates to `/tunnels`) changes to "Manage node" or similar.

**Has self-hosted tunnel + logged in**:
- `CloudTunnelList` component unchanged (renders cloud nodes with radio selection).
- Below `CloudTunnelList`: render self-hosted node as a separate selectable item with "Self-Hosted" tag.
- Selection state: track whether selected node is cloud or self-hosted via new state:
  ```typescript
  const [selectedSource, setSelectedSource] = useState<'cloud' | 'self_hosted'>('cloud');
  ```
- When self-hosted selected, `handleToggleConnection` uses `selfHostedTunnel.uri` instead of `selectedCloudTunnel?.serverUrl`.

**Connection flow integration in Dashboard.tsx**:

```typescript
// In handleToggleConnection:
let serverUrl: string | undefined;
if (selectedSource === 'self_hosted' && selfHostedTunnel) {
  serverUrl = selfHostedTunnel.uri;  // Already has credentials
} else if (selectedCloudTunnel) {
  serverUrl = await resolveServerUrl(selectedCloudTunnel.serverUrl);
}
const config = buildConnectConfig(serverUrl);
await window._k2.run('up', config);
```

**activeTunnelInfo derivation**: When self-hosted is selected, derive from stored tunnel:
```typescript
const activeTunnelInfo = useMemo(() => {
  if (selectedSource === 'self_hosted' && selfHostedTunnel) {
    const parsed = new URL(selfHostedTunnel.uri.replace('k2v5://', 'https://'));
    return {
      domain: parsed.hostname,
      name: selfHostedTunnel.name,
      country: selfHostedTunnel.country || '',
    };
  }
  // ...existing cloud tunnel logic
}, [selectedSource, selfHostedTunnel, selectedCloudTunnel]);
```

### 2.4 initializeAllStores Update

`webapp/src/main.tsx` or wherever `initializeAllStores()` is defined — add self-hosted store initialization:

```typescript
export async function initializeAllStores() {
  await useLayoutStore.getState().init();
  await useAuthStore.getState().init();
  await useSelfHostedStore.getState().loadTunnel();  // NEW
  await useVPNStore.getState().init();
  await useConfigStore.getState().loadConfig();
}
```

Order: after auth (need platform ready), before vpn (Dashboard reads it).

### 2.5 i18n Keys

New keys needed in `dashboard.json` across all 7 locales (zh-CN primary):

```json
{
  "selfHosted": {
    "inputLabel": "连接 URI",
    "inputPlaceholder": "粘贴 k2v5:// 连接地址",
    "save": "保存",
    "saved": "已保存",
    "invalidUri": "请输入有效的 k2v5:// 地址",
    "invalidUriNoAuth": "URI 缺少认证信息（需要 username:token@）",
    "tag": "自部署",
    "manageNode": "管理节点",
    "deployGuide": "还没有服务器？一键部署",
    "deployGuideDoc": "详细文档",
    "copyCommand": "复制",
    "copied": "已复制",
    "upgradeTitle": "想要更多节点？",
    "upgradeDescription": "登录即可获取全球云端节点，开箱即用",
    "upgradeCta": "免费试用"
  }
}
```

Also in `tunnels` namespace (existing keys for page title etc. can be reused).

## 3. Hop Ports Guide Page

**URL**: `kaitu.io/{locale}/k2/hop-ports`
**Files**: `web/content/zh-CN/k2/hop-ports.md` + `web/content/en-US/k2/hop-ports.md`

**Velite frontmatter**:
```yaml
---
title: 端口跳跃配置指南
date: 2026-03-05
summary: 通过 UDP 端口跳跃提升 QUIC 连接稳定性，防止单端口限速
section: getting-started
order: 4
draft: false
---
```

Content outline:

1. **What is port hopping** — QUIC UDP on single port vulnerable to QoS/blocking. Hopping across UDP port range improves stability.

2. **Prerequisites** — k2s running, 443/tcp+udp open.

3. **How it works** — Client picks random UDP port from hop range. Server needs NAT rules to redirect hop ports to 443. Diagram:
   ```
   Client --[UDP:50042]--> Server firewall --[REDIRECT to :443]--> k2s
   ```

4. **Step 1: Server port redirect rules** (per distro)
   - Ubuntu/Debian (nftables): `nft add rule ... udp dport 50000-50100 redirect to :443` + persist to `/etc/nftables.conf`
   - Ubuntu/Debian (iptables, legacy): `iptables -t nat -A PREROUTING ...` + `apt install iptables-persistent && netfilter-persistent save`
   - CentOS/RHEL/Rocky/AlmaLinux (firewalld): `firewall-cmd --permanent --add-forward-port=port=50000-50100:proto=udp:toport=443 && firewall-cmd --reload`
   - Alpine Linux (iptables): `iptables -t nat -A PREROUTING ... -j REDIRECT --to-port 443` + `rc-update add iptables && /etc/init.d/iptables save`
   - Arch Linux (nftables): same as Ubuntu nftables + `systemctl enable nftables`

5. **Step 2: Firewall allow hop ports**
   - ufw: `ufw allow 50000:50100/udp`
   - firewalld: `firewall-cmd --permanent --add-port=50000-50100/udp && firewall-cmd --reload`
   - iptables: `iptables -A INPUT -p udp --dport 50000:50100 -j ACCEPT`
   - Cloud security groups: AWS / Aliyun / GCP / Azure — add inbound rule UDP 50000-50100 in console

6. **Step 3: Update client URI** — append `&hop=50000-50100` to the k2v5:// URI. Example:
   ```
   k2v5://alice:token@1.2.3.4:443?ech=...&pin=...&hop=50000-50100&country=JP#tokyo
   ```

7. **Verify**
   - Server: `nft list ruleset | grep 50000` or `iptables -t nat -L -n | grep 50000`
   - Client: after connecting, check logs for hop activity

8. **Customization** — default 50000-50100 (101 ports), recommend minimum 50 ports. Range must not conflict with other services.

**Also update**: `web/content/*/k2/server.md` — add a "Next Steps" section at bottom linking to hop-ports guide.

## 4. Install Script Simplification

### 4.1 Short URL for k2s Server Install

Old: `curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s`
New: `curl -fsSL https://kaitu.io/i/k2s | sudo sh`

**Implementation**: `web/public/i/k2s` — standalone shell script with `NAME=k2s` hardcoded. No arguments needed. Same logic as current install.sh k2s branch (platform detect, CDN fallback, SHA256 verify, install to `/usr/local/bin/k2s`).

### 4.2 Remove k2 Client CLI Install

- Delete the `k2` client branch from `install.sh`. The script only served k2s anyway after this change.
- `install.sh` stays as-is for backward compatibility (redirects or shows usage pointing to `/i/k2s`), or can be removed entirely.
- k2 client install is GUI-only: `/install` page auto-downloads .pkg (macOS) / .exe (Windows). No CLI installer.

### 4.3 URL Updates Across Codebase

All references to the old install command must be updated:

| Location | Old | New |
|----------|-----|-----|
| `web/src/app/[locale]/page.tsx` (hero terminal) | `curl -fsSL https://kaitu.io/install.sh \| sudo sh -s k2s` | `curl -fsSL https://kaitu.io/i/k2s \| sudo sh` |
| `web/content/*/k2/server.md` (install section) | same | same |
| `webapp/src/pages/Tunnels.tsx` (deploy guide) | same | same |
| `k2s setup` output (firewall hint) | N/A (new) | uses new URL |
| `web/content/*/k2/hop-ports.md` (prerequisites) | same | same |

### 4.4 `/install` Page — No Changes

`/install` page stays as-is: GUI installer auto-download for desktop (macOS .pkg, Windows .exe) and mobile (iOS App Store, Android APK). No curl command guidance added — curl is for k2s server only.

## 5. Files to Create/Modify

### New files
| File | Purpose |
|------|---------|
| `k2/cmd/k2s/user.go` | `k2s user add/list/del/reset` subcommands |
| `webapp/src/stores/self-hosted.store.ts` | Self-hosted tunnel Zustand store |
| `web/content/zh-CN/k2/hop-ports.md` | Hop ports guide (Chinese) |
| `web/content/en-US/k2/hop-ports.md` | Hop ports guide (English) |
| `web/public/i/k2s` | Standalone k2s server install script (short URL) |

### Modified files
| File | Change |
|------|--------|
| `k2/cmd/k2s/main.go` | Add `case "user": cmdUser(args[1:])` to dispatch switch |
| `k2/server/server.go` | `logConnectionURL()`: add geolocation country, `#hostname` fragment, `k2s user add` hint in setup output. `setupAutoProvision()`: write admin user to users file + set `cfg.Auth.UsersFile` |
| `k2/wire/auth_info.go` | No change needed — `EnsureAuth()` generates `{udid, token}`, works as admin default credentials |
| `web/public/install.sh` | Remove `k2` client branch, keep `k2s` for backward compat (or redirect to `/i/k2s`) |
| `web/src/app/[locale]/page.tsx` | Update hero terminal command to `curl -fsSL https://kaitu.io/i/k2s \| sudo sh` |
| `webapp/src/pages/Tunnels.tsx` | Full rewrite: input box + deploy guide terminal + cloud CTA |
| `webapp/src/pages/Dashboard.tsx` | Add self-hosted tunnel selection: import `useSelfHostedStore`, add `selectedSource` state, modify `handleToggleConnection` for dual source, render self-hosted node below cloud list |
| `webapp/src/components/CloudTunnelList.tsx` | No change — stays cloud-only. Dashboard orchestrates both sources. |
| `webapp/src/stores/config.store.ts` | No change — `buildConnectConfig(serverUrl)` already accepts any URI string |
| `webapp/src/stores/index.ts` | Export `useSelfHostedStore`, add `loadTunnel()` to `initializeAllStores()` |
| `webapp/src/i18n/locales/zh-CN/dashboard.json` | Add `selfHosted.*` keys (~12 keys) |
| `webapp/src/i18n/locales/en-US/dashboard.json` | Add `selfHosted.*` keys |
| `webapp/src/i18n/locales/ja/dashboard.json` | Add `selfHosted.*` keys |
| `webapp/src/i18n/locales/zh-TW/dashboard.json` | Add `selfHosted.*` keys |
| `webapp/src/i18n/locales/zh-HK/dashboard.json` | Add `selfHosted.*` keys |
| `webapp/src/i18n/locales/en-AU/dashboard.json` | Add `selfHosted.*` keys |
| `webapp/src/i18n/locales/en-GB/dashboard.json` | Add `selfHosted.*` keys |
| `web/content/zh-CN/k2/server.md` | Add "Next Steps" section linking to hop-ports guide |
| `web/content/en-US/k2/server.md` | Add "Next Steps" section linking to hop-ports guide |

## 5. Edge Cases & Error Handling

### URI Validation (webapp)
- Must start with `k2v5://` — reject `k2wss://`, `https://`, plain text
- Must contain `@` in userinfo — reject URIs without credentials
- Malformed URL (URL constructor throws) — show "Invalid URI" error
- Empty host — show error
- On save: if validation fails, don't persist, show inline error under input

### k2s user edge cases
- `user add` with existing username — error: "User already exists. Use `k2s user reset <name>` to regenerate token"
- `user del` non-existent user — error: "User not found"
- `user add` when server not running (no connect-url.txt) — error with guidance
- Concurrent file access — atomic write pattern: write to temp file, rename (same approach as `os.WriteFile` with mode 0600)
- Username validation: alphanumeric + dash + underscore, 1-32 chars. Reject empty, spaces, colons (colon is delimiter).

### Dashboard edge cases
- Self-hosted tunnel set but server unreachable — standard VPN error flow (engine error → bridge → UI error state)
- User clears self-hosted tunnel while connected to it — should `_k2.run('down')` first, or at minimum show warning
- Self-hosted tunnel set + cloud login → cloud tunnel auto-selected (self-hosted not auto-connected)
- Token in URI should never appear in logs — `console.debug` calls should mask token

### Feedback page (SubmitTicket) for guests
- Log upload starts immediately on page enter (Rust IPC to S3, no Cloud API auth needed)
- Feedback logs use `feedback-logs/` S3 prefix (vs `service-logs/` for auto-uploads) for higher visibility
- Ticket submission requires Cloud API auth — guest sees "Log in to Submit" button → opens login dialog
- After login: normal submit flow. This is also a cloud conversion touchpoint.

### Storage edge cases
- Storage read fails (corrupt data) — fallback to null tunnel, log warning
- Migration: no migration needed (new key, no prior data)

## 6. Platform Support Matrix

| Platform | Self-hosted support | Notes |
|----------|-------------------|-------|
| Tauri desktop (macOS/Windows) | Yes | `_k2.run('up', config)` via daemon IPC |
| Capacitor mobile (iOS/Android) | Yes | `_k2.run('up', config)` via K2Plugin |
| Standalone web (browser) | Yes* | Requires k2 daemon running on localhost. Same as cloud mode. |

*Standalone web mode relies on daemon HTTP API at `:1777`. Self-hosted URI is passed via `_k2.run('up', config)` which POSTs to daemon. Works if daemon is running.

k2 client does NOT support Linux desktop. k2s server is Linux-only.

## 7. Not In Scope

- k2 client CLI installer (curl) — client is GUI-only (.pkg/.exe via `/install` page)
- Linux desktop client — not supported
- Subscription mode (`TunnelMode = 'subscription'`) — future
- Multiple self-hosted tunnels — single tunnel covers primary use case
- k2s web admin panel — CLI only
- Hop port auto-configuration by k2s — manual firewall setup with guide page
- k2s server-side hop port listening — requires iptables/nftables NAT redirect
- Self-hosted tunnel sharing/export — user copies URI manually

## 8. CI/CD Impact

### No changes needed to existing workflows

- `release-desktop.yml` — builds k2 sidecar as before, webapp changes are just React code
- `ci.yml` — push/PR checks unaffected
- `build-mobile.yml` — uses gomobile appext, no k2s involvement
- `amplify.yml` — auto-serves `web/public/i/k2s` as static file, no config change

### k2 submodule is the blocking dependency

All Go changes (`user.go`, `main.go`, `server.go`) live in the k2 repo (read-only submodule). Implementation order:

1. Implement in `kaitu-io/k2` repo → merge to main
2. Update k2app submodule: `cd k2 && git pull origin main && cd .. && git add k2`
3. Then proceed with webapp changes

### k2s binary publishing (manual, existing)

k2s binaries are published manually via two scripts:
- `scripts/build-k2-standalone.sh` — cross-compile k2s for linux-amd64/arm64, darwin-amd64/arm64
- `scripts/publish-k2.sh` — upload to S3, generate `k2s-cloudfront.latest.json` + `k2s-d0.latest.json` manifests

The `install.sh` (and new `i/k2s`) reads these manifests to discover latest version + checksums. Publishing must be done after k2 submodule has the user.go changes.

### Optional future improvement

Add `.github/workflows/release-k2s.yml` to automate k2s publishing on tags. Not blocking for this design — manual publish works.
