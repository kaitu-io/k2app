"use client";

export const dynamic = "force-dynamic";

import { useEffect } from "react";
import { useRouter } from "@/i18n/routing";
import { CircleDashed } from "lucide-react";

/**
 * Account Overview Page (Placeholder)
 *
 * This is a placeholder page that redirects to the members management page.
 * In the future, this will become the account overview page with quick access
 * to all account features.
 */
export default function AccountPage() {
  const router = useRouter();

  useEffect(() => {
    // Redirect to members page for now
    router.push("/account/members");
  }, [router]);

  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
    </div>
  );
}
