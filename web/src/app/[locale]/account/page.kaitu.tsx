"use client";

export const dynamic = "force-dynamic";

import KaituAccountClient from "./KaituAccountClient";

// 开途构建专属（page.kaitu.tsx）：授权到期模型（expiredAt）。Overleap 的订阅模型在 page.overleap.tsx。
export default function AccountPage() {
  return <KaituAccountClient />;
}
