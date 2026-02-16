import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import zhCommon from './locales/zh-CN/common.json';
import zhDashboard from './locales/zh-CN/dashboard.json';
import zhAuth from './locales/zh-CN/auth.json';
import zhSettings from './locales/zh-CN/settings.json';
import zhPurchase from './locales/zh-CN/purchase.json';
import zhInvite from './locales/zh-CN/invite.json';
import zhAccount from './locales/zh-CN/account.json';
import enCommon from './locales/en-US/common.json';
import enDashboard from './locales/en-US/dashboard.json';
import enAuth from './locales/en-US/auth.json';
import enSettings from './locales/en-US/settings.json';
import enPurchase from './locales/en-US/purchase.json';
import enInvite from './locales/en-US/invite.json';
import enAccount from './locales/en-US/account.json';

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      'zh-CN': { common: zhCommon, dashboard: zhDashboard, auth: zhAuth, settings: zhSettings, purchase: zhPurchase, invite: zhInvite, account: zhAccount },
      'en-US': { common: enCommon, dashboard: enDashboard, auth: enAuth, settings: enSettings, purchase: enPurchase, invite: enInvite, account: enAccount },
    },
    fallbackLng: 'zh-CN',
    defaultNS: 'common',
    interpolation: { escapeValue: false },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
    },
  });

export default i18n;
