import Image from 'next/image';
import { getTranslations } from 'next-intl/server';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { ROUTER_PRODUCTS } from '@/lib/constants';
import { Router, CheckCircle, Star, Mail } from 'lucide-react';

interface ProductImage {
  src: string;
  alt: string;
}

const PRODUCT_IMAGES: Record<keyof typeof ROUTER_PRODUCTS, ProductImage[]> = {
  k2Mini: [
    { src: '/images/routers/k2-mini.jpg', alt: '开途 K2 Mini 路由器' },
    { src: '/images/routers/k2-mini.1.jpeg', alt: '开途 K2 Mini 路由器详图' },
    { src: '/images/routers/k2-mini.2.webp', alt: '开途 K2 Mini 路由器包装' },
  ],
  k2001: [
    { src: '/images/routers/1.1.jpg', alt: '开途 K2-001 路由器' },
    { src: '/images/routers/1.2.jpg', alt: '开途 K2-001 路由器接口' },
    { src: '/images/routers/1.3.jpg', alt: '开途 K2-001 路由器配置' },
  ],
};

export async function PresaleFooterCards() {
  const t = await getTranslations('routers.presale');

  return (
    <section className="py-16 px-4 sm:px-6 lg:px-8 bg-muted">
      <div className="max-w-7xl mx-auto">
        <div className="text-center mb-10">
          <h2 className="text-3xl font-bold text-foreground mb-3">{t('title')}</h2>
          <p className="text-muted-foreground text-lg max-w-2xl mx-auto">{t('subtitle')}</p>
        </div>

        <div className="grid lg:grid-cols-2 gap-8">
          <PresaleCard
            productKey="k2Mini"
            name={ROUTER_PRODUCTS.k2Mini.name}
            englishName={ROUTER_PRODUCTS.k2Mini.englishName}
            tagline={ROUTER_PRODUCTS.k2Mini.tagline}
            features={[...ROUTER_PRODUCTS.k2Mini.features]}
            iconColor="text-blue-600"
            featureIconColor="text-blue-600"
            buttonVariant="default"
            priceLine={t('presalePriceConsult')}
            presaleTag={t('presaleTag')}
            featuresLabel={t('productFeatures')}
            ctaLabel={t('contactInquiry')}
          />
          <PresaleCard
            productKey="k2001"
            name={ROUTER_PRODUCTS.k2001.name}
            englishName={ROUTER_PRODUCTS.k2001.englishName}
            tagline={ROUTER_PRODUCTS.k2001.tagline}
            features={[...ROUTER_PRODUCTS.k2001.features]}
            iconColor="text-green-600"
            featureIconColor="text-green-600"
            buttonVariant="secondary"
            priceLine={t('presalePriceConsultFull')}
            presaleTag={t('presaleTag')}
            featuresLabel={t('productFeatures')}
            ctaLabel={t('contactInquiry')}
          />
        </div>
      </div>
    </section>
  );
}

function PresaleCard({
  productKey,
  name,
  englishName,
  tagline,
  features,
  iconColor,
  featureIconColor,
  buttonVariant,
  priceLine,
  presaleTag,
  featuresLabel,
  ctaLabel,
}: {
  productKey: keyof typeof ROUTER_PRODUCTS;
  name: string;
  englishName: string;
  tagline: string;
  features: string[];
  iconColor: string;
  featureIconColor: string;
  buttonVariant: 'default' | 'secondary';
  priceLine: string;
  presaleTag: string;
  featuresLabel: string;
  ctaLabel: string;
}) {
  const images = PRODUCT_IMAGES[productKey];
  return (
    <Card className="p-7 relative overflow-hidden">
      <div className="absolute top-5 right-5">
        <span className="bg-orange-100 text-orange-800 text-xs font-medium px-2.5 py-0.5 rounded-full dark:bg-orange-900/40 dark:text-orange-300">
          {presaleTag}
        </span>
      </div>

      <div className="mb-5">
        <Router className={`w-12 h-12 mb-3 ${iconColor}`} />
        <h3 className="text-xl font-bold text-foreground mb-1">{name}</h3>
        <p className={`text-sm font-medium mb-2 ${iconColor}`}>{englishName}</p>
        <p className="text-muted-foreground">{tagline}</p>
      </div>

      <div className="grid grid-cols-3 gap-3 mb-5">
        {images.map((img) => (
          <div key={img.src} className="relative aspect-square bg-muted rounded-lg overflow-hidden">
            <Image
              src={img.src}
              alt={img.alt}
              fill
              sizes="(max-width: 640px) 33vw, 200px"
              className="object-cover"
            />
          </div>
        ))}
      </div>

      <div className="mb-5">
        <h4 className="font-semibold mb-2 flex items-center text-sm text-foreground">
          <CheckCircle className="w-4 h-4 mr-2 text-green-600" />
          {featuresLabel}
        </h4>
        <ul className="space-y-1.5 text-sm">
          {features.map((feature, i) => (
            <li key={i} className="flex items-center text-muted-foreground">
              <Star className={`w-3.5 h-3.5 mr-2 flex-shrink-0 ${featureIconColor}`} />
              {feature}
            </li>
          ))}
        </ul>
      </div>

      <div className="space-y-2">
        <Button className="w-full" variant={buttonVariant}>
          <Mail className="w-4 h-4 mr-2" />
          {ctaLabel}
        </Button>
        <p className="text-center text-xs text-muted-foreground">{priceLine}</p>
      </div>
    </Card>
  );
}
