/* eslint-disable react/jsx-no-literals */
import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, ShieldCheck, Zap } from 'lucide-react';

interface HeroSectionProps {
  badge: string;
  title: string;
  subtitle: string;
  description: string;
  ctaPrimary: string;
  ctaSecondary: string;
  connected: string;
  nodeInfo: string;
}

export default function HeroSection({
  badge,
  title,
  subtitle,
  description,
  ctaPrimary,
  ctaSecondary,
  connected,
  nodeInfo,
}: HeroSectionProps) {
  return (
    <section id="hero" className="relative z-10 min-h-[100dvh] flex flex-col justify-center px-4 sm:px-6 lg:px-8 py-10 sm:py-20">
      <div className="max-w-7xl mx-auto text-center">
        {/* Badge */}
        <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs mb-6 bg-primary/10 text-primary border border-primary/30 font-mono">
          <span className="w-2 h-2 rounded-full animate-pulse bg-primary" />
          {badge}
        </div>

        <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold mb-6 leading-tight font-mono text-foreground">
          {title}
        </h1>

        <p className="text-xl mb-4 max-w-3xl mx-auto text-secondary font-mono">
          {subtitle}
        </p>

        <p className="text-base mb-10 max-w-2xl mx-auto text-muted-foreground leading-relaxed">
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

        {/* Client UI mockup — hidden on mobile */}
        <div className="hidden sm:flex mt-14 justify-center">
          <div className="w-60 rounded-2xl bg-card border border-border/40 shadow-2xl overflow-hidden">
            <div className="flex items-center justify-between px-4 py-2.5 border-b border-border/30">
              <span className="text-sm font-bold text-foreground">k2</span>
              <span className="text-xs text-primary font-medium flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
                {connected}
              </span>
            </div>
            <div className="py-10 flex flex-col items-center">
              <div className="w-24 h-24 rounded-full border-4 border-primary/20 flex items-center justify-center">
                <div className="w-16 h-16 rounded-full border-2 border-primary/40 flex items-center justify-center bg-primary/5">
                  <ShieldCheck className="w-8 h-8 text-primary" />
                </div>
              </div>
              <p className="mt-5 text-base font-semibold text-primary">{connected}</p>
              <p className="text-xs text-muted-foreground mt-1">{nodeInfo}</p>
            </div>
            <div className="px-4 py-3 border-t border-border/20 flex items-center justify-center gap-2 text-xs text-muted-foreground/40 font-mono">
              <span>Win</span><span>·</span><span>Mac</span><span>·</span><span>iOS</span><span>·</span><span>Android</span>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
