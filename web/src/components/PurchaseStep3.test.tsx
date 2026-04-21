/**
 * PurchaseStep3 component test.
 *
 * The global test/setup.ts mock of `useTranslations` returns the key verbatim
 * WITHOUT interpolating params, so we assert on i18n keys (full dotted path)
 * rather than localized strings. ICU param placeholders like {email} never
 * render under this mock — they must not appear in assertions.
 */
import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import PurchaseStep3 from "./PurchaseStep3";
import type { Plan, Order, DelegateInfo } from "@/lib/api";

vi.mock("@/i18n/routing", () => ({
  useRouter: () => ({ push: vi.fn() }),
  Link: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

const basePlan: Plan = {
  pid: "plan-1",
  name: "Pro 1M",
  price: 1000,
  month: 1,
  // cast-through — Plan has more fields we don't need here
} as unknown as Plan;

const baseOrder: Order = {
  uuid: "order-uuid-1",
  payAmount: 1000,
  originAmount: 1000,
} as unknown as Order;

function renderStep3(overrides: Partial<React.ComponentProps<typeof PurchaseStep3>> = {}) {
  const onPurchase = vi.fn();
  const onDelegatePay = vi.fn().mockResolvedValue(undefined);
  const onEmptyStateDelegatePay = vi.fn().mockResolvedValue(undefined);
  const onResendInvite = vi.fn().mockResolvedValue(undefined);
  const onCampaignToggle = vi.fn();
  const onCampaignCodeChange = vi.fn();
  const onCampaignErrorClear = vi.fn();

  const props: React.ComponentProps<typeof PurchaseStep3> = {
    plans: [basePlan],
    selectedPlan: "plan-1",
    orderData: baseOrder,
    showCampaign: false,
    campaignCode: "",
    campaignError: "",
    onCampaignToggle,
    onCampaignCodeChange,
    onCampaignErrorClear,
    previewLoading: false,
    isLoading: false,
    isAuthenticated: true,
    onPurchase,
    delegate: null,
    delegateLoaded: true,
    onDelegatePay,
    onEmptyStateDelegatePay,
    onResendInvite,
    confirmation: null,
    ...overrides,
  };

  const utils = render(<PurchaseStep3 {...props} />);
  return {
    ...utils,
    onPurchase,
    onDelegatePay,
    onEmptyStateDelegatePay,
    onResendInvite,
    props,
  };
}

describe("PurchaseStep3 delegate integration", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("empty state: renders inline email input + send-invite button", () => {
    renderStep3({ delegate: null });

    expect(
      screen.getByPlaceholderText("purchase.purchase.delegatePay.emailPlaceholder"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /purchase\.purchase\.delegatePay\.sendInviteButton/ }),
    ).toBeInTheDocument();
    // The big red self-pay button is still present (and renders `payNow` label)
    expect(screen.getByText("purchase.purchase.payNow")).toBeInTheDocument();
  });

  it("set state: renders chip + primary delegate CTA + secondary self-pay, NO inline input", () => {
    const delegate: DelegateInfo = { email: "alice@example.com", setAt: 1745000000 };
    renderStep3({ delegate });

    // chip label renders the i18n key (mock does not interpolate {email})
    expect(
      screen.getByText("purchase.purchase.delegatePay.chipLabel"),
    ).toBeInTheDocument();
    // chip [更改] link renders as the translated key
    expect(
      screen.getByText("purchase.purchase.delegatePay.chipChange"),
    ).toBeInTheDocument();

    // primary CTA uses the delegate key
    expect(
      screen.getByRole("button", {
        name: /purchase\.purchase\.delegatePay\.primaryCtaWithDelegate/,
      }),
    ).toBeInTheDocument();
    // secondary self-pay button
    expect(
      screen.getByRole("button", {
        name: /purchase\.purchase\.delegatePay\.secondaryCtaSelfPay/,
      }),
    ).toBeInTheDocument();

    // inline email input must NOT be rendered in set state
    expect(
      screen.queryByPlaceholderText(
        "purchase.purchase.delegatePay.emailPlaceholder",
      ),
    ).not.toBeInTheDocument();
  });

  it("confirmation state: replaces Step3 body with confirmation card + resend + back-home", () => {
    renderStep3({
      delegate: { email: "alice@example.com", setAt: 1745000000 },
      confirmation: { email: "alice@example.com" },
    });

    // confirmation title key present
    expect(
      screen.getByText("purchase.purchase.delegatePay.confirmationTitle"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: /purchase\.purchase\.delegatePay\.confirmationResend/,
      }),
    ).toBeInTheDocument();
    // back-home is a link, not a button
    expect(
      screen.getByRole("link", {
        name: /purchase\.purchase\.delegatePay\.confirmationBackHome/,
      }),
    ).toBeInTheDocument();

    // Main Step3 body should be replaced — the red 立即支付 button gone
    expect(screen.queryByText("purchase.purchase.payNow")).not.toBeInTheDocument();
  });

  it("empty state: clicking send-invite invokes onEmptyStateDelegatePay with entered email", () => {
    const { onEmptyStateDelegatePay } = renderStep3({ delegate: null });

    fireEvent.change(
      screen.getByPlaceholderText(
        "purchase.purchase.delegatePay.emailPlaceholder",
      ),
      { target: { value: "bob@example.com" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: /purchase\.purchase\.delegatePay\.sendInviteButton/,
      }),
    );

    expect(onEmptyStateDelegatePay).toHaveBeenCalledWith("bob@example.com");
  });

  it("set state: clicking primary CTA invokes onDelegatePay", () => {
    const delegate: DelegateInfo = { email: "alice@example.com", setAt: 1745000000 };
    const { onDelegatePay } = renderStep3({ delegate });

    fireEvent.click(
      screen.getByRole("button", {
        name: /purchase\.purchase\.delegatePay\.primaryCtaWithDelegate/,
      }),
    );
    expect(onDelegatePay).toHaveBeenCalledTimes(1);
  });
});
