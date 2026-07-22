"use client";

export const dynamic = "force-dynamic";

import { useEffect } from "react";
import { useRouter } from "@/i18n/routing";
import { CircleDashed } from "lucide-react";
import { siteBrand } from "@/lib/brands";
import OverleapAccountClient from "./OverleapAccountClient";

// 另一品牌无独立账户首页——保持历史行为：跳 /purchase
function KaituAccountRedirect() {
  const router = useRouter();

  useEffect(() => {
    router.push("/purchase");
  }, [router]);

  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
    </div>
  );
}

export default function AccountPage() {
  return siteBrand().id === "overleap" ? <OverleapAccountClient /> : <KaituAccountRedirect />;
}
