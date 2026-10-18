'use client';

import { useEffect, useRef } from 'react';
import { usePathname } from '@/i18n/routing';
import { externalReferrerHost, track } from '@/lib/funnel';
import { isEmbeddedPage } from '@/lib/embed';

/**
 * page_view on every route (and pricing_view on /pricing; usePathname is locale-less). The first view
 * of the loaded app carries the external referrer host; later views are internal.
 * Pages opened inside the app (embed mode) are app screens, not site visits.
 */
export default function FunnelPageViews() {
  const pathname = usePathname();
  const first = useRef(true);

  useEffect(() => {
    if (isEmbeddedPage()) return;
    track('page_view', first.current ? { ref: externalReferrerHost() } : undefined);
    first.current = false;
    if (pathname === '/pricing') track('pricing_view');
  }, [pathname]);

  return null;
}
