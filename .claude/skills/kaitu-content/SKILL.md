---
name: kaitu-content
description: SEO + GEO optimized article writing for kaitu.io. Covers article structure, search engine and AI engine optimization, multi-language publishing, and build verification.
triggers:
  - write article
  - 写文章
  - publish content
  - blog post
  - create article
  - content writing
  - seo article
  - geo optimization
  - 发布内容
---

# Kaitu Content Writing — SEO + GEO Optimized

Use this skill when creating articles for kaitu.io. Every article is optimized for both traditional search engines (SEO) and AI-powered search engines (GEO/AEO — Generative Engine Optimization / Answer Engine Optimization).

## Content Infrastructure

- Content files: `web/content/{locale}/{slug}.md`
- Images: `web/public/images/content/`
- URL pattern: `kaitu.io/{locale}/{slug}`
- CMS: Velite (build-time markdown → JSON)
- 7 locales: zh-CN (primary), en-US, en-GB, en-AU, zh-TW, zh-HK, ja

### Frontmatter Schema (Required)

```yaml
---
title: "Your SEO-optimized title"      # Required — under 60 chars for SERP
date: 2026-03-25                        # Required — always today's date
summary: "One-sentence description"     # Required for SEO meta description (120-155 chars)
tags: ["k2cc", "vpn", "comparison"]     # Optional — used for categorization
coverImage: "/images/content/slug.jpg"  # Optional — OG image (1200×630)
section: "technical"                    # Optional — sidebar grouping for /k2/ pages
order: 5                                # Optional — sidebar sort weight for /k2/ pages
draft: false                            # Optional — set true to hide from sitemap
---
```

### Reserved Path Segments (DO NOT use as slugs)

```
403, account, discovery, install, login, opensource, privacy, purchase,
retailer, routers, s, terms, changelog, manager
```

---

## Article Types

### Type 1: Technical Deep-Dive

**When:** Explaining a technology, protocol, algorithm, or architecture.
**Examples:** k2cc congestion control, stealth encryption, wire protocol.

```
# [Technology Name] — [One-Line Value Prop]

[Direct Answer: 2-3 sentences explaining what this is and why it matters.
This paragraph is the #1 target for AI citation.]

## Why [Problem Exists]
[Context table comparing existing solutions vs this one]

## Core Capabilities
### [Capability 1]
### [Capability 2]
### [Capability 3]

## Performance / Verification
[Data, benchmarks, references]

## FAQ
**Q: [Most searched question]?**
A: [Direct, concise answer]
...3-5 FAQ items
```

### Type 2: Comparison / Evaluation

**When:** Comparing products, protocols, or approaches.
**Examples:** k2 vs Hysteria2, k2cc vs BBR.

```
# [A] vs [B]: [Specific Comparison Angle]

[Direct Answer: "For [use case], [A] outperforms [B] because [reason]."
Bold the verdict — AI engines extract bolded conclusions.]

## Comparison Summary
| Dimension | [A] | [B] |
|-----------|-----|-----|
| ...       | ... | ... |

## Dimension 1: [Name]
### [A]
### [B]

## Dimension 2: [Name]
...

## Verdict
[When to use A vs B — concrete, actionable]

## FAQ
...3-5 items
```

### Type 3: How-To Guide

**When:** Step-by-step instructions for a task.
**Examples:** Getting started, installation, configuration.

```
# How to [Achieve Goal] with [Product]

[Direct Answer: "[Product] lets you [goal] by [method].
Here's the quick version: [1-2 line summary of steps]."]

## Prerequisites
- [Requirement 1]
- [Requirement 2]

## Step 1: [Action]
[Instructions + code block if applicable]

## Step 2: [Action]
...

## Verification
[How to confirm it worked]

## Troubleshooting
| Symptom | Cause | Fix |
|---------|-------|-----|

## FAQ
...3-5 items
```

### Type 4: Product Announcement

**When:** New feature, version release, milestone.

```
# [Product] [Version]: [Headline Feature]

[Direct Answer: "[Product] [version] adds [feature], which [benefit].
Available now on [platforms]."]

## What's New
### [Feature 1]
### [Feature 2]

## How to Update
[One-command update instructions]

## FAQ
...2-3 items
```

### Type 5: Industry Insight / Thought Leadership

**When:** Analysis of trends, regulations, technologies in the VPN/privacy space.

```
# [Trend/Topic]: [Angle or Thesis]

[Direct Answer: State the thesis in 2 sentences.
Back it with one specific data point.]

## Background
## Analysis
## Implications for [Users/Industry]
## What We're Doing About It

## FAQ
...3-5 items
```

---

## SEO Optimization Checklist

Apply to EVERY article before publishing:

### Title (H1)
- [ ] Under 60 characters (SERP truncation boundary)
- [ ] Contains primary keyword near the front
- [ ] Uses power words or numbers when natural (e.g., "30% Packet Loss", "14 Scenarios")
- [ ] Matches user search intent (informational / navigational / comparison)

### Meta Description (summary field)
- [ ] 120-155 characters (Google snippet length)
- [ ] Contains primary keyword
- [ ] Includes a call-to-action or value proposition
- [ ] Reads as a complete sentence, not a keyword list

### Heading Hierarchy
- [ ] Single H1 (the title)
- [ ] H2 for major sections (3-7 per article)
- [ ] H3 for subsections within H2
- [ ] Headings contain secondary keywords naturally
- [ ] No skipped levels (H1 → H3 without H2)

### Internal Linking
- [ ] Link to 2-5 other kaitu.io pages using descriptive anchor text
- [ ] Use relative paths: `[k2cc](/k2/k2cc)` not full URLs
- [ ] Link from high-traffic pages to new content when relevant
- [ ] Every article has at least one "next reading" suggestion at the end

### Keywords
- [ ] Primary keyword in: title, first paragraph, one H2, meta description
- [ ] 2-3 secondary keywords distributed naturally through the body
- [ ] Long-tail variations included in FAQ questions
- [ ] No keyword stuffing — content reads naturally for humans first

### Images
- [ ] Cover image: 1200×630px for OG/Twitter cards
- [ ] Alt text on all images (descriptive, includes keyword if natural)
- [ ] Images saved to `web/public/images/content/`
- [ ] Referenced as `/images/content/filename.jpg`

---

## GEO / AEO Optimization (AI Search Engines)

These optimizations target AI-powered search: ChatGPT Search, Perplexity, Google AI Overview, Bing Copilot. The goal is to make your content **citable** — structured so AI can extract, quote, and attribute it.

### Rule 1: Direct Answer First (DAF)

Every article opens with a 2-3 sentence paragraph that directly answers the core question. No preamble, no "In this article we'll explore...".

**Bad:**
> In today's rapidly evolving internet landscape, VPN technology plays an increasingly important role...

**Good:**
> **k2cc is a congestion control algorithm that maintains full throughput at 30% packet loss** — where traditional algorithms like Cubic drop to under 10% of capacity. It distinguishes censorship-induced packet loss from genuine congestion, avoiding unnecessary speed reduction.

AI engines heavily favor the first substantive paragraph for citation. Make it count.

### Rule 2: Structured Comparison Tables

Comparison tables are the most-cited content type by AI search engines. Use markdown tables with clear headers:

```markdown
| Dimension | k2cc | Hysteria2 Brutal | BBR |
|-----------|------|-----------------|-----|
| Configuration | Zero-config | Manual bandwidth | Zero-config |
| Packet loss handling | Censorship-aware | Ignores all loss | Bandwidth estimation |
```

Rules:
- Column headers = entity names being compared
- Row headers = evaluation dimensions
- Cells = concise factual statements (not marketing language)
- Include a "verdict" row or paragraph after the table

### Rule 3: FAQ Section with Structured Data

Every article ends with a FAQ section. Questions should be:
- Written as actual user queries (how people search)
- Long-tail keywords (specific, conversational)
- Each answer is self-contained (makes sense without reading the article)

```markdown
## FAQ

**Should I use k2cc or Brutal for China?**

For networks with censorship interference (like China), k2cc is the better choice. Brutal's fixed-rate sending triggers retransmission storms under the 26% packet loss rate measured by USENIX Security 2023. k2cc's censorship-aware algorithm maintains effective throughput by distinguishing censorship drops from real congestion.

**Does k2cc require manual bandwidth configuration?**

No. k2cc is fully automatic — it continuously probes for the optimal send rate without any user-specified bandwidth parameters.
```

The k2 docs pages auto-generate `TechArticle` JSON-LD. Content pages (`/blog/*`, `/guides/*`) currently don't — but well-structured FAQ sections still help AI engines parse the content.

### Rule 4: Citable Facts and Statistics

Bold key data points so AI can extract them:

> k2cc maintains effective throughput at **26% packet loss** (USENIX Security 2023 measured value), where Cubic drops to **under 10%** of theoretical capacity.

Rules:
- Bold the specific number + unit + context
- Always attribute the source (paper name, organization, test conditions)
- Prefer absolute numbers over relative claims ("30% packet loss" not "high packet loss")

### Rule 5: E-E-A-T Signals (Experience, Expertise, Authority, Trust)

AI engines weight authoritative sources higher:
- **Cite academic papers** by name: "USENIX Security 2023", "RFC 8867"
- **Reference your own test methodology**: "14-scenario benchmark suite based on..."
- **Link to verifiable sources**: open-source code repos, published papers
- **Show domain expertise**: use precise technical terminology correctly
- **Attribute claims to data**: every performance claim references a test scenario or measurement

### Rule 6: Long-Tail Query Coverage

Each FAQ question targets a specific long-tail search query:
- "k2cc vs hysteria2 which is better for china" → FAQ item
- "does k2cc work with 30 percent packet loss" → FAQ item
- "how to set up k2 vpn on macos" → FAQ item

Think about what actual users type into ChatGPT or Perplexity, then answer that exact question.

---

## Writing Workflow

### Step 1: Determine Article Type and Target Keywords

Identify:
- Which of the 5 article types fits
- Primary keyword (what users search for)
- 2-3 secondary keywords
- 3-5 long-tail queries for FAQ section
- Target slug path (e.g., `blog/k2cc-explained` or `k2/vs-hysteria2`)

### Step 2: Write Primary Language (zh-CN)

Create `web/content/zh-CN/{slug}.md` with:
- Proper frontmatter (title, date, summary, tags)
- Article structure matching the type template
- SEO checklist applied
- GEO optimization applied (DAF, tables, FAQ, citable facts, E-E-A-T)

### Step 3: Translate (if multi-language)

Create additional files at the same slug path:
```
web/content/en-US/{slug}.md
web/content/ja/{slug}.md
...
```

Translation rules:
- Same slug across all locales
- Adapt keywords to target language search patterns (don't literal-translate SEO keywords)
- FAQ questions should reflect how users search in that language
- Maintain the same structure and factual content

### Step 4: Build Verification

```bash
cd web && yarn build
```

Must pass without errors. Velite compiles markdown at build time — any frontmatter errors will surface here.

### Step 5: Verify Sitemap

After build, check that the new page appears:
- Non-k2 content: priority 0.6 in sitemap
- k2/ content: priority 0.9 in sitemap
- All 7 locale variants should be present with hreflang alternates

### Step 6: Publish

Commit and push. Amplify auto-deploys from the `website` branch.

---

## Quality Gate

Before publishing, every article must pass:

| Check | Requirement |
|-------|-------------|
| Direct Answer | First paragraph answers the core question in ≤3 sentences |
| Title length | ≤60 characters |
| Summary length | 120-155 characters |
| H2 count | 3-7 sections |
| Internal links | ≥2 links to other kaitu.io pages |
| FAQ section | ≥3 Q&A pairs with long-tail keywords |
| Comparison table | Required for Type 2 articles; recommended for Type 1 |
| Citable facts | ≥2 bolded data points with attribution |
| Build passes | `cd web && yarn build` succeeds |
| No reserved slugs | Path doesn't conflict with app routes |

## Anti-Patterns

- **No fluff intros.** Never start with "In today's world..." or "As technology evolves..."
- **No keyword stuffing.** If it reads awkwardly, remove the keyword
- **No unsourced claims.** Every performance/comparison claim needs a reference
- **No orphan pages.** Every new article must be linked from at least one existing page
- **No marketing speak in technical articles.** Facts and data, not adjectives
- **No duplicate content across slugs.** One topic = one canonical slug
