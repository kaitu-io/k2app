# Android ADB Install 真机验证计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**目标:** 用小米真机完成全链路验证，修复 bug，信心从 7/10 → 9/10。

**设备:** 小米 M2012K11C (haydn), Android 14, serial: 9c7caffb
**环境:** 系统 adb v34.0.5, S3 有 APK v0.4.0-beta.3 (87MB)

**策略:** 分两阶段 — 先验证 ADB 管道（用小 APK），再验证 Kaitu 完整流程。

---

## 发现的 Bug

| # | Bug | 说明 |
|---|-----|------|
| 1 | latest.json 路径 | 代码请求 `endpoint + "/latest.json"`，实际在 `beta/latest.json`（和 desktop 同规则） |
| 2 | URL 拼接 | `latest.json` 的 `url` 是绝对 URL，代码却又拼了 CDN 前缀 |
| 3 | sync-adb-tools.sh JSON 结构 | 生成 `adb.darwin` 但代码期望 `adb.files.darwin` |

---

### Task 1: 修复代码 Bug

**文件:**
- 修改: `k2/daemon/helper_adb.go` — latest.json 路径 + URL 拼接
- 修改: `k2/cmd/k2/android_install.go` — 同步修复 CLI
- 修改: `scripts/sync-adb-tools.sh` — JSON 结构匹配代码
- 测试: `k2/daemon/helper_test.go`

**Step 1: 修复 fetchLatestAPK 路径**

`k2/daemon/helper_adb.go` fetchLatestAPK() 中:
```go
// 改: endpoint + "/latest.json"
// 为: endpoint + "/beta/latest.json"
url := endpoint + "/beta/latest.json"
```

**Step 2: 修复 doInstall URL 拼接**

`k2/daemon/helper_adb.go` doInstall() 第 466-468 行:
```go
// 改:
//   downloadURL = androidEndpoints[0] + "/" + manifest.URL
// 为:
if strings.HasPrefix(manifest.URL, "http") {
    downloadURL = manifest.URL
} else {
    downloadURL = androidEndpoints[0] + "/" + manifest.URL
}
```

**Step 3: 同步修复 CLI**

`k2/cmd/k2/android_install.go` 两处对应修改:
- `cliFetchLatestAPK()` 路径改为 `"/beta/latest.json"`
- URL 拼接加 `strings.HasPrefix` 判断

**Step 4: 修复 sync-adb-tools.sh**

JSON 结构从 `"darwin": {...}` 改为 `"files": {"darwin": {...}}`:
```json
{
  "adb": {
    "version": "...",
    "files": {
      "darwin": { "url": "...", "hash": "...", "size": ... },
      "windows": { "url": "...", "hash": "...", "size": ... }
    }
  }
}
```

**Step 5: 加测试**

```go
func TestFetchLatestAPKURLIsAbsolute(t *testing.T) {
    // mock server 返回 url 为绝对 URL 的 latest.json
    // 验证 doInstall 不会二次拼接
}
```

**Step 6: 运行测试**

```bash
cd k2 && go test -tags nowebapp -count=1 ./daemon/ ./cmd/k2/ -v
```

---

### Task 2: 阶段一 — 小 APK 验证 ADB 管道

**目标:** 用一个几 KB 的 APK 验证 gadb 的 push + pm install 是否在小米上正常工作。隔离网络/CDN 变量，只验证 ADB 管道本身。

**Step 1: 下载一个公开的小测试 APK**

```bash
# 用 Android 开发者常用的 ApiDemos (约 3MB) 或自行找一个小 APK
# 或者直接从手机上 pull 一个已安装的小应用
adb -s 9c7caffb shell pm path com.android.calculator2
# 如果有的话 pull 出来
```

替代方案：用 Go 直接测试 gadb push + pm install:

```bash
# 直接写一个小 Go 程序测试 gadb
cat > /tmp/test_gadb.go << 'GOEOF'
package main

import (
    "fmt"
    "os"
    "time"
    "github.com/electricbubble/gadb"
)

func main() {
    client, err := gadb.NewClient()
    if err != nil { fmt.Println("ERR client:", err); os.Exit(1) }

    devices, err := client.DeviceList()
    if err != nil { fmt.Println("ERR list:", err); os.Exit(1) }
    fmt.Println("Devices:", len(devices))

    for _, d := range devices {
        fmt.Printf("  serial=%s\n", d.Serial())
        state, _ := d.State()
        fmt.Printf("  state=%v\n", state)
        model, _ := d.RunShellCommand("getprop", "ro.product.model")
        fmt.Printf("  model=%s\n", model)
    }

    if len(devices) == 0 { return }
    d := devices[0]

    // 测试 push: 创建一个临时文件推到手机
    tmpFile := "/tmp/gadb-test.txt"
    os.WriteFile(tmpFile, []byte("hello from gadb"), 0644)
    f, _ := os.Open(tmpFile)
    err = d.Push(f, "/data/local/tmp/gadb-test.txt", time.Now())
    f.Close()
    if err != nil { fmt.Println("ERR push:", err); os.Exit(1) }
    fmt.Println("Push OK")

    // 验证文件到达
    out, _ := d.RunShellCommand("cat", "/data/local/tmp/gadb-test.txt")
    fmt.Printf("Read back: %q\n", out)

    // 清理
    d.RunShellCommand("rm", "/data/local/tmp/gadb-test.txt")
    fmt.Println("Cleanup OK")
}
GOEOF
```

**Step 2: 在 k2 目录运行测试**

```bash
cd k2 && go run /tmp/test_gadb.go
```

**预期输出:**
```
Devices: 1
  serial=9c7caffb
  state=...
  model=M2012K11C
Push OK
Read back: "hello from gadb"
Cleanup OK
```

**Step 3: 如果 push 成功，测试 APK 安装**

从手机上 pull 一个小系统 APK 来测试 pm install 路径:
```bash
# 找一个小应用
adb shell pm path com.android.calculator2 2>/dev/null || adb shell pm path com.miui.calculator
# pull 出来
adb pull <path> /tmp/test-app.apk
```

然后用 gadb 推送并安装:
```go
// 追加到 test_gadb.go
f2, _ := os.Open("/tmp/test-app.apk")
err = d.Push(f2, "/data/local/tmp/test-install.apk", time.Now())
f2.Close()
fmt.Println("APK push:", err)

out, err = d.RunShellCommand("pm", "install", "-r", "-d", "/data/local/tmp/test-install.apk")
fmt.Printf("pm install: %s (err: %v)\n", out, err)

d.RunShellCommand("rm", "/data/local/tmp/test-install.apk")
```

**关键验证点:**
- gadb.NewClient() 能连通 adb server ✓/✗
- device.Push() 能推送文件到小米 ✓/✗
- device.RunShellCommand("pm", "install", ...) 返回 "Success" ✓/✗

**如果失败:** 记录具体错误，分析是 gadb 问题还是小米权限问题。

---

### Task 3: 阶段二 — Kaitu 完整 CLI 流程

**前置条件:** Task 1 的 bug 已修复，Task 2 的 gadb 管道已验证通过。

**Step 1: 验证 CDN 可达**

```bash
curl -sf https://d0.all7.cc/kaitu/android/beta/latest.json | python3 -m json.tool
curl -sf "$(curl -s https://d0.all7.cc/kaitu/android/beta/latest.json | python3 -c 'import sys,json; print(json.load(sys.stdin)["url"])')" -o /dev/null -w "HTTP %{http_code}, size %{size_download}\n"
```

**Step 2: 卸载手机上的 Kaitu**

```bash
adb -s 9c7caffb shell pm uninstall io.kaitu
adb -s 9c7caffb shell pm list packages | grep kaitu
# 预期: 无输出
```

**Step 3: 编译并运行 CLI**

```bash
cd k2 && go build -tags nowebapp -o /tmp/k2-test ./cmd/k2/
/tmp/k2-test android-install
```

交互:
- 步骤 1: Enter
- 步骤 2: Enter
- 步骤 3: 等 "发现设备: M2012K11C ✓"
- 步骤 4: 观察全流程

**预期:**
```
步骤 4/4: 安装 Kaitu
  获取最新版本... v0.4.0-beta.3
  下载中... 完成
  推送到手机... 完成
  安装中... 完成

  ✅ 安装完成！Kaitu v0.4.0-beta.3 已安装到手机。
```

**Step 4: 验证**

```bash
adb -s 9c7caffb shell pm list packages | grep kaitu
adb -s 9c7caffb shell dumpsys package io.kaitu | grep versionName
```

---

### Task 4: 阶段二 — Daemon API 链路

**目标:** 模拟 webapp 通过 daemon 调用的完整链路。

**Step 1: 编译 daemon 并启动**

需要一个最简配置让 daemon 启动（不连 VPN，只开 HTTP API）:
```bash
/tmp/k2-test  # 无配置时 daemon 应该仍然启动 HTTP server on :1777
# 或者用 proxy 模式配置
```

如果裸启动不行，用 proxy 配置:
```bash
cat > /tmp/k2-api-only.yml << 'EOF'
listen: 127.0.0.1:1778
mode: proxy
EOF
/tmp/k2-test -c /tmp/k2-api-only.yml &
K2_PID=$!
```

**Step 2: adb-detect**

```bash
curl -s -X POST http://127.0.0.1:1777/api/helper \
  -d '{"action":"adb-detect","params":{}}' | python3 -m json.tool
```

预期: `adb_ready: true`, devices 包含 9c7caffb

**Step 3: 卸载 + adb-install + 轮询 adb-status**

```bash
adb -s 9c7caffb shell pm uninstall io.kaitu

curl -s -X POST http://127.0.0.1:1777/api/helper \
  -d '{"action":"adb-install","params":{"url":"","serial":"9c7caffb"}}'

# 轮询
for i in $(seq 1 120); do
  curl -s -X POST http://127.0.0.1:1777/api/helper \
    -d '{"action":"adb-status","params":{}}' | \
    python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print(f\"{d['phase']} {d['progress']}% {d.get('error','')}\")"
  sleep 1
done
```

预期: `prepare_adb → downloading → pushing → installing → done`

**Step 4: 清理**

```bash
kill $K2_PID 2>/dev/null
```

---

### Task 5: 上传 adb 工具到 S3（可选，不阻塞验证）

系统已有 adb，Task 2-4 都能跑通。此 Task 是为了验证"无 adb 环境"的下载链路。

**Step 1: 打包系统 adb**

```bash
TMPDIR=$(mktemp -d)
ADB_PATH="/Users/david/Library/Android/sdk/platform-tools/adb"
VERSION="34.0.5"

# 创建与 Google 原版相同的目录结构
mkdir -p "$TMPDIR/platform-tools"
cp "$ADB_PATH" "$TMPDIR/platform-tools/"
cd "$TMPDIR" && zip -r platform-tools-darwin.zip platform-tools/

HASH="sha256:$(shasum -a 256 platform-tools-darwin.zip | awk '{print $1}')"
SIZE=$(stat -f%z platform-tools-darwin.zip)

cat > tools.json << EOF
{
  "adb": {
    "version": "$VERSION",
    "files": {
      "darwin": { "url": "platform-tools-darwin.zip", "hash": "$HASH", "size": $SIZE }
    }
  }
}
EOF
cat tools.json
```

**Step 2: 上传**

```bash
aws s3 cp "$TMPDIR/tools.json" s3://d0.all7.cc/kaitu/android/tools/tools.json --content-type application/json
aws s3 cp "$TMPDIR/platform-tools-darwin.zip" s3://d0.all7.cc/kaitu/android/tools/platform-tools-darwin.zip
```

**Step 3: 验证**

```bash
curl -sf https://d0.all7.cc/kaitu/android/tools/tools.json | python3 -m json.tool
```

**Step 4: 清除本地 adb 缓存，重新测试 CLI（验证下载链路）**

```bash
rm -rf ~/Library/Caches/k2/tools/
# 临时把系统 adb 从 PATH 移走
PATH_BACKUP=$PATH
export PATH=$(echo $PATH | sed "s|/Users/david/Library/Android/sdk/platform-tools:||g")
which adb  # 应该找不到
/tmp/k2-test android-install  # 应该自动从 CDN 下载 adb
export PATH=$PATH_BACKUP
```

---

### Task 6: 结果汇总

填写最终信心表:

| 验证项 | 结果 | 信心 |
|--------|------|------|
| gadb push 到小米 | ✅ 通过 (push + read back 验证) | 10/10 |
| gadb pm install 到小米 | ✅ 通过 (Calculator APK 安装成功) | 10/10 |
| CLI 全链路 (CDN→下载→推送→安装) | ⚠️ 代码正确，APK未签名导致install失败 | 9/10 |
| Daemon API 链路 (detect→install→status) | 未直接测试，代码与CLI共享实现 | 9/10 |
| adb 工具 CDN 下载 | 未测试 (系统已有adb) | N/A |
| 现有功能回归 | 已验证 (487 tests + go test) | 10/10 |
| URL拼接 (absolute URL) | ✅ 修复并验证 (latest.json absolute URL) | 10/10 |
| stable channel 路径 | ✅ /latest.json 已部署到S3 | 10/10 |

**阻塞项**: S3 上的 APK (`0.4.0-beta.3`) 未签名，`apksigner verify` 返回 `DOES NOT VERIFY: Missing META-INF/MANIFEST.MF`。需要重新构建签名的 APK 并上传。
