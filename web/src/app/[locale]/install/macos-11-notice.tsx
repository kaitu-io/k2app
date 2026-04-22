"use client";

import { useEffect, useState } from 'react';
import { AlertTriangle } from 'lucide-react';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import { shouldShowMacOS11Notice } from '@/lib/device-detection';

interface Props {
  t: (key: string) => string;
}

/**
 * macOS 11 supportability disclaimer for the download page.
 *
 * Self-contained: runs the version-detection policy from device-detection
 * and renders nothing when the viewer is confirmed on macOS 12+. Default is
 * visible until detection resolves, so macOS 11 users never miss it.
 */
export function MacOS11Notice({ t }: Props) {
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    let cancelled = false;
    void shouldShowMacOS11Notice().then((result) => {
      if (!cancelled) setVisible(result);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  if (!visible) return null;

  return (
    <Alert className="max-w-xl mx-auto mb-6 text-left border-amber-500/40 bg-amber-500/10">
      <AlertTriangle className="text-amber-600 dark:text-amber-400" />
      <AlertTitle>{t('install.install.macos11Notice.title')}</AlertTitle>
      <AlertDescription>{t('install.install.macos11Notice.body')}</AlertDescription>
    </Alert>
  );
}
