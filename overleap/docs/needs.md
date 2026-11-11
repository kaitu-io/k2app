# Needs

*Source: research/O1-voc-needs-2026-10-15.md. The sample is Reddit, 1–3 star App Store reviews, Trustpilot and HN from 2025-10 to 2026-10. 4 of the HN quotes were spot-checked against the source; all are real. Added to this: k2app's measurement of our own exits (10-15). Principle (David, 10-15): first see what the market wants, then decide how the technology does it.*

## What people want (by signal strength)

| # | Need | Confidence | Example quotes | Why the big brands do it badly |
|---|---|---|---|---|
| 1 | **Normal browsing with the VPN on** | High; mentioned in 26 of 30 sources | “almost impossible to browse internet normally with it”; “My bank app forces me to turn my VPN off.” (HN 2025-12-14) | Millions of people share one pool of datacenter IPs, and the exits sit in datacenter ranges that are often targeted |
| 2 | **Honest pricing** | High (14/30) | Renewal costs 2–4× more, charges continue after cancelling, refunds are hard to get | The business model is a low first-year price that is recovered on renewal |
| 3 | **Using "the network at home" while abroad** | High (10/30) | “now I have a Tailscale exit node at my mum's place in the UK it always works fine” (HN 2026-06-20) | Datacenter IPs are blocked most easily in exactly this scenario |
| 4 | **Sending only chosen sites through the VPN** | High (9/30) | After the UK OSA, people want only a few specific sites to go through the VPN | The split-tunnelling feature works the wrong way round and is buried deep |
| 5 | **Quiet, no upselling, honest status** | High (10/30) | “Most people just want something stable that quietly works in the background.” | They all pile features into a "security suite" and push upsells |
| 6 | A privacy tool that can be trusted (people new to VPNs after the UK OSA) | High | Fear of downloading a free VPN that is a scam | — |

## Anchored scenarios (requirements document 11-09; see docs/decisions.md)
- Cross-border access to government records;
- Keeping an overseas bank account working as a local would;
- Cross-border streaming access;
- Running cross-border e-commerce accounts without the accounts being linked to each other.

**Needs we do not market to**: bypassing age verification; hiding from an employer which country you are in; evading sanctions or gambling restrictions.

## Measurement of our own exits (k2app, 10-15, read-only)
- All 26 exits are datacenter IPs: 13 on AWS, 10 on Vultr, 3 on BandwagonHost. ip-api classifies all 26 as hosting, and proxycheck classifies all 26 as VPN.
- On the 23 AWS and Vultr exits, YouTube always demands “Sign in to confirm you're not a bot” and Reddit always returns 403. The control group, a home broadband line in Thailand, works normally.
- Conclusion: with datacenter exits we fail need 1 just as the big brands do. **Only changing where the IPs come from can solve it.** That is why the home network of Boxes exists.

## Search evidence (for long-tail SEO)
- Search phrases that actually appear in Google autocomplete: vpn captcha google, nordvpn captcha problem, nordvpn cloudflare captcha, proton vpn captcha not working, mullvad vpn captcha, vpn blocked by reddit / cloudflare / youtube, bank app not working with vpn, nord vpn renewal cost, surfshark vpn renewal price.
- On Bing all of these are below the reporting threshold: the intent is clear but the volume is small. So SEO is only a supporting channel.
