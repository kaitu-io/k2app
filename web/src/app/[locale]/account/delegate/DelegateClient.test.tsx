/**
 * DelegateClient component test.
 *
 * The global test/setup.ts mock of `useTranslations` returns the key verbatim,
 * so we assert on i18n keys (full dotted path) rather than localized strings.
 * Asserting on the KEYS is stronger: it catches typos in both the component
 * and any *.json namespace file whose key the component expects.
 */
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import DelegateClient from "./DelegateClient";

vi.mock("@/lib/api", () => ({
  api: {
    getDelegate: vi.fn(),
    setDelegate: vi.fn(),
    removeDelegate: vi.fn(),
  },
  ApiError: class ApiError extends Error {
    code: number;
    constructor(code: number, message: string) {
      super(message);
      this.code = code;
    }
  },
}));

vi.mock("@/i18n/routing", () => ({
  useRouter: () => ({ push: vi.fn() }),
  Link: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { api } from "@/lib/api";

describe("DelegateClient", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (api.getDelegate as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(null);
    (api.setDelegate as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      email: "alice@example.com",
      setAt: 1745000000,
    });
    (api.removeDelegate as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
  });

  it("renders empty state when no delegate set", async () => {
    (api.getDelegate as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(null);
    render(<DelegateClient />);
    await waitFor(() => {
      expect(
        screen.getByPlaceholderText("account.account.delegate.emailPlaceholder"),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "account.account.delegate.saveButton" }),
    ).toBeInTheDocument();
  });

  it("renders set state with email + modify/remove buttons", async () => {
    (api.getDelegate as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      email: "alice@example.com",
      setAt: 1745000000,
    });
    render(<DelegateClient />);
    await waitFor(() => {
      expect(screen.getByText("alice@example.com")).toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: "account.account.delegate.modifyButton" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "account.account.delegate.removeButton" }),
    ).toBeInTheDocument();
  });

  it("saves the delegate email on submit", async () => {
    (api.getDelegate as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(null);
    render(<DelegateClient />);
    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: "account.account.delegate.saveButton" }),
      ).toBeInTheDocument();
    });
    fireEvent.change(
      screen.getByPlaceholderText("account.account.delegate.emailPlaceholder"),
      { target: { value: "alice@example.com" } },
    );
    fireEvent.click(
      screen.getByRole("button", { name: "account.account.delegate.saveButton" }),
    );
    await waitFor(() => {
      expect(api.setDelegate).toHaveBeenCalledWith("alice@example.com");
    });
  });
});
