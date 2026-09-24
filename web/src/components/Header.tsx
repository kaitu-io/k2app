'use client'

import { useState, useRef, useEffect, useCallback } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { Button } from '@/components/ui/button'
import { useTranslations } from 'next-intl'
import { Link, usePathname } from '@/i18n/routing'
import NextLink from 'next/link'
import LanguageSwitcher from '@/components/LanguageSwitcher'
import { Download, Menu, X, ChevronDown } from 'lucide-react'
import Image from 'next/image'
import { useBrand } from '@/hooks/useBrand'
import { siteConfig, isExternalHref, type NavItem } from '@/lib/site'
import { cn } from '@/lib/utils'

/**
 * 顶栏：结构来自 `lib/site/<brand>.ts`。
 *
 * - 一级项都是可点击的链接；带 children 的项在桌面端悬停 / 聚焦时展开下拉，移动端为折叠段。
 * - 当前页高亮：按 locale 无关的 pathname 与配置路径做前缀匹配（子项命中则父项也高亮）。
 *   首页锚点（`/#xxx`）不参与高亮——它们指向首页内的区块，不代表"所在页面"。
 * - 桌面 / 移动切换点为 `lg`：五个一级项加登录、CTA、语言切换在平板宽度会挤压换行。
 * - 品牌名只经 `{brand}` 插值进 label，不出现在本文件。
 */

/** 去掉 hash 后的站内路径；外链返回 null。 */
function routeOf(href: string): string | null {
  if (isExternalHref(href)) return null
  const path = href.split('#')[0]
  return path === '' ? '/' : path
}

function matches(pathname: string, href: string): boolean {
  const route = routeOf(href)
  if (route === null || route === '/') return false
  return pathname === route || pathname.startsWith(`${route}/`)
}

export function isNavItemActive(pathname: string, item: NavItem): boolean {
  if (matches(pathname, item.href)) return true
  return item.children?.some((child) => matches(pathname, child.href)) ?? false
}

/**
 * 分组内只高亮最具体的那个子项：/k2/quickstart 同时匹配 `/k2` 与 `/k2/quickstart`，
 * 取路径更长的一个，避免"k2 协议"和"快速自部署"一起亮。
 */
export function activeChildKey(pathname: string, children: NavItem[]): string | null {
  let best: NavItem | null = null
  for (const child of children) {
    if (!matches(pathname, child.href)) continue
    if (!best || (routeOf(child.href)?.length ?? 0) > (routeOf(best.href)?.length ?? 0)) best = child
  }
  return best?.labelKey ?? null
}

/** 站内路径走 i18n Link（补 locale 前缀），外链走原生 Link 并新开标签。 */
function NavLink({
  href,
  className,
  children,
  onClick,
  ...rest
}: {
  href: string
  className?: string
  children: React.ReactNode
  onClick?: () => void
  'aria-current'?: 'page'
}) {
  if (isExternalHref(href)) {
    return (
      <NextLink href={href} className={className} onClick={onClick} target="_blank" rel="noopener noreferrer" {...rest}>
        {children}
      </NextLink>
    )
  }
  return (
    <Link href={href} className={className} onClick={onClick} {...rest}>
      {children}
    </Link>
  )
}

export default function Header() {
  const brand = useBrand()
  const site = siteConfig(brand)
  const { isAuthenticated } = useAuth()
  const t = useTranslations()
  const pathname = usePathname()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [openDropdown, setOpenDropdown] = useState<string | null>(null)
  const [mobileExpanded, setMobileExpanded] = useState<Set<string>>(new Set())
  const navRef = useRef<HTMLElement>(null)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    function handleOutsideClick(e: MouseEvent) {
      if (navRef.current && !navRef.current.contains(e.target as Node)) {
        setOpenDropdown(null)
      }
    }
    document.addEventListener('mousedown', handleOutsideClick)
    return () => document.removeEventListener('mousedown', handleOutsideClick)
  }, [])

  // 路由变化后收起所有菜单（点击链接后的自然行为）。
  useEffect(() => {
    setMobileMenuOpen(false)
    setOpenDropdown(null)
  }, [pathname])

  const vars = { brand: brand.wordmark }
  const label = (item: NavItem) => t(item.labelKey, vars)

  // 悬停离开时延迟收起，给鼠标从触发项移到面板留出间隙。
  const openMenu = useCallback((id: string) => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
    setOpenDropdown(id)
  }, [])
  const scheduleClose = useCallback(() => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
    closeTimer.current = setTimeout(() => setOpenDropdown(null), 120)
  }, [])

  function toggleMobileSection(section: string) {
    setMobileExpanded(prev => {
      const next = new Set(prev)
      if (next.has(section)) next.delete(section)
      else next.add(section)
      return next
    })
  }

  const desktopItemClass = (active: boolean) =>
    cn(
      'flex items-center gap-1 px-3 py-2 text-sm rounded-md transition-colors',
      active ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground',
    )

  return (
    <nav ref={navRef} aria-label={t('nav.nav.navigation')} className="border-b bg-background/95 backdrop-blur-sm sticky top-0 z-50">
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
          <div className="hidden lg:flex items-center gap-1">
            {site.nav.primary.map((item) => {
              const active = isNavItemActive(pathname, item)
              if (!item.children) {
                return (
                  <NavLink
                    key={item.labelKey}
                    href={item.href}
                    className={desktopItemClass(active)}
                    aria-current={active ? 'page' : undefined}
                  >
                    {label(item)}
                  </NavLink>
                )
              }
              const open = openDropdown === item.labelKey
              return (
                <div
                  key={item.labelKey}
                  className="relative"
                  onMouseEnter={() => openMenu(item.labelKey)}
                  onMouseLeave={scheduleClose}
                  onFocus={() => openMenu(item.labelKey)}
                  onBlur={(e) => {
                    if (!e.currentTarget.contains(e.relatedTarget as Node)) scheduleClose()
                  }}
                >
                  <NavLink
                    href={item.href}
                    className={desktopItemClass(active)}
                    aria-current={active ? 'page' : undefined}
                  >
                    {label(item)}
                    <ChevronDown aria-hidden className={`w-3.5 h-3.5 transition-transform duration-150 ${open ? 'rotate-180' : ''}`} />
                  </NavLink>
                  {open && (
                    <div
                      role="menu"
                      aria-label={label(item)}
                      className="absolute top-full left-0 pt-1 w-56 z-50"
                    >
                      <div className="bg-background border border-border rounded-lg shadow-lg p-2">
                        {item.children.map((child) => {
                          const childActive = activeChildKey(pathname, item.children!) === child.labelKey
                          return (
                            <NavLink
                              key={child.labelKey}
                              href={child.href}
                              onClick={() => setOpenDropdown(null)}
                              aria-current={childActive ? 'page' : undefined}
                              className={cn(
                                'block px-3 py-2 text-sm rounded-md transition-colors hover:bg-muted/50',
                                childActive ? 'text-foreground font-medium bg-muted/40' : 'text-foreground/80 hover:text-foreground',
                              )}
                            >
                              {label(child)}
                            </NavLink>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </div>
              )
            })}
          </div>

          {/* Right actions */}
          <div className="flex items-center space-x-2">
            <LanguageSwitcher />
            <div className="hidden lg:flex items-center space-x-2">
              {isAuthenticated ? (
                <Button asChild variant="outline" size="sm">
                  <Link href="/account">{t('admin.account.title')}</Link>
                </Button>
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
              type="button"
              className="lg:hidden p-1.5 text-muted-foreground hover:text-foreground transition-colors"
              aria-expanded={mobileMenuOpen}
              aria-controls="site-mobile-menu"
              aria-label={t('nav.nav.menu')}
              onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
            >
              {mobileMenuOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
            </button>
          </div>
        </div>

        {/* Mobile menu */}
        {mobileMenuOpen && (
          <div id="site-mobile-menu" className="lg:hidden border-t pb-4 pt-2">
            <Link
              href={site.nav.cta.href}
              className="flex items-center gap-2 px-3 py-2.5 text-sm text-primary font-medium hover:bg-muted/50 rounded-md mb-1"
              onClick={() => setMobileMenuOpen(false)}
            >
              <Download className="w-4 h-4" />
              {label(site.nav.cta)}
            </Link>

            {site.nav.primary.map((item) => {
              const active = isNavItemActive(pathname, item)
              if (!item.children) {
                return (
                  <NavLink
                    key={item.labelKey}
                    href={item.href}
                    className={cn(
                      'block px-3 py-2.5 text-sm font-medium hover:bg-muted/50 rounded-md',
                      active ? 'text-primary' : 'text-foreground',
                    )}
                    aria-current={active ? 'page' : undefined}
                    onClick={() => setMobileMenuOpen(false)}
                  >
                    {label(item)}
                  </NavLink>
                )
              }
              // 当前所在分组默认展开，点一下可收起；其余分组默认收起。
              const toggled = mobileExpanded.has(item.labelKey)
              const expanded = active ? !toggled : toggled
              return (
                <div key={item.labelKey}>
                  <button
                    type="button"
                    className={cn(
                      'w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium hover:bg-muted/50 rounded-md',
                      active ? 'text-primary' : 'text-foreground',
                    )}
                    aria-expanded={expanded}
                    onClick={() => toggleMobileSection(item.labelKey)}
                  >
                    {label(item)}
                    <ChevronDown aria-hidden className={`w-4 h-4 transition-transform ${expanded ? 'rotate-180' : ''}`} />
                  </button>
                  {expanded && (
                    <div className="pl-4 mb-1">
                      {item.children.map((child) => {
                        const childActive = activeChildKey(pathname, item.children!) === child.labelKey
                        return (
                          <NavLink
                            key={child.labelKey}
                            href={child.href}
                            className={cn(
                              'block px-3 py-2 text-sm hover:bg-muted/50 rounded-md',
                              childActive ? 'text-primary font-medium' : 'text-muted-foreground hover:text-foreground',
                            )}
                            aria-current={childActive ? 'page' : undefined}
                            onClick={() => setMobileMenuOpen(false)}
                          >
                            {label(child)}
                          </NavLink>
                        )
                      })}
                    </div>
                  )}
                </div>
              )
            })}

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
