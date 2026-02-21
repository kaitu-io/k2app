import { Metadata } from 'next';
import { routing } from '@/i18n/routing';

const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';

export function generateMetadata(locale: string, pathname: string = ''): Metadata {
  const titles: Record<string, string> = {
    'zh-CN': '开途 k2 — ECH 隐身隧道协议',
    'zh-TW': '開途 k2 — ECH 隱身隧道協議',
    'zh-HK': '開途 k2 — ECH 隱身隧道協議',
    'en-US': 'Kaitu k2 — ECH Stealth Tunnel Protocol',
    'en-GB': 'Kaitu k2 — ECH Stealth Tunnel Protocol',
    'en-AU': 'Kaitu k2 — ECH Stealth Tunnel Protocol',
    'ja': 'Kaitu k2 — ECH ステルストンネルプロトコル'
  };

  const descriptions: Record<string, string> = {
    'zh-CN': '开途 k2 基于 ECH 加密客户端问候构建的隐身隧道，QUIC+TCP-WS 双栈传输，自研自适应拥塞控制算法，一行命令部署，CT 日志零暴露，抗审查网络隧道。',
    'zh-TW': '開途 k2 基於 ECH 加密客戶端問候構建的隱身隧道，QUIC+TCP-WS 雙棧傳輸，自研自適應擁塞控制演算法，一行命令部署，CT 日誌零暴露，抗審查網路隧道。',
    'zh-HK': '開途 k2 基於 ECH 加密客戶端問候構建的隱身隧道，QUIC+TCP-WS 雙棧傳輸，自研自適應擁塞控制算法，一行命令部署，CT 日誌零暴露，抗審查網絡隧道。',
    'en-US': 'Kaitu k2 is an anti-censorship stealth tunnel built on ECH (Encrypted Client Hello), QUIC+TCP-WS dual-stack transport, proprietary adaptive congestion control, one-command deployment, zero CT log exposure.',
    'en-GB': 'Kaitu k2 is an anti-censorship stealth tunnel built on ECH (Encrypted Client Hello), QUIC+TCP-WS dual-stack transport, proprietary adaptive congestion control, one-command deployment, zero CT log exposure.',
    'en-AU': 'Kaitu k2 is an anti-censorship stealth tunnel built on ECH (Encrypted Client Hello), QUIC+TCP-WS dual-stack transport, proprietary adaptive congestion control, one-command deployment, zero CT log exposure.',
    'ja': 'Kaitu k2 は ECH（暗号化クライアントハロー）をベースにした検閲対策ステルストンネルです。QUIC+TCP-WS デュアルスタック転送、独自の適応型輻輳制御、1コマンドデプロイ、CT ログゼロ露出を実現します。'
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
