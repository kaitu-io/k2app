/**
 * App Links Hook
 *
 * 基于 useAppConfig 构建应用链接
 * 支持多语言
 */

import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { normalizeLanguageCode } from '../i18n/i18n';
import { useAppConfig } from './useAppConfig';

export interface AppLinks {
  // Discovery page link
  discoveryUrl: string;

  // Legal document links
  privacyPolicyUrl: string;
  termsOfServiceUrl: string;

  // Wallet link
  walletUrl: string;

  // Retailer management link
  retailerRulesUrl: string;

  // Security software whitelist help link
  securitySoftwareHelpUrl: string;

  // Changelog link
  changelogUrl: string;
}

/**
 * 使用应用链接
 *
 * @example
 * ```tsx
 * function MyComponent() {
 *   const { links, loading, error } = useAppLinks();
 *
 *   return (
 *     <a href={links.privacyPolicyUrl} target="_blank">
 *       隐私政策
 *     </a>
 *   );
 * }
 * ```
 */
export function useAppLinks() {
  const { i18n } = useTranslation();
  const { appConfig, loading, error } = useAppConfig();

  // 获取当前语言，并标准化为支持的语言代码
  const currentLang = useMemo(() => {
    const lang = i18n.language || 'zh-CN';
    // 标准化语言代码，确保返回的是我们支持的语言代码
    // 例如：zh-SG -> zh-CN, en -> en-US
    return normalizeLanguageCode(lang);
  }, [i18n.language]);

  // 构建链接对象
  const links = useMemo<AppLinks>(() => {
    const baseURL = appConfig?.appLinks?.baseURL || 'https://kaitu.io';
    return {
      discoveryUrl: `${baseURL}${appConfig?.appLinks?.discoveryPath || '/discovery'}`,
      privacyPolicyUrl: `${baseURL}${appConfig?.appLinks?.privacyPath || '/privacy'}`,
      termsOfServiceUrl: `${baseURL}${appConfig?.appLinks?.termsPath || '/terms'}`,
      walletUrl: `${baseURL}${appConfig?.appLinks?.walletPath || '/wallet'}`,
      retailerRulesUrl: `${baseURL}${appConfig?.appLinks?.retailerRulesPath || '/retailer-rules'}`,
      securitySoftwareHelpUrl: `${baseURL}${appConfig?.appLinks?.securitySoftwareHelpPath || '/help/security-software'}`,
      changelogUrl: `${baseURL}${appConfig?.appLinks?.changelogPath || '/changelog'}`,
    };
  }, [appConfig]);

  return {
    links,
    loading,
    error,
    // 提供当前语言供组件使用
    currentLang,
  };
}
