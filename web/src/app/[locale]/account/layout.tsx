"use client";

export const dynamic = "force-dynamic";

import { useEffect } from "react";
import { useRouter } from "@/i18n/routing";
import { useAuth } from "@/contexts/AuthContext";
import { CircleDashed, Users, Home, CreditCard, Wallet, LogOut } from "lucide-react";
import { useTranslations } from "next-intl";
import { Link } from "@/i18n/routing";
import { usePathname } from "@/i18n/routing";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";

export default function AccountLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const t = useTranslations();
  const router = useRouter();
  const pathname = usePathname();
  const { isAuthenticated, isAuthLoading, logout } = useAuth();

  useEffect(() => {
    if (isAuthLoading) {
      return; // Wait for the auth state to be loaded
    }
    if (!isAuthenticated) {
      // Redirect to login with current path as return URL
      router.push(`/login?redirect=${encodeURIComponent(pathname)}`);
    }
  }, [isAuthenticated, isAuthLoading, router, pathname]);

  if (isAuthLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!isAuthenticated) {
    return null; // Will redirect
  }

  // Navigation items for account section
  const navItems = [
    {
      href: "/purchase",
      label: t("admin.account.renew.title"),
      icon: CreditCard,
    },
    {
      href: "/account/members",
      label: t("admin.account.members.title"),
      icon: Users,
    },
    {
      href: "/account/delegate",
      label: t("admin.account.delegate.title"),
      icon: Users,
    },
    {
      href: "/account/wallet",
      label: t("admin.account.wallet.title"),
      icon: Wallet,
    },
    // Reserved for future features
    // {
    //   href: "/account/devices",
    //   label: t("admin.account.devices.title"),
    //   icon: Smartphone,
    // },
    // {
    //   href: "/account/payment-history",
    //   label: t("admin.account.paymentHistory.title"),
    //   icon: CreditCard,
    // },
  ];

  return (
    <div className="container mx-auto py-6 px-4">
      {/* Back to Home */}
      <div className="mb-4">
        <Link href="/">
          <Button variant="ghost" size="sm" className="gap-2">
            <Home className="h-4 w-4" />
            {t("nav.nav.backToHome")}
          </Button>
        </Link>
      </div>

      {/* Page Title */}
      <div className="mb-6">
        <h1 className="text-3xl font-bold">{t("admin.account.title")}</h1>
        <p className="text-muted-foreground mt-1">{t("admin.account.subtitle")}</p>
      </div>

      {/* Mobile Navigation */}
      <div className="md:hidden mb-4">
        <div className="flex gap-2 overflow-x-auto pb-2">
          {navItems.map((item) => {
            const isActive = pathname.includes(item.href);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-2 px-4 py-2 rounded-lg text-sm whitespace-nowrap transition-colors",
                  isActive
                    ? "bg-primary text-primary-foreground"
                    : "bg-muted hover:bg-muted/80"
                )}
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </Link>
            );
          })}
          <Button
            variant="ghost"
            size="sm"
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm whitespace-nowrap bg-muted hover:bg-muted/80"
            onClick={() => logout()}
          >
            <LogOut className="h-4 w-4" />
            {t("nav.nav.logout")}
          </Button>
        </div>
      </div>

      <div className="flex flex-col md:flex-row gap-6">
        {/* Desktop Sidebar Navigation */}
        <aside className="hidden md:block md:w-[220px] flex-shrink-0">
          <nav className="space-y-2">
            {navItems.map((item) => {
              const isActive = pathname.includes(item.href);
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    "flex items-center gap-3 px-4 py-2 rounded-lg text-sm transition-colors",
                    isActive
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-muted"
                  )}
                >
                  <Icon className="h-4 w-4" />
                  {item.label}
                </Link>
              );
            })}
          </nav>
          <div className="mt-4 pt-4 border-t">
            <Button
              variant="ghost"
              className="w-full justify-start gap-3 text-sm"
              onClick={() => logout()}
            >
              <LogOut className="h-4 w-4" />
              {t("nav.nav.logout")}
            </Button>
          </div>
        </aside>

        {/* Main Content */}
        <main className="flex-1 min-w-0">{children}</main>
      </div>
    </div>
  );
}
