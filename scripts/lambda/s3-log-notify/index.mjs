const S3_BUCKET_URL = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com";

/**
 * Parse log metadata from S3 event record.
 * Supported key formats:
 *   {prefix}/{udid}/YYYY/MM/DD/{type}-HHMMSS-{id}.log.gz  (6+ parts)
 *   {prefix}/YYYY/MM/DD/{type}-HHMMSS-{id}.log.gz         (5 parts, no UDID)
 * where prefix is "service-logs" or "feedback-logs"
 */
export function parseS3Record(record) {
  const s3Key = decodeURIComponent(record.s3.object.key.replace(/\+/g, " "));
  const size = record.s3?.object?.size ?? 0;

  const parts = s3Key.split("/");
  const prefix = parts[0];
  if (prefix !== "service-logs" && prefix !== "feedback-logs") return null;

  let udid, date, filename;

  if (parts.length >= 6) {
    // {prefix}/{udid}/YYYY/MM/DD/{filename}
    udid = parts[1];
    date = `${parts[2]}/${parts[3]}/${parts[4]}`;
    filename = parts[5];
  } else if (parts.length === 5 && /^\d{4}$/.test(parts[1])) {
    // {prefix}/YYYY/MM/DD/{filename} (empty UDID)
    udid = "unknown";
    date = `${parts[1]}/${parts[2]}/${parts[3]}`;
    filename = parts[4];
  } else {
    return null;
  }

  const logType = filename.split("-")[0];
  const source = prefix === "feedback-logs" ? "feedback" : "auto";
  return { s3Key, udid, date, filename, logType, size, source };
}

/**
 * Format Slack message from parsed metadata.
 */
export function formatSlackMessage(meta) {
  return {
    text: [
      `:file_folder: *S3 Log Uploaded* (${meta.source})`,
      ``,
      `*UDID:* \`${meta.udid}\``,
      `*Date:* ${meta.date}`,
      `*Type:* ${meta.logType}`,
      `*Size:* ${Math.round(meta.size / 1024)} KB`,
      `*File:* <${S3_BUCKET_URL}/${meta.s3Key}|${meta.filename}>`,
    ].join("\n"),
  };
}

/**
 * Lambda handler — sends Slack notification for each S3 log upload.
 */
export async function handler(event) {
  const webhookUrl = process.env.SLACK_WEBHOOK_URL;
  if (!webhookUrl) {
    console.error("SLACK_WEBHOOK_URL not set");
    return { statusCode: 500 };
  }

  for (const record of event.Records ?? []) {
    const meta = parseS3Record(record);
    if (!meta) continue;

    const message = formatSlackMessage(meta);
    try {
      const resp = await fetch(webhookUrl, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(message),
      });
      if (!resp.ok) {
        console.error(`Slack failed: ${resp.status} ${await resp.text()}`);
      }
    } catch (e) {
      console.error(`Slack notify error: ${e.message}`);
    }
  }

  return { statusCode: 200 };
}
