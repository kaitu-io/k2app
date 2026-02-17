/**
 * AuthGate - 认证状态路由
 *
 * 已改为开放访问模式：
 * - isAuthChecking: 显示 LoadingPage（等待认证状态确认）
 * - 其他情况: 直接显示 children（无论是否登录）
 *
 * 需要登录的页面使用 LoginRequiredGuard 包裹
 */

import React from 'react';
import { useAuthStore } from '../stores';
import LoadingPage from './LoadingPage';

interface AuthGateProps {
  children: React.ReactNode;
}

export function AuthGate({ children }: AuthGateProps) {
  const isAuthChecking = useAuthStore((s) => s.isAuthChecking);

  // 仅在检查认证状态时显示加载页面
  if (isAuthChecking) {
    return <LoadingPage />;
  }

  // 开放访问，无论是否登录都显示内容
  return <>{children}</>;
}

export default AuthGate;
