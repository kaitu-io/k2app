"use client";

import React, { Suspense } from 'react';
import NextLink from 'next/link';
import Image from 'next/image';
import { Badge } from '@/components/ui/badge';
import { useEmbedMode } from '@/hooks/useEmbedMode';

interface EmbedAwareLayoutProps {
  children: React.ReactNode;
  pageTitle?: string;
  pageBadge?: string;
  className?: string;
  heroSection?: React.ReactNode;
  footerSection?: React.ReactNode;
}

/**
 * Internal component that uses hooks requiring Suspense
 */
function EmbedAwareLayoutContent({
  children,
  pageTitle,
  pageBadge,
  className = "",
  heroSection,
  footerSection
}: EmbedAwareLayoutProps) {
  const { isEmbedded, showNavigation, showFooter, compactLayout } = useEmbedMode();

  return (
    <div className={`min-h-screen bg-background ${isEmbedded ? 'embedded-mode' : ''} ${className}`}>
      {/* Navigation - Hidden in embedded mode */}
      {showNavigation && (
        <nav className="border-b bg-card/95 backdrop-blur-sm sticky top-0 z-50">
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
            <div className="flex justify-between items-center h-16">
              <div className="flex items-center space-x-2">
                <Image 
                  src="/kaitu-icon.png" 
                  alt="Kaitu Logo" 
                  width={32}
                  height={32}
                  className="rounded-md"
                />
                <NextLink href="/" className="text-xl font-bold text-foreground hover:text-primary transition-colors">
                  {"Kaitu.io"}
                </NextLink>
              </div>
              <div className="flex items-center space-x-4">
                {pageBadge && (
                  <Badge variant="outline" className="text-secondary border-secondary">
                    {pageBadge}
                  </Badge>
                )}
              </div>
            </div>
          </div>
        </nav>
      )}

      {/* Hero Section - Simplified in embedded mode */}
      {heroSection || (pageTitle && (
        !compactLayout ? (
          <section className="py-16 px-4 sm:px-6 lg:px-8">
            <div className="max-w-7xl mx-auto text-center">
              <h1 className="text-4xl sm:text-5xl font-bold text-foreground mb-4">
                {pageTitle}
              </h1>
            </div>
          </section>
        ) : (
          <section className="py-4 px-4 sm:px-6 lg:px-8">
            <div className="max-w-7xl mx-auto text-center">
              <h1 className="text-2xl font-bold text-foreground mb-2">
                {pageTitle}
              </h1>
            </div>
          </section>
        )
      ))}

      {/* Main Content */}
      {children}

      {/* Footer Section - Hidden in embedded mode */}
      {showFooter && footerSection}
    </div>
  );
}

/**
 * Layout component that adapts to embed mode
 * Hides navigation and footer when embedded
 * Provides compact layout for embedded pages
 * Automatically wraps content in Suspense boundary for useSearchParams compatibility
 */
export function EmbedAwareLayout(props: EmbedAwareLayoutProps) {
  return (
    <Suspense fallback={
      <div className="min-h-screen bg-background flex items-center justify-center">
        <div className="text-center">
          <div className="w-8 h-8 border-4 border-secondary border-t-transparent rounded-full animate-spin mx-auto mb-4"></div>
          <p className="text-muted-foreground">{"Loading..."}</p>
        </div>
      </div>
    }>
      <EmbedAwareLayoutContent {...props} />
    </Suspense>
  );
}