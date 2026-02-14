import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useEffect } from 'react';
import { Layout } from './components/Layout';
import { ServiceReadiness } from './components/ServiceReadiness';
import { Settings } from './pages/Settings';
import { Dashboard } from './pages/Dashboard';
import { Login } from './pages/Login';
import { Servers } from './pages/Servers';
import { useAuthStore } from './stores/auth.store';
import './i18n';
import './app.css';

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
            <Route path="/" element={<Dashboard />} />
            <Route path="/servers" element={<Servers />} />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Routes>
      </ServiceReadiness>
    </BrowserRouter>
  );
}
