"use client";

export const dynamic = "force-dynamic";

import OverleapAccountClient from "./OverleapAccountClient";

// Overleap 构建专属（page.overleap.tsx）：Stripe/Apple 的 subscriptions[] 模型。开途在 page.kaitu.tsx。
export default function AccountPage() {
  return <OverleapAccountClient />;
}
