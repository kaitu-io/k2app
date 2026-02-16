import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { useEffect } from 'react';
import { Layout } from './components/Layout';
import { ServiceReadiness } from './components/ServiceReadiness';
import { UpdatePrompt } from './components/UpdatePrompt';
import { ErrorBoundary } from './components/ErrorBoundary';
import { LoginDialog } from './components/LoginDialog';
import { ForceUpgradeDialog } from './components/ForceUpgradeDialog';
import { AnnouncementBanner } from './components/AnnouncementBanner';
import { ServiceAlert } from './components/ServiceAlert';
import { AlertContainer } from './components/AlertContainer';
import { LoginRequiredGuard } from './components/LoginRequiredGuard';
import { MembershipGuard } from './components/MembershipGuard';
import { Dashboard } from './pages/Dashboard';
import { Purchase } from './pages/Purchase';
import { InviteHub } from './pages/InviteHub';
import { Account } from './pages/Account';
import { Devices } from './pages/Devices';
import { MemberManagement } from './pages/MemberManagement';
import { ProHistory } from './pages/ProHistory';
import { MyInviteCodeList } from './pages/MyInviteCodeList';
import { UpdateLoginEmail } from './pages/UpdateLoginEmail';
import { DeviceInstall } from './pages/DeviceInstall';
import { FAQ } from './pages/FAQ';
import { Issues } from './pages/Issues';
import { IssueDetail } from './pages/IssueDetail';
import { SubmitTicket } from './pages/SubmitTicket';
import { Changelog } from './pages/Changelog';
import { Discover } from './pages/Discover';
import { useAuthStore } from './stores/auth.store';
import { useUiStore } from './stores/ui.store';
import './i18n';
import './app.css';

declare const __APP_VERSION__: string;

export default function App() {
  const { restoreSession } = useAuthStore();
  const { loadAppConfig } = useUiStore();

  useEffect(() => {
    restoreSession();
    loadAppConfig();
  }, [restoreSession, loadAppConfig]);

  return (
    <ErrorBoundary>
      <BrowserRouter>
        <ServiceReadiness>
          <ForceUpgradeDialog currentVersion={__APP_VERSION__} />
          <AnnouncementBanner />
          <ServiceAlert />
          <UpdatePrompt />
          <AlertContainer />
          <LoginDialog />
          <Routes>
            <Route element={<Layout />}>
              {/* Tab routes (keep-alive managed by Layout) */}
              <Route path="/" element={<Dashboard />} />
              <Route path="/purchase" element={<Purchase />} />
              <Route path="/invite" element={<LoginRequiredGuard><InviteHub /></LoginRequiredGuard>} />
              <Route path="/account" element={<Account />} />

              {/* Sub-pages: login required */}
              <Route path="/devices" element={<LoginRequiredGuard><Devices /></LoginRequiredGuard>} />
              <Route path="/member-management" element={<LoginRequiredGuard><MemberManagement /></LoginRequiredGuard>} />
              <Route path="/pro-histories" element={<LoginRequiredGuard><ProHistory /></LoginRequiredGuard>} />
              <Route path="/invite-codes" element={<LoginRequiredGuard><MyInviteCodeList /></LoginRequiredGuard>} />
              <Route path="/issues" element={<LoginRequiredGuard><Issues /></LoginRequiredGuard>} />
              <Route path="/issues/:number" element={<LoginRequiredGuard><IssueDetail /></LoginRequiredGuard>} />

              {/* Sub-pages: membership required */}
              <Route path="/update-email" element={<MembershipGuard><UpdateLoginEmail /></MembershipGuard>} />
              <Route path="/submit-ticket" element={<MembershipGuard><SubmitTicket /></MembershipGuard>} />

              {/* Sub-pages: no guard */}
              <Route path="/device-install" element={<DeviceInstall />} />
              <Route path="/faq" element={<FAQ />} />
              <Route path="/changelog" element={<Changelog />} />
              <Route path="/discover" element={<Discover />} />
            </Route>
          </Routes>
        </ServiceReadiness>
      </BrowserRouter>
    </ErrorBoundary>
  );
}
