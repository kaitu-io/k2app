"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/contexts/AuthContext";
import { CircleDashed } from "lucide-react";
import ManagerSidebar from "@/components/manager-sidebar";

export default function ManagerLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const { isAuthenticated, isAuthLoading } = useAuth();

  useEffect(() => {
    if (isAuthLoading) {
      return; // Wait for the auth state to be loaded
    }
    if (!isAuthenticated) {
      router.push('/zh-CN/login');
    }
  }, [isAuthenticated, isAuthLoading, router]);

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