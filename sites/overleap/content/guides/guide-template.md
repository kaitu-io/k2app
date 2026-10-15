---
title: Guide template (placeholder)
description: Placeholder guide that shows the layout, the updated date and the FAQ structured data. Replace it with the first real guide.
updated: 2026-10-15
draft: true
---

This is a placeholder. It is a draft, so a production build neither serves it nor lists it in the sitemap; `yarn dev` serves it at `/en-GB/guides/guide-template` for review.

Open with the answer in two or three sentences, then explain.

## A section heading

Body text. Headings start at `##` because the page renders the title as the only `<h1>`.

## Frequently asked questions

### Is this a real guide?

No. Every `### question` under this heading becomes one entry in the page's FAQPage structured data, with the text below it as the answer.

### How do I publish a guide?

Write `content/guides/<slug>.md`, set `draft: false`, and deploy.
