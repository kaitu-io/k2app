"use client";

import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import NextLink from 'next/link';
import Image from 'next/image';
import { useBrand } from '@/hooks/useBrand';
import { useAuth } from '@/contexts/AuthContext';

export default function Footer() {
  const brand = useBrand();
  const t = useTranslations();
  const locale = useLocale();
  const { user } = useAuth();
  const showTaglineZh = Boolean(brand.taglineZh) && locale.startsWith('zh');

  return (
    <footer className="py-12 px-4 sm:px-6 lg:px-8 border-t">
      <div className="max-w-7xl mx-auto">
        <div className="grid md:grid-cols-5 gap-8">
          <div>
            <div className="flex items-center space-x-2 mb-4">
              <Image
                src={brand.logoPath}
                alt={`${brand.displayName} Logo`}
                width={32}
                height={32}
                className="rounded-md"
              />
              <span className="text-xl font-bold text-foreground">{brand.wordmark}</span>
            </div>
            <p className="text-muted-foreground text-sm">
              {t('nav.footer.brandDescription')}
            </p>
          </div>

          <div>
            <h4 className="font-semibold text-foreground mb-4">{t('nav.footer.product.title')}</h4>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li>
                <Link href="/install" className="hover:text-blue-600">
                  {t('nav.footer.product.clientDownload')}
                </Link>
              </li>
              <li>
                <Link href="/routers" className="hover:text-blue-600">
                  {t('nav.footer.product.smartRouter')}
                </Link>
              </li>
              <li>
                <Link href="/retailer/rules" className="hover:text-blue-600">
                  {t('nav.footer.product.retailerProgram')}
                </Link>
              </li>
              <li>
                <Link href="/changelog" className="hover:text-blue-600">
                  {t('changelog.title')}
                </Link>
              </li>
            </ul>
          </div>

          <div>
            <h4 className="font-semibold text-foreground mb-4">{t('nav.footer.developer.title')}</h4>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li>
                <Link href="/k2" className="hover:text-blue-600">
                  {t('nav.footer.developer.k2Docs')}
                </Link>
              </li>
              <li>
                <Link href="/k2/quickstart" className="hover:text-blue-600">
                  {t('nav.footer.developer.selfDeploy')}
                </Link>
              </li>
              <li>
                <Link href="/routers" className="hover:text-blue-600">
                  {t('nav.footer.developer.routerConfig')}
                </Link>
              </li>
              <li>
                <NextLink
                  href="https://github.com/getoverleap"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:text-blue-600"
                >
                  {t('nav.footer.developer.github')}
                </NextLink>
              </li>
              <li>
                <Link href="/releases" className="hover:text-blue-600">
                  {t('nav.footer.developer.changelog')}
                </Link>
              </li>
            </ul>
          </div>

          <div>
            <h4 className="font-semibold text-foreground mb-4">{t('nav.footer.support.title')}</h4>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li>
                <Link href="/guides" className="hover:text-blue-600">
                  {t('nav.footer.support.userGuide')}
                </Link>
              </li>
              <li>
                <Link href="/guides" className="hover:text-blue-600">
                  {t('nav.footer.support.faq')}
                </Link>
              </li>
              <li>
                <Link href="/guides" className="hover:text-blue-600">
                  {t('nav.footer.support.contact')}
                </Link>
              </li>
              <li>
                <Link href="/support" className="hover:text-blue-600">
                  {t('nav.footer.support.homeschoolGuide')}
                </Link>
              </li>
            </ul>
          </div>

          <div>
            <h4 className="font-semibold text-foreground mb-4">{t('nav.footer.legal.title')}</h4>
            <ul className="space-y-2 text-sm text-muted-foreground">
              <li>
                <Link href="/privacy" className="hover:text-blue-600">
                  {t('discovery.privacy.title')}
                </Link>
              </li>
              <li>
                <Link href="/terms" className="hover:text-blue-600">
                  {t('discovery.terms.title')}
                </Link>
              </li>
            </ul>
          </div>
        </div>

        <div className="mt-8 pt-8 border-t text-center text-sm text-muted-foreground">
          {showTaglineZh && (
            <p className="mb-2 text-muted-foreground/60 italic">{brand.taglineZh}</p>
          )}
          <p>{'©'} {new Date().getFullYear()} {brand.legalName}{'. '}{t('nav.footer.copyright')}</p>
          {user?.isAdmin && (
            <NextLink
              href="/manager"
              className="mt-2 inline-block text-xs text-muted-foreground/30 hover:text-muted-foreground transition-colors"
            >
              {t('nav.nav.adminPanel')}
            </NextLink>
          )}
        </div>
      </div>
    </footer>
  );
}
