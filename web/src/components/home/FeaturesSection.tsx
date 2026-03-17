import { Card } from '@/components/ui/card';
import {
  TrendingUp,
  ShieldCheck,
  Shuffle,
  Zap,
  EyeOff,
  Lock,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

interface FeaturesSectionProps {
  sectionTitle: string;
  features: {
    congestion: { title: string; description: string };
    ech: { title: string; description: string };
    transport: { title: string; description: string };
    zeroDeploy: { title: string; description: string };
    reverseProxy: { title: string; description: string };
    selfSign: { title: string; description: string };
  };
}

const ICON_MAP: Record<string, { icon: LucideIcon; accent: 'primary' | 'secondary' }> = {
  congestion: { icon: TrendingUp, accent: 'primary' },
  ech: { icon: ShieldCheck, accent: 'secondary' },
  transport: { icon: Shuffle, accent: 'primary' },
  zeroDeploy: { icon: Zap, accent: 'secondary' },
  reverseProxy: { icon: EyeOff, accent: 'primary' },
  selfSign: { icon: Lock, accent: 'secondary' },
};

const FEATURE_ORDER = ['congestion', 'ech', 'transport', 'zeroDeploy', 'reverseProxy', 'selfSign'] as const;

export default function FeaturesSection({ sectionTitle, features }: FeaturesSectionProps) {
  return (
    <section className="relative z-10 py-20 px-4 sm:px-6 lg:px-8 bg-[rgba(5,5,8,0.6)] backdrop-blur-sm">
      <div className="max-w-7xl mx-auto">
        <div className="text-center mb-14">
          <h2 className="text-3xl font-bold font-mono">{sectionTitle}</h2>
        </div>

        <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-6">
          {FEATURE_ORDER.map((key) => {
            const { icon: Icon, accent } = ICON_MAP[key];
            const feature = features[key];
            return (
              <Card
                key={key}
                className={`p-6 transition-all duration-300 hover:shadow-lg border-t-4 bg-card ${
                  accent === 'primary' ? 'border-t-primary' : 'border-t-secondary'
                }`}
              >
                <div className="w-12 h-12 mb-4 rounded-lg flex items-center justify-center bg-primary/10">
                  <Icon className={`w-6 h-6 ${
                    accent === 'primary' ? 'text-primary' : 'text-secondary'
                  }`} />
                </div>
                <h4 className="font-bold mb-2 text-foreground font-mono">{feature.title}</h4>
                <p className="text-sm leading-relaxed text-muted-foreground">{feature.description}</p>
              </Card>
            );
          })}
        </div>
      </div>
    </section>
  );
}
