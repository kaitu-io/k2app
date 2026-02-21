将 web/ 下剩余 18 个 "use client" 公共页面改为 Server Component，提升 SEO/GEO 效果。

涉及页面：install, purchase, login, account/*, discovery, changelog, privacy, terms, opensource, routers, retailer/rules, 403, s/[code]

改造方式：useTranslations() → getTranslations(), 移除 "use client", 交互部分提取为 client component 子组件。

参考首页改造模式（website-k2-redesign spec AC6）。
