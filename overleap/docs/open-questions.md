# Open questions and prerequisite fixes

*Updated 2026-11-11. Decisions: see docs/decisions.md.*

## For David to decide
| # | Question | Suggestion |
|---|---|---|
| 1 | Who the two external Keyholders of the Committee should be | One independent person from the privacy field and one Host representative. During the seed period the Host representative seat may stay empty and be filled by a second external person |
| 2 | What happens when the Box a Guest chose goes offline (an earlier decision set one primary and two backups; see docs/decisions.md) | Switch automatically to a backup Box in the same country at a similar price. The money for that day is split by traffic and goes to the Box that actually served |
| 3 | Whether $39 is the right price for a Box, and how to ship it | Do not sell during the seed period. After that, first find someone in the Box's country to ship on our behalf |
| 4 | Which chain to use for USDC | Solana or Base. Decide after checking deposit support at the major exchanges |
| 5 | When advertising to overseas Chinese, whether to use the Chinese language (an earlier decision said no Chinese-language marketing; see docs/decisions.md) | Open |
| 6 | How a Guest in mainland China pays without a usable card. Stripe takes Visa, Mastercard and UnionPay cards; Alipay and WeChat Pay are ruled out (`research/stripe-china-collection-2026-11-11.md`) | Start with cards only and measure how many fail. Add stablecoin payment only if the number is large |

## Numbers to verify
- Monthly broadband price and typical Uplink in the UK, the US and other countries (the current $38 and $70 are impressions, not verified).
- Current prices of residential proxies such as Bright Data, and of static residential IPs.
- The fee rate for Stripe stablecoin payouts.
- For our own USDC transfers (D-40): which US exchange gives the company an account to buy USDC; the per-transfer cost on Base; how Payout Addresses are screened against the sanctions list.
- Whether Stripe accepts a VPN-type business at all (not on its restricted list as read; not confirmed with Stripe).
- Success rate of cards issued in mainland China at a US merchant of this kind.
- How long ISPs keep the record of "which household had this IP".

## To be measured
- The hardware test checklist in hardware/box.md.
- Direct Rate (a published study of libp2p hole punching found 70% ± 7.1%; ours is unmeasured) and the Relay cost per GB.
- Actual usage per Seat per month.

## Selection questions
See the end of stack.md: Guests inside heavily censored networks, where the code lives, and a Box to test on.

## Ask a lawyer once before launch
See the list at the end of kyc-committee.md.
