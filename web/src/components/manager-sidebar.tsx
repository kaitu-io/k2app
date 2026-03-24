"use client";

import { usePathname } from "next/navigation";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { Package, Users, Server, Receipt, Mail, Tag, Wallet, FileText, Activity, LogOut, Gauge, UserCircle, ClipboardList, Cloud, BarChart3, Key } from "lucide-react";
import Image from "next/image";
import { useAuth } from "@/contexts/AuthContext";
import { Button } from "@/components/ui/button";

// 角色位掩码常量（与后端 api/type.go 一致）
const RoleMarketing = 8;
const RoleDevopsViewer = 16;  // DevOps 只读
const RoleDevopsEditor = 32;  // DevOps 读写
const RoleSupport   = 64;

// requiredRole: 0 = superadmin only, 位掩码 = 需要其中任一角色
interface MenuItem {
  href: string;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
}

interface MenuGroup {
  title: string;
  requiredRole: number;
  items: MenuItem[];
}

const menuGroups: MenuGroup[] = [
  {
    title: "用户与订单",
    requiredRole: 0, // superadmin only
    items: [
      { href: "/manager/users", icon: Users, label: "用户管理" },
      { href: "/manager/orders", icon: Receipt, label: "订单管理" },
      { href: "/manager/withdraws", icon: Wallet, label: "提现管理" },
    ]
  },
  {
    title: "运营配置",
    requiredRole: 0, // superadmin only
    items: [
      { href: "/manager/plans", icon: Package, label: "套餐管理" },
      { href: "/manager/campaigns", icon: Tag, label: "优惠活动" },
      { href: "/manager/license-keys", icon: Key, label: "授权码" },
    ]
  },
  {
    title: "基础设施",
    requiredRole: RoleDevopsViewer | RoleDevopsEditor,
    items: [
      { href: "/manager/cloud", icon: Cloud, label: "节点部署" },
      { href: "/manager/nodes", icon: Server, label: "节点管理" },
      { href: "/manager/tunnels", icon: Server, label: "隧道管理" },
    ]
  },
  {
    title: "客户支持",
    requiredRole: RoleSupport,
    items: [
      { href: "/manager/users", icon: Users, label: "用户查询" },
      { href: "/manager/tasks", icon: ClipboardList, label: "工单管理" },
    ]
  },
  {
    title: "营销管理",
    requiredRole: RoleMarketing,
    items: [
      { href: "/manager/edm/create-task", icon: Mail, label: "邮件营销" },
      { href: "/manager/edm/templates", icon: FileText, label: "邮件模板" },
      { href: "/manager/edm/send-logs", icon: Activity, label: "发送日志" },
      { href: "/manager/retailers", icon: UserCircle, label: "分销商" },
      { href: "/manager/retailers/todos", icon: ClipboardList, label: "分销待办" },
    ]
  },
  {
    title: "系统监控",
    requiredRole: 0, // superadmin only
    items: [
      { href: "/manager/usages", icon: BarChart3, label: "使用统计" },
      { href: "/manager/asynqmon", icon: Gauge, label: "任务队列" },
    ]
  },
];

const ManagerSidebar = () => {
  const pathname = usePathname();
  const { user, logout } = useAuth();

  const isSuperAdmin = user?.isAdmin === true;
  const userRoles = user?.roles ?? 1;

  // 超管看全部，其他角色按位掩码过滤
  const visibleGroups = menuGroups.filter(group => {
    if (isSuperAdmin) return true;
    if (group.requiredRole === 0) return false;
    return (userRoles & group.requiredRole) !== 0;
  });

  const isActive = (itemHref: string) => {
    if (pathname === itemHref) return true;
    if (pathname.startsWith(itemHref + '/')) return true;
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
            {visibleGroups.map((group, groupIndex) => (
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
