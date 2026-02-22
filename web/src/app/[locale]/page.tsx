import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { DOWNLOAD_LINKS } from '@/lib/constants';
import { Link } from '@/i18n/routing';
import { routing } from '@/i18n/routing';
import NextLink from 'next/link';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import {
  ExternalLink,
  Smartphone,
  Monitor,
  Download,
  Terminal,
  Zap,
  Server
} from 'lucide-react';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

/**
 * Generate metadata for the homepage (used by Next.js for <head> tags).
 * Requires server-side translation to produce locale-aware title/description.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'hero' });

  return {
    title: t('hero.title'),
    description: t('hero.description'),
  };
}

/**
 * Static JSON-LD structured data for the k2 stealth tunnel application.
 * Content is a hardcoded constant — not user input — so dangerouslySetInnerHTML is safe here.
 */
const JSON_LD_CONTENT = JSON.stringify({
  '@context': 'https://schema.org',
  '@type': 'SoftwareApplication',
  name: 'Kaitu k2',
  applicationCategory: 'NetworkingApplication',
  operatingSystem: 'Windows, macOS, iOS, Android, Linux',
  description:
    'ECH-based stealth tunnel protocol powered by k2arc adaptive rate control. QUIC+TCP-WS dual-stack transport with zero CT log exposure and one-command deployment.',
  url: 'https://kaitu.io',
  publisher: {
    '@type': 'Organization',
    name: 'Kaitu',
    url: 'https://kaitu.io',
  },
  offers: {
    '@type': 'Offer',
    price: '0',
    priceCurrency: 'USD',
  },
  featureList: [
    'ECH (Encrypted Client Hello) stealth',
    'QUIC + TCP-WebSocket dual-stack transport',
    'k2arc adaptive rate control algorithm',
    'Reverse proxy camouflage',
    'Self-signed certificate + certificate pinning',
    'Zero CT log exposure',
    'One-command deployment',
  ],
});

/**
 * Homepage Server Component — T5 rewrite for k2v5 technology.
 *
 * Sections:
 * 1. Hero — title + subtitle + CTAs + terminal animation
 * 2. Feature cards — 6 cards with Terminal Dark styling
 * 3. Comparison table — k2v5 vs 5 protocols across 9 dimensions
 * 4. Quick start — server + client terminal boxes
 * 5. Download — 4-platform cards
 *
 * Uses Next.js 15 async params pattern.
 */
export default async function Home({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  const t = await getTranslations({ locale, namespace: 'hero' });

  // Comparison table protocol data
  const protocols: Array<{
    id: string;
    name: string;
    ech: boolean;
    tlsFingerprint: boolean;
    activeProbe: boolean;
    quic: boolean;
    tcpFallback: boolean;
    congestion: boolean;
    zeroDeploy: boolean;
    ctLog: boolean;
    portReuse: boolean;
  }> = [
    {
      id: 'k2',
      name: 'k2v5',
      ech: true,
      tlsFingerprint: true,
      activeProbe: true,
      quic: true,
      tcpFallback: true,
      congestion: true,
      zeroDeploy: true,
      ctLog: true,
      portReuse: true,
    },
    {
      id: 'WireGuard',
      name: 'WireGuard',
      ech: false,
      tlsFingerprint: false,
      activeProbe: false,
      quic: false,
      tcpFallback: false,
      congestion: false,
      zeroDeploy: false,
      ctLog: false,
      portReuse: false,
    },
    {
      id: 'VLESS',
      name: 'VLESS+Reality',
      ech: false,
      tlsFingerprint: true,
      activeProbe: true,
      quic: false,
      tcpFallback: true,
      congestion: false,
      zeroDeploy: false,
      ctLog: false,
      portReuse: false,
    },
    {
      id: 'Hysteria2',
      name: 'Hysteria2',
      ech: false,
      tlsFingerprint: false,
      activeProbe: false,
      quic: true,
      tcpFallback: false,
      congestion: true,
      zeroDeploy: false,
      ctLog: true,
      portReuse: false,
    },
    {
      id: 'Shadowsocks',
      name: 'Shadowsocks',
      ech: false,
      tlsFingerprint: false,
      activeProbe: false,
      quic: false,
      tcpFallback: false,
      congestion: false,
      zeroDeploy: false,
      ctLog: false,
      portReuse: false,
    },
  ];

  type DimensionKey =
    | 'ech'
    | 'tlsFingerprint'
    | 'activeProbe'
    | 'quic'
    | 'tcpFallback'
    | 'congestion'
    | 'zeroDeploy'
    | 'ctLog'
    | 'portReuse';

  const dimensions: DimensionKey[] = [
    'ech',
    'tlsFingerprint',
    'activeProbe',
    'quic',
    'tcpFallback',
    'congestion',
    'zeroDeploy',
    'ctLog',
    'portReuse',
  ];

  const dimensionLabels: Record<DimensionKey, string> = {
    ech: t('hero.comparison.dimensions.ech'),
    tlsFingerprint: t('hero.comparison.dimensions.tlsFingerprint'),
    activeProbe: t('hero.comparison.dimensions.activeProbe'),
    quic: t('hero.comparison.dimensions.quic'),
    tcpFallback: t('hero.comparison.dimensions.tcpFallback'),
    congestion: t('hero.comparison.dimensions.congestion'),
    zeroDeploy: t('hero.comparison.dimensions.zeroDeploy'),
    ctLog: t('hero.comparison.dimensions.ctLog'),
    portReuse: t('hero.comparison.dimensions.portReuse'),
  };

  const featureCards = [
    {
      key: 'congestion',
      icon: '📈',
      title: t('hero.features.congestion.title'),
      description: t('hero.features.congestion.description'),
      borderColor: 'border-t-4',
      borderStyle: { borderTopColor: 'var(--primary)' },
    },
    {
      key: 'ech',
      icon: '🛡️',
      title: t('hero.features.ech.title'),
      description: t('hero.features.ech.description'),
      borderColor: 'border-t-4',
      borderStyle: { borderTopColor: 'var(--secondary)' },
    },
    {
      key: 'transport',
      icon: '🔀',
      title: t('hero.features.transport.title'),
      description: t('hero.features.transport.description'),
      borderColor: 'border-t-4',
      borderStyle: { borderTopColor: 'var(--primary)' },
    },
    {
      key: 'zeroDeploy',
      icon: '⚡',
      title: t('hero.features.zeroDeploy.title'),
      description: t('hero.features.zeroDeploy.description'),
      borderColor: 'border-t-4',
      borderStyle: { borderTopColor: 'var(--secondary)' },
    },
    {
      key: 'reverseProxy',
      icon: '🎭',
      title: t('hero.features.reverseProxy.title'),
      description: t('hero.features.reverseProxy.description'),
      borderColor: 'border-t-4',
      borderStyle: { borderTopColor: 'var(--primary)' },
    },
    {
      key: 'selfSign',
      icon: '🔐',
      title: t('hero.features.selfSign.title'),
      description: t('hero.features.selfSign.description'),
      borderColor: 'border-t-4',
      borderStyle: { borderTopColor: 'var(--secondary)' },
    },
  ];

  return (
    <div className="min-h-screen" style={{ backgroundColor: 'var(--background)', color: 'var(--foreground)' }}>
      {/* JSON-LD SoftwareApplication structured data — static content, safe to inject */}
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON_LD_CONTENT }}
      />
      <Header />

      {/* Hero Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto text-center">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs mb-6"
            style={{ backgroundColor: 'rgba(0,255,136,0.1)', color: 'var(--primary)', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'var(--font-mono), monospace' }}>
            <span className="w-2 h-2 rounded-full animate-pulse" style={{ backgroundColor: 'var(--primary)' }}></span>
            k2v5 — ECH Stealth Tunnel
          </div>

          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold mb-6 leading-tight"
            style={{ fontFamily: 'var(--font-mono), monospace' }}>
            <span style={{ color: 'var(--primary)' }}>k2</span>{' '}
            <span style={{ color: 'var(--foreground)' }}>
              {t('hero.title').replace(/^k2\s*/, '')}
            </span>
          </h1>

          <p className="text-xl mb-4 max-w-3xl mx-auto"
            style={{ color: 'var(--secondary)', fontFamily: 'var(--font-mono), monospace' }}>
            {t('hero.subtitle')}
          </p>

          <p className="text-base mb-10 max-w-3xl mx-auto"
            style={{ color: 'hsl(var(--muted-foreground))' }}>
            {t('hero.description')}
          </p>

          <div className="flex flex-col sm:flex-row justify-center items-center gap-4 max-w-md sm:max-w-2xl mx-auto">
            <Link href="/purchase" className="w-full sm:flex-1">
              <Button size="lg" className="w-full min-w-[200px] font-bold"
                style={{ backgroundColor: 'var(--primary)', color: '#0a0a0f', fontFamily: 'var(--font-mono), monospace' }}>
                <Zap className="w-5 h-5 mr-2" />
                {t('hero.cta_primary')}
              </Button>
            </Link>
            <Link href="/install" className="w-full sm:flex-1">
              <Button variant="outline" size="lg" className="w-full min-w-[200px]"
                style={{ borderColor: 'var(--secondary)', color: 'var(--secondary)', fontFamily: 'var(--font-mono), monospace' }}>
                <Download className="w-5 h-5 mr-2" />
                {t('hero.cta_secondary')}
              </Button>
            </Link>
          </div>

          {/* Terminal preview */}
          <div className="mt-14 max-w-2xl mx-auto rounded-lg overflow-hidden text-left"
            style={{ backgroundColor: 'var(--card)', border: '1px solid rgba(0,255,136,0.2)' }}>
            <div className="flex items-center gap-2 px-4 py-3"
              style={{ backgroundColor: 'rgba(0,255,136,0.05)', borderBottom: '1px solid rgba(0,255,136,0.1)' }}>
              <span className="w-3 h-3 rounded-full bg-red-500 opacity-70"></span>
              <span className="w-3 h-3 rounded-full bg-yellow-500 opacity-70"></span>
              <span className="w-3 h-3 rounded-full opacity-70" style={{ backgroundColor: 'var(--primary)' }}></span>
              <span className="ml-2 text-xs" style={{ color: 'hsl(var(--muted-foreground))', fontFamily: 'var(--font-mono), monospace' }}>
                k2s — server
              </span>
            </div>
            <div className="p-6 text-sm space-y-2" style={{ fontFamily: 'var(--font-mono), monospace' }}>
              <div>
                <span style={{ color: 'hsl(var(--muted-foreground))' }}>$ </span>
                <span style={{ color: 'var(--primary)' }}>curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s</span>
              </div>
              <div style={{ color: 'hsl(var(--muted-foreground))' }}>Installing k2s...</div>
              <div>
                <span style={{ color: 'hsl(var(--muted-foreground))' }}>$ </span>
                <span style={{ color: 'var(--primary)' }}>k2s run</span>
              </div>
              <div style={{ color: 'var(--secondary)' }}>[k2s] ECH stealth tunnel started on :443</div>
              <div style={{ color: 'var(--secondary)' }}>[k2s] Connection URI:</div>
              <div className="break-all" style={{ color: 'var(--primary)' }}>k2v5://Zt8x...@your-server:443</div>
            </div>
          </div>
        </div>
      </section>

      {/* Feature Cards Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8"
        style={{ backgroundColor: 'rgba(17,17,24,0.5)' }}>
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-14">
            <h2 className="text-3xl font-bold mb-4"
              style={{ fontFamily: 'var(--font-mono), monospace' }}>
              {t('hero.features.title')}
            </h2>
          </div>

          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
            {featureCards.map((card) => (
              <Card
                key={card.key}
                className={`p-6 transition-all duration-300 hover:shadow-lg ${card.borderColor}`}
                style={{ backgroundColor: 'var(--card)', ...card.borderStyle }}
              >
                <div className="w-12 h-12 mb-4 rounded-lg flex items-center justify-center text-2xl"
                  style={{ backgroundColor: 'rgba(0,255,136,0.1)' }}>
                  {card.icon}
                </div>
                <h4 className="font-bold mb-2" style={{ color: 'var(--foreground)', fontFamily: 'var(--font-mono), monospace' }}>
                  {card.title}
                </h4>
                <p className="text-sm leading-relaxed" style={{ color: 'hsl(var(--muted-foreground))' }}>
                  {card.description}
                </p>
              </Card>
            ))}
          </div>
        </div>
      </section>

      {/* Comparison Table Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-14">
            <h2 className="text-3xl font-bold mb-4"
              style={{ fontFamily: 'var(--font-mono), monospace' }}>
              {t('hero.comparison.title')}
            </h2>
            <p className="text-base" style={{ color: 'hsl(var(--muted-foreground))' }}>
              {t('hero.comparison.subtitle')}
            </p>
          </div>

          <div className="overflow-x-auto rounded-lg"
            style={{ border: '1px solid rgba(0,255,136,0.2)' }}>
            <table className="w-full text-sm"
              style={{ backgroundColor: 'var(--card)', fontFamily: 'var(--font-mono), monospace' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid rgba(0,255,136,0.2)', backgroundColor: 'rgba(0,255,136,0.05)' }}>
                  <th className="text-left px-4 py-3" style={{ color: 'hsl(var(--muted-foreground))' }}>
                    Protocol
                  </th>
                  {dimensions.map((dim) => (
                    <th key={dim} className="text-center px-2 py-3 text-xs"
                      style={{ color: 'hsl(var(--muted-foreground))' }}>
                      {dimensionLabels[dim]}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {protocols.map((protocol, idx) => (
                  <tr
                    key={protocol.id}
                    style={{
                      borderBottom: idx < protocols.length - 1 ? '1px solid rgba(255,255,255,0.05)' : undefined,
                      backgroundColor: protocol.id === 'k2' ? 'rgba(0,255,136,0.05)' : undefined,
                    }}
                  >
                    <td className="px-4 py-3 font-bold"
                      style={{ color: protocol.id === 'k2' ? 'var(--primary)' : 'var(--foreground)' }}>
                      {protocol.name}
                      {protocol.id === 'k2' && (
                        <span className="ml-2 text-xs px-1 py-0.5 rounded"
                          style={{ backgroundColor: 'rgba(0,255,136,0.2)', color: 'var(--primary)' }}>
                          ★
                        </span>
                      )}
                    </td>
                    {dimensions.map((dim) => (
                      <td key={dim} className="text-center px-2 py-3">
                        {protocol[dim] ? (
                          <span className="font-bold text-base" style={{ color: 'var(--primary)' }}>✓</span>
                        ) : (
                          <span className="text-base opacity-30" style={{ color: 'var(--foreground)' }}>✗</span>
                        )}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      {/* Quick Start Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8"
        style={{ backgroundColor: 'rgba(17,17,24,0.5)' }}>
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-14">
            <h2 className="text-3xl font-bold mb-4"
              style={{ fontFamily: 'var(--font-mono), monospace' }}>
              {t('hero.quickstart.title')}
            </h2>
          </div>

          <div className="grid md:grid-cols-2 gap-8">
            {/* Server Terminal */}
            <div className="rounded-lg overflow-hidden"
              style={{ border: '1px solid rgba(0,255,136,0.3)' }}>
              <div className="flex items-center gap-2 px-4 py-3"
                style={{ backgroundColor: 'rgba(0,255,136,0.08)', borderBottom: '1px solid rgba(0,255,136,0.15)' }}>
                <Server className="w-4 h-4" style={{ color: 'var(--primary)' }} />
                <span className="text-sm font-bold" style={{ color: 'var(--primary)', fontFamily: 'var(--font-mono), monospace' }}>
                  {t('hero.quickstart.server.title')}
                </span>
              </div>
              <div className="p-5 text-sm space-y-2"
                style={{ backgroundColor: 'var(--card)', fontFamily: 'var(--font-mono), monospace' }}>
                <div style={{ color: 'hsl(var(--muted-foreground))' }}>
                  {t('hero.quickstart.server.step1')}
                </div>
                <div>
                  <span style={{ color: 'hsl(var(--muted-foreground))' }}>$ </span>
                  <span style={{ color: 'var(--primary)' }}>{t('hero.quickstart.server.step2')}</span>
                </div>
                <div className="pt-2" style={{ color: 'hsl(var(--muted-foreground))' }}>
                  {t('hero.quickstart.server.step3')}
                </div>
                <div>
                  <span style={{ color: 'hsl(var(--muted-foreground))' }}>$ </span>
                  <span style={{ color: 'var(--primary)' }}>{t('hero.quickstart.server.step4')}</span>
                </div>
                <div className="pt-2" style={{ color: 'hsl(var(--muted-foreground))' }}>
                  {t('hero.quickstart.server.step5')}
                </div>
                <div style={{ color: 'var(--secondary)' }}>
                  {t('hero.quickstart.server.step6')}
                </div>
              </div>
            </div>

            {/* Client Terminal */}
            <div className="rounded-lg overflow-hidden"
              style={{ border: '1px solid rgba(0,212,255,0.3)' }}>
              <div className="flex items-center gap-2 px-4 py-3"
                style={{ backgroundColor: 'rgba(0,212,255,0.08)', borderBottom: '1px solid rgba(0,212,255,0.15)' }}>
                <Terminal className="w-4 h-4" style={{ color: 'var(--secondary)' }} />
                <span className="text-sm font-bold" style={{ color: 'var(--secondary)', fontFamily: 'var(--font-mono), monospace' }}>
                  {t('hero.quickstart.client.title')}
                </span>
              </div>
              <div className="p-5 text-sm space-y-2"
                style={{ backgroundColor: 'var(--card)', fontFamily: 'var(--font-mono), monospace' }}>
                <div style={{ color: 'hsl(var(--muted-foreground))' }}>
                  {t('hero.quickstart.client.step1')}
                </div>
                <div>
                  <span style={{ color: 'hsl(var(--muted-foreground))' }}>$ </span>
                  <span style={{ color: 'var(--secondary)' }}>{t('hero.quickstart.client.step2')}</span>
                </div>
                <div className="pt-2" style={{ color: 'hsl(var(--muted-foreground))' }}>
                  {t('hero.quickstart.client.step3')}
                </div>
                <div>
                  <span style={{ color: 'hsl(var(--muted-foreground))' }}>$ </span>
                  <span style={{ color: 'var(--secondary)' }}>{t('hero.quickstart.client.step4')}</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Download Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto text-center">
          <h2 className="text-3xl font-bold mb-4"
            style={{ fontFamily: 'var(--font-mono), monospace' }}>
            {t('download.title')}
          </h2>
          <p className="text-base mb-12" style={{ color: 'hsl(var(--muted-foreground))' }}>
            {t('download.subtitle')}
          </p>

          <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-6 max-w-7xl mx-auto">
            <Card className="p-6 transition-all duration-300 hover:shadow-lg"
              style={{ backgroundColor: 'var(--card)', border: '1px solid rgba(0,255,136,0.15)' }}>
              <Smartphone className="w-12 h-12 mx-auto mb-4" style={{ color: 'var(--primary)' }} />
              <h3 className="text-lg font-semibold mb-2"
                style={{ color: 'var(--foreground)', fontFamily: 'var(--font-mono), monospace' }}>
                {t('download.platforms.iosLabel')}
              </h3>
              <p className="text-sm mb-4" style={{ color: 'hsl(var(--muted-foreground))' }}>
                {t('download.platforms.ios')}
              </p>
              {DOWNLOAD_LINKS.ios ? (
                <NextLink href={DOWNLOAD_LINKS.ios} target="_blank" rel="noopener noreferrer">
                  <Button variant="outline" size="sm" className="w-full"
                    style={{ borderColor: 'var(--primary)', color: 'var(--primary)', fontFamily: 'var(--font-mono), monospace' }}>
                    <ExternalLink className="w-4 h-4 mr-2" />
                    {t('download.downloadButton')}
                  </Button>
                </NextLink>
              ) : (
                <Button variant="outline" size="sm" disabled className="w-full opacity-50 cursor-not-allowed"
                  style={{ fontFamily: 'var(--font-mono), monospace' }}>
                  <span className="mr-2">{"⏳"}</span>
                  {t('download.comingSoon')}
                </Button>
              )}
            </Card>

            <Card className="p-6 transition-all duration-300 hover:shadow-lg"
              style={{ backgroundColor: 'var(--card)', border: '1px solid rgba(0,212,255,0.15)' }}>
              <Smartphone className="w-12 h-12 mx-auto mb-4" style={{ color: 'var(--secondary)' }} />
              <h3 className="text-lg font-semibold mb-2"
                style={{ color: 'var(--foreground)', fontFamily: 'var(--font-mono), monospace' }}>
                {t('download.platforms.androidLabel')}
              </h3>
              <p className="text-sm mb-4" style={{ color: 'hsl(var(--muted-foreground))' }}>
                {t('download.platforms.android')}
              </p>
              {DOWNLOAD_LINKS.android ? (
                <NextLink href={DOWNLOAD_LINKS.android} target="_blank" rel="noopener noreferrer">
                  <Button variant="outline" size="sm" className="w-full"
                    style={{ borderColor: 'var(--secondary)', color: 'var(--secondary)', fontFamily: 'var(--font-mono), monospace' }}>
                    <ExternalLink className="w-4 h-4 mr-2" />
                    {t('download.downloadButton')}
                  </Button>
                </NextLink>
              ) : (
                <Button variant="outline" size="sm" disabled className="w-full opacity-50 cursor-not-allowed"
                  style={{ fontFamily: 'var(--font-mono), monospace' }}>
                  <span className="mr-2">{"⏳"}</span>
                  {t('download.comingSoon')}
                </Button>
              )}
            </Card>

            <Card className="p-6 transition-all duration-300 hover:shadow-lg"
              style={{ backgroundColor: 'var(--card)', border: '1px solid rgba(0,255,136,0.15)' }}>
              <Monitor className="w-12 h-12 mx-auto mb-4" style={{ color: 'var(--primary)' }} />
              <h3 className="text-lg font-semibold mb-2"
                style={{ color: 'var(--foreground)', fontFamily: 'var(--font-mono), monospace' }}>
                {t('download.platforms.windowsLabel')}
              </h3>
              <p className="text-sm mb-4" style={{ color: 'hsl(var(--muted-foreground))' }}>
                {t('download.platforms.windows')}
              </p>
              <NextLink href={DOWNLOAD_LINKS.windows} target="_blank" rel="noopener noreferrer">
                <Button variant="outline" size="sm" className="w-full"
                  style={{ borderColor: 'var(--primary)', color: 'var(--primary)', fontFamily: 'var(--font-mono), monospace' }}>
                  <ExternalLink className="w-4 h-4 mr-2" />
                  {t('download.downloadButton')}
                </Button>
              </NextLink>
            </Card>

            <Card className="p-6 transition-all duration-300 hover:shadow-lg"
              style={{ backgroundColor: 'var(--card)', border: '1px solid rgba(0,212,255,0.15)' }}>
              <Monitor className="w-12 h-12 mx-auto mb-4" style={{ color: 'var(--secondary)' }} />
              <h3 className="text-lg font-semibold mb-2"
                style={{ color: 'var(--foreground)', fontFamily: 'var(--font-mono), monospace' }}>
                {t('download.platforms.macosLabel')}
              </h3>
              <p className="text-sm mb-4" style={{ color: 'hsl(var(--muted-foreground))' }}>
                {t('download.platforms.macos')}
              </p>
              <NextLink href={DOWNLOAD_LINKS.macos} target="_blank" rel="noopener noreferrer">
                <Button variant="outline" size="sm" className="w-full"
                  style={{ borderColor: 'var(--secondary)', color: 'var(--secondary)', fontFamily: 'var(--font-mono), monospace' }}>
                  <ExternalLink className="w-4 h-4 mr-2" />
                  {t('download.downloadButton')}
                </Button>
              </NextLink>
            </Card>
          </div>
        </div>
      </section>

      <Footer />
    </div>
  );
}
