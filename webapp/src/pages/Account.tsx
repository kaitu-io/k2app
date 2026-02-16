import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { KeyRound, Smartphone, HelpCircle, ChevronRight } from 'lucide-react';
import { useAuthStore } from '../stores/auth.store';
import { useUserStore } from '../stores/user.store';
import { useUiStore } from '../stores/ui.store';
import { getPlatform } from '../platform';
import { PasswordDialog } from '../components/PasswordDialog';
import { VersionItem } from '../components/VersionItem';

interface MenuItem {
  icon: React.ComponentType<{ className?: string }>;
  labelKey: string;
  action: () => void;
}

export function Account() {
  const { t, i18n } = useTranslation('account');
  const navigate = useNavigate();
  const { logout } = useAuthStore();
  const { user, getMembershipStatus } = useUserStore();
  const { appConfig } = useUiStore();
  const [passwordDialogOpen, setPasswordDialogOpen] = useState(false);

  const membershipStatus = getMembershipStatus();
  const version = appConfig?.version ?? '0.0.0';

  const changeLanguage = async (locale: string) => {
    await i18n.changeLanguage(locale);
    const platform = getPlatform();
    await platform.syncLocale(locale);
  };

  const menuItems: MenuItem[] = [
    {
      icon: KeyRound,
      labelKey: 'menuPassword',
      action: () => setPasswordDialogOpen(true),
    },
    {
      icon: Smartphone,
      labelKey: 'menuDevices',
      action: () => navigate('/devices'),
    },
    {
      icon: HelpCircle,
      labelKey: 'menuSupport',
      action: () => navigate('/support'),
    },
  ];

  const statusColorMap: Record<string, string> = {
    active: 'from-green-500/20 to-green-600/5',
    expired: 'from-red-500/20 to-red-600/5',
  };

  const statusGradient = membershipStatus
    ? statusColorMap[membershipStatus] ?? 'from-gray-500/20 to-gray-600/5'
    : 'from-gray-500/20 to-gray-600/5';

  return (
    <div className="flex flex-col min-h-full">
      {/* Brand Banner */}
      <div
        data-testid="brand-banner"
        className="bg-[--color-bg-gradient] px-6 pt-8 pb-6"
      >
        <h1 className="text-lg font-bold text-white">{t('title')}</h1>
        {user?.email && (
          <p className="text-sm text-white/70 mt-1">{user.email}</p>
        )}
      </div>

      {/* Membership Card */}
      <div className="px-4 -mt-3">
        <div
          className={`bg-gradient-to-br ${statusGradient} rounded-xl p-4 border border-white/10`}
        >
          <div className="flex items-center justify-between mb-2">
            <span className="text-sm font-medium text-[--color-text-secondary]">
              {t('membership')}
            </span>
            <span className="text-sm font-semibold">
              {membershipStatus
                ? t(`membershipStatus.${membershipStatus}`)
                : t('membershipStatus.none')}
            </span>
          </div>
          {user?.membership && (
            <>
              <p className="text-xs text-[--color-text-secondary]">
                {t('plan', { plan: user.membership.plan })}
              </p>
              <p className="text-xs text-[--color-text-secondary] mt-1">
                {t('expireAt', { date: user.membership.expireAt })}
              </p>
            </>
          )}
        </div>
      </div>

      {/* Menu Items */}
      <div className="mt-4 px-4 space-y-1">
        {menuItems.map((item) => (
          <button
            key={item.labelKey}
            onClick={item.action}
            className="w-full flex items-center gap-3 px-4 py-3 rounded-lg hover:bg-[rgba(255,255,255,0.04)] transition-colors"
          >
            <item.icon className="w-5 h-5 text-[--color-text-secondary]" />
            <span className="flex-1 text-left text-sm">{t(item.labelKey)}</span>
            <ChevronRight className="w-4 h-4 text-[--color-text-disabled]" />
          </button>
        ))}
      </div>

      {/* Language Selector */}
      <div className="mt-4 px-4">
        <div className="px-4 py-3">
          <span className="text-sm text-[--color-text-secondary]">{t('language')}</span>
          <div className="flex gap-2 mt-2">
            <button
              onClick={() => changeLanguage('zh-CN')}
              className={`px-3 py-1.5 rounded-md text-sm transition-colors ${
                i18n.language === 'zh-CN'
                  ? 'bg-[--color-primary] text-white'
                  : 'bg-[rgba(255,255,255,0.06)] text-[--color-text-secondary] hover:bg-[rgba(255,255,255,0.1)]'
              }`}
            >
              {t('languageZh')}
            </button>
            <button
              onClick={() => changeLanguage('en-US')}
              className={`px-3 py-1.5 rounded-md text-sm transition-colors ${
                i18n.language === 'en-US'
                  ? 'bg-[--color-primary] text-white'
                  : 'bg-[rgba(255,255,255,0.06)] text-[--color-text-secondary] hover:bg-[rgba(255,255,255,0.1)]'
              }`}
            >
              {t('languageEn')}
            </button>
          </div>
        </div>
      </div>

      {/* Logout Button */}
      <div className="mt-auto px-4 pb-4">
        <button
          onClick={logout}
          className="w-full bg-[--color-error] text-white font-bold rounded-lg py-3 hover:opacity-90 transition-opacity"
        >
          {t('logout')}
        </button>
      </div>

      {/* Version Display */}
      <VersionItem version={version} />

      {/* Password Dialog */}
      <PasswordDialog
        open={passwordDialogOpen}
        onClose={() => setPasswordDialogOpen(false)}
      />
    </div>
  );
}
