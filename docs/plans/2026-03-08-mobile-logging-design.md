# Mobile Logging & Log Upload Design

## Overview

为手机端（iOS + Android）实现完整的三层日志系统：Go engine 日志、原生 VPN 服务日志、Webapp 日志，全部持久化到文件，并支持上传到 S3。

## Architecture

```
┌─────────────────────────────────────────────────┐
│                Webapp (WebView)                  │
│  console.log/warn/error → 拦截 → 批量缓冲       │
│         ↓ K2Plugin.appendLogs()                  │
├─────────────────────────────────────────────────┤
│            K2Plugin (Swift / Kotlin)             │
│  • appendLogs()  — webapp 日志 → webapp.log      │
│  • NativeLogger  — 自身关键事件 → native.log     │
│  • uploadLogs()  — 收集3文件 → gzip → S3 PUT    │
├─────────────────────────────────────────────────┤
│         Go Engine (appext / gomobile)            │
│  • EngineConfig.LogDir → k2.log                  │
├─────────────────────────────────────────────────┤
│                   文件系统                        │
│  iOS:  {AppGroup}/logs/k2.log                    │
│        {AppGroup}/logs/native.log                │
│        {AppGroup}/logs/webapp.log                │
│  Android: {filesDir}/logs/k2.log                 │
│           {filesDir}/logs/native.log             │
│           {filesDir}/logs/webapp.log             │
│  每文件上限 50MB，上传后清理                       │
└─────────────────────────────────────────────────┘
```

## File Locations

### iOS
- App Group container: `group.io.kaitu`
- Log directory: `{AppGroup}/logs/`
- NE 进程和主 App 共享访问

### Android
- Log directory: `{filesDir}/logs/`
- VPN Service 和主 App 同进程，无跨进程问题

## K2Plugin New Methods

### appendLogs

批量写入 webapp 日志到 `webapp.log`。

```typescript
appendLogs(options: {
  entries: Array<{
    level: 'debug' | 'info' | 'warn' | 'error';
    message: string;
    timestamp: number;  // Date.now() ms
  }>;
}): Promise<void>;
```

原生实现：
- 格式化每条为 `[ISO8601] [LEVEL] message\n`
- FileHandle/FileOutputStream 追加写入 `webapp.log`
- 写入前检查文件大小，超过 50MB 则截断（清空后继续写）

### uploadLogs

收集所有日志文件，脱敏、压缩、上传到 S3。

```typescript
uploadLogs(options: {
  email?: string;
  reason: string;       // 'user_feedback_report' | 'beta-auto-upload'
  feedbackId?: string;
  platform?: string;
  version?: string;
}): Promise<{
  success: boolean;
  error?: string;
  s3Keys?: Array<{ name: string; s3Key: string }>;
}>;
```

返回值与桌面端 `IPlatform.uploadLogs` 完全一致，webapp 层零改动。

原生实现流程：
1. 枚举 `{logs}/` 目录下所有 `.log` 文件
2. 逐文件：读取内容 → 正则脱敏（token, password, Bearer, X-K2-Token）→ gzip 压缩
3. PUT 到 `https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com`
4. S3 key: `feedback-logs/{udid}/{YYYY}/{MM}/{DD}/{logType}-{HHMMSS}-{feedbackId}.log.gz`（有 feedbackId 时）或 `service-logs/{udid}/{YYYY}/{MM}/{DD}/{logType}-{HHMMSS}-{uuid8}.log.gz`（自动上传时）
5. 全部上传成功后删除本地日志文件
6. 返回 `{ success: true, s3Keys: [...] }`

脱敏正则（与桌面端 log_upload.rs 对齐）：
- `token["\s:=]+["']?[A-Za-z0-9._-]+` → `token: [REDACTED]`
- `password["\s:=]+["']?[^\s"']+` → `password: [REDACTED]`
- `Bearer\s+[A-Za-z0-9._-]+` → `Bearer [REDACTED]`
- `X-K2-Token["\s:=]+["']?[A-Za-z0-9._-]+` → `X-K2-Token: [REDACTED]`

## Layer 1: Go Engine Logging

改动最小。在 VPN 服务启动 engine 时设置 `LogDir`：

### iOS (PacketTunnelProvider.swift)
```swift
let logsDir = containerURL.appendingPathComponent("logs")
try? FileManager.default.createDirectory(at: logsDir, withIntermediateDirectories: true)
engineCfg.logDir = logsDir.path
```

### Android (K2VpnService.kt)
```kotlin
val logsDir = File(filesDir, "logs").also { it.mkdirs() }
engineCfg.logDir = logsDir.absolutePath
```

Go engine 内部已实现：当 `LogDir != ""` 时写入 `{LogDir}/k2.log`。

## Layer 2: Native Logging (NativeLogger)

封装 `NativeLogger` 单例，关键 VPN 事件写入 `native.log`。

### 记录的事件
- VPN 连接/断开
- VPN 状态变化
- Engine 启动/停止
- 错误和异常
- 网络变化事件（onNetworkChanged）
- 配置加载
- TUN 设备创建
- S3 上传状态

### iOS 实现 (NativeLogger.swift)
```swift
final class NativeLogger {
    static let shared = NativeLogger()
    private var fileHandle: FileHandle?
    private let queue = DispatchQueue(label: "io.kaitu.native-logger")
    private let maxSize: UInt64 = 50 * 1024 * 1024  // 50MB

    func setup(logsDir: URL) {
        let path = logsDir.appendingPathComponent("native.log")
        FileManager.default.createFile(atPath: path.path, contents: nil)
        fileHandle = FileHandle(forWritingAtPath: path.path)
        fileHandle?.seekToEndOfFile()
    }

    func log(_ level: String, _ message: String) {
        queue.async { [weak self] in
            guard let fh = self?.fileHandle else { return }
            // 检查大小，超 50MB 截断
            if fh.offsetInFile > self?.maxSize ?? 0 {
                fh.truncateFile(atOffset: 0)
            }
            let line = "[\(ISO8601DateFormatter().string(from: Date()))] [\(level)] \(message)\n"
            fh.write(line.data(using: .utf8)!)
        }
    }
}
```

### Android 实现 (NativeLogger.kt)
```kotlin
object NativeLogger {
    private var file: File? = null
    private val maxSize = 50L * 1024 * 1024  // 50MB

    fun setup(logsDir: File) {
        file = File(logsDir, "native.log").also { it.createNewFile() }
    }

    fun log(level: String, message: String) {
        val f = file ?: return
        if (f.length() > maxSize) f.writeText("")  // 截断
        val line = "[${java.time.Instant.now()}] [$level] $message\n"
        f.appendText(line)
    }
}
```

### 插入点
在 K2Plugin、PacketTunnelProvider、K2VpnService 的关键位置替换或补充 `print()`/`Log.d()` 为 `NativeLogger.shared.log()`。

## Layer 3: Webapp Logging (JS → K2Plugin)

### JS 层拦截 (capacitor-k2.ts)

```typescript
function setupConsoleInterceptor() {
  const original = {
    log: console.log, info: console.info,
    warn: console.warn, error: console.error, debug: console.debug,
  };

  let buffer: Array<{ level: string; message: string; timestamp: number }> = [];
  let flushTimer: ReturnType<typeof setTimeout> | null = null;

  const flush = () => {
    if (buffer.length === 0) return;
    const entries = buffer;
    buffer = [];
    if (flushTimer) { clearTimeout(flushTimer); flushTimer = null; }
    K2Plugin.appendLogs({ entries }).catch(() => {});
  };

  const scheduleFlush = () => {
    if (!flushTimer) flushTimer = setTimeout(flush, 3000);
  };

  const intercept = (level: string, originalFn: Function) => (...args: any[]) => {
    originalFn.apply(console, args);
    buffer.push({ level, message: args.map(String).join(' '), timestamp: Date.now() });
    if (level === 'error' || level === 'warn' || buffer.length >= 50) {
      flush();
    } else {
      scheduleFlush();
    }
  };

  console.log = intercept('info', original.log);
  console.info = intercept('info', original.info);
  console.warn = intercept('warn', original.warn);
  console.error = intercept('error', original.error);
  console.debug = intercept('debug', original.debug);

  // App 进入后台时 flush
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') flush();
  });
}
```

### 原生端 appendLogs 实现
- 接收 entries 数组
- 格式化为 `[ISO8601] [LEVEL] message\n`
- FileHandle 追加写入 `webapp.log`
- 50MB 上限检查

## Capacitor Bridge Integration

### capacitor-k2.ts 改动

```typescript
const capacitorPlatform: IPlatform = {
  // ... 现有字段不变

  uploadLogs: async (params) => {
    return await K2Plugin.uploadLogs({
      email: params.email ?? undefined,
      reason: params.reason,
      feedbackId: params.feedbackId,
      platform: params.platform,
      version: params.version,
    });
  },
};
```

现有 webapp 代码（SubmitTicket.tsx、beta-auto-upload.ts）无需改动，因为它们已经通过 `window._platform.uploadLogs?.()` 调用。

## iOS ObjC Method Registration

`K2Plugin.m` 需要新增注册：

```objc
CAP_PLUGIN_METHOD(appendLogs, CAPPluginReturnPromise);
CAP_PLUGIN_METHOD(uploadLogs, CAPPluginReturnPromise);
```

## File Size Management

- 每个日志文件上限 50MB
- 无轮转，无备份
- 写入前检查大小，超限则截断清空后继续写
- 上传成功后删除文件
- 三文件总上限约 150MB

## S3 Upload Details

- Endpoint: `https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com`
- Method: HTTP PUT（presigned URL 或直接 PUT，与桌面端对齐）
- Content-Encoding: gzip
- Key 格式:
  - 用户反馈: `feedback-logs/{udid}/{YYYY}/{MM}/{DD}/{logType}-{HHMMSS}-{feedbackId}.log.gz`
  - 自动上传: `service-logs/{udid}/{YYYY}/{MM}/{DD}/{logType}-{HHMMSS}-{uuid8}.log.gz`
- logType 值: `k2`, `native`, `webapp`

## Backend Registration

上传成功后，JS 层已有的逻辑会调用 `POST /api/user/device-log` 注册到数据库（SubmitTicket.tsx 和 beta-auto-upload.ts），无需改动后端。

## Changes Summary

| 文件 | 改动 |
|------|------|
| `mobile/plugins/k2-plugin/src/definitions.ts` | 新增 `appendLogs()`, `uploadLogs()` 方法定义 |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | 实现 `appendLogs()`, `uploadLogs()` |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.m` | 注册新方法 |
| `mobile/plugins/k2-plugin/ios/Plugin/NativeLogger.swift` | 新文件 — 原生日志单例 |
| `mobile/plugins/k2-plugin/android/.../K2Plugin.kt` | 实现 `appendLogs()`, `uploadLogs()` |
| `mobile/plugins/k2-plugin/android/.../NativeLogger.kt` | 新文件 — 原生日志单例 |
| `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift` | 设置 `engineCfg.logDir`，加 NativeLogger 调用 |
| `mobile/android/.../K2VpnService.kt` | 设置 `engineCfg.logDir`，加 NativeLogger 调用 |
| `webapp/src/services/capacitor-k2.ts` | 加 console 拦截器，实现 `_platform.uploadLogs` |
