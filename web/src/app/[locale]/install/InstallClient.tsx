"use client";

import { useEffect, useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from '@/components/ui/accordion';
import { getDownloadLinks } from '@/lib/constants';
import type { MobileLinks } from '@/lib/downloads';
import {
  detectDevice,
  triggerDownload,
  openDownloadInNewTab,
  DeviceInfo,
  DeviceType
} from '@/lib/device-detection';
import {
  Download,
  CheckCircle,
  AlertCircle,
  RefreshCw,
  ArrowRight,
  ExternalLink,
  Copy,
} from 'lucide-react';
import { Link } from '@/i18n/routing';

// ---------------------------------------------------------------------------
// Platform SVG Icons
// ---------------------------------------------------------------------------

const platformIcons: Record<string, React.FC<{ className?: string }>> = {
  windows: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M2 6.5L20.3 3.8V22.5H2V6.5ZM22.5 3.5L46 0V22.5H22.5V3.5ZM2 24.5H20.3V43.2L2 40.5V24.5ZM22.5 24.5H46V47L22.5 43.5V24.5Z"/>
    </svg>
  ),
  macos: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M39.6 25.2c-.1-4.4 3.6-6.5 3.8-6.6-2.1-3-5.3-3.5-6.4-3.5-2.7-.3-5.4 1.6-6.7 1.6-1.4 0-3.5-1.6-5.8-1.5-3 0-5.7 1.7-7.3 4.4-3.1 5.4-.8 13.5 2.2 17.9 1.5 2.1 3.3 4.6 5.6 4.5 2.2-.1 3.1-1.5 5.8-1.5 2.7 0 3.5 1.5 5.8 1.4 2.4 0 4-2.2 5.4-4.3 1.7-2.5 2.4-4.9 2.5-5-.1 0-4.7-1.8-4.9-7.4zM35.1 11.9c1.2-1.5 2.1-3.5 1.8-5.5-1.8.1-3.9 1.2-5.2 2.7-1.1 1.3-2.1 3.4-1.9 5.4 2 .2 4-1 5.3-2.6z"/>
    </svg>
  ),
  linux: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M24 2C17.4 2 12 7.4 12 14v6c-2.2 1.6-4 4.2-4 7.2 0 2.8 1.2 5 3.2 6.6C12.8 38 16 42 20 44h8c4-2 7.2-6 8.8-10.2C38.8 32.2 40 30 40 27.2c0-3-1.8-5.6-4-7.2v-6C36 7.4 30.6 2 24 2zm-4 14c0-2.2 1.8-4 4-4s4 1.8 4 4v2h-8v-2zm-4 12a2 2 0 110-4 2 2 0 010 4zm16 0a2 2 0 110-4 2 2 0 010 4zm-8 8c-2.2 0-4-1.8-4-4h8c0 2.2-1.8 4-4 4z"/>
    </svg>
  ),
  ios: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <rect x="12" y="2" width="24" height="44" rx="5" ry="5" fill="none" stroke="currentColor" strokeWidth="2.5"/>
      <rect x="19" y="4" width="10" height="3" rx="1.5"/>
      <circle cx="24" cy="40" r="2"/>
    </svg>
  ),
  android: ({ className }) => (
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
      <path d="M15.4 8.8l-2.9-5c-.2-.4-.1-.8.3-1 .4-.2.8-.1 1 .3l2.9 5.1c2.2-1 4.7-1.5 7.3-1.5s5.1.6 7.3 1.5l2.9-5.1c.2-.4.6-.5 1-.3.4.2.5.6.3 1l-2.9 5c5.1 2.5 8.5 7.3 8.5 12.8H6.9c0-5.5 3.4-10.3 8.5-12.8zM18 16.5c-.8 0-1.5.7-1.5 1.5s.7 1.5 1.5 1.5 1.5-.7 1.5-1.5-.7-1.5-1.5-1.5zm12 0c-.8 0-1.5.7-1.5 1.5s.7 1.5 1.5 1.5 1.5-.7 1.5-1.5-.7-1.5-1.5-1.5zM6.9 24h34.2v16c0 1.7-1.3 3-3 3H9.9c-1.7 0-3-1.3-3-3V24z"/>
    </svg>
  ),
};

function PlatformIcon({ type, className }: { type: string; className?: string }) {
  const Icon = platformIcons[type] || platformIcons.windows;
  return <Icon className={className} />;
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type DownloadState = 'detecting' | 'ready' | 'downloading' | 'success' | 'failed' | 'cancelled';

interface InstallClientProps {
  betaVersion: string | null;
  stableVersion: string | null;
  mobileLinks?: MobileLinks | null;
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function CliBlock({ onCopy, copied }: { onCopy: () => void; copied: boolean }) {
  return (
    <div className="bg-card rounded-lg border font-mono text-sm p-4">
      <div className="flex items-center gap-2">
        <span className="text-muted-foreground">$</span>
        <code className="flex-1 text-foreground break-all">curl -fsSL https://kaitu.io/i/k2 | sudo bash</code>
        <button
          onClick={onCopy}
          className="shrink-0 p-1 hover:text-foreground transition-colors text-muted-foreground"
        >
          {copied ? <CheckCircle className="w-5 h-5 text-green-500" /> : <Copy className="w-5 h-5" />}
        </button>
      </div>
    </div>
  );
}



// ---------------------------------------------------------------------------
// Inline guide illustrations (dynamic, themed, no static images needed)
// ---------------------------------------------------------------------------

function GuideIframe({ srcdoc, height }: { srcdoc: string; height: number }) {
  return (
    <iframe
      srcDoc={srcdoc}
      className="w-full rounded-lg border-0 overflow-hidden"
      style={{ height, display: 'block' }}
      scrolling="no"
    />
  );
}

function BrowserBlockedGuide({ filename, browser }: { filename: string; browser?: string | null }) {
  if (browser === 'edge') {
    const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#f0f0f0;padding:12px;font-family:'Segoe UI',sans-serif;display:flex;justify-content:flex-end}.label{position:absolute;top:6px;left:0;right:0;text-align:center;color:#999;font-size:10px}.panel{background:#fff;border-radius:8px;box-shadow:0 4px 16px rgba(0,0,0,0.15);width:280px;overflow:hidden}.header{padding:10px 14px;border-bottom:1px solid #eee;display:flex;align-items:center;justify-content:space-between}.htitle{font-size:12px;font-weight:600;color:#333}.dots{color:#666;cursor:pointer;font-size:14px;width:24px;height:24px;display:flex;align-items:center;justify-content:center;border-radius:4px;border:2px solid #ff4444}.dots::after{content:' ←';color:#ff4444;font-size:9px;white-space:nowrap}.file{padding:10px 14px;display:flex;align-items:flex-start;gap:10px;border-bottom:1px solid #eee}.ficon{width:32px;height:32px;background:#e8f0fe;border-radius:4px;display:flex;align-items:center;justify-content:center;color:#4285f4;font-size:14px;flex-shrink:0}.finfo{flex:1}.fname{font-size:11px;color:#333;font-weight:500;word-break:break-all}.fdesc{font-size:10px;color:#c00;margin-top:2px}.btn-row{display:flex;gap:6px;margin-top:6px}.fbtn{font-size:10px;color:#666;background:#f5f5f5;border:1px solid #ddd;border-radius:3px;padding:2px 8px}.menu{padding:6px 0}.mitem{padding:7px 14px;font-size:11px;color:#333;display:flex;align-items:center;gap:8px}.mitem:hover{background:#f5f5f5}.mitem.hl{background:#fff5f5;border:2px solid #ff4444;border-radius:4px;margin:2px 6px;font-weight:600}.hl::after{content:'👈';color:#ff4444;font-size:10px;margin-left:auto}</style></head><body><p class="label">Edge 浏览器 — 点击 ··· 展开菜单</p><div class="panel"><div class="header"><span class="htitle">下载</span><span class="dots">···</span></div><div class="file"><div class="ficon">📄</div><div class="finfo"><div class="fname">Microsoft Defender SmartScreen 已标记 ${filename}</div><div class="fdesc">已阻止不安全的下载</div><div class="btn-row"><span class="fbtn">查看更多</span></div></div></div><div class="menu"><div class="mitem">🗑️ 删除</div><div class="mitem hl">📂 保留</div><div class="mitem" style="color:#999">🔒 将此文件报告为安全</div><div class="mitem" style="color:#999">📋 复制下载链接</div></div></div></body></html>`;
    return <GuideIframe srcdoc={html} height={300} />;
  }
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#2b2b2b;padding:12px 12px 8px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif}.label{text-align:center;color:#999;font-size:10px;margin-bottom:8px}.bar{background:#3a3a3a;border-radius:8px;padding:10px 14px;display:flex;align-items:center;gap:10px}.icon{color:#fbbf24;font-size:16px;flex-shrink:0}.text{color:#ccc;font-size:11px;flex:1}strong{color:#fff;font-weight:600}.keep{background:#4285f4;color:#fff;padding:5px 14px;border-radius:4px;font-size:11px;font-weight:500;border:2px solid #ff4444;flex-shrink:0;white-space:nowrap}.keep::after{content:' 👈';font-size:12px}.discard{background:transparent;color:#888;padding:5px 14px;border-radius:4px;font-size:11px;border:1px solid #555;flex-shrink:0}</style></head><body><p class="label">Chrome 浏览器下载栏</p><div class="bar"><span class="icon">⚠️</span><span class="text"><strong>${filename}</strong> 不是常见的下载文件，可能存在危险。</span><span class="keep">保留</span><span class="discard">丢弃</span></div></body></html>`;
  return <GuideIframe srcdoc={html} height={95} />;
}

function SmartScreenGuide({ filename, publisher }: { filename: string; publisher: string }) {
  const step1 = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#0078d4;padding:24px 28px;font-family:'Segoe UI',sans-serif;color:#fff}h1{font-size:20px;font-weight:300;margin-bottom:14px}p{font-size:12px;color:rgba(255,255,255,0.8);line-height:1.6;margin-bottom:2px}a{color:rgba(255,255,255,0.9);font-size:12px;text-decoration:underline;display:inline-block;margin-top:8px;border:2px solid #ff4444;border-radius:3px;padding:1px 6px}a::after{content:' 👈 点击这里';color:#ffcc00;font-size:11px;font-weight:600;text-decoration:none}.spacer{height:60px}.row{text-align:right}.btn{background:rgba(255,255,255,0.1);color:#fff;border:1px solid rgba(255,255,255,0.3);padding:5px 24px;font-size:12px}</style></head><body><h1>Windows 已保护你的电脑</h1><p>Microsoft Defender SmartScreen 已阻止一个未识别的应用启动。</p><p>运行此应用可能会使你的电脑面临风险。</p><a>更多信息</a><div class="spacer"></div><div class="row"><button class="btn">我知道了</button></div></body></html>`;
  const step2 = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#0078d4;padding:24px 28px;font-family:'Segoe UI',sans-serif;color:#fff}h1{font-size:20px;font-weight:300;margin-bottom:14px}p{font-size:12px;color:rgba(255,255,255,0.8);line-height:1.6;margin-bottom:2px}.info{font-size:11px;color:rgba(255,255,255,0.5);margin-top:10px}.spacer{height:30px}.row{display:flex;justify-content:flex-end;gap:8px}.btn{background:rgba(255,255,255,0.1);color:#fff;border:1px solid rgba(255,255,255,0.3);padding:5px 24px;font-size:12px}.hl{border:2px solid #ff4444}.hl::after{content:' 👈';color:#ffcc00;font-size:11px;font-weight:600}</style></head><body><h1>Windows 已保护你的电脑</h1><p>Microsoft Defender SmartScreen 已阻止一个未识别的应用启动。</p><p>运行此应用可能会使你的电脑面临风险。</p><p class="info">应用: ${filename}</p><p class="info">发布者: ${publisher}</p><div class="spacer"></div><div class="row"><button class="btn">不运行</button><button class="btn hl">仍要运行</button></div></body></html>`;

  return (
    <div className="space-y-3">
      <div>
        <p className="text-[10px] text-muted-foreground mb-1.5">第 1 步：点击「更多信息」</p>
        <GuideIframe srcdoc={step1} height={280} />
      </div>
      <div>
        <p className="text-[10px] text-muted-foreground mb-1.5">第 2 步：点击「仍要运行」</p>
        <GuideIframe srcdoc={step2} height={260} />
      </div>
    </div>
  );
}

function MacOSAllowGuide({ publisher }: { publisher: string }) {
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"><style>*{margin:0;padding:0;box-sizing:border-box}body{background:#1c1c1e;font-family:-apple-system,BlinkMacSystemFont,sans-serif;font-size:11px}.tb{background:#2c2c2e;padding:8px 12px;display:flex;align-items:center;gap:6px;border-bottom:1px solid #3a3a3c}.d{width:10px;height:10px;border-radius:50%}.r{background:#ff5f57}.y{background:#febc2e}.g{background:#28c840}.main{display:flex}.sb{width:150px;background:#262628;border-right:1px solid #3a3a3c;padding:10px}.search{background:#3a3a3c;border-radius:5px;padding:5px 8px;display:flex;align-items:center;gap:4px;margin-bottom:8px;border:2px solid #3478f6}.search span{color:#98989d;font-size:9px}.search .t{color:#e5e5e7;flex:1}.si{background:#3478f6;border-radius:5px;padding:5px 8px;display:flex;align-items:center;gap:5px}.si span{font-size:9px;color:#fff}.ct{flex:1;padding:14px}.ch{padding:8px 0 10px;display:flex;align-items:center;gap:6px;border-bottom:1px solid #3a3a3c;margin-bottom:12px}.ch span{color:#3478f6;font-size:14px}.ch .t{flex:1;font-size:11px;font-weight:600;color:#e5e5e7}.st{font-size:10px;font-weight:600;color:#e5e5e7;margin-bottom:8px}.bx{background:#2c2c2e;border-radius:8px;padding:10px;margin-bottom:8px}.rl{color:#e5e5e7;font-size:10px;margin-bottom:6px}.rd{display:flex;align-items:center;gap:5px;margin-bottom:3px}.roff{width:12px;height:12px;border-radius:50%;border:1.5px solid #58585a}.ron{width:12px;height:12px;border-radius:50%;background:#3478f6;position:relative}.ron::after{content:'';position:absolute;top:3px;left:3px;width:6px;height:6px;border-radius:50%;background:#fff}.rl2{font-size:9px;color:#98989d}.rl2.sel{color:#e5e5e7}.at{color:#98989d;font-size:10px;line-height:1.5}strong{color:#e5e5e7}.ba{float:right;background:#48484a;color:#e5e5e7;border:none;padding:3px 12px;border-radius:5px;font-size:9px;margin-top:6px;border:2px solid #ff4444}.ba::after{content:' 👈';color:#ffcc00;font-size:9px;font-weight:600}.cf::after{content:'';display:table;clear:both}</style></head><body><div class="tb"><div class="d r"></div><div class="d y"></div><div class="d g"></div></div><div class="main"><div class="sb"><div class="search"><span>🔍</span><span class="t">隐私与安全</span></div><div class="si"><span>🤚</span><span>隐私与安全性</span></div></div><div class="ct"><div class="ch"><span>‹</span><span>›</span><span class="t">隐私与安全性</span></div><div class="st">安全性</div><div class="bx"><div class="rl">允许从以下位置下载的应用程序</div><div class="rd"><div class="roff"></div><span class="rl2">App Store</span></div><div class="rd"><div class="ron"></div><span class="rl2 sel">App Store 和被认可的开发者</span></div></div><div class="bx cf"><p class="at">来自开发者 <strong>"${publisher}"</strong> 的系统软件已被阻止载入。</p><button class="ba">允许</button></div></div></div></body></html>`;

  return (
    <div>
      <p className="text-[10px] text-muted-foreground mb-1.5">在系统设置中搜索「隐私与安全」，找到底部的安全性提示：</p>
      <GuideIframe srcdoc={html} height={340} />
    </div>
  );
}

function DownloadTips({ device, t, version, browser }: { device: DeviceInfo; t: (key: string) => string; version: string; browser?: string | null }) {
  const filename = device.type === 'windows' ? `Kaitu_${version}_x64.exe` : device.type === 'macos' ? `Kaitu_${version}_universal.pkg` : `Kaitu_${version}_amd64.AppImage`;
  const publisher = 'ALL NATION CONNECT TECHNOLOGY PTE. LTD.';

  return (
    <div className="mt-6 max-w-xl mx-auto space-y-4">
      {/* Tip 1: Browser may block */}
      <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-3">
        <p className="text-xs font-medium text-yellow-500 mb-2 flex items-center gap-1.5">
          <AlertCircle className="w-3.5 h-3.5 shrink-0" />
          {t('install.install.faq.browserBlock.question')}
        </p>
        <BrowserBlockedGuide filename={filename} browser={browser} />
      </div>

      {/* Windows SmartScreen */}
      {device.type === 'windows' && (
        <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-3">
          <p className="text-xs font-medium text-yellow-500 mb-2 flex items-center gap-1.5">
            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
            {t('install.install.faq.windowsSmartScreen.question')}
          </p>
          <SmartScreenGuide filename={filename} publisher={publisher} />
        </div>
      )}

      {/* macOS system extension */}
      {device.type === 'macos' && (
        <div className="rounded-lg border border-yellow-500/30 bg-yellow-500/5 p-3">
          <p className="text-xs font-medium text-yellow-500 mb-2 flex items-center gap-1.5">
            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
            {t('install.install.faq.macosGatekeeper.question')}
          </p>
          <MacOSAllowGuide publisher={publisher} />
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// FAQ Section
// ---------------------------------------------------------------------------

const FAQ_ITEMS = ['browserBlock', 'windowsSmartScreen', 'macosGatekeeper', 'security'] as const;

function getDefaultFaqItem(deviceType: string): string | undefined {
  switch (deviceType) {
    case 'macos': return 'macosGatekeeper';
    case 'windows': return 'windowsSmartScreen';
    default: return undefined;
  }
}

function FaqSection({ device, t }: { device: DeviceInfo | null; t: (key: string) => string }) {
  const defaultValue = device ? getDefaultFaqItem(device.type) : undefined;

  const faqJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: FAQ_ITEMS.map((item) => ({
      '@type': 'Question',
      name: t(`install.install.faq.${item}.question`),
      acceptedAnswer: {
        '@type': 'Answer',
        text: t(`install.install.faq.${item}.answer`),
      },
    })),
  };

  return (
    <div className="mt-12">
      <h3 className="text-lg font-semibold text-foreground mb-4">
        {t('install.install.needHelp')}
      </h3>
      <Accordion type="single" collapsible defaultValue={defaultValue}>
        {FAQ_ITEMS.map((item) => (
          <AccordionItem key={item} value={item}>
            <AccordionTrigger>
              {t(`install.install.faq.${item}.question`)}
            </AccordionTrigger>
            <AccordionContent>
              <p className="text-muted-foreground">
                {t(`install.install.faq.${item}.answer`)}
              </p>
            </AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>

      {/* FAQPage JSON-LD structured data */}
      <script
        type="application/ld+json"
        suppressHydrationWarning
        // eslint-disable-next-line react/no-danger
        dangerouslySetInnerHTML={{ __html: JSON.stringify(faqJsonLd) }}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main Component
// ---------------------------------------------------------------------------

export default function InstallClient({ betaVersion, stableVersion: serverStable, mobileLinks }: InstallClientProps) {
  const t = useTranslations();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [downloadState, setDownloadState] = useState<DownloadState>('detecting');
  const [countdown, setCountdown] = useState(5);
  const [copied, setCopied] = useState(false);
  const [debugBrowser, setDebugBrowser] = useState<string | null>(null);
  const [skipAutoDownload, setSkipAutoDownload] = useState(false);

  const displayVersion = betaVersion || serverStable!;
  const isBeta = !!(betaVersion && betaVersion !== serverStable);
  const downloadLinks = getDownloadLinks(displayVersion);
  const stableDownloadLinks = isBeta && serverStable ? getDownloadLinks(serverStable) : null;

  const getPrimaryLink = useCallback((deviceInfo: DeviceInfo | null) => {
    if (!deviceInfo) return null;
    switch (deviceInfo.type) {
      case 'windows': return downloadLinks.windows.primary;
      case 'macos': return downloadLinks.macos.primary;
      case 'linux': return downloadLinks.linux.primary;
      default: return null;
    }
  }, [downloadLinks]);

  useEffect(() => {
    // Debug params: ?platform=windows&browser=edge&noautodownload=true
    const params = new URLSearchParams(window.location.search);
    const platformParam = params.get('platform') as DeviceType | null;
    const browserParam = params.get('browser');
    const noAutoDownload = params.get('noautodownload') === 'true';
    const validPlatforms: DeviceType[] = ['windows', 'macos', 'linux', 'ios', 'android'];

    const stateParam = params.get('state') as DownloadState | null; // ?state=success|downloading|ready

    if (browserParam) setDebugBrowser(browserParam);
    if (noAutoDownload || stateParam) setSkipAutoDownload(true);

    let deviceInfo: DeviceInfo;
    if (platformParam && validPlatforms.includes(platformParam)) {
      const isDesktop = ['windows', 'macos', 'linux'].includes(platformParam);
      const nameMap: Record<string, string> = {
        windows: 'Windows PC', macos: 'Mac', linux: 'Linux PC',
        ios: 'iPhone / iPad', android: 'Android Device',
      };
      deviceInfo = {
        type: platformParam,
        name: nameMap[platformParam] || platformParam,
        isMobile: !isDesktop,
        isDesktop,
        userAgent: '',
      };
    } else {
      deviceInfo = detectDevice();
    }

    setDevice(deviceInfo);
    // ?state=xxx forces a specific download state (debug)
    if (stateParam && ['detecting', 'ready', 'downloading', 'success', 'failed', 'cancelled'].includes(stateParam)) {
      setDownloadState(stateParam);
    } else if (deviceInfo.type === 'linux') {
      setDownloadState('cancelled');
    } else if (deviceInfo.isDesktop) {
      setDownloadState(noAutoDownload ? 'cancelled' : 'ready');
    } else {
      setDownloadState('cancelled');
    }
  }, []);

  const primaryLink = getPrimaryLink(device);

  const startDownload = useCallback(async () => {
    if (!primaryLink) return;
    setDownloadState('downloading');
    const filename = primaryLink.split('/').pop() || undefined;
    const downloadTriggered = triggerDownload(primaryLink, filename);
    if (downloadTriggered) {
      setTimeout(() => setDownloadState('success'), 2000);
    } else {
      openDownloadInNewTab(primaryLink);
      setDownloadState('failed');
    }
  }, [primaryLink]);

  // Auto-download countdown (desktop only, not linux, not mobile, not debug)
  useEffect(() => {
    if (skipAutoDownload) return; // ?noautodownload=true or ?state=xxx
    if (downloadState === 'ready' && countdown > 0) {
      const timer = setTimeout(() => setCountdown(countdown - 1), 1000);
      return () => clearTimeout(timer);
    } else if (downloadState === 'ready' && countdown === 0) {
      startDownload();
    }
  }, [downloadState, countdown, startDownload, skipAutoDownload]);

  const retryDownload = () => {
    setCountdown(5);
    setDownloadState('ready');
  };

  const copyCliCommand = useCallback(async () => {
    try {
      await navigator.clipboard.writeText('curl -fsSL https://kaitu.io/i/k2 | sudo bash');
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard API unavailable
    }
  }, []);

  const versionLabel = t('install.install.latestVersion', { version: displayVersion });

  // -------------------------------------------------------------------------
  // Render helpers for hero download state
  // -------------------------------------------------------------------------

  const renderDownloadState = () => {
    if (!device) return null;

    switch (downloadState) {
      case 'detecting':
        return (
          <p className="text-muted-foreground">{t('install.install.analyzingDevice')}</p>
        );

      case 'ready':
        return (
          <div className="space-y-3">
            <div className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
              <RefreshCw className="w-4 h-4 animate-spin" />
              <span>{t('install.install.autoDownloadCountdown', { seconds: countdown })}</span>
            </div>
            <div className="flex justify-center gap-3">
              <Button size="lg" onClick={startDownload}>
                <Download className="w-5 h-5 mr-2" />
                {t('install.install.downloadButton')} v{displayVersion}
              </Button>
              <Button variant="outline" onClick={() => setDownloadState('cancelled')}>
                {t('install.install.cancelAutoDownload')}
              </Button>
            </div>
          </div>
        );

      case 'downloading':
        return (
          <div className="space-y-2 text-center">
            <Download className="w-8 h-8 text-primary mx-auto animate-bounce" />
            <p className="text-sm font-medium text-foreground">{t('install.install.downloading')}</p>
            <p className="text-xs text-muted-foreground">{t('install.install.checkDownloadLocation')}</p>
            <DownloadTips device={device} t={t} version={displayVersion} browser={debugBrowser} />
          </div>
        );

      case 'success':
        return (
          <div className="space-y-4">
            <div className="flex items-center justify-center gap-2 text-green-500">
              <CheckCircle className="w-5 h-5" />
              <span className="text-sm font-medium">{t('install.install.downloadSuccess')}</span>
            </div>
            <DownloadTips device={device} t={t} version={displayVersion} browser={debugBrowser} />
            <div className="flex justify-center gap-3 mt-4">
              <Button onClick={retryDownload}>
                <RefreshCw className="w-4 h-4 mr-2" />
                {t('install.install.redownload')}
              </Button>
              <Link href="/">
                <Button variant="ghost">
                  {t('install.install.backToHome')}
                  <ArrowRight className="w-4 h-4 ml-2" />
                </Button>
              </Link>
            </div>
          </div>
        );

      case 'failed':
        return (
          <div className="space-y-3">
            <div className="flex items-center justify-center gap-2 text-destructive">
              <AlertCircle className="w-5 h-5" />
              <span className="text-sm font-medium">{t('install.install.downloadFailed')}</span>
            </div>
            <p className="text-xs text-muted-foreground">{t('install.install.downloadFailedMessage')}</p>
            <div className="flex justify-center gap-3">
              <Button variant="destructive" onClick={() => primaryLink && openDownloadInNewTab(primaryLink)}>
                <ExternalLink className="w-4 h-4 mr-2" />
                {t('install.install.manualDownload')}
              </Button>
              <Button variant="outline" onClick={retryDownload}>
                <RefreshCw className="w-4 h-4 mr-2" />
                {t('install.install.retryAutoDownload')}
              </Button>
            </div>
          </div>
        );

      case 'cancelled':
        // For non-linux desktop: show manual download button
        if (device.isDesktop && device.type !== 'linux' && primaryLink) {
          return (
            <div className="space-y-2">
              <Button size="lg" onClick={startDownload}>
                <Download className="w-5 h-5 mr-2" />
                {t('install.install.downloadButton')} v{displayVersion}
              </Button>
            </div>
          );
        }
        return null;

      default:
        return null;
    }
  };

  // -------------------------------------------------------------------------
  // Hero content by platform
  // -------------------------------------------------------------------------

  const renderHero = () => {
    if (!device) {
      return (
        <div className="text-center py-8">
          <p className="text-muted-foreground">{t('install.install.detectingDevice')}</p>
        </div>
      );
    }

    const heroTitle = t(`install.install.heroTitle.${device.type}`);
    const showCli = device.type === 'macos' || device.type === 'linux';

    // Mobile: direct links, no download state machine
    if (device.type === 'ios' || device.type === 'android') {
      return (
        <div className="text-center">
          <div className="bg-primary/10 rounded-2xl p-3 w-16 h-16 mx-auto mb-6 flex items-center justify-center">
            <PlatformIcon type={device.type} className="w-10 h-10 text-primary" />
          </div>
          <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
            {heroTitle}
          </h1>
          <div className="mt-6">
            {device.type === 'ios' && mobileLinks?.ios && (
              <Button size="lg" onClick={() => openDownloadInNewTab(mobileLinks.ios)}>
                <ExternalLink className="w-5 h-5 mr-2" />
                App Store
              </Button>
            )}
            {device.type === 'android' && mobileLinks?.android && (
              <Button size="lg" onClick={() => openDownloadInNewTab(mobileLinks.android)}>
                <Download className="w-5 h-5 mr-2" />
                {t('install.install.downloadButton')}
              </Button>
            )}
            {!mobileLinks && (
              <p className="text-sm text-muted-foreground">
                {t('install.install.downloadCancelled')}
              </p>
            )}
          </div>
        </div>
      );
    }

    // Unknown: show "choose your platform" without auto-download
    if (device.type === 'unknown') {
      return (
        <div className="text-center">
          <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
            {heroTitle}
          </h1>
          <p className="text-sm text-muted-foreground">
            {versionLabel}
          </p>
        </div>
      );
    }

    // Linux special: CLI as primary CTA
    if (device.type === 'linux') {
      return (
        <div className="text-center">
          <div className="bg-primary/10 rounded-2xl p-3 w-16 h-16 mx-auto mb-6 flex items-center justify-center">
            <PlatformIcon type={device.type} className="w-10 h-10 text-primary" />
          </div>
          <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
            {heroTitle}
          </h1>
          <p className="text-sm text-muted-foreground mb-6">
            {versionLabel}
            {isBeta && (
              <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
                {t('install.install.beta')}
              </span>
            )}
          </p>

          {/* CLI as primary CTA */}
          <div className="max-w-lg mx-auto mb-4">
            <p className="text-sm text-muted-foreground mb-2">
              {t('install.install.linuxCliRecommended')}
            </p>
            <CliBlock onCopy={copyCliCommand} copied={copied} />
          </div>

          {/* AppImage as secondary */}
          <Button variant="outline" onClick={startDownload}>
            <Download className="w-4 h-4 mr-2" />
            {t('install.install.downloadAppImage')}
          </Button>
        </div>
      );
    }

    // Windows / macOS: standard desktop flow
    return (
      <div className="text-center">
        <div className="bg-primary/10 rounded-2xl p-3 w-16 h-16 mx-auto mb-6 flex items-center justify-center">
          <PlatformIcon type={device.type} className="w-10 h-10 text-primary" />
        </div>
        <h1 className="text-3xl sm:text-4xl font-bold font-mono text-foreground mb-2">
          {heroTitle}
        </h1>
        <p className="text-sm text-muted-foreground mb-6">
          {versionLabel}
          {isBeta && (
            <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
              {t('install.install.beta')}
            </span>
          )}
        </p>

        {/* Download state */}
        {renderDownloadState()}

        {/* CLI block for macOS */}
        {showCli && (
          <div className="max-w-lg mx-auto mt-6">
            <p className="text-xs text-muted-foreground mb-2">
              {t('install.install.terminalInstall')}
            </p>
            <CliBlock onCopy={copyCliCommand} copied={copied} />
          </div>
        )}
      </div>
    );
  };

  // -------------------------------------------------------------------------
  // Main render
  // -------------------------------------------------------------------------

  return (
    <>
      {/* Section 1: Smart Hero */}
      <div className="py-8 mb-8">
        {renderHero()}
      </div>

      {/* Section 2: Platform List — clickable rows */}
      <h3 className="text-lg font-semibold text-foreground mb-3">
        {t('install.install.otherPlatforms')}
      </h3>
      <div className="border rounded-lg divide-y divide-border mb-8">
        {/* Windows */}
        <button
          onClick={() => openDownloadInNewTab(downloadLinks.windows.primary)}
          className={`w-full flex items-center gap-3 px-4 py-3.5 hover:bg-muted/50 transition-colors text-left ${device?.type === 'windows' ? 'bg-primary/5' : ''}`}
        >
          <PlatformIcon type="windows" className="w-5 h-5 text-foreground opacity-70 shrink-0" />
          <span className="text-sm font-medium text-foreground">{t('install.install.windows')}</span>
          <span className="text-xs text-muted-foreground">{t('install.install.windowsVersion')}</span>
          <span className="ml-auto text-xs text-primary flex items-center gap-1 shrink-0">
            <Download className="w-3.5 h-3.5" />
            .exe
          </span>
        </button>

        {/* macOS */}
        <div className={`${device?.type === 'macos' ? 'bg-primary/5' : ''}`}>
          <button
            onClick={() => openDownloadInNewTab(downloadLinks.macos.primary)}
            className="w-full flex items-center gap-3 px-4 py-3.5 hover:bg-muted/50 transition-colors text-left"
          >
            <PlatformIcon type="macos" className="w-5 h-5 text-foreground opacity-70 shrink-0" />
            <span className="text-sm font-medium text-foreground">{t('install.install.macos')}</span>
            <span className="text-xs text-muted-foreground">{t('install.install.macosVersion')}</span>
            <span className="ml-auto flex items-center gap-3 shrink-0">
              <span className="text-xs text-primary flex items-center gap-1">
                <Download className="w-3.5 h-3.5" />
                .pkg
              </span>
            </span>
          </button>
          <div className="px-4 pb-3 -mt-1">
            <button
              onClick={copyCliCommand}
              className="text-xs text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1"
            >
              {copied ? <CheckCircle className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
              {copied ? t('install.install.copied') : t('install.install.cliInstall')}
            </button>
          </div>
        </div>

        {/* Linux */}
        <div className={`${device?.type === 'linux' ? 'bg-primary/5' : ''}`}>
          <button
            onClick={() => openDownloadInNewTab(downloadLinks.linux.primary)}
            className="w-full flex items-center gap-3 px-4 py-3.5 hover:bg-muted/50 transition-colors text-left"
          >
            <PlatformIcon type="linux" className="w-5 h-5 text-foreground opacity-70 shrink-0" />
            <span className="text-sm font-medium text-foreground">{t('install.install.linux')}</span>
            <span className="text-xs text-muted-foreground">{t('install.install.linuxVersion')}</span>
            <span className="ml-auto text-xs text-primary flex items-center gap-1 shrink-0">
              <Download className="w-3.5 h-3.5" />
              .AppImage
            </span>
          </button>
          <div className="px-4 pb-3 -mt-1">
            <button
              onClick={copyCliCommand}
              className="text-xs text-muted-foreground hover:text-foreground transition-colors flex items-center gap-1"
            >
              {copied ? <CheckCircle className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
              {copied ? t('install.install.copied') : t('install.install.cliInstall')}
            </button>
          </div>
        </div>

        {/* iOS */}
        {mobileLinks?.ios && (
          <a
            href={mobileLinks.ios}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/50 transition-colors"
          >
            <PlatformIcon type="ios" className="w-5 h-5 text-foreground opacity-70 shrink-0" />
            <span className="text-sm font-medium text-foreground">{t('install.install.ios')}</span>
            <span className="text-xs text-muted-foreground">{t('install.install.iosDevices')}</span>
            <span className="ml-auto text-xs text-primary flex items-center gap-1 shrink-0">
              <ExternalLink className="w-3.5 h-3.5" />
              App Store
            </span>
          </a>
        )}

        {/* Android */}
        {mobileLinks?.android && (
          <a
            href={mobileLinks.android}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-3 px-4 py-3.5 hover:bg-muted/50 transition-colors"
          >
            <PlatformIcon type="android" className="w-5 h-5 text-foreground opacity-70 shrink-0" />
            <span className="text-sm font-medium text-foreground">{t('install.install.android')}</span>
            <span className="text-xs text-muted-foreground">{t('install.install.androidVersion')}</span>
            <span className="ml-auto text-xs text-primary flex items-center gap-1 shrink-0">
              <Download className="w-3.5 h-3.5" />
              APK
            </span>
          </a>
        )}
      </div>

      {/* Section 3: Footer links */}

      {/* Stable version alternative */}
      {isBeta && stableDownloadLinks && (
        <p className="text-xs text-muted-foreground text-center mt-2">
          {t('install.install.alsoAvailableStable', { version: serverStable! })}
          {': '}
          <a href={stableDownloadLinks.windows.primary} target="_blank" rel="noopener noreferrer"
             className="hover:text-foreground hover:underline">Windows</a>
          {' \u00B7 '}
          <a href={stableDownloadLinks.macos.primary} target="_blank" rel="noopener noreferrer"
             className="hover:text-foreground hover:underline">macOS</a>
          {' \u00B7 '}
          <a href={stableDownloadLinks.linux.primary} target="_blank" rel="noopener noreferrer"
             className="hover:text-foreground hover:underline">Linux</a>
        </p>
      )}

      {/* Backup download + View all releases */}
      <div className="text-center mt-6 space-y-2">
        <p className="text-xs text-muted-foreground">
          {t('install.install.backupDownload')}
          {': '}
          <a href={downloadLinks.windows.backup} target="_blank" rel="noopener noreferrer"
             className="hover:text-foreground hover:underline">Windows</a>
          {' \u00B7 '}
          <a href={downloadLinks.macos.backup} target="_blank" rel="noopener noreferrer"
             className="hover:text-foreground hover:underline">macOS</a>
          {' \u00B7 '}
          <a href={downloadLinks.linux.backup} target="_blank" rel="noopener noreferrer"
             className="hover:text-foreground hover:underline">Linux</a>
        </p>
        <Link href="/releases" className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1">
          {t('install.install.viewAllReleases')}
          <ArrowRight className="w-3 h-3" />
        </Link>
      </div>

      {/* Section 4: FAQ Accordion */}
      <FaqSection device={device} t={t} />
    </>
  );
}
