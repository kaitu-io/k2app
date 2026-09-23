"use client";

import { useTranslations } from "next-intl";
import { Link } from "@/i18n/routing";
import { useEmbedMode } from "@/hooks/useEmbedMode";
import { Button } from "@/components/ui/button";
import Header from "@/components/Header";
import Footer from "@/components/Footer";
import { CheckCircle2Icon } from "lucide-react";

/**
 * Stripe Checkout 成功回跳落点。故意不发任何请求：代付人未登录也要能看到；
 * 买家点「查看账号」由 /account 现有逻辑展示到期时间。入账走服务端 webhook。
 */
export default function PayResultClient({ orderUuid }: { orderUuid: string }) {
  const t = useTranslations("payResult");
  const { showNavigation, showFooter } = useEmbedMode();

  return (
    <>
      {showNavigation && <Header />}
      <div className="container max-w-xl mx-auto py-12 px-4 text-center space-y-6">
        <CheckCircle2Icon className="w-16 h-16 mx-auto text-emerald-600" aria-hidden />
        <h1 className="text-3xl font-black text-foreground">{t("title")}</h1>
        <p className="text-muted-foreground">{t("subtitle")}</p>
        <p className="text-sm">
          <span className="font-semibold">{t("orderLabel")}</span>
          <span className="ml-2 font-mono break-all">{orderUuid}</span>
        </p>
        <div className="flex flex-col sm:flex-row gap-3 justify-center">
          <Button asChild>
            <Link href="/account">{t("account")}</Link>
          </Button>
          <Button asChild variant="outline">
            <Link href="/install">{t("install")}</Link>
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">{t("help")}</p>
      </div>
      {showFooter && <Footer />}
    </>
  );
}
