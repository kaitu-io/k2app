# P1: Embed wintun.dll in k2 Binary (Windows)

## Background

sing-tun requires wintun.dll on Windows (gVisor stack is Linux-only). Currently
the old kaitu project bundles wintun.dll as a Tauri resource file, which couples
the DLL distribution to the app layer.

k2 should own the wintun.dll dependency — download it during `go generate` and
embed it in the binary, just like the webapp embedding pattern in `cloud/`.

## Design

### New package: `wintun/`

```
k2/
└── wintun/
    ├── embed_windows.go       # //go:build windows — embeds and extracts DLL
    ├── embed_other.go         # //go:build !windows — no-op
    ├── gen.go                 # //go:build ignore — downloads wintun zip
    ├── wintun.dll             # downloaded by gen.go (gitignored)
    └── .gitignore             # ignore wintun.dll
```

### gen.go — Download

```go
//go:build ignore

package main

// Downloads wintun-0.14.1.zip from wintun.net,
// extracts the amd64 DLL to ./wintun.dll.

const wintunURL = "https://www.wintun.net/builds/wintun-0.14.1.zip"
const wintunSHA256 = "07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51"

// 1. Download zip
// 2. Verify SHA-256
// 3. Extract wintun/bin/amd64/wintun.dll → ./wintun.dll
// 4. Clean up zip
```

### embed_windows.go — Embed + Extract

```go
//go:build windows

package wintun

import (
    _ "embed"
    "os"
    "path/filepath"
    "sync"
)

//go:generate go run gen.go

//go:embed wintun.dll
var dllBytes []byte

var once sync.Once
var dllPath string

// EnsureExtracted extracts wintun.dll next to the running executable
// and returns the directory path. This must be called before sing-tun
// creates a TUN adapter. The DLL is extracted once per process.
func EnsureExtracted() (string, error) {
    var err error
    once.Do(func() {
        exe, e := os.Executable()
        if e != nil {
            err = e
            return
        }
        dir := filepath.Dir(exe)
        target := filepath.Join(dir, "wintun.dll")

        // Skip if already exists and same size.
        if info, e := os.Stat(target); e == nil && info.Size() == int64(len(dllBytes)) {
            dllPath = dir
            return
        }

        if e := os.WriteFile(target, dllBytes, 0o644); e != nil {
            err = e
            return
        }
        dllPath = dir
    })
    return dllPath, err
}
```

### embed_other.go — No-op on non-Windows

```go
//go:build !windows

package wintun

// EnsureExtracted is a no-op on non-Windows platforms.
func EnsureExtracted() (string, error) {
    return "", nil
}
```

### Usage in k2 daemon startup

```go
// daemon/daemon.go or cmd/k2/main.go
import "github.com/kaitu-io/k2/wintun"

func main() {
    // Extract wintun.dll before any TUN operations.
    if _, err := wintun.EnsureExtracted(); err != nil {
        log.Fatalf("failed to extract wintun: %v", err)
    }
    // ... start daemon
}
```

### Why "next to executable" works

sing-tun uses `windows.NewLazyDLL("wintun.dll")` which follows Windows DLL
search order — application directory is searched first. By extracting wintun.dll
next to the k2 binary, it's found automatically without any configuration.

## Build Integration

```makefile
# Makefile
generate:
	go generate ./wintun/  # downloads wintun.dll if missing
	go generate ./cloud/   # downloads webapp if URL set

build-windows:
	GOOS=windows GOARCH=amd64 go build -tags nowebapp -o k2.exe ./cmd/k2
```

## .gitignore

```
wintun/wintun.dll
```

The DLL is ~400KB and downloaded on demand, not checked into git.

## Impact on k2app

k2app no longer needs to bundle wintun.dll as a Tauri resource.
The k2 binary handles it internally on Windows.

Remove from k2app:
- `desktop/src-tauri/resources/wintun.dll`
- `tauri.bundle.conf.json` resources entry for wintun
