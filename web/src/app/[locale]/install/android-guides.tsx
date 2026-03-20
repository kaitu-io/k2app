/* eslint-disable react/jsx-no-literals */
"use client";

import { useState, useEffect } from 'react';
import Image from 'next/image';
import { Tabs, TabsContent } from '@/components/ui/tabs';
import { DesktopUsbInstallGuide } from './install-guides';
import { BRAND_GUIDES, detectDefaultTab, type GuideStep } from './android-guides-data';

// ---------------------------------------------------------------------------
// BrandTabBar — horizontal scroll on mobile, custom styled buttons
// ---------------------------------------------------------------------------

function BrandTabBar({
  selected,
  onSelect,
  t,
}: {
  selected: string;
  onSelect: (id: string) => void;
  t: (key: string) => string;
}) {
  return (
    <div className="flex overflow-x-auto gap-2 mb-6 pb-1 scrollbar-none">
      {BRAND_GUIDES.map((guide) => (
        <button
          key={guide.id}
          onClick={() => onSelect(guide.id)}
          className={`flex-shrink-0 px-4 py-2 rounded-lg border text-sm font-medium transition-all ${
            selected === guide.id
              ? 'border-primary bg-primary/10 text-foreground shadow-sm'
              : 'border-transparent hover:bg-muted/50 text-muted-foreground'
          }`}
        >
          {t(guide.labelKey)}
        </button>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// StepImage — Next.js Image with error fallback to numbered placeholder
// ---------------------------------------------------------------------------

function StepImage({ src, stepNumber }: { src: string; stepNumber: number }) {
  const [hasError, setHasError] = useState(false);

  if (hasError) {
    return (
      <div className="bg-muted rounded-lg flex items-center justify-center w-full aspect-[9/16] max-w-[160px]">
        <span className="text-muted-foreground text-2xl font-bold">{stepNumber}</span>
      </div>
    );
  }

  return (
    <div className="relative w-full max-w-[160px] aspect-[9/16] rounded-lg overflow-hidden flex-shrink-0">
      <Image
        src={src}
        alt={`Step ${stepNumber}`}
        fill
        className="object-cover"
        onError={() => setHasError(true)}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// GuideStepList — renders steps vertically
// ---------------------------------------------------------------------------

function GuideStepList({
  steps,
  t,
}: {
  steps: GuideStep[];
  t: (key: string) => string;
}) {
  return (
    <div className="space-y-6">
      {steps.map((step, index) => {
        const stepNumber = index + 1;

        if (step.image) {
          // Desktop: side-by-side (flex-row); mobile: stacked (flex-col)
          return (
            <div key={step.titleKey} className="flex flex-col sm:flex-row gap-4 items-start">
              <StepImage src={step.image} stepNumber={stepNumber} />
              <div className="flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <span className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-primary/20 text-primary text-xs font-bold flex-shrink-0">
                    {stepNumber}
                  </span>
                  <p className="font-semibold text-foreground text-sm">{t(step.titleKey)}</p>
                </div>
                <p className="text-sm text-muted-foreground leading-relaxed pl-8">{t(step.descriptionKey)}</p>
              </div>
            </div>
          );
        }

        // No image — just number + title + description
        return (
          <div key={step.titleKey} className="flex gap-3 items-start">
            <span className="inline-flex items-center justify-center w-7 h-7 rounded-full bg-primary/20 text-primary text-sm font-bold flex-shrink-0 mt-0.5">
              {stepNumber}
            </span>
            <div>
              <p className="font-semibold text-foreground text-sm mb-1">{t(step.titleKey)}</p>
              <p className="text-sm text-muted-foreground leading-relaxed">{t(step.descriptionKey)}</p>
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ---------------------------------------------------------------------------
// AndroidGuides — main export
// ---------------------------------------------------------------------------

export function AndroidGuides({ t }: { t: (key: string) => string }) {
  const [selectedBrand, setSelectedBrand] = useState("desktopUsb");

  useEffect(() => {
    setSelectedBrand(detectDefaultTab(navigator.userAgent));
  }, []);

  return (
    <div className="mt-8 max-w-2xl mx-auto text-left">
      <p className="text-sm font-semibold text-muted-foreground mb-3 text-center">
        {t('install.androidGuides.sectionTitle')}
      </p>

      <BrandTabBar selected={selectedBrand} onSelect={setSelectedBrand} t={t} />

      <Tabs value={selectedBrand} onValueChange={setSelectedBrand}>
        {BRAND_GUIDES.map((guide) => (
          <TabsContent key={guide.id} value={guide.id}>
            {guide.id === 'desktopUsb' ? (
              <DesktopUsbInstallGuide />
            ) : (
              <GuideStepList steps={guide.steps} t={t} />
            )}
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}
