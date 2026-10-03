import { getTranslations, setRequestLocale } from "next-intl/server";
import type { Metadata } from "next";
import { routing } from "@/i18n/routing";
import RouterAccountClient from "./RouterAccountClient";

type Locale = (typeof routing.locales)[number];

export const dynamic = "force-dynamic";

// 路由器版账户页。
// 服务端 page 只负责 metadata（文档标题）；鉴权仍由客户端 account/layout.tsx 承担，
// 页面主体是客户端组件 RouterAccountClient。
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const t = await getTranslations({ locale: rawLocale as Locale, namespace: "routers" });
  return {
    title: t("edition.account.metaTitle"),
  };
}

export default async function AccountRouterPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  setRequestLocale(rawLocale as Locale);
  return <RouterAccountClient />;
}
