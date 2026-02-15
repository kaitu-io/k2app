"""
AWS S3 and SQS operations for kaitu-signer.
"""

import json
import logging
import os

import boto3
from botocore.config import Config as BotoConfig

from config import (
    AWS_ACCESS_KEY_ID,
    AWS_SECRET_ACCESS_KEY,
    AWS_DEFAULT_REGION,
    S3_BUCKET,
    SQS_QUEUE_URL,
)

logger = logging.getLogger("kaitu-signer")

_boto_config = BotoConfig(
    region_name=AWS_DEFAULT_REGION,
    retries={"max_attempts": 3, "mode": "standard"},
)


def _session():
    return boto3.Session(
        aws_access_key_id=AWS_ACCESS_KEY_ID,
        aws_secret_access_key=AWS_SECRET_ACCESS_KEY,
        region_name=AWS_DEFAULT_REGION,
    )


def _s3():
    return _session().client("s3", config=_boto_config)


def _sqs():
    return _session().client("sqs", config=_boto_config)


# --- SQS ---


def poll_sqs(wait_time=20):
    """
    Long-poll SQS for a signing job message.

    Returns (parsed_body, receipt_handle, receive_count) or (None, None, 0).
    receive_count is the ApproximateReceiveCount from SQS.
    """
    try:
        resp = _sqs().receive_message(
            QueueUrl=SQS_QUEUE_URL,
            MaxNumberOfMessages=1,
            WaitTimeSeconds=wait_time,
            MessageAttributeNames=["All"],
            AttributeNames=["ApproximateReceiveCount"],
        )
        messages = resp.get("Messages", [])
        if not messages:
            return None, None, 0

        msg = messages[0]
        body = json.loads(msg["Body"])
        receipt_handle = msg["ReceiptHandle"]
        receive_count = int(msg.get("Attributes", {}).get("ApproximateReceiveCount", "1"))
        logger.info(f"Received SQS message: run_id={body.get('run_id')}, attempt={receive_count}")
        return body, receipt_handle, receive_count
    except Exception as e:
        logger.error(f"SQS poll error: {e}")
        return None, None, 0


def delete_sqs_message(receipt_handle):
    """Delete a processed SQS message."""
    try:
        _sqs().delete_message(
            QueueUrl=SQS_QUEUE_URL,
            ReceiptHandle=receipt_handle,
        )
        logger.debug("SQS message deleted")
    except Exception as e:
        logger.error(f"SQS delete error: {e}")


# --- S3 ---


def download_from_s3(key, local_path):
    """Download a file from S3."""
    os.makedirs(os.path.dirname(local_path), exist_ok=True)
    logger.info(f"Downloading s3://{S3_BUCKET}/{key} -> {local_path}")
    _s3().download_file(S3_BUCKET, key, local_path)
    logger.info(f"Downloaded {os.path.getsize(local_path)} bytes")


def upload_to_s3(local_path, key):
    """Upload a file to S3."""
    size = os.path.getsize(local_path)
    logger.info(f"Uploading {local_path} ({size} bytes) -> s3://{S3_BUCKET}/{key}")
    _s3().upload_file(local_path, S3_BUCKET, key)
    logger.info(f"Upload complete: s3://{S3_BUCKET}/{key}")


def upload_status_json(prefix, status_dict):
    """Upload a status.json to the given S3 prefix."""
    key = f"{prefix}status.json"
    body = json.dumps(status_dict, indent=2)
    _s3().put_object(Bucket=S3_BUCKET, Key=key, Body=body, ContentType="application/json")
    logger.info(f"Status uploaded: s3://{S3_BUCKET}/{key}")
