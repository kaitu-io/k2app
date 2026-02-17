import { useState, useEffect } from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import * as Sentry from "@sentry/react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
} from "@mui/material";
import Layout from "./components/Layout";
import MyInviteCodeList from "./pages/MyInviteCodeList";
import Devices from "./pages/Devices";
import DeviceInstall from "./pages/DeviceInstall";
import UpdateLoginEmail from "./pages/UpdateLoginEmail";
import { ThemeProvider } from "./contexts/ThemeContext";
import AuthGate from "./components/AuthGate";
import AlertContainer from "./components/AlertContainer";
import MembershipGuard from "./components/MembershipGuard";
import ForceUpgradeDialog from "./components/ForceUpgradeDialog";
import LoginDialog from "./components/LoginDialog";
import LoginRequiredGuard from "./components/LoginRequiredGuard";
import { UpdateNotification } from "./components/UpdateNotification";

import Tunnels from "./pages/Tunnels";
import Purchase from "./pages/Purchase";
import ProHistory from "./pages/ProHistory";
import FAQ from "./pages/FAQ";
import Issues from "./pages/Issues";
import IssueDetail from "./pages/IssueDetail";
import SubmitTicket from "./pages/SubmitTicket";
import ServiceError from "./pages/ServiceError";
import MemberManagement from "./pages/MemberManagement";
import Changelog from "./pages/Changelog";
import { getCurrentAppConfig } from "./config/apps";


// 应用路由组件
function AppRoutes() {
  const { t } = useTranslation();
  const [error, setError] = useState<string | null>(null);

  // Get app configuration for conditional routing
  const appConfig = getCurrentAppConfig();

  return (
    <>
      <Routes>
        <Route path="/" element={<Layout />}>
          {/* Tab pages - rendered in Layout for keep-alive, marked as null here */}
          <Route index element={null} />
          {appConfig.features.invite && <Route path="invite" element={null} />}
          {appConfig.features.discover && <Route path="discover" element={null} />}
          <Route path="account" element={null} />

          {/* Non-Tab routes */}
          {/* Purchase 移出 keep-alive，每次访问重新渲染（避免与 LoginRequiredGuard 冲突） */}
          <Route path="purchase" element={<Purchase />} />
          <Route path="tunnels" element={<Tunnels />} />
          <Route path="changelog" element={<Changelog />} />
          <Route path="service-error" element={<ServiceError />} />
          <Route path="devices" element={<LoginRequiredGuard pagePath="/devices"><Devices /></LoginRequiredGuard>} />

          {/* Conditional routes based on app configuration */}
          {appConfig.features.proHistory && (
            <Route path="pro-histories" element={<LoginRequiredGuard pagePath="/pro-histories"><ProHistory /></LoginRequiredGuard>} />
          )}

          {appConfig.features.invite && (
            <Route path="invite-codes" element={<LoginRequiredGuard pagePath="/invite-codes"><MyInviteCodeList /></LoginRequiredGuard>} />
          )}

          {appConfig.features.memberManagement && (
            <Route path="member-management" element={<LoginRequiredGuard pagePath="/member-management"><MemberManagement /></LoginRequiredGuard>} />
          )}

          {appConfig.features.deviceInstall && (
            <Route path="device-install" element={<DeviceInstall />} />
          )}

          {appConfig.features.feedback && (
            <>
              <Route path="faq" element={<FAQ />} />
              <Route path="issues" element={<LoginRequiredGuard pagePath="/issues"><Issues /></LoginRequiredGuard>} />
              <Route path="issues/:number" element={<LoginRequiredGuard pagePath="/issues"><IssueDetail /></LoginRequiredGuard>} />
              <Route path="submit-ticket" element={<MembershipGuard><SubmitTicket /></MembershipGuard>} />
            </>
          )}

          {appConfig.features.updateLoginEmail && (
            <Route path="update-email" element={<MembershipGuard><UpdateLoginEmail /></MembershipGuard>} />
          )}
        </Route>
      </Routes>

      {error && (
        <Dialog open={!!error} onClose={() => setError(null)}>
          <DialogTitle>{t('startup:app.error')}</DialogTitle>
          <DialogContent>{error}</DialogContent>
          <DialogActions>
            <Button onClick={() => setError(null)}>{t('common:common.ok')}</Button>
          </DialogActions>
        </Dialog>
      )}

      {/* Global login dialog */}
      <LoginDialog />

      {/* Force upgrade dialog (from app config minClientVersion) */}
      <ForceUpgradeDialog />

      {/* Update notification (desktop only) */}
      <UpdateNotification />

      {/* Global alert container */}
      <AlertContainer />
    </>
  );
}

function App() {
  // 禁用右键上下文菜单
  useEffect(() => {
    const handleContextMenu = (e: MouseEvent) => {
      e.preventDefault();
      return false;
    };

    document.addEventListener('contextmenu', handleContextMenu);

    return () => {
      document.removeEventListener('contextmenu', handleContextMenu);
    };
  }, []);

  return (
    <ThemeProvider>
      {/* 状态轮询管理器 */}

      <BrowserRouter>
        <AuthGate>
          <AppRoutes />
        </AuthGate>
      </BrowserRouter>
    </ThemeProvider>
  );
}

// Error boundary fallback - MUST NOT use any hooks or context
// (rendered outside of providers when error occurs)
function ErrorFallback({ error, resetError }: { error: unknown; resetError: () => void }) {
  return (
    <div style={{
      padding: '20px',
      textAlign: 'center',
      minHeight: '100vh',
      display: 'flex',
      flexDirection: 'column',
      justifyContent: 'center',
      alignItems: 'center',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif'
    }}>
      <h1>Application Error</h1>
      <p>Something went wrong. Please try refreshing the page.</p>
      <details style={{ marginTop: '20px', textAlign: 'left', maxWidth: '600px' }}>
        <summary>Error details</summary>
        <pre style={{ whiteSpace: 'pre-wrap', fontSize: '12px', color: '#666' }}>
          {error?.toString()}
        </pre>
      </details>
      <button
        onClick={resetError}
        style={{
          marginTop: '20px',
          padding: '10px 20px',
          backgroundColor: '#1976d2',
          color: 'white',
          border: 'none',
          borderRadius: '4px',
          cursor: 'pointer',
          fontSize: '14px',
          fontWeight: 500
        }}
      >
        Retry
      </button>
    </div>
  );
}

// Simple error boundary
export default Sentry.withErrorBoundary(App, {
  fallback: ErrorFallback,
  showDialog: false,
});
