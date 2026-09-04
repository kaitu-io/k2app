import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, ShieldCheck, ArrowRight } from 'lucide-react';

interface Props {
  badge: string;
  title: string;
  subtitle: string;
  description: string;
  ctaPrimary: string;
  ctaSecondary: string;
  mockConnected: string;
  mockNode: string;
  brandName: string;
}

export default function OverleapHero(p: Props) {
  return (
    <section id="hero" className="relative min-h-[88dvh] flex flex-col justify-center px-4 sm:px-6 lg:px-8 py-16 sm:py-24 overflow-hidden">
      <div aria-hidden className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(60%_50%_at_50%_0%,rgba(124,92,255,0.22),transparent_70%)]" />
      <div className="max-w-6xl mx-auto grid lg:grid-cols-[1.2fr_0.8fr] gap-12 items-center">
        <div className="text-center lg:text-left">
          <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs mb-6 bg-primary/10 text-primary border border-primary/30">
            <span className="w-2 h-2 rounded-full bg-secondary" />
            {p.badge}
          </div>
          <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold tracking-tight mb-5 text-foreground">{p.title}</h1>
          <p className="text-xl mb-4 text-secondary">{p.subtitle}</p>
          <p className="text-base mb-10 max-w-2xl mx-auto lg:mx-0 text-muted-foreground leading-relaxed">{p.description}</p>
          <div className="flex flex-col sm:flex-row justify-center lg:justify-start gap-4">
            <Button asChild size="lg" className="font-semibold min-w-[200px]">
              <Link href="/purchase">{p.ctaPrimary}<ArrowRight className="w-4 h-4 ml-2" /></Link>
            </Button>
            <Button asChild variant="outline" size="lg" className="font-semibold min-w-[200px] border-border">
              <Link href="/install"><Download className="w-4 h-4 mr-2" />{p.ctaSecondary}</Link>
            </Button>
          </div>
        </div>
        <div className="hidden lg:flex justify-center">
          <div className="w-64 rounded-3xl bg-card border border-border shadow-2xl overflow-hidden">
            <div className="flex items-center justify-between px-4 py-3 border-b border-border">
              <span className="text-sm font-semibold text-foreground">{p.brandName}</span>
              <span className="text-xs text-secondary flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-secondary" />{p.mockConnected}
              </span>
            </div>
            <div className="py-12 flex flex-col items-center">
              <div className="w-28 h-28 rounded-full border-4 border-secondary/25 flex items-center justify-center">
                <div className="w-20 h-20 rounded-full bg-secondary/10 border border-secondary/40 flex items-center justify-center">
                  <ShieldCheck className="w-9 h-9 text-secondary" />
                </div>
              </div>
              <p className="mt-6 text-base font-semibold text-secondary">{p.mockConnected}</p>
              <p className="mt-1 text-xs text-muted-foreground">{p.mockNode}</p>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
