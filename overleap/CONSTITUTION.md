# Overleap Constitution

These rules bind every design, every line of code and every release. A change that breaks an article is rejected, however good it is otherwise. To change an article, David decides, the decision is recorded in `docs/decisions.md`, and this file is edited in the same commit.

Terms in capitals are defined in `docs/glossary.md`.

## I. Language and names

1. Everything developers read is in English: code, comments, commit messages, API fields, config keys, log messages, tests, specs, this repository's `docs/` and every `CLAUDE.md`.
2. Conversation with David may be in Chinese. Files under `research/` may be in any language.
3. One concept, one name. Use the terms in `docs/glossary.md` exactly. A new concept gets a glossary entry before it gets code.

## II. The Host comes first

4. When interests conflict, protect the Host. The product exists only while Hosts keep their Boxes plugged in.
5. A Host can always perform a Cut-off instantly, and can always get a Host Attestation without anyone's approval.
6. Host Priority is enforced on the Box, not promised in copy: the Host's own traffic goes first and shared traffic never exceeds the configured share of Uplink.
7. Everything a Host is told about earnings is honest: figures are measured, the formulas are public, and a Host who cannot reach Target Load is told so before buying a Box.

## III. Privacy claims are code

8. Every public privacy statement maps to an implementation and an automated test. No test, no claim.
9. The Platform alone must never be able to read a Sealed Identity or a Connection Record. No code path, key, backup, debug flag or admin tool may break this. Disclosure needs two of three Keyholders.
10. Destinations exist in exactly one place: Sealed inside Connection Records. They never appear in Telemetry, operational logs, metrics, traces, crash reports or analytics. A test guards this.
11. Content of traffic is never inspected or stored.
12. Disclosure happens only under the Charter, and every request and vote is written to the Transparency Log.
13. We say what is true: US courts can compel US Keyholders; Guests are not identity-checked by default; an Identity Submission passes through our code at upload and only the Sealed copy is kept; payment records sit unsealed at the payment processor, so we never say the Platform does not know who pays.

## IV. Money

14. The Platform pays out only from money it has collected. No fixed per-GB promises, no advances, no guarantees of earnings.
15. The three formulas (Reference Price, Sellable Load, Host Earnings) live in one module, are versioned, and are shown to Members exactly as computed.
16. Every economic parameter is configuration, not code: shares, Target Load per country, Data Allowances, Relay Fee rate and cap, discounts.
17. Money is stored as integers in the smallest unit. Never floating point.
18. The Contribution Ledger and the payout ledger are append-only. Corrections are new entries.
19. Speed, traffic, Direct Rate and uptime are measured by the Platform. Self-reported values are never used for pricing or payment.
20. No token. The Contribution Ledger is a service record with no promised value. Nothing public mentions tokens or airdrops.

## V. The Box is an appliance

21. Zero touch: a Host plugs in power and Ethernet and scans a QR code. No screen, no keyboard, no router configuration required.
22. A Box only makes outbound connections. It never requires an open inbound port.
23. The system partition is read-only. All Updates are signed, written to the inactive Slot, health-checked, and rolled back automatically on failure. An Update must never leave a Box unreachable.
24. A Box recovers by itself from power loss, network loss and crashes.
25. Nobody enters a Box remotely unless the Host has started a Support Session. Sessions expire automatically and are logged.
26. Persistent writes are minimised to protect the eMMC. Logs live in memory.
27. The Box enforces the Egress Policy, Data Allowances and Throttle locally. It must behave correctly when Center is unreachable.

## VI. Releases and compatibility

28. Rollout order is fixed: Seed Boxes, then 10% of Boxes, then all. Each stage has written halt criteria (failed Updates, offline rate, crash rate) that stop the Rollout automatically.
29. Center is deployed before any Box or app feature that depends on it. Center keeps serving the previous two Box Agent versions.
30. Every Box ↔ Center and app ↔ Center interface is versioned and has a committed contract test.
31. No Image is released without passing the hardware checklist on a real Wyse 3040.
32. Every release can be rolled back, and the rollback has been tried.

## VII. Lean

33. We run at near-zero fixed cost. A component with a recurring bill needs a written reason and David's approval.
34. Prefer boring, well-understood technology and what already exists. One way to do each thing. Version 1 runs on the existing k2 protocol, Nodes, Center and app; add to them rather than build beside them.
35. No paid legal retainer. Read public sources first; collect legal questions and ask once before launch.

## VIII. Growth is part of the design

36. Every Host-facing feature answers: how does this help a Host earn, see what they earn, or bring in a Guest?
37. Every Guest-facing feature answers: how does this make choosing and paying for a Seat simpler?
38. Prices are understandable in one screen: three Seat Types, one monthly price each. No time-of-day pricing, no per-GB billing.
39. Subscriptions are sold on the web, not through app-store billing.

## IX. Evidence and decisions

40. A number in a document cites a file in `research/` or is marked as an assumption. Research used for a decision is saved in `research/` on the day it is done.
41. Decisions go in `docs/decisions.md` with the date and David's own words. Documents that contradict a new decision are fixed in the same change.
42. Implementation, launch, spending money and anything published externally require David's explicit confirmation.

## X. Boundaries

43. Brand isolation: nothing external mentions the sister brand. It may later buy capacity as an internal customer.
44. Legal risk of hosting is assessed by the Host. We disclose honestly (including ISP terms of service) and do not argue Hosts out of it.
45. We respond only to Valid US Legal Process. Requests sent directly by any other government are declined without exception.
46. We do not publish Box IP addresses, do not sell bulk traffic to data-collection companies, and do not advertise age-verification bypass.

## XI. Engineering practice

47. Tests first. A bug fix starts with a failing test.
48. Secrets never enter a repository. Each Box generates its own identity key on first boot; the private key never leaves the Box.
49. Work happens in an isolated branch or worktree and reaches the main branch through review.
50. Overleap is a separate business: its own brand, app, accounts, pricing and documents. Behind the scenes it runs on the existing architecture. Code and documents live in `k2app/overleap/`.
51. Technology is chosen on evidence: a comparison saved in `research/` and, for anything on the data path, a measurement on a real Box.
