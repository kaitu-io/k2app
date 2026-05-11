import { ExternalLink, ShoppingCart } from 'lucide-react';
import {
  PurchaseLinks as PurchaseLinksData,
  orderedPurchaseLinks,
  PurchasePlatform,
} from '@/lib/router-hardware';

export function PurchaseLinksRow({
  links,
  locale,
  formatBuyAt,
  morePlatformsLabel,
  platformLabel,
}: {
  links: PurchaseLinksData;
  locale: string;
  /** Resolves the "Buy on {platform}" string with the platform display name. */
  formatBuyAt: (platformName: string) => string;
  morePlatformsLabel: string;
  platformLabel: (platform: PurchasePlatform) => string;
}) {
  const ordered = orderedPurchaseLinks(links, locale);
  if (ordered.length === 0) return null;

  const [primary, ...rest] = ordered;
  const primaryText = formatBuyAt(platformLabel(primary.platform));

  return (
    <div className="space-y-2">
      <a
        href={primary.url}
        target="_blank"
        rel="noopener noreferrer sponsored"
        className="inline-flex items-center justify-center gap-2 w-full px-4 py-2.5 rounded-md bg-primary text-primary-foreground font-medium text-sm hover:bg-primary/90 transition-colors"
      >
        <ShoppingCart className="w-4 h-4" />
        {primaryText}
        <ExternalLink className="w-3.5 h-3.5 opacity-70" />
      </a>
      {rest.length > 0 && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>{morePlatformsLabel}{':'}</span>
          {rest.map(({ platform, url }) => (
            <a
              key={platform}
              href={url}
              target="_blank"
              rel="noopener noreferrer sponsored"
              className="hover:text-foreground hover:underline"
            >
              {platformLabel(platform)}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
