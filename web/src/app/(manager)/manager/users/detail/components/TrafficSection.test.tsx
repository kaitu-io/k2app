/**
 * TrafficSection component test.
 *
 * Mocks `@/lib/api` (getTrafficUserDetail) and asserts on the rendered
 * bytes/labels TrafficSection derives from the response — not on i18n keys,
 * since this component uses literal Chinese copy rather than next-intl.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { TrafficSection } from "./TrafficSection";

vi.mock("@/lib/api", () => ({
  api: {
    getTrafficUserDetail: vi.fn(),
  },
}));

import { api } from "@/lib/api";

const FIXTURE = {
  month: "2026-07",
  totalBytes: 1536, // 1.5 KB
  daily: [
    { date: "2026-07-01", bytes: 1024 },
    { date: "2026-07-02", bytes: 512 },
  ],
  devices: [{ key: "dev-abc123", bytes: 1536 }],
  nodes: [{ key: "1.2.3.4", bytes: 1536 }],
};

describe("TrafficSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders title, formatted total, and device key (not the 未识别 fallback)", async () => {
    (api.getTrafficUserDetail as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(FIXTURE);
    render(<TrafficSection uuid="u-1" />);

    await waitFor(() => {
      expect(screen.getByText(/流量/)).toBeInTheDocument();
    });
    // formatBytes(1536) => "1.5 KB" — appears in the title plus per-device/
    // per-node breakdown rows, so assert at least one match rather than a
    // single unique node.
    expect(screen.getAllByText(/1\.5 KB/).length).toBeGreaterThan(0);
    // Device with a real key renders the key itself, not the "未识别" fallback.
    expect(screen.getByText("dev-abc123")).toBeInTheDocument();
    expect(screen.queryByText("未识别")).not.toBeInTheDocument();
    // Node key renders too.
    expect(screen.getByText("1.2.3.4")).toBeInTheDocument();

    expect(api.getTrafficUserDetail).toHaveBeenCalledWith(
      expect.objectContaining({ uuid: "u-1" }),
    );
  });

  it("renders 暂无数据 empty state when the response has no data", async () => {
    (api.getTrafficUserDetail as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      month: "2026-07",
      totalBytes: 0,
      daily: [],
      devices: [],
      nodes: [],
    });
    render(<TrafficSection uuid="u-2" />);

    await waitFor(() => {
      expect(screen.getAllByText("暂无数据").length).toBeGreaterThan(0);
    });
  });
});
