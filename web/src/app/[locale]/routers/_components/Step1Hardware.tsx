import { getTranslations } from 'next-intl/server';
import { RECOMMENDED_HARDWARE, PurchasePlatform } from '@/lib/router-hardware';
import { StepShell } from './StepShell';
import { HardwareCard } from './HardwareCard';

export async function Step1Hardware({ locale }: { locale: string }) {
  const t = await getTranslations('routers');
  const wt = await getTranslations('routers.wizard.step1');

  const platformLabel = (p: PurchasePlatform) => wt(`platforms.${p}`);

  return (
    <StepShell
      id="step-1"
      number={1}
      stepLabel={t('wizard.step', { n: 1 })}
      title={wt('title')}
      subtitle={wt('subtitle')}
    >
      <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-6">
        {RECOMMENDED_HARDWARE.map((hw) => (
          <HardwareCard
            key={hw.id}
            hardware={hw}
            locale={locale}
            copy={{
              name: wt(`hardware.${hw.id}.name`),
              tagline: wt(`hardware.${hw.id}.tagline`),
              pros: wt.raw(`hardware.${hw.id}.pros`) as string[],
              cons: wt.raw(`hardware.${hw.id}.cons`) as string[],
              fit: wt(`hardware.${hw.id}.fit`),
            }}
            specsLabel={{
              soc: wt('specsLabel.soc'),
              ram: wt('specsLabel.ram'),
              flash: wt('specsLabel.flash'),
              wifi: wt('specsLabel.wifi'),
              ports: wt('specsLabel.ports'),
              throughput: wt('specsLabel.throughput'),
            }}
            tierLabel={wt(`tierBadge.${hw.tier}`)}
            typeLabel={wt(`typeBadge.${hw.type}`)}
            priceLabel={wt('priceLabel')}
            prosLabel={wt('prosLabel')}
            consLabel={wt('consLabel')}
            fitLabel={wt('fitLabel')}
            imageAlt={wt('imageAlt', { model: wt(`hardware.${hw.id}.name`) })}
            formatBuyAt={(platform) => wt('buyAt', { platform })}
            morePlatformsLabel={wt('morePlatforms')}
            platformLabel={platformLabel}
          />
        ))}
      </div>
    </StepShell>
  );
}
