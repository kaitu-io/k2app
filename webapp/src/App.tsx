import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Layout } from './components/Layout';
import { ServiceReadiness } from './components/ServiceReadiness';
import { Settings } from './pages/Settings';
import { Dashboard } from './pages/Dashboard';
import './i18n';
import './app.css';

function PlaceholderServers() {
  return <div className="p-4">Servers (W4)</div>;
}

function PlaceholderLogin() {
  return <div className="p-4">Login (W2)</div>;
}

export default function App() {
  return (
    <BrowserRouter>
      <ServiceReadiness>
        <Routes>
          <Route path="/login" element={<PlaceholderLogin />} />
          <Route element={<Layout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/servers" element={<PlaceholderServers />} />
            <Route path="/settings" element={<Settings />} />
          </Route>
        </Routes>
      </ServiceReadiness>
    </BrowserRouter>
  );
}
