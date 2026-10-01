'use client';

import { useEffect } from 'react';
import { track, type WebFunnelEvent } from '@/lib/funnel';

/** Fires one funnel event when mounted. Lets a server-rendered page report a view without becoming a client page. */
export default function FunnelEvent({ event }: { event: WebFunnelEvent }) {
  useEffect(() => {
    track(event);
  }, [event]);
  return null;
}
