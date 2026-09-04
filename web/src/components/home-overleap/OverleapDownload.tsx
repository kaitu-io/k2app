import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, Monitor, Smartphone } from 'lucide-react';

export default function OverleapDownload({ title, subtitle, platforms, button }: { title: string; subtitle: string; platforms: string; button: string }) {
  return (
    <section id="download" className="py-20 px-4 sm:px-6 lg:px-8 bg-card/60">
      <div className="max-w-3xl mx-auto text-center">
        <div className="flex justify-center gap-4 mb-6">
          <Monitor className="w-8 h-8 text-primary" />
          <Smartphone className="w-8 h-8 text-secondary" />
        </div>
        <h2 className="text-3xl font-bold mb-3">{title}</h2>
        <p className="text-muted-foreground mb-2">{subtitle}</p>
        <p className="text-sm text-muted-foreground/70 mb-8">{platforms}</p>
        <Button asChild size="lg" className="font-semibold"><Link href="/install"><Download className="w-5 h-5 mr-2" />{button}</Link></Button>
      </div>
    </section>
  );
}
