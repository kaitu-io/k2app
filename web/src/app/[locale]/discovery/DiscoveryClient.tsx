"use client";

import { Suspense } from 'react';
import { useEmbedMode } from '@/hooks/useEmbedMode';

interface DiscoveryClientProps {
  /** Content rendered inside the embed-aware wrapper */
  children: React.ReactNode;
}

/**
 * Inner component that reads embed mode (requires Suspense for useSearchParams).
 * Conditionally adds embedded-mode CSS class and hides nav/footer signals.
 */
function DiscoveryClientInner({ children }: DiscoveryClientProps) {
  const { isEmbedded } = useEmbedMode();

  return (
    <div className={isEmbedded ? 'embedded-mode' : ''}>
      {children}
    </div>
  );
}

/**
 * Client island for the Discovery page.
 *
 * Wraps page content in a Suspense boundary required by useEmbedMode()
 * (which calls useSearchParams internally). The server shell renders all
 * static content; this component only handles embed mode detection.
 */
export default function DiscoveryClient({ children }: DiscoveryClientProps) {
  return (
    <Suspense fallback={<div>{children}</div>}>
      <DiscoveryClientInner>{children}</DiscoveryClientInner>
    </Suspense>
  );
}
