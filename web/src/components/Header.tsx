'use client'

import { useState, useRef, useEffect } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { useTranslations } from 'next-intl'
import { Link } from '@/i18n/routing'
import LanguageSwitcher from '@/components/LanguageSwitcher'
import { Download, Menu, X, ChevronDown } from 'lucide-react'
import Image from 'next/image'
import { useBrand } from '@/hooks/useBrand'

type DropdownId = 'why'
type MobileSection = 'why'

export default function Header() {
  const brand = useBrand()
  const { isAuthenticated } = useAuth()
  const t = useTranslations()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [openDropdown, setOpenDropdown] = useState<DropdownId | null>(null)
  const [mobileExpanded, setMobileExpanded] = useState<Set<MobileSection>>(new Set())
  const navRef = useRef<HTMLElement>(null)

  useEffect(() => {
    function handleOutsideClick(e: MouseEvent) {
      if (navRef.current && !navRef.current.contains(e.target as Node)) {
        setOpenDropdown(null)
      }
    }
    document.addEventListener('mousedown', handleOutsideClick)
    return () => document.removeEventListener('mousedown', handleOutsideClick)
  }, [])

  function toggleDropdown(id: DropdownId) {
    setOpenDropdown(prev => (prev === id ? null : id))
  }

  function toggleMobileSection(section: MobileSection) {
    setMobileExpanded(prev => {
      const next = new Set(prev)
      if (next.has(section)) next.delete(section)
      else next.add(section)
      return next
    })
  }

  return (
    <nav ref={navRef} className="border-b bg-background/95 backdrop-blur-sm sticky top-0 z-50">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between items-center h-16">

          {/* Logo */}
          <Link href="/" className="flex items-center space-x-2 shrink-0">
            <Image
              src={brand.logoPath}
              alt={`${brand.displayName} Logo`}
              width={32}
              height={32}
              className="rounded-md"
            />
            <span className="text-xl font-bold text-foreground">{brand.wordmark}</span>
          </Link>

          {/* Desktop nav */}
          <div className="hidden sm:flex items-center gap-1">

            {/* Why Brand */}
            <div className="relative">
              <button
                onClick={() => toggleDropdown('why')}
                className="flex items-center gap-1 px-3 py-2 text-sm text-muted-foreground hover:text-foreground transition-colors rounded-md"
              >
                {t('nav.nav.whyBrand', { brand: brand.wordmark })}
                <ChevronDown className={`w-3.5 h-3.5 transition-transform duration-150 ${openDropdown === 'why' ? 'rotate-180' : ''}`} />
              </button>
              {openDropdown === 'why' && (
                <div className="absolute top-full left-0 mt-1 w-52 bg-background border border-border rounded-lg shadow-lg p-2 z-50">
                  {(
                    [
                      { key: 'whySpeed', href: '/#hero' },
                      { key: 'whyTech', href: '/#features' },
                      { key: 'whyTestimonials', href: '/#testimonials' },
                      { key: 'faq', href: '/#faq' },
                    ] as const
                  ).map(({ key, href }) => (
                    <Link
                      key={key}
                      href={href}
                      onClick={() => setOpenDropdown(null)}
                      className="block px-3 py-2 text-sm text-foreground/80 hover:text-foreground hover:bg-muted/50 rounded-md transition-colors"
                    >
                      {t(`nav.nav.${key}`)}
                    </Link>
                  ))}
                </div>
              )}
            </div>

            {/* Pricing — direct link */}
            <Link
              href="/purchase"
              className="px-3 py-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              {t('nav.nav.pricing')}
            </Link>

          </div>

          {/* Right actions */}
          <div className="flex items-center space-x-2">
            <LanguageSwitcher />
            <div className="hidden sm:flex items-center space-x-2">
              {isAuthenticated ? (
                <>
                  <Button asChild variant="outline" size="sm">
                    <Link href="/account">{t('admin.account.title')}</Link>
                  </Button>
                </>
              ) : (
                <Button asChild variant="ghost" size="sm">
                  <Link href="/login">{t('nav.nav.login')}</Link>
                </Button>
              )}
              <Button asChild size="sm">
                <Link href="/install">
                  <Download className="w-3.5 h-3.5 mr-1" />
                  {t('nav.nav.freeDownload')}
                </Link>
              </Button>
            </div>
            <button
              className="sm:hidden p-1.5 text-muted-foreground hover:text-foreground transition-colors"
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            >
              {mobileMenuOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
            </button>
          </div>
        </div>

        {/* Mobile menu */}
        {mobileMenuOpen && (
          <div className="sm:hidden border-t pb-4 pt-2">
            <Link
              href="/install"
              className="flex items-center gap-2 px-3 py-2.5 text-sm text-primary font-medium hover:bg-muted/50 rounded-md mb-1"
              onClick={() => setMobileMenuOpen(false)}
            >
              <Download className="w-4 h-4" />
              {t('nav.nav.freeDownload')}
            </Link>

            {/* Why Brand accordion */}
            <button
              className="w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/50 rounded-md"
              onClick={() => toggleMobileSection('why')}
            >
              {t('nav.nav.whyBrand', { brand: brand.wordmark })}
              <ChevronDown className={`w-4 h-4 transition-transform ${mobileExpanded.has('why') ? 'rotate-180' : ''}`} />
            </button>
            {mobileExpanded.has('why') && (
              <div className="pl-4 mb-1">
                {(
                  [
                    { key: 'whySpeed', href: '/#hero' },
                    { key: 'whyTech', href: '/#features' },
                    { key: 'whyTestimonials', href: '/#testimonials' },
                    { key: 'faq', href: '/#faq' },
                  ] as const
                ).map(({ key, href }) => (
                  <Link
                    key={key}
                    href={href}
                    className="block px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded-md"
                    onClick={() => setMobileMenuOpen(false)}
                  >
                    {t(`nav.nav.${key}`)}
                  </Link>
                ))}
              </div>
            )}

            {/* Pricing */}
            <Link
              href="/purchase"
              className="block px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/50 rounded-md"
              onClick={() => setMobileMenuOpen(false)}
            >
              {t('nav.nav.pricing')}
            </Link>

            {!isAuthenticated && (
              <Link
                href="/login"
                className="block px-3 py-2.5 text-sm text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded-md"
                onClick={() => setMobileMenuOpen(false)}
              >
                {t('nav.nav.login')}
              </Link>
            )}
          </div>
        )}
      </div>
    </nav>
  )
}
