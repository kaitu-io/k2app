"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/contexts/AuthContext";
import { CircleDashed } from "lucide-react";
import ManagerSidebar from "@/components/manager-sidebar";

// 任何 ops 角色（排除 RoleUser=1）
const ANY_OPS_ROLE = 0xFFFFFFFE; // 除 bit 0 外的所有位

export default function ManagerLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const { user, isAuthenticated, isAuthLoading } = useAuth();

  useEffect(() => {
    if (isAuthLoading) {
      return;
    }
    if (!isAuthenticated || !user) {
      router.push('/zh-CN/login');
      return;
    }
    // 超级管理员或拥有任何 ops 角色的用户可访问
    const hasAccess = user.isAdmin || (user.roles & ANY_OPS_ROLE) !== 0;
    if (!hasAccess) {
      router.push('/zh-CN/login');
    }
  }, [isAuthenticated, isAuthLoading, user, router]);

  if (isAuthLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <CircleDashed className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="flex min-h-screen">
      <div className="hidden md:block w-[280px] flex-shrink-0">
        <div className="fixed h-screen w-[280px] border-r bg-muted/40 overflow-y-auto">
          <ManagerSidebar />
        </div>
      </div>
      <main className="flex-1 p-6 overflow-auto">{children}</main>
    </div>
  );
} 