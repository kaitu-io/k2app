"""
Worker process — SQS polling, S3 download, signing, S3 upload.

Runs in a separate process from the tray UI to avoid Windows message pump
conflicts between pystray and pywinauto.
"""

import json
import logging
import os
import shutil
import tempfile
import time
import traceback
from datetime import datetime, timezone
from multiprocessing import Queue

import config
import aws_client
import signer
import simplisign

logger = logging.getLogger("kaitu-signer")


def _setup_worker_logging():
    """Configure logging for the worker process."""
    os.makedirs(config.LOG_DIR, exist_ok=True)
    log_path = os.path.join(config.LOG_DIR, config.LOG_FILE)

    root = logging.getLogger("kaitu-signer")
    root.setLevel(logging.DEBUG)
    root.handlers.clear()

    from logging.handlers import RotatingFileHandler

    fh = RotatingFileHandler(log_path, maxBytes=10 * 1024 * 1024, backupCount=3)
    fh.setLevel(logging.DEBUG)
    fh.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(message)s", datefmt="%Y-%m-%d %H:%M:%S"
    ))
    root.addHandler(fh)

    ch = logging.StreamHandler()
    ch.setLevel(logging.INFO)
    ch.setFormatter(logging.Formatter(
        "%(asctime)s [%(levelname)s] %(message)s", datefmt="%H:%M:%S"
    ))
    root.addHandler(ch)


def _send_status(status_queue, msg_type, **kwargs):
    """Send a status message to the main (tray) process."""
    if status_queue is None:
        return
    try:
        status_queue.put_nowait({"type": msg_type, **kwargs})
    except Exception:
        pass


MAX_RECEIVE_COUNT = 3  # Give up after this many SQS delivery attempts


def _process_job(job, receipt_handle, receive_count, status_queue):
    """
    Process a single signing job.

    On success: delete SQS message, upload success status.json.
    On failure with retries remaining: DON'T delete — let SQS visibility
    timeout expire so the message returns for retry.
    On failure with retries exhausted: delete message, upload failure status.json.

    Job format:
    {
        "run_id": "12345678",
        "s3_prefix": "kaitu/signing/pending/12345678/",
        "files": ["Kaitu_0.4.0_x64-setup.exe"],
        "cert_name": "Wordgate LLC",
        "timestamp_url": "http://timestamp.sectigo.com",
        "completed_prefix": "kaitu/signing/completed/12345678/"
    }
    """
    run_id = job.get("run_id", "unknown")
    files = job.get("files", [])
    s3_prefix = job.get("s3_prefix", "")
    completed_prefix = job.get("completed_prefix", "")
    cert_name = job.get("cert_name", config.CERT_NAME)
    timestamp_url = job.get("timestamp_url", config.TIMESTAMP_URL)

    logger.info(f"Processing job run_id={run_id}, files={files}, attempt={receive_count}/{MAX_RECEIVE_COUNT}")
    _send_status(status_queue, "job_start", run_id=run_id, files=files)

    # Create temp directory for this job
    temp_dir = os.path.join(config.SIGN_TEMP_DIR, run_id)
    os.makedirs(temp_dir, exist_ok=True)

    try:
        # Ensure SimpliSign is logged in before signing
        logger.info("Checking SimpliSign status before signing...")
        simplisign.check_and_login_if_needed(
            config.SIMPLISIGN_TOTP_URI,
            config.SIMPLISIGN_USERNAME,
        )

        signed_files = []
        for filename in files:
            s3_key = f"{s3_prefix}{filename}"
            local_path = os.path.join(temp_dir, filename)

            # Download from S3
            aws_client.download_from_s3(s3_key, local_path)

            # Sign
            signer.sign_file(local_path, cert_name=cert_name, timestamp_url=timestamp_url)

            # Verify
            signer.verify_signature(local_path)

            # Upload signed file to completed path
            completed_key = f"{completed_prefix}{filename}"
            aws_client.upload_to_s3(local_path, completed_key)

            signed_files.append(filename)
            logger.info(f"Signed and uploaded: {filename}")

        # Upload status.json
        aws_client.upload_status_json(completed_prefix, {
            "success": True,
            "signed_at": datetime.now(timezone.utc).isoformat(),
            "verified": True,
            "files": signed_files,
            "run_id": run_id,
        })

        # Delete SQS message on success
        aws_client.delete_sqs_message(receipt_handle)

        logger.info(f"Job completed: run_id={run_id}")
        _send_status(
            status_queue, "job_complete",
            run_id=run_id, files=signed_files, success=True,
        )

    except Exception as e:
        logger.error(f"Job failed: run_id={run_id}: {e}")
        logger.debug(traceback.format_exc())

        retries_exhausted = receive_count >= MAX_RECEIVE_COUNT

        if retries_exhausted:
            # Permanent failure — upload failure status and delete message
            logger.error(f"Retries exhausted ({receive_count}/{MAX_RECEIVE_COUNT}), giving up on run_id={run_id}")
            try:
                aws_client.upload_status_json(completed_prefix, {
                    "success": False,
                    "error": str(e),
                    "attempts": receive_count,
                    "signed_at": datetime.now(timezone.utc).isoformat(),
                    "run_id": run_id,
                })
            except Exception:
                logger.error("Failed to upload failure status.json")

            try:
                aws_client.delete_sqs_message(receipt_handle)
            except Exception:
                pass
        else:
            # Transient failure — don't delete message, let SQS retry
            logger.warning(
                f"Transient failure, attempt {receive_count}/{MAX_RECEIVE_COUNT}. "
                f"Message will return to queue after visibility timeout."
            )

        _send_status(
            status_queue, "job_complete",
            run_id=run_id, success=False, error=str(e),
        )

    finally:
        # Cleanup temp files
        try:
            shutil.rmtree(temp_dir, ignore_errors=True)
        except Exception:
            pass


def _keeper_loop(stop_event):
    """Periodically check SimpliSign status and re-login if needed."""
    while not stop_event.is_set():
        try:
            simplisign.check_and_login_if_needed(
                config.SIMPLISIGN_TOTP_URI,
                config.SIMPLISIGN_USERNAME,
            )
        except Exception as e:
            logger.error(f"SimpliSign keeper error: {e}")

        stop_event.wait(config.SIMPLISIGN_CHECK_INTERVAL)


def _drain_commands(command_queue):
    """Process any pending commands from the main (tray) process."""
    while True:
        try:
            cmd = command_queue.get_nowait()
        except Exception:
            break

        cmd_type = cmd.get("type")
        if cmd_type == "force_login":
            logger.info("Force login requested from tray menu")
            try:
                simplisign.check_and_login_if_needed(
                    config.SIMPLISIGN_TOTP_URI,
                    config.SIMPLISIGN_USERNAME,
                )
            except Exception as e:
                logger.error(f"Force login error: {e}")
        else:
            logger.warning(f"Unknown command: {cmd_type}")


def run(status_queue=None, command_queue=None, stop_event=None):
    """
    Main worker loop. Runs in a separate process.

    Args:
        status_queue: multiprocessing.Queue for sending status to tray UI (worker → main).
        command_queue: multiprocessing.Queue for receiving commands (main → worker).
        stop_event: multiprocessing.Event to signal shutdown.
    """
    _setup_worker_logging()
    logger.info("Worker process starting")

    missing = config.validate()
    if missing:
        logger.error(f"Missing required config: {', '.join(missing)}")
        _send_status(status_queue, "error", message=f"Missing config: {', '.join(missing)}")
        return

    # Ensure temp directory exists
    os.makedirs(config.SIGN_TEMP_DIR, exist_ok=True)

    # Start SimpliSign keeper in a background thread
    import threading

    if stop_event is None:
        stop_event = threading.Event()

    keeper_thread = threading.Thread(
        target=_keeper_loop, args=(stop_event,), daemon=True
    )
    keeper_thread.start()
    logger.info("SimpliSign keeper thread started")

    _send_status(status_queue, "ready")

    # SQS polling loop
    while not stop_event.is_set():
        try:
            # Check for commands from tray UI between SQS polls
            if command_queue is not None:
                _drain_commands(command_queue)

            job, receipt_handle, receive_count = aws_client.poll_sqs(
                wait_time=config.SQS_POLL_WAIT_SECONDS
            )

            if job is None:
                continue

            _process_job(job, receipt_handle, receive_count, status_queue)

        except Exception as e:
            logger.error(f"Poll loop error: {e}")
            logger.debug(traceback.format_exc())
            # Brief pause before retrying
            time.sleep(5)

    logger.info("Worker process stopping")
