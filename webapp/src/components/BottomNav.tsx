import { NavLink } from 'react-router-dom';
import { Home, Server, Settings } from 'lucide-react';
import { useTranslation } from 'react-i18next';

const navItems = [
  { to: '/', icon: Home, labelKey: 'dashboard:title' },
  { to: '/servers', icon: Server, labelKey: 'common:servers' },
  { to: '/settings', icon: Settings, labelKey: 'settings:title' },
];

export function BottomNav() {
  const { t } = useTranslation();

  return (
    <nav className="flex border-t border-gray-200 bg-white">
      {navItems.map(({ to, icon: Icon, labelKey }) => (
        <NavLink
          key={to}
          to={to}
          className={({ isActive }) =>
            `flex-1 flex flex-col items-center py-2 text-xs ${
              isActive ? 'text-blue-600' : 'text-gray-500'
            }`
          }
        >
          <Icon className="w-5 h-5 mb-1" />
          <span>{t(labelKey)}</span>
        </NavLink>
      ))}
    </nav>
  );
}
