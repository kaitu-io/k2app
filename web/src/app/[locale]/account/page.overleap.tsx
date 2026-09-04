"use client";

export const dynamic = "force-dynamic";

import OverleapAccountClient from "./OverleapAccountClient";

// overleap 构建专属（page.overleap.tsx）：Stripe/Apple 的 subscriptions[] 模型。kaitu在 page.kaitu.tsx。
export default function AccountPage() {
  return <OverleapAccountClient />;
}
