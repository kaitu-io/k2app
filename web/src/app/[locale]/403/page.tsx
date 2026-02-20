"use client";

import { useRouter } from "@/i18n/routing";
import { Button } from "@/components/ui/button";
import { ArrowLeft, Home, ShieldX } from "lucide-react";
import { useTranslations } from "next-intl";

export default function ForbiddenPage() {
  const t = useTranslations();
  const router = useRouter();

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 via-white to-slate-100 flex items-center justify-center px-4 sm:px-6 lg:px-8">
      <div className="max-w-2xl w-full">
        {/* Main content */}
        <div className="text-center">
          {/* Icon and error code */}
          <div className="flex justify-center mb-8">
            <div className="relative">
              <div className="w-32 h-32 bg-gradient-to-br from-red-100 to-red-50 rounded-full flex items-center justify-center shadow-lg">
                <ShieldX className="w-16 h-16 text-red-500" />
              </div>
              <div className="absolute -top-2 -right-2 bg-red-500 text-white text-sm font-bold px-3 py-1 rounded-full shadow-md">
                {"403"}
              </div>
            </div>
          </div>

          {/* Title */}
          <h1 className="text-4xl sm:text-5xl font-bold text-gray-900 mb-4">
            {t('purchase.error403.title')}
          </h1>

          {/* Subtitle */}
          <h2 className="text-xl sm:text-2xl font-medium text-gray-700 mb-6">
            {t('purchase.error403.subtitle')}
          </h2>

          {/* Description */}
          <div className="bg-white rounded-xl shadow-sm border border-gray-200 p-6 sm:p-8 mb-8">
            <p className="text-base sm:text-lg text-gray-800 leading-relaxed mb-4">
              {t('purchase.error403.description')}
            </p>
            <p className="text-sm text-gray-600">
              {t('purchase.error403.suggestions.item2')}
            </p>
          </div>

          {/* Action buttons */}
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

          {/* Additional info */}
          <div className="mt-12 pt-6 border-t border-gray-200">
            <p className="text-sm text-gray-500">
              {t('purchase.error403.errorCode')}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}