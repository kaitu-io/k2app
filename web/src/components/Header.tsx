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
import { siteConfig, type NavItem } from '@/lib/site'

/**
 * 顶栏：结构来自 `lib/site/<brand>.ts`（spec 2026-09-04-overleap-site-decoupling §2）。
 * 有 children 的主项渲染为下拉（桌面）/ 折叠段（移动），其余为直链。
 * 品牌名只经 `{brand}` 插值进 label，不出现在本文件。
 */
export default function Header() {
  const brand = useBrand()
  const site = siteConfig(brand)
  const { isAuthenticated } = useAuth()
  const t = useTranslations()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [openDropdown, setOpenDropdown] = useState<string | null>(null)
  const [mobileExpanded, setMobileExpanded] = useState<Set<string>>(new Set())
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

  const vars = { brand: brand.wordmark }
  const label = (item: NavItem) => t(item.labelKey, vars)

  function toggleDropdown(id: string) {
    setOpenDropdown(prev => (prev === id ? null : id))
  }

  function toggleMobileSection(section: string) {
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
            {site.nav.primary.map((item) =>
              item.children ? (
                <div key={item.labelKey} className="relative">
                  <button
                    onClick={() => toggleDropdown(item.labelKey)}
                    className="flex items-center gap-1 px-3 py-2 text-sm text-muted-foreground hover:text-foreground transition-colors rounded-md"
                  >
                    {label(item)}
                    <ChevronDown className={`w-3.5 h-3.5 transition-transform duration-150 ${openDropdown === item.labelKey ? 'rotate-180' : ''}`} />
                  </button>
                  {openDropdown === item.labelKey && (
                    <div className="absolute top-full left-0 mt-1 w-52 bg-background border border-border rounded-lg shadow-lg p-2 z-50">
                      {item.children.map((child) => (
                        <Link
                          key={child.labelKey}
                          href={child.href}
                          onClick={() => setOpenDropdown(null)}
                          className="block px-3 py-2 text-sm text-foreground/80 hover:text-foreground hover:bg-muted/50 rounded-md transition-colors"
                        >
                          {label(child)}
                        </Link>
                      ))}
                    </div>
                  )}
                </div>
              ) : (
                <Link
                  key={item.labelKey}
                  href={item.href}
                  className="px-3 py-2 text-sm text-muted-foreground hover:text-foreground transition-colors"
                >
                  {label(item)}
                </Link>
              ),
            )}
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
                <Link href={site.nav.cta.href}>
                  <Download className="w-3.5 h-3.5 mr-1" />
                  {label(site.nav.cta)}
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
              href={site.nav.cta.href}
              className="flex items-center gap-2 px-3 py-2.5 text-sm text-primary font-medium hover:bg-muted/50 rounded-md mb-1"
              onClick={() => setMobileMenuOpen(false)}
            >
              <Download className="w-4 h-4" />
              {label(site.nav.cta)}
            </Link>

            {site.nav.primary.map((item) =>
              item.children ? (
                <div key={item.labelKey}>
                  <button
                    className="w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/50 rounded-md"
                    onClick={() => toggleMobileSection(item.labelKey)}
                  >
                    {label(item)}
                    <ChevronDown className={`w-4 h-4 transition-transform ${mobileExpanded.has(item.labelKey) ? 'rotate-180' : ''}`} />
                  </button>
                  {mobileExpanded.has(item.labelKey) && (
                    <div className="pl-4 mb-1">
                      {item.children.map((child) => (
                        <Link
                          key={child.labelKey}
                          href={child.href}
                          className="block px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded-md"
                          onClick={() => setMobileMenuOpen(false)}
                        >
                          {label(child)}
                        </Link>
                      ))}
                    </div>
                  )}
                </div>
              ) : (
                <Link
                  key={item.labelKey}
                  href={item.href}
                  className="block px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/50 rounded-md"
                  onClick={() => setMobileMenuOpen(false)}
                >
                  {label(item)}
                </Link>
              ),
            )}

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
