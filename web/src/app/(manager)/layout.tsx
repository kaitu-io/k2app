import { ManagerAuthWrapper } from "./auth-wrapper";
import { Inter } from "next/font/google";
import "../globals.css";

const inter = Inter({ subsets: ["latin"] });

// Force all manager routes to be dynamic (not statically generated)
export const dynamic = 'force-dynamic'

export default function ManagerGroupLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body className={inter.className} suppressHydrationWarning>
        <ManagerAuthWrapper>
          {children}
        </ManagerAuthWrapper>
      </body>
    </html>
  )
}
