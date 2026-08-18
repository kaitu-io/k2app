"use client";

export const dynamic = "force-dynamic";

import { siteBrand } from "@/lib/brands";
import OverleapAccountClient from "./OverleapAccountClient";
import KaituAccountClient from "./KaituAccountClient";

// 两个品牌的订阅模型不同（Stripe/Apple 的 subscriptions[] vs 授权到期 expiredAt），
// 各自一份客户端组件，不强行合并。
export default function AccountPage() {
  return siteBrand().id === "overleap" ? <OverleapAccountClient /> : <KaituAccountClient />;
}
