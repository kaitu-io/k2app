"""
Configuration management for kaitu-signer.

Reads from environment variables, with optional .env file support.
"""

import os
import sys


def _load_dotenv():
    """Load .env file from same directory as executable/script."""
    if getattr(sys, "frozen", False):
        base_dir = os.path.dirname(sys.executable)
    else:
        base_dir = os.path.dirname(os.path.abspath(__file__))

    env_path = os.path.join(base_dir, ".env")
    if not os.path.exists(env_path):
        return

    with open(env_path, "r") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            if "=" not in line:
                continue
            key, _, value = line.partition("=")
            key = key.strip()
            value = value.strip().strip('"').strip("'")
            if key not in os.environ:
                os.environ[key] = value


_load_dotenv()


# --- SimpliSign ---
SIMPLISIGN_TOTP_URI = os.environ.get("SIMPLISIGN_TOTP_URI", "")
SIMPLISIGN_USERNAME = os.environ.get("SIMPLISIGN_USERNAME", "")

# --- AWS ---
AWS_ACCESS_KEY_ID = os.environ.get("AWS_ACCESS_KEY_ID", "")
AWS_SECRET_ACCESS_KEY = os.environ.get("AWS_SECRET_ACCESS_KEY", "")
AWS_DEFAULT_REGION = os.environ.get("AWS_DEFAULT_REGION", "ap-northeast-1")
SQS_QUEUE_URL = os.environ.get("SQS_QUEUE_URL", "")
S3_BUCKET = os.environ.get("S3_BUCKET", "d0.all7.cc")

# --- Signing ---
CERT_NAME = os.environ.get("CERT_NAME", "Wordgate LLC")
TIMESTAMP_URL = os.environ.get("TIMESTAMP_URL", "http://timestamp.sectigo.com")
SIGN_TEMP_DIR = os.environ.get("SIGN_TEMP_DIR", r"C:\ProgramData\KaituSigner\temp")

# --- Logging ---
LOG_DIR = os.environ.get("SIGNER_LOG_DIR", r"C:\ProgramData\KaituSigner\logs")
LOG_FILE = "kaitu-signer.log"

# --- Intervals ---
SQS_POLL_WAIT_SECONDS = 20
SIMPLISIGN_CHECK_INTERVAL = 300  # 5 minutes


def validate():
    """Return list of missing required config keys."""
    required = {
        "SIMPLISIGN_TOTP_URI": SIMPLISIGN_TOTP_URI,
        "SIMPLISIGN_USERNAME": SIMPLISIGN_USERNAME,
        "SQS_QUEUE_URL": SQS_QUEUE_URL,
        "AWS_ACCESS_KEY_ID": AWS_ACCESS_KEY_ID,
        "AWS_SECRET_ACCESS_KEY": AWS_SECRET_ACCESS_KEY,
    }
    return [k for k, v in required.items() if not v]
