"use client";

import { useState, useEffect, useRef } from "react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { Cookie, X } from "lucide-react";
import { safeStorage } from "@/lib/safeStorage";
import { useBrand } from "@/hooks/useBrand";
import { COOKIE_BANNER_OFFSET_VAR } from "@/lib/cookie-banner";

export const COOKIE_CONSENT_KEY = "kaitu_cookie_consent";
// Increment when the cookie policy or the banner copy changes, so visitors who
// answered an earlier version see the new text once.
// 2: the site added a first-party visit / purchase-conversion statistics cookie.
export const COOKIE_CONSENT_VERSION = "2";

/**
 * The statistics cookie is HttpOnly, so only the server can replace it with
 * the opt-out value. `redirect: 'manual'` keeps the browser from following the
 * endpoint's redirect to a page nobody will read. Sent at click time rather
 * than after the close animation so it is on the wire before the banner
 * unmounts; a failure is swallowed — declining must never look broken.
 */
function optOutOfStatistics() {
  try {
    fetch('/api/px/optout', { credentials: 'same-origin', redirect: 'manual' }).catch(() => {});
  } catch {
    /* noop */
  }
}

export default function CookieConsent() {
  const t = useTranslations();
  // The layout loads a third-party analytics script only for a brand whose
  // registry entry has a measurement id; the banner discloses it for that brand.
  const hasThirdPartyAnalytics = Boolean(useBrand().gaMeasurementId);
  const [show, setShow] = useState(false);
  const [isClient, setIsClient] = useState(false);
  const [isVisible, setIsVisible] = useState(false);
  const bannerRef = useRef<HTMLDivElement>(null);

  // While the banner is on screen, publish how much of the bottom edge it
  // covers (its height + the gap below it) so other bottom-corner overlays —
  // the chat launcher — can sit above it instead of underneath. See
  // lib/cookie-banner.ts. Uses layout height + CSS `bottom`, not the bounding
  // rect: the entrance animation translates the box and would skew the rect.
  useEffect(() => {
    if (!show) return;
    const el = bannerRef.current;
    if (!el) return;
    const root = document.documentElement;
    const publish = () => {
      const gap = parseFloat(window.getComputedStyle(el).bottom) || 0;
      root.style.setProperty(COOKIE_BANNER_OFFSET_VAR, `${Math.round(el.offsetHeight + gap)}px`);
    };
    publish();
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(publish);
    observer?.observe(el);
    window.addEventListener('resize', publish);
    return () => {
      observer?.disconnect();
      window.removeEventListener('resize', publish);
      root.style.removeProperty(COOKIE_BANNER_OFFSET_VAR);
    };
  }, [show]);

  useEffect(() => {
    setIsClient(true);

    // Skip in embed mode
    const params = new URLSearchParams(window.location.search);
    if (params.get('embed') === 'true') {
      return;
    }

    // Check if user has already consented
    const consent = safeStorage.get(COOKIE_CONSENT_KEY);
    if (!consent || consent !== COOKIE_CONSENT_VERSION) {
      // Show banner after a short delay for better UX
      const timer = setTimeout(() => {
        setShow(true);
        // Trigger animation after render
        requestAnimationFrame(() => {
          setIsVisible(true);
        });
      }, 1500);
      return () => clearTimeout(timer);
    }
  }, []);

  const handleClose = (accepted: boolean) => {
    if (!accepted) optOutOfStatistics();
    setIsVisible(false);
    // Wait for animation to complete before hiding
    setTimeout(() => {
      safeStorage.set(COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION);
      if (!accepted) {
        // Clear invite code cookie if declined
        document.cookie = "kaitu_invite_code=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/;";
      }
      setShow(false);
    }, 300);
  };

  // Don't render on server or if already consented
  if (!isClient || !show) {
    return null;
  }

  return (
    <div
      ref={bannerRef}
      className={`fixed bottom-4 left-4 right-4 sm:left-auto sm:right-6 sm:bottom-6 z-[9999] max-w-md transition-all duration-300 ease-out ${
        isVisible
          ? 'opacity-100 translate-y-0'
          : 'opacity-0 translate-y-4'
      }`}
    >
      <div className="bg-card rounded-xl shadow-2xl border border-border overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 bg-muted border-b border-border">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 bg-muted rounded-full flex items-center justify-center">
              <Cookie className="w-4 h-4 text-secondary" />
            </div>
            <h3 className="text-sm font-semibold text-foreground">
              {t('discovery.cookieConsent.title')}
            </h3>
          </div>
          <button
            onClick={() => handleClose(false)}
            className="text-muted-foreground hover:text-foreground transition-colors p-1 rounded-full hover:bg-muted"
            aria-label="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content */}
        <div className="px-4 py-3">
          <p className="text-xs text-muted-foreground leading-relaxed">
            {t('discovery.cookieConsent.description')}
          </p>
          <p className="text-[10px] text-muted-foreground mt-2 leading-relaxed">
            {t('discovery.cookieConsent.details')}
          </p>
          {hasThirdPartyAnalytics && (
            <p className="text-[10px] text-muted-foreground mt-2 leading-relaxed">
              {t('discovery.cookieConsent.detailsThirdParty')}
            </p>
          )}
        </div>

        {/* Actions */}
        <div className="flex gap-2 px-4 py-3 bg-muted border-t border-border">
          <Button
            onClick={() => handleClose(true)}
            className="flex-1 bg-primary hover:bg-primary/90 text-primary-foreground text-xs font-medium h-8"
            size="sm"
          >
            {t('discovery.cookieConsent.accept')}
          </Button>
          <Button
            onClick={() => handleClose(false)}
            variant="outline"
            size="sm"
            className="flex-1 border-border text-xs h-8"
          >
            {t('discovery.cookieConsent.decline')}
          </Button>
        </div>
      </div>
    </div>
  );
}
