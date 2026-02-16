# Feature: Mobile VPN — Android

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | mobile-vpn-android                       |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-17                               |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: K2Plugin.kt + K2VpnService + gomobile AAR               |

## Overview

Android VPN 全栈实现：K2Plugin.kt（Capacitor 插件）+ K2VpnService（前台服务运行
gomobile Engine）+ AAR 集成 + CI/CD 到 S3 APK 分发。

与 iOS NE 双进程不同，Android 是单进程模型——K2VpnService 和 Capacitor 运行在同一进程，
Engine 直接通过方法调用交互，无需 IPC。

前置依赖：mobile-webapp-bridge（NativeVpnClient + K2Plugin TS 定义）

## Product Requirements

- PR1: K2Plugin.kt 实现完整 Capacitor 插件方法（checkReady, connect, disconnect, getStatus, getVersion, getUDID, getConfig, setRuleMode）
- PR2: K2VpnService 继承 VpnService，在前台服务中运行 gomobile Engine
- PR3: `VpnService.prepare()` 必须使用 Activity context（非 Application context）
- PR4: 前台通知（Android 要求 VPN 前台服务必须显示通知）
- PR5: EventHandler 回调通过 K2Plugin.notifyListeners() 传播到 webapp
- PR6: 首次连接显示 VPN 权限对话框
- PR7: gomobile bind 生成 k2mobile.aar，通过 flatDir 集成
- PR8: CI 构建签名 APK 并上传到 S3

## Technical Decisions

### TD1: 单进程架构

```
App Process (single)
┌──────────────────────────────────────────┐
│ Capacitor + Webapp                       │
│ K2Plugin.kt ──bind──→ K2VpnService       │
│   ← notifyListeners ── EventHandler      │
│                         gomobile Engine   │
└──────────────────────────────────────────┘
```

K2VpnService 运行在 App 进程内。K2Plugin 通过 ServiceConnection bind 到 VpnService，
直接调用 `engine.statusJSON()` 等方法，无需 IPC 序列化。

### TD2: VpnServiceBridge 接口

K2Plugin（在 plugin module）和 K2VpnService（在 app module）存在循环依赖。
通过 `VpnServiceBridge` 接口解耦：

```kotlin
// k2plugin/VpnServiceBridge.kt
interface VpnServiceBridge {
    fun getStatusJSON(): String
    fun stopVpn()
}
```

K2VpnService 实现此接口，K2Plugin 通过接口类型引用 Service。

### TD3: Activity context 关键要求

`VpnService.prepare()` 必须传入 Activity context：
```kotlin
val intent = VpnService.prepare(activity)  // ← activity, NOT context
```
使用 Application context 会导致 `establish()` 在 Android 15+ 返回 null。
这是 Android 15 (API 35) 引入的行为变更。

### TD4: AAR 集成（flatDir 模式）

```gradle
// app/build.gradle
repositories {
    flatDir { dirs 'libs' }
}
dependencies {
    implementation(name: 'k2mobile', ext: 'aar')
}
```

不使用 wrapper module（`api files()`），直接在 app module 用 flatDir 引用 AAR。
简单直接，gomobile 生成的 AAR 包含所有 ABI 的 .so 文件。

### TD5: 前台服务配置

```xml
<service android:name="io.kaitu.K2VpnService"
         android:exported="false"
         android:foregroundServiceType="specialUse"
         android:permission="android.permission.BIND_VPN_SERVICE">
    <intent-filter>
        <action android:name="android.net.VpnService" />
    </intent-filter>
    <property android:name="android.app.PROPERTY_SPECIAL_USE_FGS_SUBTYPE"
              android:value="vpn" />
</service>
```

Android 14+ 要求 `foregroundServiceType`。VPN 使用 `specialUse` 类型 +
`PROPERTY_SPECIAL_USE_FGS_SUBTYPE=vpn` 属性。

### TD6: SDK 版本

| Item | Value |
|------|-------|
| Package ID | `io.kaitu` |
| Min SDK | 24 (Android 7.0) |
| Target SDK | 35 (Android 15) |
| Compile SDK | 35 |
| Java | 17 |
| ABIs | arm64-v8a, armeabi-v7a, x86_64 |

## Acceptance Criteria

- AC1: K2VpnService 以前台服务启动并显示通知
- AC2: `VpnService.Builder.establish()` 提供 fd 给 Engine
- AC3: EventHandler 回调通过 notifyListeners 传播到 webapp
- AC4: 首次连接显示 VPN 权限对话框
- AC5: APK 在 arm64、armv7、x86_64 设备上构建并安装
- AC6: `gomobile bind -target=android` 生成 k2mobile.aar
- AC7: `gradlew assembleRelease` 生成签名 APK
- AC8: CI 构建并上传 APK 到 S3
- AC9: FileProvider 配置正确（用于原生更新 APK 安装）

## Deployment & CI/CD

构建流程：
```
gomobile bind -target=android -androidapi 24 → k2mobile.aar
  → cp 到 mobile/android/app/libs/
  → cap sync android
  → gradlew assembleRelease → signed APK
  → upload to S3 (d.all7.cc/kaitu/android/)
```

CI (`.github/workflows/build-mobile.yml` Android job)：
- Runner: ubuntu-latest
- Android NDK: 26.1
- 触发: v* tag push 或手动 dispatch
- 产物: APK artifact，自动上传 S3

分发：APK 从 kaitu.io 下载（Google Play 暂缓——主要用户在中国，Play 被墙）

## Testing Strategy

- Manual on-device testing: Primary validation — VPN permission dialog and foreground service require real device
- K2Plugin.kt tested via `debug.html` page (mobile-debug feature)
- `adb logcat` for VpnService lifecycle and gomobile Engine logs
- Gradle build verification: `gradlew assembleRelease` must produce signed APK
- Multi-ABI testing: Verify on arm64 (primary) and x86_64 (emulator) devices
- CI validation: `build-mobile.yml` Android job must produce uploadable APK

## Key Files

| File | Lines | Role |
|------|-------|------|
| `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` | ~537 | Capacitor 插件（VPN 控制 + 更新） |
| `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/VpnServiceBridge.kt` | ~16 | 接口解耦 Plugin ↔ Service |
| `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt` | ~150 | VPN 前台服务 + Engine 生命周期 |
| `mobile/android/app/src/main/AndroidManifest.xml` | — | 权限 + Service 声明 + FileProvider |
| `mobile/android/app/build.gradle` | — | AAR 依赖 + SDK 版本 |
| `scripts/build-mobile-android.sh` | — | Android 构建脚本 |
