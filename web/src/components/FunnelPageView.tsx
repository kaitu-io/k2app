'use client';

import { useEffect } from 'react';
import { usePathname } from '@/i18n/routing';
import { externalReferrerHost, track } from '@/lib/funnel';

// Only the first page view of a visit carries the external referrer.
let firstViewSent = false;

export default function FunnelPageView() {
  const pathname = usePathname();

  useEffect(() => {
    const ref = firstViewSent ? undefined : externalReferrerHost();
    firstViewSent = true;
    track('page_view', { ref });
  }, [pathname]);

  return null;
}
