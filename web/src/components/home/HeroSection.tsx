/* eslint-disable react/jsx-no-literals */
import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, Zap } from 'lucide-react';

interface HeroSectionProps {
  title: string;
  subtitle: string;
  description: string;
  ctaPrimary: string;
  ctaSecondary: string;
  terminalTitle: string;
}

export default function HeroSection({
  title,
  subtitle,
  description,
  ctaPrimary,
  ctaSecondary,
  terminalTitle,
}: HeroSectionProps) {
  return (
    <section className="relative z-10 min-h-[100dvh] flex flex-col justify-center px-4 sm:px-6 lg:px-8 py-10 sm:py-20">
      <div className="max-w-7xl mx-auto text-center">
        {/* Badge */}
        <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs mb-6 bg-primary/10 text-primary border border-primary/30 font-mono">
          <span className="w-2 h-2 rounded-full animate-pulse bg-primary" />
          k2v5 — k2cc Anti-QoS Congestion Control
        </div>

        <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold mb-6 leading-tight font-mono text-foreground">
          {title}
        </h1>

        <p className="text-xl mb-4 max-w-3xl mx-auto text-secondary font-mono">
          {subtitle}
        </p>

        <p className="text-base mb-10 max-w-3xl mx-auto text-muted-foreground">
          {description}
        </p>

        {/* CTAs */}
        <div className="flex flex-col sm:flex-row justify-center items-center gap-4 max-w-md sm:max-w-2xl mx-auto">
          <Link href="/purchase" className="w-full sm:flex-1">
            <Button size="lg" className="w-full min-w-[200px] font-bold bg-primary text-primary-foreground font-mono">
              <Zap className="w-5 h-5 mr-2" />
              {ctaPrimary}
            </Button>
          </Link>
          <Link href="/install" className="w-full sm:flex-1">
            <Button variant="outline" size="lg" className="w-full min-w-[200px] border-secondary text-secondary hover:bg-secondary/10 hover:text-secondary font-mono">
              <Download className="w-5 h-5 mr-2" />
              {ctaSecondary}
            </Button>
          </Link>
        </div>

        {/* Terminal preview — hidden on mobile to keep hero clean */}
        <div className="hidden sm:block mt-14 max-w-2xl mx-auto rounded-lg overflow-hidden text-left bg-card border border-primary/20">
          <div className="flex items-center gap-2 px-4 py-3 bg-primary/5 border-b border-primary/10">
            <span className="w-3 h-3 rounded-full bg-red-500 opacity-70" />
            <span className="w-3 h-3 rounded-full bg-yellow-500 opacity-70" />
            <span className="w-3 h-3 rounded-full bg-primary opacity-70" />
            <span className="ml-2 text-xs text-muted-foreground font-mono">
              k2s — {terminalTitle}
            </span>
          </div>
          <div className="p-6 text-sm space-y-2 font-mono">
            <div>
              <span className="text-muted-foreground">$ </span>
              <span className="text-primary">curl -fsSL https://kaitu.io/i/k2s | sudo sh</span>
            </div>
            <div className="text-muted-foreground">Installing k2s...</div>
            <div className="text-secondary">[k2s] ECH stealth tunnel started on :443</div>
            <div className="text-secondary">[k2s] Connection URI:</div>
            <div className="break-all text-primary">k2v5://Zt8x...@your-server:443</div>
          </div>
        </div>
      </div>
    </section>
  );
}
