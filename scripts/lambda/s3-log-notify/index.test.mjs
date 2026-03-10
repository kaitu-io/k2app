import { describe, it, expect } from "vitest";
import { parseS3Record, formatSlackMessage } from "./index.mjs";

describe("parseS3Record", () => {
  // === New platform-based format ===

  it("parses desktop auto-upload key", () => {
    const record = {
      s3: {
        object: {
          key: "desktop/0.3.22/abc12345def67890/2026/03/10/logs-143022-a1b2c3d4.tar.gz",
          size: 8192,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta).toEqual({
      s3Key: "desktop/0.3.22/abc12345def67890/2026/03/10/logs-143022-a1b2c3d4.tar.gz",
      platform: "desktop",
      version: "0.3.22",
      udid: "abc12345def67890",
      date: "2026/03/10",
      filename: "logs-143022-a1b2c3d4.tar.gz",
      logType: "logs",
      size: 8192,
      source: "auto",
    });
  });

  it("parses mobile feedback key (identifier is UUID with hyphens)", () => {
    const record = {
      s3: {
        object: {
          key: "mobile/0.3.22/62073654d43f5b51/2026/03/10/logs-120000-c9d6eeca-d3e9-43a8-98b8-887b2b639205.zip",
          size: 2048,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("mobile");
    expect(meta.version).toBe("0.3.22");
    expect(meta.udid).toBe("62073654d43f5b51");
    expect(meta.source).toBe("feedback");
  });

  it("parses mobile auto-upload key", () => {
    const record = {
      s3: {
        object: {
          key: "mobile/0.3.22/62073654d43f5b51/2026/03/10/logs-143000-a1b2c3d4.zip",
          size: 3000,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("mobile");
    expect(meta.source).toBe("auto");
  });

  it("returns null for desktop key with too few segments", () => {
    const record = {
      s3: { object: { key: "desktop/short.gz", size: 0 } },
    };
    expect(parseS3Record(record)).toBeNull();
  });

  // === Legacy format backward compatibility ===

  it("parses legacy 7-part service-logs key", () => {
    const record = {
      s3: {
        object: {
          key: "service-logs/0.4.1/ABC-DEF-123/2026/03/10/logs-143022-abcd1234.tar.gz",
          size: 8192,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("unknown");
    expect(meta.version).toBe("0.4.1");
    expect(meta.udid).toBe("ABC-DEF-123");
    expect(meta.source).toBe("auto");
  });

  it("parses legacy 7-part feedback-logs key", () => {
    const record = {
      s3: {
        object: {
          key: "feedback-logs/0.4.1/UDID456/2026/03/10/logs-120000-fb-999.tar.gz",
          size: 2048,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("unknown");
    expect(meta.source).toBe("feedback");
  });

  it("parses legacy 6-part key as beta.1 format", () => {
    const record = {
      s3: {
        object: {
          key: "service-logs/ABC-DEF-123/2026/03/05/service-143022-fb123.log.gz",
          size: 4096,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("unknown");
    expect(meta.version).toBe("v0.4.0-beta.1");
    expect(meta.udid).toBe("ABC-DEF-123");
    expect(meta.source).toBe("auto");
  });

  it("parses legacy 5-part key as v0.3.x format", () => {
    const record = {
      s3: {
        object: {
          key: "service-logs/2026/03/06/service-150838-4e7afad0.log.gz",
          size: 99,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("unknown");
    expect(meta.version).toBe("v0.3.x");
    expect(meta.udid).toBe("unknown");
    expect(meta.source).toBe("auto");
  });

  it("parses legacy feedback-logs 6-part key", () => {
    const record = {
      s3: {
        object: {
          key: "feedback-logs/UDID123/2026/03/06/service-152531-fb-id.log.gz",
          size: 84,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.platform).toBe("unknown");
    expect(meta.source).toBe("feedback");
    expect(meta.udid).toBe("UDID123");
  });

  // === Edge cases ===

  it("handles URL-encoded keys", () => {
    const record = {
      s3: {
        object: {
          key: "desktop/0.3.22/ABC+DEF/2026/03/05/logs-120000-abcd1234.tar.gz",
          size: 1024,
        },
      },
    };
    const meta = parseS3Record(record);
    expect(meta.udid).toBe("ABC DEF");
    expect(meta.platform).toBe("desktop");
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
  it("formats message with platform and source label", () => {
    const meta = {
      s3Key: "desktop/0.3.22/UDID123/2026/03/05/logs-143022-abc.tar.gz",
      platform: "desktop",
      version: "0.3.22",
      udid: "UDID123",
      date: "2026/03/05",
      filename: "logs-143022-abc.tar.gz",
      logType: "logs",
      size: 2048,
      source: "auto",
    };
    const msg = formatSlackMessage(meta);
    expect(msg.text).toContain("(auto)");
    expect(msg.text).toContain("*Platform:* desktop");
    expect(msg.text).toContain("*Version:* 0.3.22");
    expect(msg.text).toContain("UDID123");
    expect(msg.text).toContain("2 KB");
  });

  it("formats feedback message with mobile platform", () => {
    const meta = {
      s3Key: "mobile/0.3.22/UDID456/2026/03/10/logs-120000-fb-uuid.zip",
      platform: "mobile",
      version: "0.3.22",
      udid: "UDID456",
      date: "2026/03/10",
      filename: "logs-120000-fb-uuid.zip",
      logType: "logs",
      size: 4096,
      source: "feedback",
    };
    const msg = formatSlackMessage(meta);
    expect(msg.text).toContain("(feedback)");
    expect(msg.text).toContain("*Platform:* mobile");
  });
});
