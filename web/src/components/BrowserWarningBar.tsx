'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { AlertTriangle } from 'lucide-react';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import { detectBrowser } from '@/lib/browser-detection';

interface BrowserWarningBarProps {
  brandDomain: string;
}

export default function BrowserWarningBar({ brandDomain }: BrowserWarningBarProps) {
  const t = useTranslations();
  const { isEmbedded } = useEmbedMode();
  const [show, setShow] = useState(false);

  useEffect(() => {
    if (isEmbedded) {
      setShow(false);
      return;
    }
    const info = detectBrowser(window.navigator.userAgent);
    setShow(!info.isMainstream);
  }, [isEmbedded]);

  if (!show) return null;

  return (
    <div
      role="alert"
      className="bg-yellow-50 border-b border-yellow-300 text-yellow-900 dark:bg-yellow-900/20 dark:border-yellow-800 dark:text-yellow-200 px-4 py-2.5 text-sm leading-relaxed"
    >
      <div className="mx-auto max-w-7xl flex items-start gap-2">
        <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <span>{t('common.browserWarning.message', { domain: brandDomain })}</span>
      </div>
    </div>
  );
}
