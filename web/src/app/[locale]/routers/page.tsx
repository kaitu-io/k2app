"use client";

import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { useTranslations } from 'next-intl';
import { ROUTER_PRODUCTS } from '@/lib/constants';
import Image from 'next/image';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import {
  Router,
  Wifi,
  Home as HomeIcon,
  Clock,
  HeartHandshake,
  DollarSign,
  Smartphone,
  CheckCircle,
  Star,
  Users,
  Mail
} from 'lucide-react';

export default function RoutersPage() {
  const t = useTranslations();

  return (
    <div className="min-h-screen bg-gradient-to-b from-gray-50 to-white dark:from-gray-900 dark:to-gray-800">
      <Header />

      {/* Hero Section */}
      <section className="py-20 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto text-center">
          <div className="mb-8">
            <Router className="w-20 h-20 text-blue-600 mx-auto mb-6" />
            <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-gray-900 dark:text-white mb-6">
              {t('hero.routers.title')}
            </h1>
            <p className="text-xl text-gray-600 dark:text-gray-300 mb-8 max-w-3xl mx-auto">
              {t('hero.routers.subtitle')}
            </p>
          </div>
        </div>
      </section>

      {/* Product Showcase */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto">
          <div className="grid lg:grid-cols-2 gap-12">
            
            {/* K2 Mini Router */}
            <Card className="p-8 relative overflow-hidden">
              <div className="absolute top-6 right-6">
                <span className="bg-orange-100 text-orange-800 text-sm font-medium px-3 py-1 rounded-full dark:bg-orange-900 dark:text-orange-300">
                  {t('hero.routers.presaleTag')}
                </span>
              </div>
              
              <div className="mb-8">
                <Router className="w-16 h-16 text-blue-600 mb-4" />
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">
                  {ROUTER_PRODUCTS.k2Mini.name}
                </h2>
                <p className="text-blue-600 font-medium mb-4">
                  {ROUTER_PRODUCTS.k2Mini.englishName}
                </p>
                <p className="text-gray-600 dark:text-gray-300 text-lg">
                  {ROUTER_PRODUCTS.k2Mini.tagline}
                </p>
              </div>

              {/* Product Images */}
              <div className="grid grid-cols-3 gap-4 mb-8">
                <div className="relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden">
                  <Image
                    src="/images/routers/k2-mini.jpg"
                    alt={"开途 K2 Mini 路由器"}
                    width={200}
                    height={200}
                    className="object-cover w-full h-full"
                  />
                </div>
                <div className="relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden">
                  <Image
                    src="/images/routers/k2-mini.1.jpeg"
                    alt={"开途 K2 Mini 路由器详图"}
                    width={200}
                    height={200}
                    className="object-cover w-full h-full"
                  />
                </div>
                <div className="relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden">
                  <Image
                    src="/images/routers/k2-mini.2.webp"
                    alt={"开途 K2 Mini 路由器包装"}
                    width={200}
                    height={200}
                    className="object-cover w-full h-full"
                  />
                </div>
              </div>

              {/* Features */}
              <div className="mb-8">
                <h3 className="font-semibold mb-4 flex items-center text-gray-900 dark:text-white">
                  <CheckCircle className="w-5 h-5 mr-2 text-green-600" />
                  {t('hero.routers.productFeatures')}
                </h3>
                <ul className="space-y-2">
                  {ROUTER_PRODUCTS.k2Mini.features.map((feature, index) => (
                    <li key={index} className="flex items-center text-gray-600 dark:text-gray-300">
                      <Star className="w-4 h-4 mr-2 text-blue-600 flex-shrink-0" />
                      {feature}
                    </li>
                  ))}
                </ul>
              </div>

              <div className="space-y-3">
                <Button className="w-full" size="lg">
                  <Mail className="w-4 h-4 mr-2" />
                  {t('hero.routers.contactInquiry')}
                </Button>
                <p className="text-center text-sm text-gray-500 dark:text-gray-400">
                  {t('hero.routers.presalePriceConsult')}
                </p>
              </div>
            </Card>

            {/* K2-001 Router */}
            <Card className="p-8 relative overflow-hidden">
              <div className="absolute top-6 right-6">
                <span className="bg-orange-100 text-orange-800 text-sm font-medium px-3 py-1 rounded-full dark:bg-orange-900 dark:text-orange-300">
                  {t('hero.routers.presaleTag')}
                </span>
              </div>
              
              <div className="mb-8">
                <Router className="w-16 h-16 text-green-600 mb-4" />
                <h2 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">
                  {ROUTER_PRODUCTS.k2001.name}
                </h2>
                <p className="text-green-600 font-medium mb-4">
                  {ROUTER_PRODUCTS.k2001.englishName}
                </p>
                <p className="text-gray-600 dark:text-gray-300 text-lg">
                  {ROUTER_PRODUCTS.k2001.tagline}
                </p>
              </div>

              {/* Product Images */}
              <div className="grid grid-cols-3 gap-4 mb-8">
                <div className="relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden">
                  <Image
                    src="/images/routers/1.1.jpg"
                    alt={"开途 K2-001 路由器"}
                    width={200}
                    height={200}
                    className="object-cover w-full h-full"
                  />
                </div>
                <div className="relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden">
                  <Image
                    src="/images/routers/1.2.jpg"
                    alt={"开途 K2-001 路由器接口"}
                    width={200}
                    height={200}
                    className="object-cover w-full h-full"
                  />
                </div>
                <div className="relative aspect-square bg-gray-100 dark:bg-gray-800 rounded-lg overflow-hidden">
                  <Image
                    src="/images/routers/1.3.jpg"
                    alt={"开途 K2-001 路由器配置"}
                    width={200}
                    height={200}
                    className="object-cover w-full h-full"
                  />
                </div>
              </div>

              {/* Features */}
              <div className="mb-8">
                <h3 className="font-semibold mb-4 flex items-center text-gray-900 dark:text-white">
                  <CheckCircle className="w-5 h-5 mr-2 text-green-600" />
                  {t('hero.routers.productFeatures')}
                </h3>
                <ul className="space-y-2">
                  {ROUTER_PRODUCTS.k2001.features.map((feature, index) => (
                    <li key={index} className="flex items-center text-gray-600 dark:text-gray-300">
                      <Star className="w-4 h-4 mr-2 text-green-600 flex-shrink-0" />
                      {feature}
                    </li>
                  ))}
                </ul>
              </div>

              <div className="space-y-3">
                <Button className="w-full" size="lg" variant="secondary">
                  <Mail className="w-4 h-4 mr-2" />
                  {t('hero.routers.contactInquiry')}
                </Button>
                <p className="text-center text-sm text-gray-500 dark:text-gray-400">
                  {t('hero.routers.presalePriceConsultFull')}
                </p>
              </div>
            </Card>
          </div>
        </div>
      </section>

      {/* Router Benefits */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 bg-gray-50 dark:bg-gray-800/50">
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-4">
              {t('hero.routers.benefits.title')}
            </h2>
            <p className="text-gray-600 dark:text-gray-300 text-lg max-w-2xl mx-auto">
              {t('hero.routers.benefits.subtitle')}
            </p>
          </div>
          
          <div className="grid md:grid-cols-2 lg:grid-cols-3 gap-8">
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <Wifi className="w-12 h-12 text-blue-600 mb-4" />
              <h3 className="text-xl font-semibold mb-3">
                {t('hero.routers.benefits.items.easySetup.title')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('hero.routers.benefits.items.easySetup.description')}
              </p>
            </Card>
            
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <HomeIcon className="w-12 h-12 text-green-600 mb-4" />
              <h3 className="text-xl font-semibold mb-3">
                {t('hero.routers.benefits.items.familyFriendly.title')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('hero.routers.benefits.items.familyFriendly.description')}
              </p>
            </Card>
            
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <Clock className="w-12 h-12 text-purple-600 mb-4" />
              <h3 className="text-xl font-semibold mb-3">
                {t('hero.routers.benefits.items.alwaysOn.title')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('hero.routers.benefits.items.alwaysOn.description')}
              </p>
            </Card>
            
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <Smartphone className="w-12 h-12 text-orange-600 mb-4" />
              <h3 className="text-xl font-semibold mb-3">
                {t('hero.routers.benefits.items.multiDevice.title')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('hero.routers.benefits.items.multiDevice.description')}
              </p>
            </Card>
            
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <HeartHandshake className="w-12 h-12 text-red-600 mb-4" />
              <h3 className="text-xl font-semibold mb-3">
                {t('hero.routers.benefits.items.techSupport.title')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('hero.routers.benefits.items.techSupport.description')}
              </p>
            </Card>
            
            <Card className="p-6 hover:shadow-lg transition-shadow">
              <DollarSign className="w-12 h-12 text-indigo-600 mb-4" />
              <h3 className="text-xl font-semibold mb-3">
                {t('hero.routers.benefits.items.costEffective.title')}
              </h3>
              <p className="text-gray-600 dark:text-gray-300">
                {t('hero.routers.benefits.items.costEffective.description')}
              </p>
            </Card>
          </div>
        </div>
      </section>

      {/* Technical Comparison */}
      <section className="py-16 px-4 sm:px-6 lg:px-8">
        <div className="max-w-7xl mx-auto">
          <div className="text-center mb-16">
            <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-4">
              {t('hero.routers.routerVsClient')}
            </h2>
            <p className="text-gray-600 dark:text-gray-300 text-lg">
              {t('hero.routers.whyChooseRouter')}
            </p>
          </div>

          <div className="grid lg:grid-cols-2 gap-8">
            <Card className="p-8 border-2 border-green-500 bg-green-50 dark:bg-green-900/20">
              <div className="flex items-center mb-6">
                <Router className="w-10 h-10 text-green-600 mr-3" />
                <div>
                  <h3 className="text-xl font-bold text-green-800 dark:text-green-300">{t('hero.routers.smartRouter')}</h3>
                  <p className="text-green-600 dark:text-green-400">{t('hero.routers.recommended')}</p>
                </div>
              </div>
              <ul className="space-y-3 text-gray-700 dark:text-gray-300">
                <li className="flex items-start">
                  <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                  <span>{t('hero.routers.autoConnect')}</span>
                </li>
                <li className="flex items-start">
                  <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                  <span>{t('hero.routers.alwaysRunning')}</span>
                </li>
                <li className="flex items-start">
                  <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                  <span>{t('hero.routers.allDevices')}</span>
                </li>
                <li className="flex items-start">
                  <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                  <span>{t('hero.routers.easyUse')}</span>
                </li>
                <li className="flex items-start">
                  <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                  <span>{t('hero.routers.onePurchase')}</span>
                </li>
              </ul>
            </Card>

            <Card className="p-8">
              <div className="flex items-center mb-6">
                <Smartphone className="w-10 h-10 text-gray-600 mr-3" />
                <div>
                  <h3 className="text-xl font-bold text-gray-800 dark:text-gray-300">{t('hero.routers.clientSoftware')}</h3>
                  <p className="text-gray-600 dark:text-gray-400">{t('hero.routers.traditional')}</p>
                </div>
              </div>
              <ul className="space-y-3 text-gray-600 dark:text-gray-400">
                <li className="flex items-start">
                  <span className="w-5 h-5 text-gray-400 mr-2 mt-0.5 flex-shrink-0">{t('hero.routers.bullet')}</span>
                  <span>{t('hero.routers.individualSetup')}</span>
                </li>
                <li className="flex items-start">
                  <span className="w-5 h-5 text-gray-400 mr-2 mt-0.5 flex-shrink-0">{t('hero.routers.bullet')}</span>
                  <span>{t('hero.routers.manualStart')}</span>
                </li>
                <li className="flex items-start">
                  <span className="w-5 h-5 text-gray-400 mr-2 mt-0.5 flex-shrink-0">{t('hero.routers.bullet')}</span>
                  <span>{t('hero.routers.deviceLimits')}</span>
                </li>
                <li className="flex items-start">
                  <span className="w-5 h-5 text-gray-400 mr-2 mt-0.5 flex-shrink-0">{t('hero.routers.bullet')}</span>
                  <span>{t('hero.routers.complexConfig')}</span>
                </li>
                <li className="flex items-start">
                  <span className="w-5 h-5 text-gray-400 mr-2 mt-0.5 flex-shrink-0">{t('hero.routers.bullet')}</span>
                  <span>{t('hero.routers.ongoing')}</span>
                </li>
              </ul>
            </Card>
          </div>
        </div>
      </section>

      {/* Contact Section */}
      <section className="py-16 px-4 sm:px-6 lg:px-8 bg-blue-50 dark:bg-blue-900/20">
        <div className="max-w-4xl mx-auto text-center">
          <h2 className="text-3xl font-bold text-gray-900 dark:text-white mb-6">
            {t('hero.routers.contactUsMore')}
          </h2>
          <p className="text-gray-600 dark:text-gray-300 text-lg mb-8">
            {t('hero.routers.teamDescription')}
          </p>
          
          <div className="grid sm:grid-cols-2 gap-6 max-w-2xl mx-auto">
            <Card className="p-6">
              <Mail className="w-8 h-8 text-blue-600 mx-auto mb-3" />
              <h3 className="font-semibold mb-2">{t('hero.routers.emailConsult')}</h3>
              <p className="text-gray-600 dark:text-gray-300 text-sm mb-3">
                {t('hero.routers.productDetailsTech')}
              </p>
              <Button variant="outline" className="w-full" asChild>
                <a href="mailto:contact@kaitu.io">
                  {"contact@kaitu.io"}
                </a>
              </Button>
            </Card>
            
            <Card className="p-6">
              <Users className="w-8 h-8 text-green-600 mx-auto mb-3" />
              <h3 className="font-semibold mb-2">{t('hero.routers.onlineService')}</h3>
              <p className="text-gray-600 dark:text-gray-300 text-sm mb-3">
                {t('hero.routers.realTimeResponse')}
              </p>
              <Button variant="outline" className="w-full">
                {t('hero.routers.startOnlineService')}
              </Button>
            </Card>
          </div>
          
          <div className="mt-8 p-4 bg-white dark:bg-gray-800 rounded-lg">
            <p className="text-sm text-gray-500 dark:text-gray-400">
              {t('hero.routers.warranty')}
            </p>
          </div>
        </div>
      </section>

      <Footer />
    </div>
  );
}