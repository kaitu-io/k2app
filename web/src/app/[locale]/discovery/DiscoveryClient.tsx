"use client";

import { Suspense } from 'react';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import Header from '@/components/Header';
import Footer from '@/components/Footer';

interface DiscoveryClientProps {
  children: React.ReactNode;
}

function DiscoveryClientInner({ children }: DiscoveryClientProps) {
  const { isEmbedded, showNavigation, showFooter } = useEmbedMode();

  return (
    <div className={`min-h-screen bg-background ${isEmbedded ? 'embedded-mode' : ''}`}>
      {showNavigation && <Header />}
      {children}
      {showFooter && <Footer />}
    </div>
  );
}

export default function DiscoveryClient({ children }: DiscoveryClientProps) {
  return (
    <Suspense fallback={<div className="min-h-screen bg-background"><Header />{children}<Footer /></div>}>
      <DiscoveryClientInner>{children}</DiscoveryClientInner>
    </Suspense>
  );
}
