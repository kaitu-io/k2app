import { ArrowRight, Download, ShieldCheck } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';

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

export default function Hero(p: Props) {
  return (
    <section id="hero" className="relative flex min-h-[88dvh] flex-col justify-center overflow-hidden px-4 py-16 sm:px-6 sm:py-24 lg:px-8">
      <div aria-hidden className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(60%_50%_at_50%_0%,rgba(124,92,255,0.22),transparent_70%)]" />
      <div className="mx-auto grid max-w-6xl items-center gap-12 lg:grid-cols-[1.2fr_0.8fr]">
        <div className="text-center lg:text-start">
          <div className="mb-6 inline-flex items-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-3 py-1 text-xs text-primary">
            <span className="h-2 w-2 rounded-full bg-secondary" />
            {p.badge}
          </div>
          <h1 className="mb-5 text-4xl font-bold tracking-tight text-foreground sm:text-5xl lg:text-6xl">{p.title}</h1>
          <p className="mb-4 text-xl text-secondary">{p.subtitle}</p>
          <p className="mx-auto mb-10 max-w-2xl text-base leading-relaxed text-muted-foreground lg:mx-0">{p.description}</p>
          <div className="flex flex-col justify-center gap-4 sm:flex-row lg:justify-start">
            <Button asChild size="lg" className="min-w-[200px] font-semibold">
              <Link href="/purchase">
                {p.ctaPrimary}
                <ArrowRight className="rtl:rotate-180" />
              </Link>
            </Button>
            <Button asChild variant="outline" size="lg" className="min-w-[200px] border-border font-semibold">
              <Link href="/install">
                <Download />
                {p.ctaSecondary}
              </Link>
            </Button>
          </div>
        </div>
        <div className="hidden justify-center lg:flex">
          <div className="w-64 overflow-hidden rounded-3xl border border-border bg-card shadow-2xl">
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <span className="text-sm font-semibold text-foreground">{p.brandName}</span>
              <span className="flex items-center gap-1.5 text-xs text-secondary">
                <span className="h-1.5 w-1.5 rounded-full bg-secondary" />
                {p.mockConnected}
              </span>
            </div>
            <div className="flex flex-col items-center py-12">
              <div className="flex h-28 w-28 items-center justify-center rounded-full border-4 border-secondary/25">
                <div className="flex h-20 w-20 items-center justify-center rounded-full border border-secondary/40 bg-secondary/10">
                  <ShieldCheck className="h-9 w-9 text-secondary" />
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
