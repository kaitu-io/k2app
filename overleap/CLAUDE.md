# overleap

Everything Overleap-specific inside k2app: governance, product and design documents, and (later) the Box code.

Overleap is a **marketplace for home residential IPs**. A Host wires a Box (a Dell Wyse 3040 running Linux) to their home broadband and lists it. A Guest picks a Box and buys a monthly Seat on it. A Host is also a Guest. Revenue is split Platform 40% / Host 50% / Referrer 10%, and Hosts are paid in USDC. The company is a Wyoming, US company and runs at ultra-low cost. This product replaces the released Overleap VPN, which had no purchases.

## Read first
1. `CONSTITUTION.md` — the rules every design, change and release must obey.
2. `docs/glossary.md` — the only allowed names. Use them exactly; add an entry before inventing a term.

## Language
- Everything in this repository outside `research/` is in English. So is all code, every comment, commit message and `CLAUDE.md` in `k2app`.
- Talk to David in Chinese.

## Map
```
k2app/overleap/
├── CONSTITUTION.md         binding rules
├── docs/
│   ├── glossary.md         vocabulary
│   ├── decisions.md        standing decisions (append only)
│   ├── business-model.md   Marketplace, Seats, the three formulas, split, USDC payouts
│   ├── kyc-committee.md    Connection Records, Usage Undertaking, Identity Submission, Committee, Charter, public privacy wording
│   ├── growth.md           growth mechanisms built into the product; Seed Boxes → Guests → selling Boxes
│   ├── positioning.md      one-liner, audience, comparison, wording limits
│   ├── needs.md            demand evidence
│   ├── stack.md            technology selection, where the code lives, first spikes
│   ├── relay.md            design proposal: relay mode in the k2 protocol
│   ├── open-questions.md   what David still has to decide; what to verify and measure
│   └── hardware/box.md     the Box: hardware, wiring, system, operations, hardware checklist
└── research/               research originals and evidence (may be Chinese); file names carry the research date
```

## Where things are
This directory, `k2app/overleap/`, holds everything Overleap-specific: governance, product and design documents now; the Box Agent (`box/`), Image build (`image/`) and Update tooling (`ota/`) as they are written. The relay mode is added in `k2app/k2/`, Overleap's Center modules in `k2app/api/`, and the app is the existing client built with `BRAND=overleap`. See `docs/stack.md` and `docs/relay.md`.

The rules of `k2app/CLAUDE.md` apply here too, including work isolation (worktree + feature branch). No code has been written yet.

**This repository is public on GitHub.** David decided on 2026-11-11 that all Overleap material lives here and goes to the main branch. Everything committed under `overleap/` is public: never put secrets, personal data or anything about a specific Member in it.

## Working rules
- A decision goes into `docs/decisions.md` with the date and David's words. Fix every document it contradicts in the same change.
- Research used for a decision is saved under `research/` the day it is done. Numbers in `docs/` cite a research file or are marked as assumptions.
- Public privacy wording comes only from `docs/kyc-committee.md`, and only once the implementation and test exist.
- Nothing external mentions the sister brand.
- Implementation, launch, spending and external publication need David's explicit confirmation.
