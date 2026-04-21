"use client";

export const dynamic = "force-dynamic";

import { useEffect } from "react";
import { useRouter } from "@/i18n/routing";
import { CircleDashed } from "lucide-react";

export default function AccountPage() {
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
