# Growth path

*Final as of 2026-11-10. Decisions: see docs/decisions.md. Principle (David): growth is built into the product design. "Who is using my network" is a strong personal tie, so Hosts will promote it themselves.*

## Growth mechanisms built into the product
| Mechanism | How it works |
|---|---|
| A Host who brings in Guests earns more | For a Guest the Host brought in, the Host gets 60% (50% + 10% Referral Share), for as long as the Guest keeps paying |
| The invite link goes straight to the Host's own Box | Someone who follows a Host's link lands directly on the Listing of that Host's Box |
| A Box comes with invite places | Each Box includes a few "first month 30% off" places, so the Host has something to give away from day one |
| Earnings are visible | App home screen: how much was earned this month, how much is still missing to cover the broadband bill, how many people are on the Box, how much traffic it carried |
| Shareable earnings card | One tap creates an image saying "My Box earned $X this month", with the invite link |
| Guests become Hosts | A Guest who has paid for a few months gets a discount on a Box |
| Gap map | Publish which countries are short of Boxes and how much a Box earns there on average, to steer new Hosts to the places that lack Boxes |
| The Marketplace itself | Every Box has a Listing: country, measured Uplink, Direct Rate, price, uptime. Hosts who do this as a business will post it everywhere themselves |

## Will overseas households be willing to be Hosts (research, 11-10)
Source: research/overseas-host-side-income-2026-11-10.md. Most figures come from vendors' own statements or promotional blogs. Use them for order of magnitude only.

- **The habit exists.** Grass has about 2.5–3 million active nodes. Helium peaked at about 850,000–980,000 hotspots. 27% of US adults have a side job; a survey in the Philippines says 75%.
- **But in similar products one device earns only $1–15 per month.** We need to deliver $10–50, and that depends on Seats actually selling.
- **Helium and Grass grew on the expectation that the token would rise.** We pay in stablecoin and do not have this lever. We have to replace it with "it covers your broadband bill" and "the Host can use the network too".
- **The biggest difference from the bandwidth-sharing boxes in China**: those carry the operator's own content, and the household is not responsible for strangers' traffic. We are an exit, and this is exactly what Western households fear most (there are real cases of homes being searched for running a Tor exit). Connection Records, the Usage Undertaking, the Host Attestation and the Host's option to require an Identity Submission are the answer to this question. They must be on the first screen a Host sees.
- **ISP terms for home broadband in most countries forbid reselling or sharing.** Doing so is a breach of contract; enforcement is mostly case by case. The Host Notice states this honestly.

**Which people to look for first as Hosts (a judgement from the research, to be verified):**
1. Home-server hobbyists and "earn pocket money" communities in the US: the largest communities, and US IPs are worth the most.
2. Spain, Portugal, Romania: good fibre Uplink, middle incomes, so covering the broadband bill is attractive.
3. The Philippines: the strongest side-job culture, plus demand on both ends from the overseas diaspora.
4. Vietnam, Indonesia: the highest density of this kind of node, and USDC is accepted; we must guard against people running many instances to farm payments.
5. The UK and Germany come later: this amount of money is less attractive there, and Germany has the highest risk of copyright claim letters.

We found no survey at all on "how much per month makes it worth doing" or on "would you rent out your home network". We can only find out by trying.

## Phase 0: prerequisites
- Test the first 10 Wyse 3040 units on real hardware, following the checklist in hardware/box.md.
- Run the spikes in stack.md and settle the networking stack.
- Build Connection Records, the Usage Undertaking, Identity Submission and the Committee mechanism, and finalise the Charter. Only then can the matching promises be written externally.
- Rewrite the overleap.io copy for the new positioning (the live site is still the old product).

## Phase 1: Seed Boxes run by us
- The team places the first 10 units in real homes, either with friends and family or rented. Start with countries where broadband is cheap and Uplink is good (for example Thailand), then the UK and the US.
- **What must be measured**:
  - The share of target sites (government, banks, streaming, YouTube, Reddit) that do not block us;
  - How many days it takes for a home IP to be flagged by IP-intelligence databases;
  - Direct Rate, and the Relay cost per GB;
  - How many GB each Seat actually uses per month, and how much of the Host's Uplink it takes at evening peak;
  - The sales rate of each Seat Type in each country.
- **Stop condition**: home IPs are generally flagged within 7 days.

## Phase 2: Guests
- Aimed at overseas Chinese and digital-nomad communities.
- The entry product is a `lite` Seat in a cheap country (about $8/month): log in to bank and government sites of your home country.
- Scenarios: cross-border access to government records, keeping an overseas bank account working, streaming, cross-border e-commerce accounts.

## Phase 3: selling Boxes
- Sell Boxes externally only after the Seed Box data is in (how much a Box earns per month, how long until it pays for itself).
- The pace of Box sales follows the number of Guests: if there are many Boxes and the Seats do not sell, Hosts cannot cover their broadband bill and will leave.

## Supporting work
- Public dashboard: every day, measure the CAPTCHA rate and block rate of Boxes in each country on the major sites.
- Show HN: publish the measurement data, the Committee's Charter and the record of Disclosures.
- overleap.io/guides: long-tail SEO. The list of search phrases is in needs.md.

## What we do not do
- Pay review sites for rankings, or bid on big keywords such as "best vpn";
- Post on Reddit with sock-puppet accounts;
- Mention the sister brand (brand isolation);
- Mention a "future token" or an "airdrop".

## Metrics
- Load per Box (goal: reach the Target Load of its country) and the Host's monthly earnings.
- Number of Boxes, number of Seats and vacancy rate, per country.
- Referral coefficient: how many Guests each Host brings in on average; the share of Guests who become Hosts.
- Guest retention; Host uptime.
