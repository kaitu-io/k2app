import { EyeOff, Globe, ShieldCheck, Shuffle, TrendingUp, type LucideIcon } from 'lucide-react';
import { Card } from '@/components/ui/card';

export const FEATURE_KEYS = ['isp', 'logs', 'speed', 'roaming', 'travel'] as const;
export type FeatureKey = (typeof FEATURE_KEYS)[number];

export interface Feature {
  key: FeatureKey;
  title: string;
  description: string;
}

const ICONS: Record<FeatureKey, LucideIcon> = {
  isp: EyeOff,
  logs: ShieldCheck,
  speed: TrendingUp,
  roaming: Shuffle,
  travel: Globe,
};

export default function Features({ title, features }: { title: string; features: Feature[] }) {
  return (
    <section id="features" className="scroll-mt-16 px-4 py-20 sm:px-6 lg:px-8">
      <div className="mx-auto max-w-6xl">
        <h2 className="mb-14 text-center text-3xl font-bold">{title}</h2>
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {features.map((f) => {
            const Icon = ICONS[f.key];
            return (
              <Card key={f.key} className="gap-0 border-border bg-card p-7">
                <div className="mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-primary/10">
                  <Icon className="h-6 w-6 text-primary" />
                </div>
                <h3 className="mb-2 text-lg font-semibold text-foreground">{f.title}</h3>
                <p className="text-sm leading-relaxed text-muted-foreground">{f.description}</p>
              </Card>
            );
          })}
        </div>
      </div>
    </section>
  );
}
