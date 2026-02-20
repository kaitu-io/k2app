"use client";

import { useTranslations } from 'next-intl';
import { COMPANY_INFO } from '@/lib/constants';
import { Link } from '@/i18n/routing';
import Image from 'next/image';

export default function Footer() {
  const t = useTranslations();

  return (
    <footer className="py-12 px-4 sm:px-6 lg:px-8 border-t">
      <div className="max-w-7xl mx-auto">
        <div className="grid md:grid-cols-4 gap-8">
          <div>
            <div className="flex items-center space-x-2 mb-4">
              <Image 
                src="/kaitu-icon.png" 
                alt="Kaitu Logo" 
                width={32}
                height={32}
                className="rounded-md"
              />
              <span className="text-xl font-bold text-gray-900 dark:text-white">{t('nav.footer.brandName')}</span>
            </div>
            <p className="text-gray-600 dark:text-gray-300 text-sm">
              {t('nav.footer.brandDescription')}
            </p>
          </div>
          
          <div>
            <h4 className="font-semibold text-gray-900 dark:text-white mb-4">{t('nav.footer.product.title')}</h4>
            <ul className="space-y-2 text-sm text-gray-600 dark:text-gray-300">
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
              <li><a href="#" className="hover:text-blue-600">{t('nav.footer.product.nodeStatus')}</a></li>
            </ul>
          </div>

          <div>
            <h4 className="font-semibold text-gray-900 dark:text-white mb-4">{t('nav.footer.support.title')}</h4>
            <ul className="space-y-2 text-sm text-gray-600 dark:text-gray-300">
              <li><a href="#" className="hover:text-blue-600">{t('nav.footer.support.userGuide')}</a></li>
              <li><a href="#" className="hover:text-blue-600">{t('nav.footer.support.faq')}</a></li>
              <li><a href="#" className="hover:text-blue-600">{t('nav.footer.support.contact')}</a></li>
            </ul>
          </div>

          <div>
            <h4 className="font-semibold text-gray-900 dark:text-white mb-4">{t('nav.footer.legal.title')}</h4>
            <ul className="space-y-2 text-sm text-gray-600 dark:text-gray-300">
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
        
        <div className="mt-8 pt-8 border-t text-center text-sm text-gray-600 dark:text-gray-300">
          <p>{"©"} {COMPANY_INFO.year} {"Kaitu LLC"}{". "}{t('nav.footer.copyright')}</p>
        </div>
      </div>
    </footer>
  );
}