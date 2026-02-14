# Framework Gotchas

Platform-specific issues and workarounds discovered during implementation.

---

## WebKit Mixed Content Blocking on macOS (2026-02-14, k2app-rewrite)

**Problem**: macOS WebKit blocks `https://` → `http://` requests even for loopback (127.0.0.1, localhost).

**Symptom**:
- Tauri default origin: `https://tauri.localhost`
- k2 daemon: `http://127.0.0.1:1777`
- Console error: "Blocked loading mixed active content"

**Root cause**:
- WebKit enforces mixed content policy strictly
- Chromium (Windows) allows loopback mixed content as exception
- WebKit has no such exception

**Solution**: tauri-plugin-localhost
- Changes webapp origin from `https://tauri.localhost` to `http://localhost:{port}`
- HTTP→HTTP calls are allowed (no mixed content)
- Tauri IPC (`window.__TAURI__`) still works over HTTP

**Configuration**:
```rust
// desktop/src-tauri/src/main.rs
use tauri_plugin_localhost::Builder;

tauri::Builder::default()
    .plugin(Builder::new(1420).build())  // Serve webapp on http://localhost:1420
    .run(tauri::generate_context!())
```

**Security consideration**:
- localhost port is accessible to other local processes
- k2 daemon already exposes :1777 to localhost (CORS protected)
- Security model unchanged (same attack surface as daemon)

**Validation**:
- Integration test: fetch `/ping` from webview on macOS succeeds
- No console errors in production build

**Related**: Linux WebKitGTK has same issue, same solution works.

---

## Vite Dev Proxy vs Production baseUrl (2026-02-14, k2app-rewrite)

**Problem**: HttpVpnClient needs different baseUrl in dev vs production.

**Dev mode**:
- Webapp: `http://localhost:1420` (Vite dev server)
- Daemon: `http://127.0.0.1:1777`
- Solution: Vite proxy `/api/*` and `/ping` to daemon
- HttpVpnClient uses relative URLs: `fetch('/api/core')`

**Production**:
- Webapp: `http://localhost:{port}` (Tauri localhost plugin)
- Daemon: `http://127.0.0.1:1777`
- No proxy available (Tauri serves static files)
- HttpVpnClient must use absolute URLs: `fetch('http://127.0.0.1:1777/api/core')`

**Implementation**:
```typescript
export class HttpVpnClient implements VpnClient {
  private readonly baseUrl: string;

  constructor() {
    this.baseUrl = import.meta.env.DEV ? '' : 'http://127.0.0.1:1777';
  }

  private async coreRequest(action: string, params?: any) {
    const resp = await fetch(`${this.baseUrl}/api/core`, { ... });
    return resp.json();
  }
}
```

**Vite config**:
```typescript
// webapp/vite.config.ts
export default defineConfig({
  server: {
    port: 1420,
    proxy: {
      '/api': 'http://127.0.0.1:1777',
      '/ping': 'http://127.0.0.1:1777',
    },
  },
});
```

**Why it works**:
- `import.meta.env.DEV` is compile-time constant (true in dev, false in prod build)
- Vite proxy is dev-only feature (not available in production)
- Relative URLs in dev avoid CORS preflight (same-origin after proxy)
- Absolute URLs in prod required because no proxy exists

**Validation**:
- Dev mode: `make dev` → API calls work, HMR works
- Production: `make build-macos` → API calls work

---

## Tauri Version Reference from Parent package.json (2026-02-14, k2app-rewrite)

**Problem**: Tauri `version` field must match root `package.json` version for updater.

**Naive approach** (doesn't work):
```json
{
  "version": "0.4.0"  // Hardcoded, drifts from package.json
}
```

**Correct approach**:
```json
{
  "version": "../../package.json"  // Tauri resolves reference at build time
}
```

**How it works**:
- Tauri CLI reads `tauri.conf.json`
- Sees string ending in `.json`
- Resolves path relative to config file
- Reads `version` field from referenced JSON
- Uses that version in build

**Benefits**:
- Single source of truth (root package.json)
- No build script needed to sync versions
- Tauri native feature (no custom tooling)
- Works in CI without special steps

**Gotcha**: Path must be relative from `desktop/src-tauri/tauri.conf.json` to root, hence `../../package.json` not `../package.json`.

**Validation**:
- `make build-macos` → DMG has version matching package.json
- Updater sees correct version for semver comparison

---

## Zustand Store Initialization with Async VpnClient (2026-02-14, k2app-rewrite)

**Problem**: Zustand stores are synchronous, but `VpnClient.checkReady()` is async.

**Anti-pattern** (doesn't work):
```typescript
export const useVpnStore = create<VpnStore>((set) => ({
  ready: await getVpnClient().checkReady(),  // ❌ await not allowed here
}));
```

**Solution**: Separate initialization action
```typescript
export const useVpnStore = create<VpnStore>((set) => ({
  ready: null,  // Initial state
  init: async () => {
    const client = getVpnClient();
    const ready = await client.checkReady();
    set({ ready });
    if (ready.ready) {
      client.subscribe((event) => { /* ... */ });
    }
  },
}));
```

**Usage in React**:
```typescript
function App() {
  const init = useVpnStore((s) => s.init);

  useEffect(() => {
    init();  // Call async init on mount
  }, []);

  // ...
}
```

**Why it works**:
- Store creation is synchronous (no async in `create()`)
- `init()` action is async (can await inside action)
- React `useEffect` handles async action call
- Store updates via `set()` trigger re-renders

**Validation**:
- `webapp/src/stores/__tests__/vpn.store.test.ts` — init() tests
- ServiceReadiness component tests with store init flow

---

## Git Submodule in Monorepo Workspace (2026-02-14, k2app-rewrite)

**Problem**: k2 is git submodule, but yarn workspaces doesn't support submodules by default.

**Symptom**:
- `yarn install` ignores `k2/` directory
- k2 has its own `go.mod`, not a yarn package
- Workspace array includes non-existent packages

**Solution**: Only include actual yarn packages in workspaces
```json
{
  "workspaces": ["webapp", "desktop"]  // NOT "k2"
}
```

**Why**:
- k2 is Go module, not Node.js package (no package.json in k2/)
- Yarn workspaces expects package.json in each workspace
- k2 is built via Makefile (`cd k2 && go build`), not yarn
- Submodule initialization via `git submodule update --init`, not yarn

**Build flow**:
```makefile
build-k2:
    cd k2 && go build -tags nowebapp -o ../desktop/src-tauri/binaries/k2
```

**Validation**:
- `yarn install` succeeds without errors
- `git clone --recursive` initializes k2 submodule
- `make build-k2` compiles k2 binary successfully

---
