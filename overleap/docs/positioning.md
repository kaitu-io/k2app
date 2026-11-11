# Positioning

*Revised 2026-11-10. Decisions: see docs/decisions.md. Why the old positioning (a privacy VPN for the UK) was dropped: see research/market-2026-10-15.md.*

## One line
**Go online like you're at home there.** The exit is a real home broadband line. You go online like an ordinary resident of that place: you are not treated as a bot, and you are not limited by the network where you are.

Selling point: **a clean, real home residential IP + privacy protection**. The Overleap that is live today (an ordinary VPN) has had no purchases. This is the new product that replaces it.

## Who it is for
**Guests**: people who are in place A and need to go online as someone in place B. The first groups are overseas Chinese and digital-nomad communities.
- Cross-border access to government records;
- Keeping an overseas bank account working as a local would;
- Cross-border streaming access;
- Running cross-border e-commerce accounts without the accounts being linked to each other.

**Hosts**: households that want other people to share the cost of their broadband bill. Some people will also do this as a business. A Host is also a Guest.

## Compared with whom

| | Big-brand VPNs (Nord, Surfshark, Express, Proton) | Commercial residential proxies (Bright Data, Oxylabs) | Decentralised VPNs (Mysterium) | Overleap |
|---|---|---|---|---|
| Exit IP | Datacenter IPs, shared by millions of people, often blocked | Residential IPs, mostly sourced from SDKs bundled inside other apps | Volunteers run software nodes themselves; quality is uneven | A dedicated Box wired to a real home broadband line; you choose which Box, and you can see how many people are on it |
| Aimed at | Ordinary people | Companies, data collection | Technical people | Ordinary people; install the app and it works |
| Identity | Email | Corporate KYC | Crypto wallet | No identity check by default; Sealed Connection Records and a Usage Undertaking protect the Host. A Host may require an Identity Submission, Sealed so that only the Committee can open it |
| Supply side | None | Mostly unaware | Install the software yourself, get paid in a token that falls in value | Buy a Box, plug in the Ethernet cable, get paid in USDC; the goal is to cover your own broadband bill |
| Price | Deep discount in year one, renewal costs 2–4× more | Per GB, expensive | By usage | Monthly Seat, priced by the country the Box is in, from about $8; the renewal price does not go up |

Moat: the hardware Box + the Marketplace + the personal relationships each Host brings in.

## How we say it (limits on copy)
- **We can say**: "not treated as a bot" (must be backed by public measured data), "real home broadband exit", "the renewal price does not go up".
- **How we talk about privacy**: follow the "How we say it externally" section of kyc-committee.md. "No logs of visited sites" is retired.
- **Privacy statements must match the code**: a statement can be written only after its implementation and test exist in the Overleap code.
- **We cannot say**: "circumvention / jump the firewall", "bypass age verification", "unbreakable", "untraceable / even we can't know".
- **Tone**: crisp and direct. No exaggeration, no scaring people.
