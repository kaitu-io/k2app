/**
 * LoginRequiredGuard - 需要登录的路由守卫
 *
 * 包裹需要登录的页面，未登录时触发登录弹窗（但不跳转，允许用户查看页面内容）
 */

import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../stores";
import { useLoginDialogStore } from "../stores/login-dialog.store";

interface LoginRequiredGuardProps {
  children: React.ReactNode;
  /** Guard 所属页面的路径（用于 keep-alive 场景下判断是否是当前活跃页面） */
  pagePath: string;
  /** 登录弹窗显示的说明文案 key（i18n） */
  messageKey?: string;
}

// 页面路径到 i18n message key 的映射
const PAGE_MESSAGE_MAP: Record<string, string> = {
  "/account": "guard.accountMessage",
  "/devices": "guard.devicesMessage",
  "/invite": "guard.inviteMessage",
  "/invite-codes": "guard.inviteMessage",
  "/member-management": "guard.memberManagementMessage",
  "/pro-histories": "guard.proHistoriesMessage",
};

export function LoginRequiredGuard({
  children,
  pagePath,
  messageKey,
}: LoginRequiredGuardProps) {
  const { t } = useTranslation("auth");
  const location = useLocation();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isAuthChecking = useAuthStore((s) => s.isAuthChecking);
  const openLoginDialog = useLoginDialogStore((s) => s.open);

  useEffect(() => {
    // 等待认证检查完成
    if (isAuthChecking) return;

    // 只有当前活跃页面的 Guard 才触发副作用（解决 keep-alive 场景下多个 Guard 同时响应的问题）
    if (location.pathname !== pagePath) return;

    // 未登录时触发登录弹窗并返回首页
    if (!isAuthenticated) {
      const message = messageKey
        ? t(messageKey)
        : PAGE_MESSAGE_MAP[pagePath]
          ? t(PAGE_MESSAGE_MAP[pagePath])
          : t("guard.defaultMessage", "请登录以继续");

      openLoginDialog({
        trigger: `guard:${pagePath}`,
        redirectPath: pagePath,
        message,
      });
      // 不再跳转，让用户保持在当前页面查看内容
    }
  }, [
    isAuthenticated,
    isAuthChecking,
    location.pathname,
    pagePath,
    messageKey,
    openLoginDialog,
    t,
  ]);

  // 认证检查中时不渲染（避免闪烁）
  if (isAuthChecking) {
    return null;
  }

  // 即使未登录也渲染子组件，让页面可见
  return <>{children}</>;
}

export default LoginRequiredGuard;
