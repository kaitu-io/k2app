'use client';

import { useEffect } from 'react';
import { usePathname } from '@/i18n/routing';
import { externalReferrerHost, isEmbeddedPage, track } from '@/lib/funnel';

// Only the first page view of a visit carries the external referrer.
let firstViewSent = false;

export default function FunnelPageView() {
  const pathname = usePathname();

  useEffect(() => {
    // An in-app embedded page is not a site visit; counting it would put every
    // app user into the website funnels.
    if (isEmbeddedPage()) return;
    const ref = firstViewSent ? undefined : externalReferrerHost();
    firstViewSent = true;
    track('page_view', { ref });
  }, [pathname]);

  return null;
}
