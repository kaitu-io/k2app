import Image from 'next/image';
import { CheckCircle, AlertCircle, Target } from 'lucide-react';
import { Card } from '@/components/ui/card';
import { RouterHardware, PurchasePlatform } from '@/lib/router-hardware';
import { PurchaseLinksRow } from './PurchaseLinks';

interface HardwareCardI18n {
  name: string;
  tagline: string;
  pros: string[];
  cons: string[];
  fit: string;
}

interface SpecsLabel {
  soc: string;
  ram: string;
  flash: string;
  wifi: string;
  ports: string;
  throughput: string;
}

export function HardwareCard({
  hardware,
  locale,
  copy,
  specsLabel,
  tierLabel,
  typeLabel,
  priceLabel,
  prosLabel,
  consLabel,
  fitLabel,
  imageAlt,
  formatBuyAt,
  morePlatformsLabel,
  platformLabel,
}: {
  hardware: RouterHardware;
  locale: string;
  copy: HardwareCardI18n;
  specsLabel: SpecsLabel;
  tierLabel: string;
  typeLabel: string;
  priceLabel: string;
  prosLabel: string;
  consLabel: string;
  fitLabel: string;
  imageAlt: string;
  formatBuyAt: (platformName: string) => string;
  morePlatformsLabel: string;
  platformLabel: (p: PurchasePlatform) => string;
}) {
  return (
    <Card className="flex flex-col p-6 hover:shadow-lg transition-shadow">
      {/* Header: badges */}
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
          {tierLabel}
        </span>
        <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-muted text-muted-foreground">
          {typeLabel}
        </span>
      </div>

      {/* Image */}
      <div className="bg-muted rounded-lg overflow-hidden mb-4">
        <Image
          src={hardware.image}
          alt={imageAlt}
          width={800}
          height={600}
          sizes="(max-width: 640px) 100vw, (max-width: 1024px) 50vw, 33vw"
          className="w-full h-auto object-cover aspect-[4/3]"
        />
      </div>

      {/* Name + tagline */}
      <h3 className="text-xl font-bold text-foreground mb-1">{copy.name}</h3>
      <p className="text-sm text-muted-foreground mb-4 leading-relaxed">{copy.tagline}</p>

      {/* Price */}
      <div className="mb-4 pb-4 border-b border-border">
        <span className="text-xs text-muted-foreground">{priceLabel}</span>
        <div className="text-2xl font-bold text-foreground">
          ¥{hardware.priceMin}
          <span className="text-base font-normal text-muted-foreground"> – ¥{hardware.priceMax}</span>
        </div>
      </div>

      {/* Specs */}
      <dl className="space-y-1.5 text-sm mb-4">
        <SpecRow label={specsLabel.soc} value={hardware.soc} />
        <SpecRow label={specsLabel.ram} value={hardware.ram} />
        <SpecRow label={specsLabel.flash} value={hardware.flash} />
        {hardware.wifi && <SpecRow label={specsLabel.wifi} value={hardware.wifi} />}
        <SpecRow label={specsLabel.ports} value={hardware.ports} />
        <SpecRow label={specsLabel.throughput} value={hardware.k2rThroughput} highlight />
      </dl>

      {/* Pros */}
      <div className="mb-3">
        <h4 className="text-xs font-semibold text-green-600 dark:text-green-400 mb-1.5 flex items-center gap-1.5">
          <CheckCircle className="w-3.5 h-3.5" />
          {prosLabel}
        </h4>
        <ul className="space-y-1 text-sm text-foreground/80">
          {copy.pros.map((p, i) => (
            <li key={i} className="flex gap-2">
              <span className="text-green-600 dark:text-green-400 mt-0.5">✓</span>
              <span>{p}</span>
            </li>
          ))}
        </ul>
      </div>

      {/* Cons */}
      <div className="mb-3">
        <h4 className="text-xs font-semibold text-amber-600 dark:text-amber-400 mb-1.5 flex items-center gap-1.5">
          <AlertCircle className="w-3.5 h-3.5" />
          {consLabel}
        </h4>
        <ul className="space-y-1 text-sm text-foreground/80">
          {copy.cons.map((c, i) => (
            <li key={i} className="flex gap-2">
              <span className="text-amber-600 dark:text-amber-400 mt-0.5">!</span>
              <span>{c}</span>
            </li>
          ))}
        </ul>
      </div>

      {/* Fit */}
      <div className="mb-5 p-3 rounded-md bg-muted">
        <h4 className="text-xs font-semibold text-foreground mb-1 flex items-center gap-1.5">
          <Target className="w-3.5 h-3.5" />
          {fitLabel}
        </h4>
        <p className="text-sm text-foreground/80 leading-relaxed">{copy.fit}</p>
      </div>

      {/* Purchase links — push to bottom of card */}
      <div className="mt-auto">
        <PurchaseLinksRow
          links={hardware.purchaseLinks}
          locale={locale}
          formatBuyAt={formatBuyAt}
          morePlatformsLabel={morePlatformsLabel}
          platformLabel={platformLabel}
        />
      </div>
    </Card>
  );
}

function SpecRow({
  label,
  value,
  highlight,
}: {
  label: string;
  value: string;
  highlight?: boolean;
}) {
  return (
    <div className="flex justify-between gap-3">
      <dt className="text-muted-foreground shrink-0">{label}</dt>
      <dd className={`text-right ${highlight ? 'font-semibold text-primary' : 'text-foreground'}`}>
        {value}
      </dd>
    </div>
  );
}
