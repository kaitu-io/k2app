---
name: publish-content
description: Use when user asks to write articles, publish content, or create blog posts. Triggers: "写文章", "发布内容", "publish content", "write an article", "create a post"
---

# Publish Content Skill

Standardized workflow for creating and publishing markdown content in the `web/` website.

## Content Frontmatter Schema

Every content file must include this frontmatter:

```yaml
---
title: "文章标题"           # Required string
date: 2026-02-20            # Required ISO date — always use today's date
summary: "一句话摘要"       # Optional, used for listing + SEO description
tags: ["vpn", "update"]     # Optional string array
coverImage: "/images/content/x.jpg"  # Optional, OG image
draft: false                # Optional, default false
---
```

## Directory Structure

- Content files: `web/content/{locale}/{path}.md`
- Images: `web/public/images/content/`
- Example: `web/content/zh-CN/blog/new-feature.md` → URL: `/zh-CN/blog/new-feature`

## Supported Languages

| Code | Language |
|------|----------|
| `zh-CN` | Simplified Chinese (default) |
| `en-US` | English (US) |
| `en-GB` | English (UK) |
| `en-AU` | English (AU) |
| `zh-TW` | Traditional Chinese |
| `zh-HK` | Traditional Chinese (HK) |
| `ja` | Japanese |

## Reserved Paths

Content must NOT use these path segments (already claimed by app routes):

```
403, account, discovery, install, login, opensource, privacy, purchase,
retailer, routers, s, terms, changelog, manager
```

## Workflow (6 Steps)

1. **Confirm topic and target path** — e.g., `blog/new-feature`
2. **Confirm target language** — default zh-CN; ask if multi-language needed
3. **Generate markdown file** with proper frontmatter and content
4. **Translate if multi-language** — create one file per requested locale at the same path
5. **Build verification** — run `cd web && yarn build` and confirm it passes
6. **Ask to commit + push** — ask the user whether to commit and push for deployment

## Key Rules

- Always use today's date in the `date` frontmatter field
- zh-CN is the primary language — always create it first
- Same filename path across locales = same article in different languages
- Creating a new directory path automatically creates a listing page at that URL
- Images: save to `web/public/images/content/`, reference as `/images/content/filename.jpg`
- After writing content files, always verify with `cd web && yarn build`
- Never use reserved paths listed above as content slugs

## File Creation Example

```
web/content/zh-CN/blog/v0-4-release.md   ← primary
web/content/en-US/blog/v0-4-release.md   ← translation (if requested)
```
