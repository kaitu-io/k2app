# Header / Footer Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the developer-focused website header with a user-focused nav (Payload-style dropdowns), add a Developer column to the footer, and add Header+Footer to bare blog pages.

**Architecture:** 4 independent changes in `web/` — brands.ts wordmark fix, i18n key additions across 7 locales, Header.tsx full rewrite with dropdown state, Footer.tsx 5-column expansion, blog pages wrapped. No new files created; no Payload Globals needed.

**Tech Stack:** Next.js 15, React 19, TypeScript, Tailwind CSS 4, next-intl, lucide-react, shadcn/ui Button, existing BrandProvider/AuthContext hooks.

---

## File Map

| Action | File |
|--------|------|
| Modify | `web/src/lib/brands.ts` |
| Modify | `web/messages/zh-CN/nav.json` |
| Modify | `web/messages/en-US/nav.json` |
| Modify | `web/messages/en-GB/nav.json` |
| Modify | `web/messages/en-AU/nav.json` |
| Modify | `web/messages/zh-TW/nav.json` |
| Modify | `web/messages/zh-HK/nav.json` |
| Modify | `web/messages/ja/nav.json` |
| Rewrite | `web/src/components/Header.tsx` |
| Modify | `web/src/components/Footer.tsx` |
| Modify | `web/src/app/[locale]/blog/page.tsx` |
| Modify | `web/src/app/[locale]/blog/[slug]/page.tsx` |

---

## Task 1: Fix KAITU brand wordmark

**Files:** Modify `web/src/lib/brands.ts`

- [ ] **Step 1: Edit brands.ts**

  Change line 23:
  ```ts
  // Before
  wordmark: 'Kaitu.io',

  // After
  wordmark: '开途',
  ```

- [ ] **Step 2: Verify lint passes**

  ```bash
  cd web && yarn lint --max-warnings=0 2>&1 | tail -5
  ```
  Expected: no errors.

- [ ] **Step 3: Commit**

  ```bash
  git add web/src/lib/brands.ts
  git commit -m "fix(brand): KAITU wordmark '开途'"
  ```

---

## Task 2: Add i18n keys — zh-CN

**Files:** Modify `web/messages/zh-CN/nav.json`

- [ ] **Step 1: Add keys to `nav` object**

  Inside `"nav": { ... }` add after the existing keys:

  ```json
  "productFeatures": "产品功能",
  "whyBrand": "为什么选 {brand}",
  "freeDownload": "免费下载",
  "pricing": "定价",
  "help": "帮助",
  "useCases": "使用场景",
  "breakGFW": "突破网络限制",
  "familyProtection": "家庭全设备保护",
  "mobilePlusDesktop": "移动端 + 桌面端",
  "supportedPlatforms": "支持平台",
  "whySpeed": "速度与稳定性",
  "whySecurity": "安全与隐私承诺",
  "whyTestimonials": "用户评价",
  "quickStart": "快速入门",
  "faq": "常见问题",
  "contactUs": "联系我们"
  ```

- [ ] **Step 2: Add keys to `footer.developer` object**

  Inside `"developer": { ... }` add after existing keys:

  ```json
  "k2Docs": "k2 协议文档",
  "selfDeploy": "快速自部署",
  "routerConfig": "路由器配置",
  "github": "GitHub 开源",
  "changelog": "更新日志"
  ```

- [ ] **Step 3: Commit**

  ```bash
  git add web/messages/zh-CN/nav.json
  git commit -m "i18n(zh-CN): add header nav + footer developer keys"
  ```

---

## Task 3: Add i18n keys — remaining 6 locales

**Files:** `web/messages/{en-US,en-GB,en-AU,zh-TW,zh-HK,ja}/nav.json`

- [ ] **Step 1: Update en-US/nav.json**

  Add to `"nav"`:
  ```json
  "productFeatures": "Product",
  "whyBrand": "Why {brand}",
  "freeDownload": "Free Download",
  "pricing": "Pricing",
  "help": "Help",
  "useCases": "Use Cases",
  "breakGFW": "Bypass Restrictions",
  "familyProtection": "Family Device Protection",
  "mobilePlusDesktop": "Mobile & Desktop",
  "supportedPlatforms": "Supported Platforms",
  "whySpeed": "Speed & Stability",
  "whySecurity": "Security & Privacy",
  "whyTestimonials": "User Reviews",
  "quickStart": "Quick Start",
  "faq": "FAQ",
  "contactUs": "Contact Us"
  ```

  Add to `"footer"."developer"`:
  ```json
  "k2Docs": "k2 Protocol Docs",
  "selfDeploy": "Self-Deploy Guide",
  "routerConfig": "Router Setup",
  "github": "GitHub Open Source",
  "changelog": "Changelog"
  ```

- [ ] **Step 2: Update en-GB/nav.json** (identical to en-US additions)

  Same additions as en-US above.

- [ ] **Step 3: Update en-AU/nav.json** (identical to en-US additions)

  Same additions as en-US above.

- [ ] **Step 4: Update zh-TW/nav.json**

  Add to `"nav"`:
  ```json
  "productFeatures": "產品功能",
  "whyBrand": "為什麼選 {brand}",
  "freeDownload": "免費下載",
  "pricing": "定價",
  "help": "幫助",
  "useCases": "使用場景",
  "breakGFW": "突破網路限制",
  "familyProtection": "家庭全設備保護",
  "mobilePlusDesktop": "行動端 + 桌面端",
  "supportedPlatforms": "支援平台",
  "whySpeed": "速度與穩定性",
  "whySecurity": "安全與隱私承諾",
  "whyTestimonials": "用戶評價",
  "quickStart": "快速入門",
  "faq": "常見問題",
  "contactUs": "聯絡我們"
  ```

  Add to `"footer"."developer"`:
  ```json
  "k2Docs": "k2 協議文件",
  "selfDeploy": "快速自部署",
  "routerConfig": "路由器設定",
  "github": "GitHub 開源",
  "changelog": "更新日誌"
  ```

- [ ] **Step 5: Update zh-HK/nav.json**

  Add to `"nav"`:
  ```json
  "productFeatures": "產品功能",
  "whyBrand": "點解揀 {brand}",
  "freeDownload": "免費下載",
  "pricing": "定價",
  "help": "幫助",
  "useCases": "使用場景",
  "breakGFW": "突破網絡限制",
  "familyProtection": "家庭全設備保護",
  "mobilePlusDesktop": "移動端 + 桌面端",
  "supportedPlatforms": "支援平台",
  "whySpeed": "速度與穩定性",
  "whySecurity": "安全與私隱承諾",
  "whyTestimonials": "用戶評價",
  "quickStart": "快速入門",
  "faq": "常見問題",
  "contactUs": "聯繫我哋"
  ```

  Add to `"footer"."developer"`:
  ```json
  "k2Docs": "k2 協議文檔",
  "selfDeploy": "快速自部署",
  "routerConfig": "路由器配置",
  "github": "GitHub 開源",
  "changelog": "更新日誌"
  ```

- [ ] **Step 6: Update ja/nav.json**

  Add to `"nav"`:
  ```json
  "productFeatures": "製品機能",
  "whyBrand": "{brand}を選ぶ理由",
  "freeDownload": "無料ダウンロード",
  "pricing": "料金",
  "help": "ヘルプ",
  "useCases": "使用ケース",
  "breakGFW": "ネット制限を突破",
  "familyProtection": "家族のデバイス保護",
  "mobilePlusDesktop": "モバイル＆デスクトップ",
  "supportedPlatforms": "対応プラットフォーム",
  "whySpeed": "速度と安定性",
  "whySecurity": "セキュリティとプライバシー",
  "whyTestimonials": "ユーザーレビュー",
  "quickStart": "クイックスタート",
  "faq": "よくある質問",
  "contactUs": "お問い合わせ"
  ```

  Add to `"footer"."developer"`:
  ```json
  "k2Docs": "k2プロトコルドキュメント",
  "selfDeploy": "自己デプロイガイド",
  "routerConfig": "ルーター設定",
  "github": "GitHub オープンソース",
  "changelog": "更新履歴"
  ```

- [ ] **Step 7: Commit**

  ```bash
  git add web/messages/en-US/nav.json web/messages/en-GB/nav.json web/messages/en-AU/nav.json \
          web/messages/zh-TW/nav.json web/messages/zh-HK/nav.json web/messages/ja/nav.json
  git commit -m "i18n(all): add header nav + footer developer keys (6 locales)"
  ```

---

## Task 4: Rewrite Header.tsx

**Files:** Rewrite `web/src/components/Header.tsx`

- [ ] **Step 1: Replace the full file**

  ```tsx
  'use client'

  import { useState, useRef, useEffect } from 'react'
  import { useAuth } from '@/contexts/AuthContext'
  import { Button } from '@/components/ui/button'
  import { useTranslations } from 'next-intl'
  import { Link } from '@/i18n/routing'
  import NextLink from 'next/link'
  import LanguageSwitcher from '@/components/LanguageSwitcher'
  import { Download, Menu, X, ChevronDown } from 'lucide-react'
  import Image from 'next/image'
  import { useBrand } from '@/components/providers/BrandProvider'

  type DropdownId = 'product' | 'why' | 'help'
  type MobileSection = 'product' | 'why' | 'help'

  const PLATFORMS = ['macOS', 'Windows', 'iOS', 'Android', 'Linux']

  export default function Header() {
    const brand = useBrand()
    const { isAuthenticated, user } = useAuth()
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

              {/* Product Features */}
              <div className="relative">
                <button
                  onClick={() => toggleDropdown('product')}
                  className="flex items-center gap-1 px-3 py-2 text-sm text-muted-foreground hover:text-foreground transition-colors rounded-md"
                >
                  {t('nav.nav.productFeatures')}
                  <ChevronDown className={`w-3.5 h-3.5 transition-transform duration-150 ${openDropdown === 'product' ? 'rotate-180' : ''}`} />
                </button>
                {openDropdown === 'product' && (
                  <div className="absolute top-full left-0 mt-1 w-72 bg-background border border-border rounded-lg shadow-lg p-4 z-50">
                    <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
                      {t('nav.nav.useCases')}
                    </p>
                    <div className="space-y-0.5 mb-4">
                      {(
                        [
                          { key: 'breakGFW', href: '/' },
                          { key: 'familyProtection', href: '/' },
                          { key: 'mobilePlusDesktop', href: '/install' },
                        ] as const
                      ).map(({ key, href }) => (
                        <Link
                          key={key}
                          href={href}
                          onClick={() => setOpenDropdown(null)}
                          className="block px-2 py-1.5 text-sm text-foreground/80 hover:text-foreground hover:bg-muted/50 rounded-md transition-colors"
                        >
                          {t(`nav.nav.${key}`)}
                        </Link>
                      ))}
                    </div>
                    <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
                      {t('nav.nav.supportedPlatforms')}
                    </p>
                    <div className="flex flex-wrap gap-1.5">
                      {PLATFORMS.map(p => (
                        <Link
                          key={p}
                          href="/install"
                          onClick={() => setOpenDropdown(null)}
                          className="px-2 py-0.5 text-xs bg-muted text-muted-foreground hover:text-foreground rounded transition-colors"
                        >
                          {p}
                        </Link>
                      ))}
                    </div>
                  </div>
                )}
              </div>

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
                        { key: 'whySpeed', href: '/' },
                        { key: 'whySecurity', href: '/' },
                        { key: 'whyTestimonials', href: '/' },
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

              {/* Help */}
              <div className="relative">
                <button
                  onClick={() => toggleDropdown('help')}
                  className="flex items-center gap-1 px-3 py-2 text-sm text-muted-foreground hover:text-foreground transition-colors rounded-md"
                >
                  {t('nav.nav.help')}
                  <ChevronDown className={`w-3.5 h-3.5 transition-transform duration-150 ${openDropdown === 'help' ? 'rotate-180' : ''}`} />
                </button>
                {openDropdown === 'help' && (
                  <div className="absolute top-full left-0 mt-1 w-44 bg-background border border-border rounded-lg shadow-lg p-2 z-50">
                    {(
                      [
                        { key: 'quickStart', href: '/guides' },
                        { key: 'faq', href: '/support' },
                        { key: 'contactUs', href: '/support' },
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
                    {user?.isAdmin && (
                      <Button asChild variant="outline" size="sm">
                        <NextLink href="/manager">{t('nav.nav.adminPanel')}</NextLink>
                      </Button>
                    )}
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

              {/* Product accordion */}
              <button
                className="w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/50 rounded-md"
                onClick={() => toggleMobileSection('product')}
              >
                {t('nav.nav.productFeatures')}
                <ChevronDown className={`w-4 h-4 transition-transform ${mobileExpanded.has('product') ? 'rotate-180' : ''}`} />
              </button>
              {mobileExpanded.has('product') && (
                <div className="pl-4 mb-1">
                  {(
                    [
                      { key: 'breakGFW', href: '/' },
                      { key: 'familyProtection', href: '/' },
                      { key: 'mobilePlusDesktop', href: '/install' },
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
                      { key: 'whySpeed', href: '/' },
                      { key: 'whySecurity', href: '/' },
                      { key: 'whyTestimonials', href: '/' },
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

              {/* Help accordion */}
              <button
                className="w-full flex items-center justify-between px-3 py-2.5 text-sm font-medium text-foreground hover:bg-muted/50 rounded-md"
                onClick={() => toggleMobileSection('help')}
              >
                {t('nav.nav.help')}
                <ChevronDown className={`w-4 h-4 transition-transform ${mobileExpanded.has('help') ? 'rotate-180' : ''}`} />
              </button>
              {mobileExpanded.has('help') && (
                <div className="pl-4 mb-1">
                  {(
                    [
                      { key: 'quickStart', href: '/guides' },
                      { key: 'faq', href: '/support' },
                      { key: 'contactUs', href: '/support' },
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
  ```

- [ ] **Step 2: Lint**

  ```bash
  cd web && yarn lint --max-warnings=0 2>&1 | tail -10
  ```
  Expected: no errors. If `@/i18n/routing` import rule fires on `Link`, it's already using the correct import.

- [ ] **Step 3: Start dev server and visually verify**

  ```bash
  cd web && yarn dev
  ```
  Open `http://localhost:3000/zh-CN`. Verify:
  - Logo shows **开途** (not Kaitu.io)
  - Nav shows: 产品功能 / 为什么选 开途 / 定价 / 帮助
  - Clicking 产品功能 opens dropdown with 使用场景 + 支持平台 chips
  - Clicking outside closes dropdown
  - 定价 links to `/zh-CN/purchase`
  - Hamburger works on narrow viewport; each section expands/collapses
  - Old links (k2协议, 快速自部署, 路由器, GitHub) are gone

  Open `http://localhost:3000/en-US`. Verify:
  - Logo shows **Overleap**
  - Nav shows: Product / Why Overleap / Pricing / Help

- [ ] **Step 4: Commit**

  ```bash
  git add web/src/components/Header.tsx
  git commit -m "feat(header): user-focused nav with dropdowns, remove technical links"
  ```

---

## Task 5: Update Footer.tsx — add Developer column

**Files:** Modify `web/src/components/Footer.tsx`

- [ ] **Step 1: Add NextLink import**

  At the top, after the existing imports, add:
  ```tsx
  import NextLink from 'next/link'
  ```

- [ ] **Step 2: Change grid from 4 to 5 columns**

  Find:
  ```tsx
  <div className="grid md:grid-cols-4 gap-8">
  ```
  Replace with:
  ```tsx
  <div className="grid md:grid-cols-5 gap-8">
  ```

- [ ] **Step 3: Add Developer column after the Product `<div>`**

  The Product column closes with `</div>` before the Support column. Insert a new Developer column between them:

  ```tsx
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
  ```

- [ ] **Step 4: Lint + visual verify**

  ```bash
  cd web && yarn lint --max-warnings=0 2>&1 | tail -5
  ```

  In the dev server (`http://localhost:3000/zh-CN`), scroll to footer. Verify 5 columns appear on desktop: brand description | 产品 | 开发者 | 支持 | 法律条款. Click the GitHub link and verify it opens `https://github.com/getoverleap` in a new tab.

- [ ] **Step 5: Commit**

  ```bash
  git add web/src/components/Footer.tsx
  git commit -m "feat(footer): add Developer column with technical links"
  ```

---

## Task 6: Add Header + Footer to blog pages

**Files:** Modify `web/src/app/[locale]/blog/page.tsx` and `web/src/app/[locale]/blog/[slug]/page.tsx`

- [ ] **Step 1: Update blog/page.tsx**

  Add imports at the top:
  ```tsx
  import Header from '@/components/Header'
  import Footer from '@/components/Footer'
  ```

  Change the return statement from:
  ```tsx
  return (
    <div className="mx-auto max-w-3xl px-4 py-12">
  ```
  to:
  ```tsx
  return (
    <>
      <Header />
      <div className="mx-auto max-w-3xl px-4 py-12">
  ```

  And close with:
  ```tsx
      </div>
      <Footer />
    </>
  )
  ```

- [ ] **Step 2: Update blog/[slug]/page.tsx**

  Add imports after existing imports:
  ```tsx
  import Header from '@/components/Header'
  import Footer from '@/components/Footer'
  ```

  Change the return in `BlogDetailPage` from:
  ```tsx
  return (
    <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
  ```
  to:
  ```tsx
  return (
    <>
      <Header />
      <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
  ```

  And close with:
  ```tsx
      </article>
      <Footer />
    </>
  )
  ```

- [ ] **Step 3: Visual verify**

  In dev server, open `http://localhost:3000/zh-CN/blog`. Verify Header and Footer appear.
  Open `http://localhost:3000/zh-CN/blog/mcp-smoke-test-2`. Verify Header and Footer appear.

- [ ] **Step 4: Run vitest to ensure no regressions**

  ```bash
  cd web && yarn test 2>&1 | tail -20
  ```
  Expected: all existing tests pass.

- [ ] **Step 5: Commit**

  ```bash
  git add web/src/app/\[locale\]/blog/page.tsx web/src/app/\[locale\]/blog/\[slug\]/page.tsx
  git commit -m "feat(blog): add Header and Footer to blog pages"
  ```

---

## Final Check

- [ ] **Run full lint**

  ```bash
  cd web && yarn lint --max-warnings=0
  ```

- [ ] **Run vitest**

  ```bash
  cd web && yarn test
  ```

- [ ] **Verify i18n completeness** — no raw key passthrough on any locale

  Open `http://localhost:3000/en-US`, `http://localhost:3000/zh-TW`, `http://localhost:3000/ja`. Confirm nav labels render as translated text, not raw key strings like `nav.nav.productFeatures`.
