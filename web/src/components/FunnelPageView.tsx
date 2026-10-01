'use client';

import { useEffect } from 'react';
import { usePathname } from '@/i18n/routing';
import { externalReferrerHost, pxUrl } from '@/lib/funnel';

// Only the first page view of a visit carries the external referrer.
let firstViewSent = false;

export default function FunnelPageView() {
  const pathname = usePathname();

  useEffect(() => {
    try {
      const ref = firstViewSent ? undefined : externalReferrerHost();
      firstViewSent = true;
      new Image().src = pxUrl('page_view', { ref });
    } catch {
      // analytics must never break a product path
    }
  }, [pathname]);

  return null;
}
