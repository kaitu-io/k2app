import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { Layout } from './components/Layout';
import { ServiceReadiness } from './components/ServiceReadiness';
import { Settings } from './pages/Settings';
import { Login } from './pages/Login';
import { useAuthStore } from './stores/auth.store';
import { useEffect } from 'react';
import './i18n';
import './app.css';

function PlaceholderDashboard() {
  return <div className="p-4">Dashboard (W3)</div>;
}

function PlaceholderServers() {
  return <div className="p-4">Servers (W4)</div>;
}

function AuthGuard({ children }: { children: React.ReactNode }) {
  const { isLoggedIn } = useAuthStore();
  if (!isLoggedIn) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export default function App() {
  const { restoreSession } = useAuthStore();

  useEffect(() => {
    restoreSession();
  }, [restoreSession]);

  return (
    <BrowserRouter>
      <ServiceReadiness>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<AuthGuard><Layout /></AuthGuard>}>
            <Route path="/" element={<PlaceholderDashboard />} />
            <Route path="/servers" element={<PlaceholderServers />} />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Routes>
      </ServiceReadiness>
    </BrowserRouter>
  );
}
