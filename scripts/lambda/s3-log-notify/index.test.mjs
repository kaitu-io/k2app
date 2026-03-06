import { describe, it, expect } from "vitest";
import { parseS3Record, formatSlackMessage } from "./index.mjs";

describe("parseS3Record", () => {
  it("parses 6-part service-logs key (with UDID)", () => {
    const record = {
      s3: {
        object: {
          key: "service-logs/ABC-DEF-123/2026/03/05/service-143022-fb123.log.gz",
          size: 4096,
        },
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
      source: "auto",
    });
  });

  it("parses 5-part service-logs key (no UDID)", () => {
    const record = {
      s3: {
        object: {
          key: "service-logs/2026/03/06/service-150838-4e7afad0.log.gz",
          size: 99,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta).toEqual({
      s3Key: "service-logs/2026/03/06/service-150838-4e7afad0.log.gz",
      udid: "unknown",
      date: "2026/03/06",
      filename: "service-150838-4e7afad0.log.gz",
      logType: "service",
      size: 99,
      source: "auto",
    });
  });

  it("parses feedback-logs key", () => {
    const record = {
      s3: {
        object: {
          key: "feedback-logs/UDID123/2026/03/06/service-152531-fb-id.log.gz",
          size: 84,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.source).toBe("feedback");
    expect(meta.udid).toBe("UDID123");
    expect(meta.logType).toBe("service");
  });

  it("handles URL-encoded keys", () => {
    const record = {
      s3: {
        object: {
          key: "service-logs/ABC+DEF/2026/03/05/crash-120000-id.log.gz",
          size: 1024,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.udid).toBe("ABC DEF");
    expect(meta.logType).toBe("crash");
  });

  it("returns null for non-matching prefix", () => {
    const record = {
      s3: { object: { key: "other/path/file.gz", size: 0 } },
    };
    expect(parseS3Record(record)).toBeNull();
  });

  it("returns null for too few path segments", () => {
    const record = {
      s3: { object: { key: "service-logs/short.gz", size: 0 } },
    };
    expect(parseS3Record(record)).toBeNull();
  });
});

describe("formatSlackMessage", () => {
  it("formats message with source label", () => {
    const meta = {
      s3Key: "feedback-logs/UDID123/2026/03/05/desktop-143022-abc.log.gz",
      udid: "UDID123",
      date: "2026/03/05",
      filename: "desktop-143022-abc.log.gz",
      logType: "desktop",
      size: 2048,
      source: "feedback",
    };
    const msg = formatSlackMessage(meta);
    expect(msg.text).toContain("(feedback)");
    expect(msg.text).toContain("UDID123");
    expect(msg.text).toContain("desktop");
    expect(msg.text).toContain("2 KB");
  });
});
