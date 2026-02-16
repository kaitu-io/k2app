import { NavLink } from 'react-router-dom';
import { Home, ShoppingCart, Gift, User } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useUiStore } from '../stores/ui.store';

const navItems = [
  { to: '/', icon: Home, labelKey: 'dashboard:title' },
  { to: '/purchase', icon: ShoppingCart, labelKey: 'purchase:title' },
  { to: '/invite', icon: Gift, labelKey: 'invite:title', featureFlag: 'showInviteTab' as const },
  { to: '/account', icon: User, labelKey: 'account:title' },
];

export function BottomNav() {
  const { t } = useTranslation();
  const { getFeatureFlags } = useUiStore();
  const flags = getFeatureFlags();

  const visibleItems = navItems.filter(
    (item) => !item.featureFlag || flags[item.featureFlag] !== false
  );

  return (
    <nav className="relative z-40 border-t border-divider bg-bg-paper pb-[env(safe-area-inset-bottom,0px)]">
      <div className="flex h-14">
        {visibleItems.map(({ to, icon: Icon, labelKey }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              `flex-1 flex flex-col items-center justify-center min-w-[60px] max-w-[100px] transition-colors duration-200 ${
                isActive ? 'text-primary' : 'text-text-secondary'
              }`
            }
          >
            <Icon className="w-5 h-5" />
            <span className="mt-1 text-xs">{t(labelKey)}</span>
          </NavLink>
        ))}
      </div>
    </nav>
  );
}
