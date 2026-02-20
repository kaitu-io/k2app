"use client";

import { AuthProvider } from "@/contexts/AuthContext";

export function ManagerAuthWrapper({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <AuthProvider>
      {children}
    </AuthProvider>
  )
}
