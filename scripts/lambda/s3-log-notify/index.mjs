const S3_BUCKET_URL = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com";

const PLATFORM_PREFIXES = new Set(["desktop", "mobile"]);
const LEGACY_PREFIXES = new Set(["service-logs", "feedback-logs"]);

/**
 * Parse log metadata from S3 event record.
 * Supported key formats:
 *   {platform}/{version}/{udid}/YYYY/MM/DD/{filename}          (new, platform = desktop|mobile)
 *   {prefix}/{version}/{udid}/YYYY/MM/DD/{filename}            (legacy 7-part, prefix = service-logs|feedback-logs)
 *   {prefix}/{udid}/YYYY/MM/DD/{filename}                      (legacy 6-part, beta.1)
 *   {prefix}/YYYY/MM/DD/{filename}                             (legacy 5-part, v0.3.x)
 */
export function parseS3Record(record) {
  const s3Key = decodeURIComponent(record.s3.object.key.replace(/\+/g, " "));
  const size = record.s3?.object?.size ?? 0;

  const parts = s3Key.split("/");
  const prefix = parts[0];

  if (!PLATFORM_PREFIXES.has(prefix) && !LEGACY_PREFIXES.has(prefix)) return null;

  let platform, version, udid, date, filename, source;

  if (PLATFORM_PREFIXES.has(prefix)) {
    // New format: {platform}/{version}/{udid}/YYYY/MM/DD/{filename}
    if (parts.length < 7) return null;
    platform = prefix;
    version = parts[1];
    udid = parts[2];
    date = `${parts[3]}/${parts[4]}/${parts[5]}`;
    filename = parts[6];
    // Detect source from identifier: feedbackId is a UUID with hyphens (>8 chars)
    const idPart = filename.replace(/^logs-\d{6}-/, "").replace(/\.(tar\.gz|zip|log\.gz)$/, "");
    source = (idPart.includes("-") && idPart.length > 8) ? "feedback" : "auto";
  } else if (parts.length >= 7) {
    // Legacy 7-part: {service-logs|feedback-logs}/{version}/{udid}/YYYY/MM/DD/{filename}
    platform = "unknown";
    version = parts[1];
    udid = parts[2];
    date = `${parts[3]}/${parts[4]}/${parts[5]}`;
    filename = parts[6];
    source = prefix === "feedback-logs" ? "feedback" : "auto";
  } else if (parts.length >= 6 && !/^\d{4}$/.test(parts[1])) {
    // Legacy 6-part beta.1: {prefix}/{udid}/YYYY/MM/DD/{filename}
    platform = "unknown";
    version = "v0.4.0-beta.1";
    udid = parts[1];
    date = `${parts[2]}/${parts[3]}/${parts[4]}`;
    filename = parts[5];
    source = prefix === "feedback-logs" ? "feedback" : "auto";
  } else if (parts.length === 5 && /^\d{4}$/.test(parts[1])) {
    // Legacy 5-part v0.3.x: {prefix}/YYYY/MM/DD/{filename}
    platform = "unknown";
    version = "v0.3.x";
    udid = "unknown";
    date = `${parts[1]}/${parts[2]}/${parts[3]}`;
    filename = parts[4];
    source = "auto";
  } else {
    return null;
  }

  const logType = filename.split("-")[0];
  return { s3Key, platform, version, udid, date, filename, logType, size, source };
}

/**
 * Format Slack message from parsed metadata.
 */
export function formatSlackMessage(meta) {
  return {
    text: [
      `:file_folder: *S3 Log Uploaded* (${meta.source})`,
      ``,
      `*Platform:* ${meta.platform}`,
      `*Version:* ${meta.version}`,
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
