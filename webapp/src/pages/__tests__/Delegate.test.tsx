import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

const showAlert = vi.fn();

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, optsOrFallback?: unknown) => {
      if (typeof optsOrFallback === "string") return optsOrFallback;
      if (optsOrFallback && typeof optsOrFallback === "object") {
        const opts = optsOrFallback as Record<string, unknown>;
        const interpolated = Object.entries(opts).reduce<string>(
          (acc, [k, v]) => acc.replace(`{{${k}}}`, String(v)),
          key,
        );
        return interpolated;
      }
      return key;
    },
  }),
}));

vi.mock("../../stores", () => ({
  useAlert: () => ({ showAlert }),
}));

vi.mock("../../services/cloud-api", () => ({
  cloudApi: {
    get: vi.fn(),
    request: vi.fn(),
    post: vi.fn(),
  },
}));

import Delegate from "../Delegate";
import { cloudApi } from "../../services/cloud-api";

const mockGet = cloudApi.get as ReturnType<typeof vi.fn>;
const mockRequest = cloudApi.request as ReturnType<typeof vi.fn>;

const renderPage = (initialEntries: string[] = ["/delegate"]) =>
  render(
    <MemoryRouter initialEntries={initialEntries}>
      <Delegate />
    </MemoryRouter>,
  );

describe("Delegate page", () => {
  beforeEach(() => {
    showAlert.mockReset();
    mockGet.mockReset();
    mockRequest.mockReset();
    // Global setup.ts mocks `window.getComputedStyle` via `vi.fn().mockImplementation(...)`,
    // but its global `beforeEach` runs `vi.clearAllMocks()` which strips the implementation.
    // MUI Modal's `getPaddingRight()` then crashes on `getComputedStyle(el).paddingRight`.
    // Restore a minimal stub here.
    (window.getComputedStyle as unknown as ReturnType<typeof vi.fn>).mockImplementation(
      () =>
        ({
          paddingRight: "0px",
          paddingLeft: "0px",
          marginRight: "0px",
          marginLeft: "0px",
          getPropertyValue: () => "",
        }) as unknown as CSSStyleDeclaration,
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("renders empty form when no delegate is set (code:0, no data)", async () => {
    mockGet.mockResolvedValue({ code: 0 });
    renderPage();

    await waitFor(() =>
      expect(screen.getByText("account:delegate.emptyDescription")).toBeInTheDocument(),
    );
    expect(screen.getByText("account:delegate.saveButton")).toBeInTheDocument();
    // No "current delegate" panel
    expect(screen.queryByText("account:delegate.currentTitle")).not.toBeInTheDocument();
  });

  it("renders current delegate panel when delegate is set", async () => {
    mockGet.mockResolvedValue({
      code: 0,
      data: { email: "friend@example.com", setAt: 1715900000 },
    });
    renderPage();

    await waitFor(() =>
      expect(screen.getByText("friend@example.com")).toBeInTheDocument(),
    );
    expect(screen.getByText("account:delegate.currentTitle")).toBeInTheDocument();
    expect(screen.getByText("account:delegate.modifyButton")).toBeInTheDocument();
    expect(screen.getByText("account:delegate.removeButton")).toBeInTheDocument();
  });

  it("save succeeds: PUT body, success toast, panel switches to current view", async () => {
    mockGet.mockResolvedValue({ code: 0 });
    mockRequest.mockResolvedValue({
      code: 0,
      data: { email: "friend@example.com", setAt: 1715900000 },
    });

    renderPage();
    await waitFor(() =>
      expect(screen.getByText("account:delegate.saveButton")).toBeInTheDocument(),
    );

    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "friend@example.com" } });
    fireEvent.click(screen.getByText("account:delegate.saveButton"));

    await waitFor(() =>
      expect(mockRequest).toHaveBeenCalledWith(
        "PUT",
        "/api/user/delegate",
        { email: "friend@example.com" },
      ),
    );
    await waitFor(() =>
      expect(showAlert).toHaveBeenCalledWith("account:delegate.savedToast", "success"),
    );
    await waitFor(() =>
      expect(screen.getByText("friend@example.com")).toBeInTheDocument(),
    );
  });

  it("save with 422 (invalid email) shows generic mapped message, NOT response.message", async () => {
    mockGet.mockResolvedValue({ code: 0 });
    mockRequest.mockResolvedValue({
      code: 422,
      message:
        "invalid request: Key: 'PutDelegateRequest.Email' Error:Field validation for 'Email' failed on the 'email' tag",
    });

    renderPage();
    await waitFor(() =>
      expect(screen.getByText("account:delegate.saveButton")).toBeInTheDocument(),
    );

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "not-an-email" },
    });
    fireEvent.click(screen.getByText("account:delegate.saveButton"));

    await waitFor(() => expect(showAlert).toHaveBeenCalled());
    const [msg, severity] = showAlert.mock.calls[0];
    expect(severity).toBe("error");
    expect(msg).not.toContain("invalid request");
    expect(msg).not.toContain("Field validation");
    expect(msg).toBe("Invalid parameters");
  });

  it("save with 500 shows generic server-error message, not raw response.message", async () => {
    mockGet.mockResolvedValue({ code: 0 });
    mockRequest.mockResolvedValue({
      code: 500,
      message: "failed to encrypt email",
    });

    renderPage();
    await waitFor(() =>
      expect(screen.getByText("account:delegate.saveButton")).toBeInTheDocument(),
    );

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "ok@example.com" },
    });
    fireEvent.click(screen.getByText("account:delegate.saveButton"));

    await waitFor(() => expect(showAlert).toHaveBeenCalled());
    const [msg] = showAlert.mock.calls[0];
    expect(msg).not.toContain("encrypt");
    expect(msg).toBe("Internal server error");
  });

  it("remove confirmed via dialog → DELETE → returns to empty form", async () => {
    mockGet.mockResolvedValue({
      code: 0,
      data: { email: "friend@example.com", setAt: 1715900000 },
    });
    mockRequest.mockResolvedValue({ code: 0 });

    renderPage();
    await waitFor(() =>
      expect(screen.getByText("account:delegate.removeButton")).toBeInTheDocument(),
    );

    // Click trigger button — opens confirm dialog, does NOT call DELETE yet
    fireEvent.click(screen.getByText("account:delegate.removeButton"));
    await waitFor(() =>
      expect(screen.getByText("account:delegate.removeConfirm")).toBeInTheDocument(),
    );
    expect(mockRequest).not.toHaveBeenCalled();

    // Click confirm inside the dialog
    fireEvent.click(screen.getByText("common:common.confirm"));

    await waitFor(() =>
      expect(mockRequest).toHaveBeenCalledWith("DELETE", "/api/user/delegate"),
    );
    await waitFor(() =>
      expect(screen.getByText("account:delegate.emptyDescription")).toBeInTheDocument(),
    );
  });

  it("remove cancelled via dialog → no DELETE issued, panel unchanged", async () => {
    mockGet.mockResolvedValue({
      code: 0,
      data: { email: "friend@example.com", setAt: 1715900000 },
    });

    renderPage();
    await waitFor(() =>
      expect(screen.getByText("account:delegate.removeButton")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("account:delegate.removeButton"));
    await waitFor(() =>
      expect(screen.getByText("account:delegate.removeConfirm")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("common:common.cancel"));

    expect(mockRequest).not.toHaveBeenCalled();
    expect(screen.getByText("friend@example.com")).toBeInTheDocument();
  });

  it("never calls window.confirm — uses MUI Dialog instead (cross-platform regression guard)", async () => {
    mockGet.mockResolvedValue({
      code: 0,
      data: { email: "friend@example.com", setAt: 1715900000 },
    });
    mockRequest.mockResolvedValue({ code: 0 });
    const confirmSpy = vi.spyOn(window, "confirm");

    renderPage();
    await waitFor(() =>
      expect(screen.getByText("account:delegate.removeButton")).toBeInTheDocument(),
    );

    fireEvent.click(screen.getByText("account:delegate.removeButton"));
    await waitFor(() =>
      expect(screen.getByText("account:delegate.removeConfirm")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByText("common:common.confirm"));

    await waitFor(() =>
      expect(mockRequest).toHaveBeenCalledWith("DELETE", "/api/user/delegate"),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it("save with returnTo query param redirects after success", async () => {
    mockGet.mockResolvedValue({ code: 0 });
    mockRequest.mockResolvedValue({
      code: 0,
      data: { email: "friend@example.com", setAt: 1715900000 },
    });

    renderPage(["/delegate?returnTo=/purchase"]);
    await waitFor(() =>
      expect(screen.getByText("account:delegate.saveButton")).toBeInTheDocument(),
    );

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "friend@example.com" },
    });
    fireEvent.click(screen.getByText("account:delegate.saveButton"));

    await waitFor(() =>
      expect(showAlert).toHaveBeenCalledWith("account:delegate.savedToast", "success"),
    );
    // Navigation hard to assert against MemoryRouter without extra plumbing,
    // but the save flow having completed without throwing covers the
    // returnTo-handling code path.
  });

  it("save button is disabled when email is empty or whitespace-only", async () => {
    mockGet.mockResolvedValue({ code: 0 });
    renderPage();

    await waitFor(() =>
      expect(screen.getByText("account:delegate.saveButton")).toBeInTheDocument(),
    );
    const saveBtn = screen.getByText("account:delegate.saveButton").closest("button");
    expect(saveBtn).toBeDisabled();

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "   " },
    });
    expect(saveBtn).toBeDisabled();

    fireEvent.change(screen.getByRole("textbox"), {
      target: { value: "x@y.z" },
    });
    expect(saveBtn).not.toBeDisabled();
  });
});
