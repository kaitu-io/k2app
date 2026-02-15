"""
Windows signtool discovery and execution.

Ported from scripts/ci/windows/sign.ps1 and release-desktop.yml signtool logic.
"""

import glob
import logging
import os
import re
import subprocess

from config import CERT_NAME, TIMESTAMP_URL

logger = logging.getLogger("kaitu-signer")

SDK_ROOT = r"C:\Program Files (x86)\Windows Kits\10\bin"


def find_signtool():
    """
    Auto-discover signtool.exe from installed Windows SDK versions.

    Searches SDK_ROOT for x64 signtool.exe, preferring the newest SDK version.
    Returns the full path or None.
    """
    if not os.path.isdir(SDK_ROOT):
        logger.error(f"Windows SDK root not found: {SDK_ROOT}")
        return None

    candidates = []
    for path in glob.glob(os.path.join(SDK_ROOT, "**", "signtool.exe"), recursive=True):
        if "\\x64\\" in path or "/x64/" in path:
            # Extract version from path like .../10.0.22621.0/x64/signtool.exe
            parts = path.replace("/", "\\").split("\\")
            version_str = None
            for part in parts:
                if re.match(r"^\d+\.\d+\.\d+\.\d+$", part):
                    version_str = part
                    break
            candidates.append((version_str or "0.0.0.0", path))

    if not candidates:
        logger.error("signtool.exe not found in any Windows SDK version")
        return None

    # Sort by version descending, pick newest
    candidates.sort(key=lambda x: [int(n) for n in x[0].split(".")], reverse=True)
    best = candidates[0][1]
    logger.info(f"Found signtool: {best}")
    return best


def sign_file(file_path, cert_name=None, timestamp_url=None):
    """
    Sign a file using signtool.

    Args:
        file_path: Path to the file to sign.
        cert_name: Certificate subject name (default from config).
        timestamp_url: RFC 3161 timestamp server URL (default from config).

    Returns:
        True if signing succeeded.

    Raises:
        FileNotFoundError: If signtool.exe not found.
        RuntimeError: If signing fails.
    """
    signtool = find_signtool()
    if not signtool:
        raise FileNotFoundError(
            "signtool.exe not found. Install Windows SDK: "
            "winget install Microsoft.WindowsSDK.10.0.22621"
        )

    cert_name = cert_name or CERT_NAME
    timestamp_url = timestamp_url or TIMESTAMP_URL

    cmd = [
        signtool, "sign",
        "/fd", "SHA256",
        "/tr", timestamp_url,
        "/td", "SHA256",
        "/n", cert_name,
        "/d", "Kaitu Desktop",
        file_path,
    ]

    logger.info(f"Signing: {file_path}")
    logger.debug(f"Command: {' '.join(cmd)}")

    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=120,
    )

    if result.returncode != 0:
        logger.error(f"signtool sign failed (exit {result.returncode})")
        logger.error(f"stdout: {result.stdout}")
        logger.error(f"stderr: {result.stderr}")
        raise RuntimeError(f"signtool sign failed: {result.stderr or result.stdout}")

    logger.info(f"Signed successfully: {file_path}")
    return True


def verify_signature(file_path):
    """
    Verify a file's digital signature.

    Returns True if valid, raises RuntimeError if invalid.
    """
    signtool = find_signtool()
    if not signtool:
        raise FileNotFoundError("signtool.exe not found")

    cmd = [signtool, "verify", "/pa", file_path]
    logger.info(f"Verifying: {file_path}")

    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        timeout=60,
    )

    if result.returncode != 0:
        logger.error(f"signtool verify failed (exit {result.returncode})")
        logger.error(f"stdout: {result.stdout}")
        logger.error(f"stderr: {result.stderr}")
        raise RuntimeError(f"Signature verification failed: {result.stderr or result.stdout}")

    logger.info(f"Signature verified: {file_path}")
    return True
