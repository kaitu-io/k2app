"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { Network, Package, Users, Server, Receipt, Mail, Tag, Wallet, FileText, Activity, LogOut, Gauge, PenSquare, UserCircle, ClipboardList, Cloud } from "lucide-react";
import Image from "next/image";
import { useAuth } from "@/contexts/AuthContext";
import { Button } from "@/components/ui/button";

const ManagerSidebar = () => {
  const pathname = usePathname();
  const { logout } = useAuth();

  const menuGroups = [
    {
      title: "系统管理",
      items: [
        { href: "/manager/plans", icon: Package, label: "套餐管理" },
        { href: "/manager/cloud", icon: Cloud, label: "节点部署" },
        { href: "/manager/nodes", icon: Server, label: "节点运维" },
        { href: "/manager/tunnels", icon: Network, label: "隧道管理" },
      ]
    },
    {
      title: "业务管理",
      items: [
        { href: "/manager/users", icon: Users, label: "用户管理" },
        { href: "/manager/orders", icon: Receipt, label: "订单管理" },
        { href: "/manager/campaigns", icon: Tag, label: "优惠活动管理" },
        { href: "/manager/withdraws", icon: Wallet, label: "提现管理" },
      ]
    },
    {
      title: "运营管理",
      items: [
        { href: "/manager/retailers", icon: UserCircle, label: "分销商" },
        { href: "/manager/retailers/todos", icon: ClipboardList, label: "分销待办" },
      ]
    },
    {
      title: "内容管理",
      items: [
        { href: "/manager/cms", icon: PenSquare, label: "CMS 内容管理" },
      ]
    },
    {
      title: "营销管理",
      items: [
        { href: "/manager/edm/create-task", icon: Mail, label: "邮件营销" },
        { href: "/manager/edm/templates", icon: FileText, label: "邮件模板" },
        { href: "/manager/edm/send-logs", icon: Activity, label: "邮件发送日志" },
      ]
    },
    {
      title: "系统监控",
      items: [
        { href: "/manager/asynqmon", icon: Gauge, label: "任务队列监控" },
      ]
    }
  ];

  // Helper function to check if a menu item is active
  const isActive = (itemHref: string) => {
    // Exact match
    if (pathname === itemHref) {
      return true;
    }

    // For paths with sub-routes, check if it starts with the href followed by /
    if (pathname.startsWith(itemHref + '/')) {
      return true;
    }

    return false;
  };

  return (
    <div className="h-full">
      <div className="flex h-full flex-col gap-2">
        <div className="flex h-14 items-center border-b px-4 lg:h-[60px] lg:px-6">
          <Link href="/manager" className="flex items-center gap-2 font-semibold">
            <Image
              src="/kaitu-icon.png"
              alt="Kaitu Logo"
              width={24}
              height={24}
              className="rounded-md"
            />
            <span className="">后台管理</span>
          </Link>
        </div>
        <div className="flex-1 overflow-auto">
          <nav className="grid items-start px-2 text-sm font-medium lg:px-4 gap-4 py-4">
            {menuGroups.map((group, groupIndex) => (
              <div key={groupIndex}>
                <div className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  {group.title}
                </div>
                <div className="space-y-1">
                  {group.items.map((item) => {
                    const Icon = item.icon;
                    return (
                      <Link
                        key={item.href}
                        href={item.href}
                        className={cn(
                          "flex items-center gap-3 rounded-lg px-3 py-2 text-muted-foreground transition-all hover:text-primary",
                          isActive(item.href) && "text-primary bg-muted"
                        )}
                      >
                        <Icon className="h-4 w-4" />
                        {item.label}
                      </Link>
                    );
                  })}
                </div>
              </div>
            ))}
          </nav>
        </div>
        <div className="mt-auto p-4 border-t">
          <Button
            variant="ghost"
            className="w-full justify-start text-muted-foreground hover:text-primary hover:bg-muted"
            onClick={() => logout()}
          >
            <LogOut className="h-4 w-4 mr-3" />
            退出登录
          </Button>
        </div>
      </div>
    </div>
  );
};

export default ManagerSidebar;
