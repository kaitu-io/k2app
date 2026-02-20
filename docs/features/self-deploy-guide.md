# Feature: Self-Deploy Guide

## Meta

| Field      | Value                              |
|------------|------------------------------------|
| Feature    | self-deploy-guide                  |
| Version    | v1                                 |
| Status     | draft                              |
| Created    | 2026-02-20                         |
| Depends on | config-driven-connect              |

## Version History

| Version | Date       | Summary                                      |
|---------|------------|----------------------------------------------|
| v1      | 2026-02-20 | Initial: guided deploy + URL import + Dashboard integration |

## Overview

Transform the Tunnels page from a "Coming Soon" placeholder into an active self-deploy guide that walks users through deploying k2s on their own server in minutes, then importing the connection URL into the client for immediate use.

**Current state**: Tunnels.tsx shows a static "Coming Soon" card. Self-deploy servers have no data model, no storage, and no integration with the Dashboard server selector.

**Target state**: Tunnels page is a guided deploy tutorial with URL import. Self-deployed servers are stored locally via `_platform.storage`, appear in Dashboard alongside cloud nodes, and can be selected for connection like any cloud tunnel.

## Context

- `k2s run` is production-ready: zero-config mode auto-generates certs/ECH/auth, installs as systemd service, prints a `k2v5://` connect URL
- Install script: `curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s`
- `k2v5://` URL is the complete connection credential — contains host, auth token, ECH config, cert pin
- `buildConnectConfig(serverUrl)` in config.store already accepts an arbitrary server URL
- Dashboard currently only shows cloud tunnels (from API) for authenticated users

## Product Requirements

- **PR1: Deploy Guide** — Tunnels page shows a clear, step-by-step guide for deploying k2s on a VPS. Two steps: install + run, then copy URL. Commands are copy-to-clipboard ready.
- **PR2: URL Import** — Users can paste a `k2v5://` URL to add a self-deployed server. Basic validation (protocol prefix, parseable URL). Auto-extract host as default display name.
- **PR3: Self-Deploy Server List** — Added servers are displayed on the Tunnels page with host, added time, and delete action. Stored locally in `_platform.storage`.
- **PR4: Dashboard Integration** — Self-deployed servers appear in Dashboard as a separate section above cloud tunnels. Selecting one sets it as the active server for connection. No login required.
- **PR5: Connection Flow** — When a self-deployed server is selected, `buildConnectConfig(url)` uses its `k2v5://` URL directly. Same connect/disconnect flow as cloud tunnels. No special handling needed.
- **PR6: No Login Required** — Self-deploy flow is fully usable without authentication. Guest users can deploy, import, and connect. Dashboard shows self-deploy servers even when not logged in.

## Technical Design

### Data Model

```ts
// New file: webapp/src/types/self-deploy.ts
interface SelfDeployServer {
  id: string;        // crypto.randomUUID()
  name: string;      // user-editable, defaults to host from URL
  url: string;       // k2v5://... — full connection URL
  host: string;      // parsed from URL (display only)
  createdAt: number; // Date.now()
}
```

### Storage

Key: `k2.self-deploy.servers`
Value: `SelfDeployServer[]`
Storage: `_platform.storage` (same as config store — persisted per-device)

### New Store: self-deploy.store.ts

```ts
interface SelfDeployState {
  servers: SelfDeployServer[];
  loaded: boolean;
}

interface SelfDeployActions {
  loadServers: () => Promise<void>;
  addServer: (url: string, name?: string) => Promise<SelfDeployServer>;
  removeServer: (id: string) => Promise<void>;
  updateServerName: (id: string, name: string) => Promise<void>;
}
```

- `addServer` validates URL format, parses host, deduplicates by URL, persists
- Store loaded during `initializeAllStores()` chain (after auth, before vpn — non-blocking)

### URL Validation

```ts
function validateK2Url(url: string): { valid: boolean; host?: string; error?: string } {
  const trimmed = url.trim();
  if (!trimmed.startsWith('k2v5://')) {
    return { valid: false, error: 'invalidProtocol' }; // i18n key
  }
  try {
    const parsed = new URL(trimmed.replace('k2v5://', 'https://'));
    return { valid: true, host: parsed.hostname };
  } catch {
    return { valid: false, error: 'invalidUrl' };
  }
}
```

### Page Changes

#### Tunnels.tsx — Full Rewrite

Replace the entire "Coming Soon" placeholder. New structure:

```
┌─────────────────────────────────────┐
│ ← 节点管理                           │
├─────────────────────────────────────┤
│                                     │
│ 📋 自部署节点服务器                    │
│                                     │
│ Step 1: 安装并运行                   │
│ ┌─────────────────────────────────┐ │
│ │ $ curl -fsSL https://dl.k2.52j │ │
│ │ .me/install.sh | sudo sh -s k2s│ │
│ │ $ sudo k2s run          [复制]  │ │
│ └─────────────────────────────────┘ │
│ 首次运行会自动安装为系统服务、         │
│ 生成证书、打印连接地址                │
│                                     │
│ Step 2: 复制连接地址                  │
│ 运行后终端会输出类似这样的地址:        │
│ ┌─────────────────────────────────┐ │
│ │ k2v5://abc:tok@1.2.3.4:443?... │ │
│ └─────────────────────────────────┘ │
│                                     │
│ ─── 导入连接地址 ──────────────────── │
│ ┌─────────────────────────────────┐ │
│ │ 粘贴 k2v5:// 地址...      [添加] │ │
│ └─────────────────────────────────┘ │
│                                     │
│ ─── 我的服务器 ──────────────────── │
│ ┌─────────────────────────────────┐ │
│ │ 🖥 203.0.113.5        2分钟前   │ │
│ │                          [删除] │ │
│ ├─────────────────────────────────┤ │
│ │ 🖥 my-vps.example.com  1天前    │ │
│ │                          [删除] │ │
│ └─────────────────────────────────┘ │
│                                     │
│ ─── 云端节点 ──────────────────────  │
│ (guest: 登录获取云端节点 button)     │
│ (authed: 您正在使用云端节点...)      │
└─────────────────────────────────────┘
```

#### Dashboard.tsx — Self-Deploy Section

Add a `SelfDeployTunnelList` section between CollapsibleConnectionSection and CloudTunnelList:

```
┌─────────────────────────────────────┐
│  [Connection Button]                 │
├─────────────────────────────────────┤
│ 自部署  (only if servers.length > 0) │
│ ┌───────────────────────────────┐   │
│ │ 🖥 203.0.113.5       [自部署]  │   │
│ │ 🖥 my-vps.com        [自部署]  │   │
│ └───────────────────────────────┘   │
│                                     │
│ 云端节点 (if authenticated)          │
│ ┌───────────────────────────────┐   │
│ │ 🇯🇵 Tokyo-1            [云端]  │   │
│ │ 🇺🇸 US-West-1          [云端]  │   │
│ └───────────────────────────────┘   │
│                                     │
│ (guest empty state + self-deploy    │
│  link, unchanged)                   │
└─────────────────────────────────────┘
```

When a self-deploy server is selected:
- `activeTunnelInfo` is set from the self-deploy server (host as name, no country)
- `handleToggleConnection` calls `buildConnectConfig(server.url)` — identical to cloud tunnel flow
- Self-deploy and cloud tunnel selection are mutually exclusive (selecting one deselects the other)

### New Components

1. **`SelfDeployGuide.tsx`** — The instructional steps (install command, URL example). Reusable in Tunnels page.
2. **`SelfDeployUrlInput.tsx`** — URL paste + validate + add. Text field + button.
3. **`SelfDeployServerList.tsx`** — List of added servers with delete. Used in both Tunnels page (full) and Dashboard (compact, selectable).

### i18n

New keys under `dashboard.selfDeploy` namespace (expand existing):

```json
{
  "tunnels.selfDeploy": {
    "title": "自部署节点服务器",
    "step1Title": "安装并运行",
    "step1Command": "curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s && sudo k2s run",
    "step1Desc": "首次运行会自动安装为系统服务、生成证书并输出连接地址。之后可随时运行 k2s run 查看地址。",
    "step2Title": "复制连接地址",
    "step2Desc": "终端会输出类似以下格式的连接地址：",
    "step2Example": "k2v5://token:secret@your-server-ip:443?ech=...&pin=sha256:...",
    "importTitle": "导入连接地址",
    "importPlaceholder": "粘贴 k2v5:// 连接地址",
    "importButton": "添加",
    "importSuccess": "服务器添加成功",
    "myServers": "我的服务器",
    "noServers": "还没有添加服务器",
    "deleteConfirm": "确定要删除服务器 \"{{name}}\" 吗？",
    "invalidProtocol": "地址必须以 k2v5:// 开头",
    "invalidUrl": "地址格式不正确",
    "duplicateUrl": "该服务器已添加",
    "copied": "已复制"
  }
}
```

### Store Initialization

In `initializeAllStores()`:

```ts
export async function initializeAllStores() {
  await useLayoutStore.getState().init();
  await useAuthStore.getState().init();
  await useConfigStore.getState().loadConfig();
  await useSelfDeployStore.getState().loadServers(); // NEW
  // vpn store init continues...
}
```

### Cross-Platform Notes

- **Desktop (Tauri)**: Full flow works. Users have terminal access for k2s install.
- **Mobile (Capacitor)**: Guide is read-only reference (users deploy from computer, paste URL on phone). Consider clipboard auto-detect for URL import.
- **Web/OpenWrt**: Self-deploy guide is visible but less relevant (these users already self-deployed the client). Could hide via feature flag if needed — out of scope for v1.

## Acceptance Criteria

- [ ] **AC1**: Tunnels page shows step-by-step deploy guide with copyable install command
- [ ] **AC2**: User can paste a `k2v5://` URL and add it as a server
- [ ] **AC3**: Invalid URLs show appropriate error messages
- [ ] **AC4**: Added servers persist across app restarts (stored in _platform.storage)
- [ ] **AC5**: Added servers appear in Dashboard server list
- [ ] **AC6**: Selecting a self-deployed server in Dashboard enables connect button
- [ ] **AC7**: Connecting through a self-deployed server works (up/down/status cycle)
- [ ] **AC8**: Servers can be deleted from Tunnels page
- [ ] **AC9**: Duplicate URL import is rejected with message
- [ ] **AC10**: Feature works without login (guest users can deploy + connect)
- [ ] **AC11**: Self-deploy and cloud tunnel selections are mutually exclusive
- [ ] **AC12**: All text is i18n'd (zh-CN primary, en-US secondary)

## Out of Scope (Future)

- Server health monitoring / ping test
- QR code scan for URL import (mobile)
- Deep link handler for `k2v5://` protocol
- Server-side validation of k2v5 URL (actually connect-test before saving)
- Cloud sync of self-deployed servers (keep local-only)
- Edit server URL (delete + re-add is sufficient)
- Token rotation / expiry handling
