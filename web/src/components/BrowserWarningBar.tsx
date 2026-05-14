'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { AlertTriangle } from 'lucide-react';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import { detectBrowser } from '@/lib/browser-detection';

interface BrowserWarningBarProps {
  brandDomain: string;
}

type Reason = 'inAppWebView' | 'outdatedIos' | null;

export default function BrowserWarningBar({ brandDomain }: BrowserWarningBarProps) {
  const t = useTranslations();
  const { isEmbedded } = useEmbedMode();
  const [reason, setReason] = useState<Reason>(null);

  useEffect(() => {
    if (isEmbedded) {
      setReason(null);
      return;
    }
    const info = detectBrowser(window.navigator.userAgent);
    // Outdated iOS takes precedence — even mainstream iOS Safari can be too old
    // to parse our chunks, and upgrading the browser won't help (system WebKit).
    if (info.isOutdatedIOS) setReason('outdatedIos');
    else if (!info.isMainstream) setReason('inAppWebView');
    else setReason(null);
  }, [isEmbedded]);

  if (!reason) return null;

  const message =
    reason === 'outdatedIos'
      ? t('common.browserWarning.outdatedIos')
      : t('common.browserWarning.message', { domain: brandDomain });

  return (
    <div
      role="alert"
      className="bg-yellow-50 border-b border-yellow-300 text-yellow-900 dark:bg-yellow-900/20 dark:border-yellow-800 dark:text-yellow-200 px-4 py-2.5 text-sm leading-relaxed"
    >
      <div className="mx-auto max-w-7xl flex items-start gap-2">
        <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <span>{message}</span>
      </div>
    </div>
  );
}
