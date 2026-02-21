"use client";

import { useRouter } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { ArrowLeft, Home } from 'lucide-react';
import { useTranslations } from 'next-intl';

/**
 * ErrorActions — client component for the 403 page interactive buttons.
 *
 * Extracted from the 403 page Server Component because useRouter()
 * and onClick handlers require client-side rendering.
 */
export default function ErrorActions() {
  const t = useTranslations();
  const router = useRouter();

  return (
    <div className="flex flex-col sm:flex-row gap-4 justify-center items-center">
      <Button
        variant="outline"
        size="lg"
        onClick={() => router.back()}
        className="w-full sm:w-auto min-w-[140px] h-12 text-base font-medium border-gray-300 hover:bg-gray-50 hover:border-gray-400 transition-colors"
      >
        <ArrowLeft className="w-5 h-5 mr-2" />
        {t('purchase.error403.actions.goBack')}
      </Button>
      <Button
        size="lg"
        onClick={() => router.push('/')}
        className="w-full sm:w-auto min-w-[140px] h-12 text-base font-medium bg-gray-900 hover:bg-gray-800 text-white transition-colors"
      >
        <Home className="w-5 h-5 mr-2" />
        {t('purchase.error403.actions.goToDashboard')}
      </Button>
    </div>
  );
}
