import { Metadata } from 'next';
import { routing } from '@/i18n/routing';

const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';

export function generateMetadata(locale: string, pathname: string = ''): Metadata {
  const titles: Record<string, string> = {
    'zh-CN': '开途网络代理 - 安全稳定的网络加速服务',
    'zh-TW': '開途網路代理 - 安全穩定的網路加速服務',
    'zh-HK': '開途網絡代理 - 安全穩定的網絡加速服務',
    'en-US': 'Kaitu Network Proxy - Secure & Stable Network Acceleration',
    'en-GB': 'Kaitu Network Proxy - Secure & Stable Network Acceleration',
    'en-AU': 'Kaitu Network Proxy - Secure & Stable Network Acceleration',
    'ja': 'Kaitu ネットワークプロキシ - 安全で安定したネットワーク高速化'
  };
  
  const descriptions: Record<string, string> = {
    'zh-CN': '开途提供专业级的网络代理服务，采用先进的CA证书模拟技术，支持多平台客户端，确保您的网络连接安全、快速、稳定。',
    'zh-TW': '開途提供專業級的網路代理服務，採用先進的CA憑證模擬技術，支援多平台客戶端，確保您的網路連接安全、快速、穩定。',
    'zh-HK': '開途提供專業級的網絡代理服務，採用先進的CA證書模擬技術，支援多平台客戶端，確保您的網絡連接安全、快速、穩定。',
    'en-US': 'Kaitu provides professional network proxy services with advanced CA certificate simulation technology, multi-platform support, ensuring secure, fast, and stable network connections.',
    'en-GB': 'Kaitu provides professional network proxy services with advanced CA certificate simulation technology, multi-platform support, ensuring secure, fast, and stable network connections.',
    'en-AU': 'Kaitu provides professional network proxy services with advanced CA certificate simulation technology, multi-platform support, ensuring secure, fast, and stable network connections.',
    'ja': 'Kaituは高度なCA証明書シミュレーション技術を使用したプロフェッショナルなネットワークプロキシサービスを提供し、マルチプラットフォームをサポートし、安全で高速かつ安定したネットワーク接続を保証します。'
  };
  
  const title = titles[locale] || titles['zh-CN'];
  const description = descriptions[locale] || descriptions['zh-CN'];
  
  // Generate alternate links for all locales
  const languages: Record<string, string> = {};
  routing.locales.forEach(loc => {
    languages[loc.toLowerCase()] = `${baseUrl}/${loc}${pathname}`;
  });
  
  return {
    title,
    description,
    openGraph: {
      title,
      description,
      url: `${baseUrl}/${locale}${pathname}`,
      siteName: 'Kaitu',
      locale: locale.replace('-', '_'),
      type: 'website',
    },
    twitter: {
      card: 'summary_large_image',
      title,
      description,
    },
    alternates: {
      canonical: `${baseUrl}/${locale}${pathname}`,
      languages,
    },
    icons: {
      icon: [
        { url: '/favicon-16x16.png', sizes: '16x16', type: 'image/png' },
        { url: '/favicon-32x32.png', sizes: '32x32', type: 'image/png' },
        { url: '/icon-48x48.png', sizes: '48x48', type: 'image/png' },
        { url: '/icon-96x96.png', sizes: '96x96', type: 'image/png' },
        { url: '/icon-192x192.png', sizes: '192x192', type: 'image/png' },
        { url: '/icon-512x512.png', sizes: '512x512', type: 'image/png' },
      ],
      shortcut: '/favicon.ico',
      apple: [
        { url: '/icon-192x192.png', sizes: '192x192', type: 'image/png' },
        { url: '/icon-512x512.png', sizes: '512x512', type: 'image/png' },
      ],
    },
  };
}