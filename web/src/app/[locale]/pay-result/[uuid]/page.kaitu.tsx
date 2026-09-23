import { getTranslations, setRequestLocale } from "next-intl/server";
import type { Metadata } from "next";
import { routing } from "@/i18n/routing";
import PayResultClient from "./PayResultClient";

type Locale = (typeof routing.locales)[number];

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const t = await getTranslations({ locale: rawLocale as Locale, namespace: "payResult" });
  // 订单落地页：不进索引
  return { title: t("title"), robots: { index: false, follow: false } };
}

// kaitu构建专属（page.kaitu.tsx）：NextPay → Stripe Checkout 的成功回跳。overleap 的 Stripe 订阅回跳在 /account。
export default async function PayResultPage({
  params,
}: {
  params: Promise<{ locale: string; uuid: string }>;
}) {
  const { locale: rawLocale, uuid } = await params;
  setRequestLocale(rawLocale as Locale);
  return <PayResultClient orderUuid={uuid} />;
}
