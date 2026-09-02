# ASO Constitutional Rules (App Store Optimization)

Moved verbatim from `mobile/CLAUDE.md` on 2026-09-02 (the layer doc is loaded on every mobile session; these rules have no code hooks and belong with the marketing archive). Apply to all App Store Connect submissions for both brands — 开途 (China storefront) and Overleap (overseas). Brand isolation rules in [`brand-naming-strategy.md`](./brand-naming-strategy.md) take precedence over anything here. The 2026-04-21 audit snapshot is [`audits/2026-04-21-aso.md`](./audits/2026-04-21-aso.md).

- **Name + Subtitle + Keywords are one system**: Never duplicate words across name, subtitle, and keyword fields. Apple indexes all three together — duplication wastes character budget.
- **Cross-locale keyword strategy**: China storefront indexes both zh-Hans AND en-US fields. Use zh-Hans for Chinese intent words (加速器, 翻墙, 科学上网), en-US for English competitor names (shadowsocks, clash, v2ray, surge). Zero overlap between the two.
- **100-character keyword budget**: Keywords are comma-separated, no spaces after commas. Every character counts. Prioritize by search volume × relevance. Drop low-conversion terms ruthlessly.
- **First 3 lines of description**: App Store folds description after ~3 lines. Core value proposition must be above the fold. Lead with user benefit, not feature list.
- **Screenshots convert**: First 3 screenshots determine install rate. Screenshot 1 must show core function + brand slogan overlay. Use device frames. Localize screenshot text per storefront.
- **Version updates = keyword refresh opportunity**: Every new version submission is a chance to A/B test keyword changes. Track keyword rankings before and after.
- **No "VPN" in user-visible text**: Apple flags VPN-related terminology. Use "network accelerator", "secure tunnel", "proxy" instead. Internal JSON keys (e.g., `"vpn"` namespace) are fine — only user-facing strings matter.
- **Review notes template**: Always include: app category justification, demo account credentials (if subscription), explanation of network extension usage.
- **Privacy nutrition labels**: Keep privacy declarations current with actual data collection. Discrepancies trigger review rejection.
- **Custom Product Pages**: Create locale-specific pages for different traffic sources (organic search vs social vs ads) when budget allows.

Related skill: `marketing-skills:aso-audit`.
