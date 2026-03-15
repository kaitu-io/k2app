# Log Infrastructure Unification

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Unify log rotation across all platforms (20MB/3/7d/gzip), merge stderr into k2.log, add build-time debug switch, redesign upload to be read-only with per-file dedup.

**Architecture:** (1) Go `config.SetupLogging()` — unified rotation + stderr redirect + debug switch. (2) Platform rotation alignment (Tauri/iOS/Android). (3) Upload redesign — feedback keeps bundle, beta auto-upload changes to per-file PUT with HEAD dedup. (4) Remove all truncation.

**Tech Stack:** Go (lumberjack, slog, syscall), Rust (tauri-plugin-log, reqwest), Swift (K2Plugin, FileManager), Kotlin (K2Plugin, HttpURLConnection), TypeScript (webapp config)

---

## Design Principles

1. **写入者管 rotate** — 每个写日志的进程自己负责轮转
2. **上传者只读** — 上传模块只 copy + 打包，永不 truncate 源文件
3. **构建时 debug 开关** — `grep BUILD_DEBUG_SWITCH` 找到全部 3 处

## Rotation Parameters (All Platforms)

| 参数 | 值 | 理由 |
|------|-----|------|
| MaxSize | 20 MB | debug 阶段写量大，20MB 覆盖 1-2 小时 |
| MaxBackups | 3 | active + 3 backup = 4 文件 |
| MaxAge | 7 天 | 用户反馈通常 1-3 天内 |
| Compress | true | rotated 文件 gzip，5:1 压缩比 |
| 单类型磁盘上限 | ~26 MB | 20MB active + ~6MB (3× compressed) |

## Upload Architecture

两种上传场景，不同策略：

### Feedback Report（用户主动，所有平台）

**保持 bundle 模式。** 扩展 glob 包含 `.gz` 文件，打包为 tar.gz/zip。

S3 key 不变：`{platform}/{version}/{udid}/{date}/logs-{ts}-{feedbackId}.tar.gz`

每次 feedback 有唯一 feedbackId → 天然不重复。

### Beta Auto-Upload（定时 24h，仅 Desktop）

**改为逐文件 PUT + HEAD 去重。**

S3 key 结构：`auto/{udid}/{filename}`

| 文件类型 | S3 key 示例 | 策略 |
|---------|------------|------|
| Active `.log` | `auto/{udid}/k2.log` | 每次 PUT 覆盖（最新快照） |
| Active `.log` | `auto/{udid}/desktop.log` | 每次 PUT 覆盖 |
| Rotated `.gz` | `auto/{udid}/k2-2026-03-15T10-30-00.000.log.gz` | HEAD → 存在则 skip |

去重机制：应用层 HEAD 检查。lumberjack rotated 文件名含精确时间戳 → 文件名唯一 → S3 key 唯一 → HEAD 200 = 已上传 = skip。

---

## Platform Flow Verification (设计后)

### macOS Desktop

```
写入：
  daemon → config.SetupLogging() → lumberjack → /var/log/kaitu/k2.log (20MB/3/7d/gzip)
    ├─ rotate → /var/log/kaitu/k2-2026-03-15T10-30-00.000.log.gz (不可变)
    └─ stderr pipe → lj.Write → k2.log 内（go-deadlock/panic 输出）
  Tauri → ~/Library/Logs/kaitu/desktop.log (20MB/KeepOne)

Feedback upload (log_upload.rs):
  1. glob /var/log/kaitu/ → "k2*.log" + "k2*.log.gz" + "panic-*.log"
     ✓ k2.log (active), k2-*.log.gz (rotated, 不可变), panic-*.log
     ✗ 不再找 k2-stderr.log（已不存在）
  2. glob ~/Library/Logs/kaitu/ → "desktop*log*"
     ✓ desktop.log, desktop.log.1
  3. macOS system logs (log show command)
  4. copy → staging → sanitize → tar.gz
  5. PUT s3://desktop/{ver}/{udid}/{date}/logs-{ts}-{fbId}.tar.gz
  6. 不 truncate ✓
  7. 返回 s3Keys → webapp 注册到 Center API

Beta auto-upload (log_upload.rs, 新逻辑):
  1. 扫描同上目录
  2. Active 文件:
     k2.log → PUT s3://auto/{udid}/k2.log (覆盖) ✓
     desktop.log → PUT s3://auto/{udid}/desktop.log (覆盖) ✓
  3. Rotated .gz 文件:
     k2-2026-03-15T10-30-00.000.log.gz → HEAD s3://auto/{udid}/k2-2026...
       200 → skip ✓
       404 → PUT ✓
  4. 不 truncate ✓
  5. 返回 s3Keys → webapp 注册到 Center API

第二次 auto-upload (24h 后):
  k2.log → PUT 覆盖（新内容）✓
  旧 .gz → HEAD 200 → skip ✓
  新 rotate 的 .gz → HEAD 404 → PUT ✓
  → 不重复传输 ✓
```

### Windows Desktop

```
写入：
  daemon → lumberjack → C:\ProgramData\kaitu\k2.log (20MB/3/7d/gzip)
  rotate → C:\ProgramData\kaitu\k2-*.log.gz
  stderr → pipe → windows.SetStdHandle → lj.Write → k2.log 内
  Tauri → %LOCALAPPDATA%\kaitu\logs\desktop.log

流程与 macOS 完全相同，只是路径不同。✓
```

### iOS

```
写入：
  Go engine → lumberjack → {AppGroup}/logs/k2.log (20MB/3/7d/gzip)
  rotate → {AppGroup}/logs/k2-*.log.gz
  NativeLogger → native.log (20MB truncate-to-0)
  webapp console → webapp.log

Feedback upload (K2Plugin.swift):
  1. 扫描 {AppGroup}/logs/ 目录（改为 glob，不再硬编码 3 个文件名）
     ✓ k2.log, k2-*.log.gz, native.log, webapp.log
  2. .gz 文件作为 binary 直接加入 zip（不做 sanitize — 已压缩）
  3. .log 文件做 sanitize
  4. zip → PUT s3://mobile/{ver}/{udid}/{date}/logs-{ts}-{fbId}.zip
  5. 不 truncate ✓

无 auto-upload（iOS 没有 beta auto-upload 机制）。✓
```

### Android

```
写入：
  Go engine → lumberjack → {filesDir}/logs/k2.log (20MB/3/7d/gzip)
  rotate → {filesDir}/logs/k2-*.log.gz
  NativeLogger → native.log (20MB truncate-to-0)
  webapp console → webapp.log

Feedback upload (K2Plugin.kt):
  与 iOS 相同流程，zip 用 java.util.zip。✓

无 auto-upload。✓
```

---

## Task 1: Go Lumberjack — Unified Rotation + Compression + Debug Switch

**Files:**
- Modify: `k2/config/log.go`

**Step 1: Add constants and update lumberjack**

At the top of `log.go`, after `var LogLevel slog.LevelVar` (line 16), add:

```go
// BuildDebugLogging forces debug-level logging in all builds.    // BUILD_DEBUG_SWITCH
// Set to false for production release.
const BuildDebugLogging = true
```

Update `parseLevel()` (line 20):

```go
func parseLevel(level string) slog.Level {
	if level == "" && BuildDebugLogging { // BUILD_DEBUG_SWITCH
		return slog.LevelDebug
	}
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
```

Update lumberjack config in `SetupLogging()` (line 131):

```go
lj := &lumberjack.Logger{
	Filename:   logPath,
	MaxSize:    20, // MB (was 50)
	MaxBackups: 3,
	MaxAge:     7,  // days (was 30)
	Compress:   true,
}
```

**Step 2: Run tests**

```bash
cd k2 && go test ./config/... -v
cd k2 && go test ./engine/... -v
cd k2 && go test ./daemon/... -v
```

**Step 3: Commit**

```
feat(config): unify log rotation + build-time debug switch

Lumberjack: 20MB/3 backups/7 days/gzip (was 50MB/3/30d/no compress).
BuildDebugLogging constant forces debug level in all builds.
grep BUILD_DEBUG_SWITCH to find all switch locations.
```

---

## Task 2: Go — Redirect os.Stderr into k2.log

**Files:**
- Modify: `k2/config/log.go` (add stderr redirect in `SetupLogging`)
- Create: `k2/config/log_stderr_unix.go`
- Create: `k2/config/log_stderr_windows.go`
- Create: `k2/config/log_stderr_mobile.go`
- Modify: `k2/daemon/service_darwin.go` (remove `StandardErrorPath`)

**Context:** go-deadlock writes directly to `os.Stderr` via `fmt.Fprintln(os.Stderr, ...)`. On macOS, launchd captures this to a separate `k2-stderr.log`. On Windows service mode, stderr is closed — output lost entirely. By redirecting stderr into the lumberjack writer via pipe, deadlock reports appear inline in k2.log on all platforms.

**Step 1: Create `k2/config/log_stderr_unix.go`**

```go
//go:build (darwin && !ios) || linux

package config

import (
	"os"
	"syscall"
)

// redirectStderrFD redirects file descriptor 2 to the given file.
// Captures runtime panics and CGo crashes that bypass Go's os.Stderr.
func redirectStderrFD(f *os.File) error {
	return syscall.Dup2(int(f.Fd()), 2)
}
```

**Step 2: Create `k2/config/log_stderr_windows.go`**

```go
//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

func redirectStderrFD(f *os.File) error {
	return windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
}
```

**Step 3: Create `k2/config/log_stderr_mobile.go`**

```go
//go:build ios || android

package config

import "os"

func redirectStderrFD(_ *os.File) error {
	return nil // Mobile: stderr visible in Xcode console / logcat
}
```

**Step 4: Add stderr redirect in `SetupLogging()` (log.go)**

In the `default:` case of the output switch, after the lumberjack `Logger{}` block (after current line 136) and BEFORE `if cfg.ExtraWriter != nil` (current line 137), insert:

```go
		// Redirect os.Stderr to lumberjack so go-deadlock output,
		// runtime panics, and CGo crashes appear inline in k2.log.
		stderrR, stderrW, pipeErr := os.Pipe()
		if pipeErr == nil {
			os.Stderr = stderrW
			_ = redirectStderrFD(stderrW) // also redirect fd 2 for runtime/CGo
			ljForStderr := lj             // capture for goroutine
			go func() {
				buf := make([]byte, 4096)
				for {
					n, err := stderrR.Read(buf)
					if n > 0 {
						ljForStderr.Write(buf[:n]) //nolint:errcheck
					}
					if err != nil {
						return
					}
				}
			}()
			prevCloseFn := closeFn
			closeFn = func() {
				stderrW.Close()
				stderrR.Close()
				prevCloseFn()
			}
		}
```

**Step 5: Remove `StandardErrorPath` from macOS launchd plist**

In `k2/daemon/service_darwin.go`, find the plist template string and delete the `StandardErrorPath` line:

```xml
<!-- DELETE THIS LINE: -->
<key>StandardErrorPath</key>
<string>/var/log/kaitu/k2-stderr.log</string>
```

**Step 6: Run tests and build**

```bash
cd k2 && go build ./cmd/k2
cd k2 && go test ./config/... -v
cd k2 && go test ./daemon/... -v
```

**Step 7: Commit**

```
feat(config): redirect stderr into k2.log via pipe

go-deadlock reports and runtime panics now appear inline in k2.log.
Removes k2-stderr.log on macOS (launchd StandardErrorPath deleted).
Fixes Windows service mode where stderr was lost entirely.
Platform-specific fd redirect: dup2 (unix), SetStdHandle (windows),
no-op (mobile — Xcode/logcat already capture stderr).
```

---

## Task 3: Tauri Desktop Log — Align Rotation + Force Debug

**Files:**
- Modify: `desktop/src-tauri/src/main.rs:68-87`

**Step 1: Update tauri-plugin-log config**

Find the `tauri_plugin_log::Builder::new()` chain. Replace the level conditional with hardcoded debug, and update rotation:

```rust
tauri_plugin_log::Builder::new()
    .level(log::LevelFilter::Debug) // BUILD_DEBUG_SWITCH — change to conditional for production
    .timezone_strategy(tauri_plugin_log::TimezoneStrategy::UseLocal)
    .filter(|metadata| !metadata.target().starts_with("reqwest"))
    .target(tauri_plugin_log::Target::new(
        tauri_plugin_log::TargetKind::Folder {
            path: log_dir,
            file_name: Some("desktop".into()),
        },
    ))
    .max_file_size(20_000_000) // 20 MB (was 50 MB)
    .rotation_strategy(tauri_plugin_log::RotationStrategy::KeepOne) // was KeepAll
    .build()
```

Remove the `let level = if channel::is_beta_early() ...` block that was computing the level.

**Step 2: Verify build**

```bash
cd desktop/src-tauri && cargo check
```

**Step 3: Commit**

```
feat(desktop): align desktop.log rotation + force debug level

20MB/KeepOne (was 50MB/KeepAll). Log level always debug
(BUILD_DEBUG_SWITCH). Removes beta-conditional level logic.
```

---

## Task 4: iOS NativeLogger — Lower Threshold

**Files:**
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/NativeLogger.swift`

**Step 1: Find and change max file size**

```swift
private let maxFileSize: UInt64 = 20 * 1024 * 1024 // 20 MB (was 50 MB)
```

**Step 2: Commit**

```
feat(ios): lower NativeLogger max size to 20MB
```

---

## Task 5: Android NativeLogger — Lower Threshold

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/NativeLogger.kt`

**Step 1: Find and change max file size**

```kotlin
private const val MAX_FILE_SIZE = 20L * 1024 * 1024 // 20 MB (was 50 MB)
```

**Step 2: Commit**

```
feat(android): lower NativeLogger max size to 20MB
```

---

## Task 6: Webapp — Force Debug Log Level

**Files:**
- Modify: `webapp/src/stores/config.store.ts:128-146`

**Step 1: Simplify `buildConnectConfig()` log level**

Replace the current logic in both code paths. The current code (lines 135 and 139-140):

```typescript
// Current (object params path, line 135):
result.log = { ...result.log, level: params.isBeta ? 'debug' : (params.logLevel || 'info') };

// Current (legacy path, lines 139-140):
const isBeta = window._platform?.updater?.channel === 'beta';
result.log = { ...result.log, level: isBeta ? 'debug' : (localStorage.getItem('k2_log_level') || 'info') };
```

Replace both with:

```typescript
// BUILD_DEBUG_SWITCH — change to conditional logic for production
result.log = { ...result.log, level: 'debug' };
```

Remove unused variables: `isBeta`, `params.isBeta`, `params.logLevel`, the `localStorage.getItem('k2_log_level')` call.

Also remove `logLevel` from `ConnectConfigParams` interface (line 45) if it's no longer used.

**Step 2: Run tests**

```bash
cd webapp && npx vitest run
```

**Step 3: Commit**

```
feat(webapp): force debug log level (BUILD_DEBUG_SWITCH)
```

---

## Task 7: Desktop Upload — Expand Glob + Remove Truncation

**Files:**
- Modify: `desktop/src-tauri/src/log_upload.rs`

**Step 1: Expand glob to include `.gz` files**

In `collect_logs_to_staging()` (line 145), after the existing `k2*.log` glob block, add a second glob for `.gz` files:

```rust
// k2*.log.gz — lumberjack rotated compressed files
if let Ok(entries) = glob::glob(&dir.join("k2*.log.gz").to_string_lossy()) {
    for entry in entries.flatten() {
        let dest_name = format!(
            "{}--{}",
            label,
            entry.file_name().unwrap_or_default().to_string_lossy()
        );
        // .gz files: copy as binary, no sanitization needed (already compressed)
        if copy_file_to_staging(&entry, &staging_dir.join(&dest_name)) {
            staged_count += 1;
            // Don't add to source_files — we never truncate
        }
    }
}
```

**Step 2: Remove k2-stderr.log collection**

Delete lines 174-182 (the `k2-stderr.log` block):

```rust
// DELETE THIS BLOCK:
// k2-stderr.log — macOS launchd stderr capture
let stderr_log = dir.join("k2-stderr.log");
if stderr_log.exists() {
    let dest_name = format!("{}--k2-stderr.log", label);
    if copy_file_to_staging(&stderr_log, &staging_dir.join(&dest_name)) {
        source_files.push(stderr_log);
        staged_count += 1;
    }
}
```

**Step 3: Skip sanitization for `.gz` files**

In `sanitize_staging_dir()` (line 288), skip `.gz` files:

```rust
fn sanitize_staging_dir(dir: &Path) -> Result<(), String> {
    let entries = std::fs::read_dir(dir).map_err(|e| format!("Read staging dir: {}", e))?;
    for entry in entries.flatten() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        // Skip binary/compressed files
        let name = path.file_name().unwrap_or_default().to_string_lossy();
        if name.ends_with(".gz") {
            continue;
        }
        // ... rest unchanged
    }
    Ok(())
}
```

**Step 4: Remove truncation from `upload_service_log_command`**

Replace lines 503-523 with:

```rust
/// IPC: Upload service logs (runs in blocking thread).
#[tauri::command]
pub async fn upload_service_log_command(
    params: UploadLogParams,
) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || {
        let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());
        upload_service_log(params, udid)
    })
    .await
    .map_err(|e| format!("Task failed: {}", e))
}
```

**Step 5: Delete `truncate_log_files_in_dir` function entirely (lines 525-554)**

**Step 6: Remove `source_files` tracking from `collect_logs_to_staging`**

Since we no longer truncate, `source_files` is unused. Change the return type from `(PathBuf, Vec<PathBuf>)` to just `PathBuf`, and remove all `source_files.push()` calls. Update the caller in `upload_service_log()` to match.

**Step 7: Update tests**

- Delete `test_truncate_log_files_in_dir` (line 719-755)
- Delete `test_truncate_log_files_nonexistent_dir` (line 757-760)
- Update `test_staging_and_tar_gz_roundtrip` to include a `.gz` file

**Step 8: Verify build and tests**

```bash
cd desktop/src-tauri && cargo check
cd desktop/src-tauri && cargo test
```

**Step 9: Commit**

```
refactor(desktop): expand glob to .gz + remove truncation

Feedback upload now includes lumberjack rotated .gz files.
Removes k2-stderr.log collection (file no longer exists).
Skips sanitization for .gz files (binary, already compressed).
Upload is now fully read-only — no truncation of source files.
```

---

## Task 8: Desktop Beta Auto-Upload — Per-File PUT with HEAD Dedup

**Files:**
- Modify: `desktop/src-tauri/src/log_upload.rs` (add per-file upload functions)
- Modify: `webapp/src/services/beta-auto-upload.ts` (no change needed — calls same IPC)

**Context:** Beta auto-upload runs every 24h from webapp (`beta-auto-upload.ts`) via `window._platform.uploadLogs({ reason: 'beta-auto-upload' })`. Currently uses the same bundle approach as feedback. We change it to per-file PUT for dedup.

**Step 1: Add HEAD check helper**

```rust
/// Check if an S3 object exists.
fn s3_object_exists(s3_key: &str) -> bool {
    let url = format!("{}/{}", S3_BUCKET_URL, s3_key);
    let client = match reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build() {
        Ok(c) => c,
        Err(_) => return false,
    };
    match client.head(&url).send() {
        Ok(resp) => resp.status().is_success(),
        Err(_) => false,
    }
}
```

**Step 2: Add per-file upload function**

```rust
/// Upload a single file to S3 with the given key.
fn upload_file_to_s3(s3_key: &str, file_path: &Path) -> Result<(), String> {
    let data = std::fs::read(file_path)
        .map_err(|e| format!("Read {}: {}", file_path.display(), e))?;
    let content_type = if s3_key.ends_with(".gz") {
        "application/gzip"
    } else {
        "text/plain; charset=utf-8"
    };
    let url = format!("{}/{}", S3_BUCKET_URL, s3_key);
    let client = reqwest::blocking::Client::builder()
        .timeout(std::time::Duration::from_secs(REQUEST_TIMEOUT_SECS))
        .build()
        .map_err(|e| format!("HTTP client: {}", e))?;
    let response = client
        .put(&url)
        .header("Content-Type", content_type)
        .body(data)
        .send()
        .map_err(|e| format!("S3 PUT: {}", e))?;
    if !response.status().is_success() {
        return Err(format!("S3 PUT failed: {}", response.status()));
    }
    Ok(())
}
```

**Step 3: Add auto-upload orchestrator**

```rust
/// Per-file upload for beta auto-upload.
/// Active .log files: always PUT (overwrite). Rotated .gz: HEAD → skip if exists.
fn upload_auto(udid: &str) -> UploadLogResult {
    log::info!("[log_upload] Starting auto-upload (per-file mode)");
    let mut uploaded: Vec<UploadedFileInfo> = Vec::new();

    for dir in get_all_service_log_dirs() {
        if !dir.exists() { continue; }
        upload_auto_dir(&dir, udid, &mut uploaded);
    }
    let desktop_dir = crate::get_desktop_log_dir();
    if desktop_dir.exists() {
        upload_auto_dir(&desktop_dir, udid, &mut uploaded);
    }

    if uploaded.is_empty() {
        return UploadLogResult { success: false, error: Some("No log files found".into()), s3_keys: None };
    }
    log::info!("[log_upload] Auto-upload complete: {} files", uploaded.len());
    UploadLogResult { success: true, error: None, s3_keys: Some(uploaded) }
}

fn upload_auto_dir(dir: &Path, udid: &str, uploaded: &mut Vec<UploadedFileInfo>) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    for entry in entries.flatten() {
        let path = entry.path();
        if !path.is_file() { continue; }
        let name = path.file_name().unwrap_or_default().to_string_lossy().to_string();

        // Only process log-related files
        let is_log = name.ends_with(".log") && (name.starts_with("k2") || name.starts_with("desktop") || name.starts_with("panic"));
        let is_gz = name.ends_with(".log.gz") && name.starts_with("k2");
        if !is_log && !is_gz { continue; }

        // Skip empty files
        if let Ok(meta) = std::fs::metadata(&path) {
            if meta.len() == 0 { continue; }
        }

        let s3_key = format!("auto/{}/{}", udid, name);

        if is_gz {
            // Rotated .gz: HEAD check — skip if already uploaded
            if s3_object_exists(&s3_key) {
                log::debug!("[log_upload] Auto: skip (exists) {}", name);
                continue;
            }
            // Upload .gz as-is (already compressed, no sanitization)
            match upload_file_to_s3(&s3_key, &path) {
                Ok(()) => {
                    log::info!("[log_upload] Auto: uploaded {}", s3_key);
                    uploaded.push(UploadedFileInfo { name: name.clone(), s3_key });
                }
                Err(e) => log::warn!("[log_upload] Auto: failed {}: {}", name, e),
            }
        } else {
            // Active .log: sanitize → always PUT (overwrite)
            match std::fs::read_to_string(&path) {
                Ok(content) => {
                    let sanitized = sanitize_content(&content);
                    let tmp = std::env::temp_dir().join(format!("kaitu-auto-{}", name));
                    if std::fs::write(&tmp, sanitized).is_ok() {
                        match upload_file_to_s3(&s3_key, &tmp) {
                            Ok(()) => {
                                log::info!("[log_upload] Auto: uploaded {}", s3_key);
                                uploaded.push(UploadedFileInfo { name: name.clone(), s3_key });
                            }
                            Err(e) => log::warn!("[log_upload] Auto: failed {}: {}", name, e),
                        }
                        let _ = std::fs::remove_file(&tmp);
                    }
                }
                Err(_) => {} // Binary/unreadable — skip
            }
        }
    }
}
```

**Step 4: Route auto-upload in the command handler**

In `upload_service_log_command()`, branch on reason:

```rust
#[tauri::command]
pub async fn upload_service_log_command(
    params: UploadLogParams,
) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || {
        let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());
        if params.reason == "beta-auto-upload" {
            upload_auto(&udid)
        } else {
            upload_service_log(params, udid)
        }
    })
    .await
    .map_err(|e| format!("Task failed: {}", e))
}
```

**Step 5: Verify build and tests**

```bash
cd desktop/src-tauri && cargo check
cd desktop/src-tauri && cargo test
```

**Step 6: Commit**

```
feat(desktop): per-file auto-upload with HEAD dedup

Beta auto-upload now uploads files individually:
- Active .log files: sanitized then PUT (overwrite latest snapshot)
- Rotated .gz files: HEAD check, skip if already exists on S3
Feedback uploads unchanged (bundle tar.gz approach).
```

---

## Task 9: iOS Upload — Expand Glob + Remove Truncation

**Files:**
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift:556-660`

**Step 1: Change file discovery from hardcoded to directory scan**

Replace the current hardcoded `logTypes = ["k2", "native", "webapp"]` loop (lines 576-611) with a directory scan:

```swift
// 2. Scan log directory for all log files
var sourceFiles: [URL] = []
let fileManager = FileManager.default
if let contents = try? fileManager.contentsOfDirectory(
    at: logsDir, includingPropertiesForKeys: [.fileSizeKey],
    options: [.skipsHiddenFiles]
) {
    for fileURL in contents {
        let name = fileURL.lastPathComponent
        guard name.hasSuffix(".log") || name.hasSuffix(".log.gz") else { continue }
        guard let attrs = try? fileURL.resourceValues(forKeys: [.fileSizeKey]),
              let size = attrs.fileSize, size > 0 else { continue }

        if name.hasSuffix(".log.gz") {
            // .gz files: copy as binary (no sanitization — already compressed)
            let destFile = stagingDir.appendingPathComponent(name)
            try? fileManager.copyItem(at: fileURL, to: destFile)
            diagLog("uploadLogs: \(name) included (binary, size=\(size))")
        } else {
            // .log files: read as UTF-8, sanitize
            guard let content = try? String(contentsOf: fileURL, encoding: .utf8),
                  !content.isEmpty else {
                diagLog("uploadLogs: \(name) exists (size=\(size)) but empty or unreadable")
                continue
            }
            diagLog("uploadLogs: \(name) included (size=\(size), contentLen=\(content.count))")
            let sanitized = self.sanitizeLogs(content)
            let destFile = stagingDir.appendingPathComponent(name)
            try sanitized.write(to: destFile, atomically: true, encoding: .utf8)
        }
        sourceFiles.append(fileURL)
    }
}
```

**Step 2: Delete the post-upload truncation block (lines 634-644)**

Delete entirely:

```swift
// DELETE: lines 634-644
// 5. Truncate source files ...
// ... truncateFile(atOffset: 0) ...
// ... webappLogHandle?.seekToEndOfFile() ...
```

**Step 3: Commit**

```
refactor(ios): scan log dir for all files + remove truncation

Upload now includes lumberjack rotated .gz files via directory scan.
.gz files copied as binary (no UTF-8 sanitization).
Upload is fully read-only — no truncation of source files.
```

---

## Task 10: Android Upload — Expand Glob + Remove Truncation

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt:575-660`

**Step 1: Change file discovery from hardcoded to directory scan**

Replace the hardcoded `logTypes = listOf("k2", "native", "webapp")` loop (lines 598-610):

```kotlin
// 2. Scan log directory for all log files
val sourceFiles = mutableListOf<File>()
dir.listFiles()?.filter {
    it.isFile && (it.name.endsWith(".log") || it.name.endsWith(".log.gz"))
}?.forEach { logFile ->
    if (logFile.length() == 0L) return@forEach

    if (logFile.name.endsWith(".log.gz")) {
        // .gz files: copy as binary (no sanitization)
        val destFile = File(stagingDir, logFile.name)
        logFile.copyTo(destFile, overwrite = true)
        Log.d(TAG, "uploadLogs: ${logFile.name} included (binary, size=${logFile.length()})")
    } else {
        // .log files: read as UTF-8, sanitize
        val content = try { logFile.readText() } catch (e: Exception) { return@forEach }
        if (content.isEmpty()) return@forEach
        val sanitized = sanitizeLogContent(content)
        File(stagingDir, logFile.name).writeText(sanitized)
        Log.d(TAG, "uploadLogs: ${logFile.name} included (size=${logFile.length()})")
    }
    sourceFiles.add(logFile)
}
```

**Step 2: Delete the post-upload truncation block (lines 636-639)**

Delete:

```kotlin
// DELETE: lines 636-639
// 5. Truncate source files
// for (logFile in sourceFiles) { ... setLength(0) ... }
```

**Step 3: Commit**

```
refactor(android): scan log dir for all files + remove truncation

Upload now includes lumberjack rotated .gz files via directory scan.
.gz files copied as binary (no sanitization).
Upload is fully read-only — no truncation of source files.
```

---

## Task 11: Update Documentation

**Files:**
- Modify: `CLAUDE.md` (root)
- Modify: `k2/CLAUDE.md` (if needed)

**Step 1: Update root CLAUDE.md Key Conventions**

Add:

```markdown
- **Log rotation (unified)**: All platforms: 20MB/3 backups/7 days/gzip compressed. Go lumberjack (`config.SetupLogging`), Tauri plugin-log (20MB/KeepOne), iOS/Android NativeLogger (20MB truncate-to-0). Upload modules are read-only — never truncate source files.
- **stderr → k2.log**: `config.SetupLogging()` redirects `os.Stderr` into lumberjack via pipe. go-deadlock reports and runtime panics appear inline in k2.log. No separate `k2-stderr.log` file. Platform-specific: `dup2` (unix), `SetStdHandle` (windows), no-op (mobile).
- **Build-time debug switch**: `grep BUILD_DEBUG_SWITCH` finds all 3 locations: Go (`config.BuildDebugLogging`), Rust (`main.rs` level), TS (`config.store.ts` level). Set to debug for beta phase. Change to info/false for production.
- **Upload dedup (desktop auto-upload)**: Active `.log` files PUT to `auto/{udid}/{filename}` (overwrite = latest snapshot). Rotated `.log.gz` files use HEAD check before PUT — filename contains timestamp → unique → skip if exists. Feedback uploads use bundle tar.gz with unique feedbackId key — no dedup needed.
```

Remove or update:
- References to `k2-stderr.log` and `StandardErrorPath`
- References to truncation after upload
- Old rotation parameters (50MB/30d)

**Step 2: Commit**

```
docs: update CLAUDE.md with unified log infrastructure
```

---

## Dependency Graph

```
Task 1 (Go rotation + debug)      ─── independent
Task 2 (Go stderr redirect)       ─── independent
Task 3 (Tauri rotation + debug)   ─── independent
Task 4 (iOS NativeLogger 20MB)    ─── independent
Task 5 (Android NativeLogger 20MB)─── independent
Task 6 (Webapp debug switch)      ─── independent
Task 7 (Desktop glob + no trunc)  ─── independent
Task 8 (Desktop auto-upload dedup)─── depends on Task 7 (uses expanded glob)
Task 9 (iOS glob + no trunc)      ─── independent
Task 10 (Android glob + no trunc) ─── independent
Task 11 (Documentation)           ─── after all others
```

**Parallelizable groups:**
- Group A (Go): Tasks 1, 2
- Group B (Platform alignment): Tasks 3, 4, 5, 6
- Group C (Desktop upload): Task 7 → 8
- Group D (Mobile upload): Tasks 9, 10
- Group E (Docs): Task 11

Groups A, B, C, D are fully independent.
