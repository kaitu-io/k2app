import React from "react";
import { Navigate, useLocation } from "react-router-dom";
import { useUser } from "../hooks/useUser";

interface MembershipGuardProps {
  children: React.ReactNode;
}

export default function MembershipGuard({ children }: MembershipGuardProps) {
  const { isExpired, loading, user } = useUser();
  const location = useLocation();
  
  // 允许访问的页面（即使在过期状态下）
  const allowedPaths = ['/purchase', '/account'];
  
  // 如果正在加载用户信息或用户数据还未加载完成，暂时不做重定向
  if (loading || !user) {
    return <>{children}</>;
  }
  
  // 添加调试日志
  // console.debug('[MembershipGuard] 检查访问权限:', {
  //   pathname: location.pathname,
  //   isExpired,
  //   loading,
  //   hasUser: !!user,
  //   allowedPaths,
  //   shouldRedirect: isExpired && !allowedPaths.includes(location.pathname)
  // });
  
  // 只有在用户数据已加载且用户过期的情况下，才进行重定向
  if (isExpired && !allowedPaths.includes(location.pathname)) {
    console.debug('[MembershipGuard] 重定向到购买页面');
    return <Navigate to="/purchase" replace />;
  }
  
  return <>{children}</>;
} 