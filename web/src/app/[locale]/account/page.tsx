"use client";

export const dynamic = "force-dynamic";

import KaituAccountClient from "./KaituAccountClient";

// 授权到期模型（expiredAt）。
export default function AccountPage() {
  return <KaituAccountClient />;
}
