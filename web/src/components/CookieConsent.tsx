"use client";

import { useState, useEffect } from "react";
import { useTranslations } from "next-intl";
import { Button } from "@/components/ui/button";
import { Cookie, X } from "lucide-react";

const COOKIE_CONSENT_KEY = "kaitu_cookie_consent";
const COOKIE_CONSENT_VERSION = "1"; // Increment when cookie policy changes

export default function CookieConsent() {
  const t = useTranslations();
  const [show, setShow] = useState(false);
  const [isClient, setIsClient] = useState(false);
  const [isVisible, setIsVisible] = useState(false);

  useEffect(() => {
    setIsClient(true);

    // Check if user has already consented
    const consent = localStorage.getItem(COOKIE_CONSENT_KEY);
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
    setIsVisible(false);
    // Wait for animation to complete before hiding
    setTimeout(() => {
      localStorage.setItem(COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION);
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
      className={`fixed bottom-4 left-4 right-4 sm:left-auto sm:right-6 sm:bottom-6 z-[9999] max-w-md transition-all duration-300 ease-out ${
        isVisible
          ? 'opacity-100 translate-y-0'
          : 'opacity-0 translate-y-4'
      }`}
    >
      <div className="bg-white dark:bg-gray-900 rounded-xl shadow-2xl border border-gray-200 dark:border-gray-700 overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 bg-gradient-to-r from-blue-50 to-purple-50 dark:from-blue-900/30 dark:to-purple-900/30 border-b border-gray-100 dark:border-gray-800">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 bg-blue-100 dark:bg-blue-900/50 rounded-full flex items-center justify-center">
              <Cookie className="w-4 h-4 text-blue-600 dark:text-blue-400" />
            </div>
            <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
              {t('discovery.cookieConsent.title')}
            </h3>
          </div>
          <button
            onClick={() => handleClose(false)}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 transition-colors p-1 rounded-full hover:bg-gray-100 dark:hover:bg-gray-800"
            aria-label="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content */}
        <div className="px-4 py-3">
          <p className="text-xs text-gray-600 dark:text-gray-300 leading-relaxed">
            {t('discovery.cookieConsent.description')}
          </p>
          <p className="text-[10px] text-gray-400 dark:text-gray-500 mt-2 leading-relaxed">
            {t('discovery.cookieConsent.details')}
          </p>
        </div>

        {/* Actions */}
        <div className="flex gap-2 px-4 py-3 bg-gray-50 dark:bg-gray-800/50 border-t border-gray-100 dark:border-gray-800">
          <Button
            onClick={() => handleClose(true)}
            className="flex-1 bg-blue-600 hover:bg-blue-700 text-white text-xs font-medium h-8"
            size="sm"
          >
            {t('discovery.cookieConsent.accept')}
          </Button>
          <Button
            onClick={() => handleClose(false)}
            variant="outline"
            size="sm"
            className="flex-1 border-gray-300 dark:border-gray-600 text-xs h-8"
          >
            {t('discovery.cookieConsent.decline')}
          </Button>
        </div>
      </div>
    </div>
  );
}
