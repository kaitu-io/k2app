import { Card } from '@/components/ui/card';
import { ShieldCheck, TrendingUp, Shuffle, EyeOff } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export interface Feature { key: string; title: string; description: string }

const ICONS: Record<string, LucideIcon> = { ech: ShieldCheck, k2cc: TrendingUp, transport: Shuffle, privacy: EyeOff };

export default function OverleapFeatures({ title, features }: { title: string; features: Feature[] }) {
  return (
    <section id="features" className="py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-6xl mx-auto">
        <h2 className="text-3xl font-bold text-center mb-14">{title}</h2>
        <div className="grid md:grid-cols-2 gap-6">
          {features.map((f) => {
            const Icon = ICONS[f.key] ?? ShieldCheck;
            return (
              <Card key={f.key} className="p-7 bg-card border-border">
                <div className="w-11 h-11 mb-4 rounded-xl bg-primary/10 flex items-center justify-center">
                  <Icon className="w-6 h-6 text-primary" />
                </div>
                <h3 className="text-lg font-semibold mb-2 text-foreground">{f.title}</h3>
                <p className="text-sm leading-relaxed text-muted-foreground">{f.description}</p>
              </Card>
            );
          })}
        </div>
      </div>
    </section>
  );
}
