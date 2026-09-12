#!/usr/bin/env python3
"""Assert every native library in an APK/AAB is 16 KB page aligned.

Android 15+ devices may run a 16 KB page kernel and Google Play refuses uploads
targeting API 35+ whose .so files are only 4 KB aligned — the dynamic linker
cannot map them, so the app dies at startup rather than degrading.

The alignment comes from `-Wl,-z,max-page-size=16384`, passed to the external
linker by the root Makefile's `appext-android` target. That flag is one careless
edit away from vanishing, and nothing else would notice: the build still
succeeds, CI still goes green, and only the Play Console (or a user on a 16 KB
device) reports it. This gate reads the SHIPPED artifact instead of trusting the
build command.

Parsing is done here rather than by shelling out to readelf/llvm-readelf so the
gate behaves identically on the CI runner and on a macOS dev machine, and so a
missing tool cannot turn into a silent pass.

Usage: check-elf-page-align.py <app.apk|app.aab> [min_align_bytes]
"""
import struct
import sys
import zipfile

MIN_ALIGN = 16384
PT_LOAD = 1


def load_aligns(blob: bytes):
    """Yield p_align of every PT_LOAD segment in a 64-bit little-endian ELF."""
    if blob[:4] != b"\x7fELF":
        raise ValueError("not an ELF file")
    if blob[4] != 2:
        raise ValueError("not ELF64 (32-bit ABIs are not shipped)")
    if blob[5] != 1:
        raise ValueError("not little-endian")
    e_phoff, = struct.unpack_from("<Q", blob, 0x20)
    e_phentsize, e_phnum = struct.unpack_from("<HH", blob, 0x36)
    for i in range(e_phnum):
        off = e_phoff + i * e_phentsize
        p_type, = struct.unpack_from("<I", blob, off)
        if p_type == PT_LOAD:
            p_align, = struct.unpack_from("<Q", blob, off + 0x30)
            yield p_align


def main() -> int:
    if len(sys.argv) < 2:
        print(__doc__.strip().splitlines()[-1], file=sys.stderr)
        return 2
    path = sys.argv[1]
    minimum = int(sys.argv[2]) if len(sys.argv) > 2 else MIN_ALIGN

    print(f"=== ELF page-alignment gate (>= {minimum} bytes) ===")
    print(f"artifact: {path}")

    failures = []
    checked = 0
    with zipfile.ZipFile(path) as z:
        # APK: lib/<abi>/x.so   AAB: base/lib/<abi>/x.so   AAR: jni/<abi>/x.so
        # (the AAR is accepted so the gate can run before the APK is packaged)
        names = [
            n for n in z.namelist()
            if n.endswith(".so") and ("/lib/" in f"/{n}" or "/jni/" in f"/{n}")
        ]
        for name in sorted(names):
            try:
                aligns = sorted(set(load_aligns(z.read(name))))
            except ValueError as exc:
                failures.append(f"{name}: unreadable ({exc})")
                continue
            checked += 1
            if not aligns:
                failures.append(f"{name}: no PT_LOAD segments")
                continue
            worst = min(aligns)
            mark = "OK " if worst >= minimum else "BAD"
            shown = ", ".join(hex(a) for a in aligns)
            print(f"  [{mark}] {name}  LOAD align: {shown}")
            if worst < minimum:
                failures.append(f"{name}: LOAD align {hex(worst)} < {hex(minimum)}")

    # Fail closed: a glob that matches nothing must not read as success.
    if checked == 0:
        print("ERROR: no native libraries found in the artifact.", file=sys.stderr)
        print("       Either the archive layout changed or the wrong file was passed;", file=sys.stderr)
        print("       either way this gate verified nothing, so it fails.", file=sys.stderr)
        return 1

    if failures:
        print(file=sys.stderr)
        print("ERROR: 16 KB page alignment gate FAILED", file=sys.stderr)
        for f in failures:
            print(f"  - {f}", file=sys.stderr)
        print(file=sys.stderr)
        print("Fix: keep -extldflags=-Wl,-z,max-page-size=16384 on the gomobile bind", file=sys.stderr)
        print("     in the root Makefile's appext-android target (or build with NDK r28+).", file=sys.stderr)
        return 1

    print(f"OK: {checked} native librar{'y' if checked == 1 else 'ies'} are >= {minimum}-byte aligned.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
