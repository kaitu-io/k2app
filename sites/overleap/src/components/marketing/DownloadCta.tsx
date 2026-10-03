import { Download, Monitor, Smartphone } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';

export default function DownloadCta({ title, subtitle, platforms, button }: { title: string; subtitle: string; platforms: string; button: string }) {
  return (
    <section id="download" className="bg-card/60 px-4 py-20 sm:px-6 lg:px-8">
      <div className="mx-auto max-w-3xl text-center">
        <div className="mb-6 flex justify-center gap-4">
          <Monitor className="h-8 w-8 text-primary" />
          <Smartphone className="h-8 w-8 text-secondary" />
        </div>
        <h2 className="mb-3 text-3xl font-bold">{title}</h2>
        <p className="mb-2 text-muted-foreground">{subtitle}</p>
        <p className="mb-8 text-sm text-muted-foreground/70">{platforms}</p>
        <Button asChild size="lg" className="font-semibold">
          <Link href="/install">
            <Download className="h-5 w-5" />
            {button}
          </Link>
        </Button>
      </div>
    </section>
  );
}
