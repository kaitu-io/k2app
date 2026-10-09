#!/usr/bin/env bash
# Tests for scripts/check-macos-minos.sh — run on macOS (needs vtool/lipo/clang).
# Builds throwaway Mach-O binaries with known minos values into a fake .app and
# asserts the gate accepts / rejects them.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GATE="$SCRIPT_DIR/check-macos-minos.sh"
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

FAILED=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; FAILED=1; }

echo 'int main(void){return 0;}' > "$T/m.c"
mk() { # mk <out> <arch> <minos>
  clang -arch "$2" -mmacosx-version-min="$3" -o "$1" "$T/m.c"
}

# make_app <dir> <LSMinimumSystemVersion> ; binaries are added by the caller
make_app() {
  mkdir -p "$1/Contents/MacOS"
  cat > "$1/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>LSMinimumSystemVersion</key><string>$2</string></dict></plist>
EOF
}

expect() { # expect <ok|fail> <name> <app>
  if bash "$GATE" "$3" >"$T/out" 2>&1; then got=ok; else got=fail; fi
  if [ "$got" = "$1" ]; then pass "$2"; else fail "$2 (expected $1, got $got)"; cat "$T/out"; fi
}

# 1. all binaries at the floor → ok
A="$T/ok.app"; make_app "$A" 12.0
mk "$A/Contents/MacOS/k2app" arm64 12.0
mk "$T/k2a" arm64 12.0; mk "$T/k2x" x86_64 12.0
lipo -create -output "$A/Contents/MacOS/k2" "$T/k2a" "$T/k2x"
expect ok "universal + thin binaries at floor" "$A"

# 2. the 0.4.7–0.4.13 bug: sidecar stamped with the build host's macOS
A="$T/host.app"; make_app "$A" 12.0
mk "$A/Contents/MacOS/k2app" arm64 12.0
mk "$A/Contents/MacOS/k2" arm64 26.0
expect fail "sidecar minos 26.0 above 12.0 floor" "$A"

# 3. only ONE slice of a universal binary is too new → still rejected
A="$T/slice.app"; make_app "$A" 12.0
mk "$A/Contents/MacOS/k2app" arm64 12.0
mk "$T/s1" arm64 12.0; mk "$T/s2" x86_64 26.0
lipo -create -output "$A/Contents/MacOS/k2" "$T/s1" "$T/s2"
expect fail "x86_64 slice too new in universal binary" "$A"

# 3b. same, but the too-new slice is the one lipo lists second (arm64)
A="$T/slice2.app"; make_app "$A" 12.0
mk "$A/Contents/MacOS/k2app" arm64 12.0
mk "$T/s3" arm64 26.0; mk "$T/s4" x86_64 12.0
lipo -create -output "$A/Contents/MacOS/k2" "$T/s3" "$T/s4"
expect fail "arm64 slice too new in universal binary" "$A"

# 4. numeric compare, not string compare: 12.4 > 12.10 is false
A="$T/num.app"; make_app "$A" 12.10
mk "$A/Contents/MacOS/k2app" arm64 12.4
expect ok "minos 12.4 under floor 12.10 (numeric compare)" "$A"

# 4b. a minor-version overshoot is still an overshoot
A="$T/minor.app"; make_app "$A" 12.0
mk "$A/Contents/MacOS/k2app" arm64 12.4
expect fail "minos 12.4 above floor 12.0 (minor version counts)" "$A"

# 5. no LSMinimumSystemVersion → refuse rather than silently pass
A="$T/noplist.app"; mkdir -p "$A/Contents/MacOS"
echo '<?xml version="1.0"?><plist version="1.0"><dict/></plist>' > "$A/Contents/Info.plist"
mk "$A/Contents/MacOS/k2app" arm64 12.0
expect fail "missing LSMinimumSystemVersion" "$A"

# 6. a bundle with no Mach-O at all → refuse (the gate checked nothing)
A="$T/empty.app"; make_app "$A" 12.0
echo hi > "$A/Contents/MacOS/readme.txt"
expect fail "no Mach-O files found" "$A"

# 7. Mach-O nested outside Contents/MacOS (e.g. a dylib in Frameworks) is checked too
A="$T/nested.app"; make_app "$A" 12.0
mk "$A/Contents/MacOS/k2app" arm64 12.0
mkdir -p "$A/Contents/Frameworks"
clang -arch arm64 -mmacosx-version-min=26.0 -dynamiclib -o "$A/Contents/Frameworks/libx.dylib" "$T/m.c"
expect fail "too-new dylib under Contents/Frameworks" "$A"

exit $FAILED
