import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { Download, Monitor, Smartphone } from 'lucide-react';

interface DownloadCTAProps {
  title: string;
  subtitle: string;
  buttonText: string;
  platforms: string;
}

export default function DownloadCTA({ title, subtitle, buttonText, platforms }: DownloadCTAProps) {
  return (
    <section className="relative z-10 py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-3xl mx-auto text-center">
        <div className="flex justify-center gap-4 mb-6">
          <Monitor className="w-8 h-8 text-primary" />
          <Smartphone className="w-8 h-8 text-secondary" />
        </div>
        <h2 className="text-3xl font-bold mb-4 font-mono">{title}</h2>
        <p className="text-muted-foreground mb-3">{subtitle}</p>
        <p className="text-sm text-muted-foreground/70 mb-8 font-mono">{platforms}</p>
        <Link href="/install">
          <Button size="lg" className="font-bold bg-primary text-primary-foreground font-mono">
            <Download className="w-5 h-5 mr-2" />
            {buttonText}
          </Button>
        </Link>
      </div>
    </section>
  );
}
