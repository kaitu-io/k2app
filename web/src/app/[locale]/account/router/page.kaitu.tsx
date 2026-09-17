"use client";

export const dynamic = "force-dynamic";

import RouterAccountClient from "./RouterAccountClient";

// kaitu构建专属（page.kaitu.tsx）：路由器版是 kaitu-only 功能（Brand.features.routers）。
// layout.tsx 是客户端鉴权，本页跟 account/page.kaitu.tsx 一样做客户端页。
export default function AccountRouterPage() {
  return <RouterAccountClient />;
}
