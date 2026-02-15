#!/usr/bin/env python3
"""
PyInstaller build script for kaitu-signer.

Usage:
    pip install pyinstaller
    python build.py

Output: dist/kaitu-signer.exe
"""

import subprocess
import sys


def main():
    cmd = [
        sys.executable, "-m", "PyInstaller",
        "--onefile",
        "--windowed",
        "--name", "kaitu-signer",
        "--add-data", "config.py;.",
        "--add-data", "aws_client.py;.",
        "--add-data", "signer.py;.",
        "--add-data", "simplisign.py;.",
        "--add-data", "worker.py;.",
        "--hidden-import", "pystray._win32",
        "--hidden-import", "PIL._tkinter_finder",
    ]

    # Add icon if present
    import os
    if os.path.exists("icon.ico"):
        cmd.extend(["--icon", "icon.ico"])

    cmd.append("main.py")

    print(f"Running: {' '.join(cmd)}")
    result = subprocess.run(cmd)
    sys.exit(result.returncode)


if __name__ == "__main__":
    main()
