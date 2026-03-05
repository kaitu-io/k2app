import { describe, it, expect } from "vitest";
import { parseS3Record, formatSlackMessage } from "./index.mjs";

describe("parseS3Record", () => {
  it("parses valid S3 key", () => {
    const record = {
      s3: {
        key: "service-logs/ABC-DEF-123/2026/03/05/service-143022-fb123.log.gz",
        object: { size: 4096 },
      },
    };
    const meta = parseS3Record(record);
    expect(meta).toEqual({
      s3Key: "service-logs/ABC-DEF-123/2026/03/05/service-143022-fb123.log.gz",
      udid: "ABC-DEF-123",
      date: "2026/03/05",
      filename: "service-143022-fb123.log.gz",
      logType: "service",
      size: 4096,
    });
  });

  it("handles URL-encoded keys", () => {
    const record = {
      s3: {
        key: "service-logs/ABC+DEF/2026/03/05/crash-120000-id.log.gz",
        object: { size: 1024 },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.udid).toBe("ABC DEF");
    expect(meta.logType).toBe("crash");
  });

  it("returns null for non-matching prefix", () => {
    const record = { s3: { key: "other/path/file.gz", object: { size: 0 } } };
    expect(parseS3Record(record)).toBeNull();
  });

  it("returns null for too few path segments", () => {
    const record = {
      s3: { key: "service-logs/short.gz", object: { size: 0 } },
    };
    expect(parseS3Record(record)).toBeNull();
  });
});

describe("formatSlackMessage", () => {
  it("formats message with all fields", () => {
    const meta = {
      s3Key: "service-logs/UDID123/2026/03/05/desktop-143022-abc.log.gz",
      udid: "UDID123",
      date: "2026/03/05",
      filename: "desktop-143022-abc.log.gz",
      logType: "desktop",
      size: 2048,
    };
    const msg = formatSlackMessage(meta);
    expect(msg.text).toContain("UDID123");
    expect(msg.text).toContain("desktop");
    expect(msg.text).toContain("2 KB");
    expect(msg.text).toContain("S3 Log Uploaded");
  });
});
