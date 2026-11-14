# Business model

*Final as of 2026-11-10. Decisions: see docs/decisions.md. The two earlier versions, "peer exchange + points" and "$7.99 for the whole network", are void. All price, usage and cost figures are estimates and must be calibrated with measurements from Seed Boxes. Anything marked "unverified" must not be used as a basis. Sources: research/stablecoin-payouts-2026-11-10.md (settlement), research/token-issuance-2026-11-10.md (token).*

## What it is
The live Overleap has been released but has no purchases. The product stops being one more VPN like those already on the market. It becomes home IPs and a Box.

It is a **Marketplace**: a Host lists a Box that is wired to their home broadband. A Guest chooses one Box and buys a Seat on it by the month. The Platform is the seller: the Guest buys the Seat from the Platform and all of the payment is the Platform's revenue. Payouts to Hosts and Referrers are the Platform's cost of supply, computed with the formulas below; they are not a profit share and not the Guest's money passed on (D-40).

| Role | What they do |
|---|---|
| Host | Buys a Box and lists it on the Marketplace: country, price, how many Guests it takes. Most Hosts only want to cover their broadband bill. Some do this as a business, lower the price and take more Guests |
| Guest | Chooses a Box on the Marketplace and buys a Seat. Accepts the Usage Undertaking; an Identity Submission is needed only after an Account Freeze or where the Host requires it (see kyc-committee.md) |
| Referrer | The Member who brought the Guest in. Can be the Host |
| Platform | Marketplace, collecting payment, Payouts, connections and the Relay, Connection Records and the Committee, support |

A Host is both a seller and a buyer. A Host who wants to use someone else's Box buys a Seat on the Marketplace like everyone else, and can pay for it directly from their Balance.

## What is sold: monthly Seats with a Data Allowance

| Seat Type | Data Allowance | Load Units | For whom |
|---|---|---|---|
| `lite` | 5 GB/month | 0.33 | People who only use bank and government websites |
| `standard` | 50 GB/month | 1 | Everyday browsing plus some video |
| `dedicated` | 200 GB/month, and the IP is yours alone | 1.5 | People who need a fixed, clean IP, such as cross-border e-commerce sellers |

- A Seat that goes over its Data Allowance stays connected. It gets a Throttle to 256 kbps. The Guest can buy a Top-up.
- No unlimited Seats: video watchers would fill the Host's line, and the Host would unplug the Box.
- No pure per-GB billing: Guests cannot work out what they will spend, so they do not buy.
- No day/night pricing: the Box gives the Host's own traffic priority and limits the shared speed, and the list shows the current speed of each Box. Guests will choose off-peak Boxes by themselves.

## The three formulas

```
reference_price = broadband_monthly ÷ target_load ÷ 0.5 × load_units

sellable_load   = min(5, uplink_mbps × 0.7 ÷ 5)

host_earnings   = Σ seat_revenue × 0.5
                + Σ referred_seat_revenue × 0.1
                − relayed_gb × 0.01
```

- `broadband_monthly` is the local monthly broadband bill. `load_units` is the Load Units of the Seat.
- `referred_seat_revenue` is the Seat Revenue of Seats the Host brought in as Referrer.
- `relayed_gb` counts only Relayed traffic whose cause was the Box side, at $0.01/GB, with a limit per Seat. See "Shares and Relay Fee" below.

### Reference Price
- Principle (David): when a Box reaches Target Load, the Host receives enough to pay their home broadband bill.
- Target Load is set per country: 1.5 in countries with cheap broadband; 3 for now in countries with expensive broadband (at 1.5 the resulting price would find no buyers).
- A Host can set the Host Price anywhere between 0.5× and 2× the Reference Price.

| Country of the Box | Monthly broadband bill | Target Load | `lite` | `standard` | `dedicated` |
|---|---|---|---|---|---|
| Thailand | about $18 (125 RMB; David, 2026-11-10) | 1.5 | $8 | $24 | $36 |
| United Kingdom | about $38 (unverified) | 3 | $8 | $25 | $38 |
| United States | about $70 (unverified) | 3 | $15 | $47 | $70 |

### Sellable Load
- It depends on Uplink, not on download speed: when someone else goes out through a Box, the data is sent over that home's uplink.
- The Platform measures the speed regularly. The Host does not enter it. Each Load Unit reserves 5 Mbps, and 30% is kept for the Host.
- An Uplink of 10 Mbps can sell only 1.4 Load Units, 20 Mbps sells 2.8, and 36 Mbps or more is capped at 5.
- Hosts must be told honestly: in a country where Target Load is 3, an Uplink below 22 Mbps cannot cover the broadband bill.

### Shares and Relay Fee
| Goes to | Share |
|---|---|
| Platform | 40% |
| Host | 50% |
| Referrer | 10% (for as long as the Guest keeps paying) |

- When there is no Referrer, that 10% goes to the Platform. For a Guest the Host brought in, the Host gets 60% in total.
- **The Relay Fee is deducted by the traffic that was actually Relayed** (David, 2026-11-10). Hole punching happens per Session; the same Box sometimes gets through and sometimes does not. When a Session was Relayed and the network on the Box side was the one that could not be reached at that time, that traffic is deducted from Host Earnings at $0.01/GB. When the cause was on the Guest side, the Platform bears it.
- The Relay Fee deducted for one Seat is never more than 20% of the Host Share of that Seat.
- $0.01/GB is a starting value taken from the old estimate ($0.003–0.014 per GB). It is adjusted to measured cost, and the rate is public.
- The Marketplace list shows the Direct Rate of each Box. A Host who opens a port on the home router can raise the Direct Rate and have less deducted.
- Every month the Host sees a Statement in the app: for each Seat, how much was collected, how much the Host received, and how much Relay Fee was deducted.

## The Platform's money
- The Platform gets $9–19 per `standard` Seat. Card processing (Stripe US standard rate 2.9% + $0.30) and Relayed traffic caused by the Guest side each take only a small part of it. There is no per-Guest identity cost (D-39).
- **Subscriptions are sold on the web**: in-app purchase through Apple or Google would take 15%–30%.
- The 30%-off first month is paid for out of the Platform Share. Seat Revenue is still counted at the full Host Price, so Host Share and Referral Share are not reduced.
- Box: purchase cost 100–140 RMB (about $14–20); proposed selling price $39. The margin pays for pre-installing the Image, shipping and replacing faulty units. The first 10 units are Seed Boxes: the Platform places them itself and does not sell them.

## Settlement (Wyoming, US company)
- **Collecting**: mainly bank cards; USDC is optional.
- **Paying**: USDC only, on one chain only (Solana or Base, decided after checking which one exchanges accept for deposits). Payouts are made in a monthly batch once the Balance reaches $10.
- **Channel**: first choice is Stripe's stablecoin payouts (available to US companies, in public preview, fee not found). The Platform then holds no USDC itself. Where Stripe does not cover the recipient's country, the Platform buys USDC and sends it in batches itself. Mainland China is such a country (`research/stripe-china-collection-2026-11-11.md`). Turning USDC into local currency is the Host's own matter.
- **Hosts who cannot use a crypto wallet**: they register with an exchange in their own country and paste its USDC deposit address into the app.
- **To be built**: collecting Payout Addresses and verifying them with a small transfer, sanctions-list screening, the payout ledger, W-9 / W-8 tax forms for Hosts.
- **Contribution Ledger**: for each Box it records time, traffic, uptime, region and amount paid. It cannot be altered.

## Token
None now; this is a matter for much later (see docs/decisions.md). The Platform runs at very low cost and cannot afford the legal cost of issuing a token in a compliant way. Almost all comparable bandwidth tokens have fallen by more than 90%, and Host earnings fell with the token price. Paying in USDC is itself a point of difference. The Contribution Ledger is only a service record. Nothing external mentions a "future token" or an "airdrop".

## Adjustable parameters
| Parameter | Starting value |
|---|---|
| Target Load (per country) | 1.5 / 3 |
| Host Price range | 0.5×–2× the Reference Price |
| Maximum Load of one Box | 5 Load Units |
| Uplink reserved per Load Unit | 5 Mbps |
| Data Allowance of the three Seat Types | 5 / 50 / 200 GB |
| Relay Fee rate and cap | 0 in version 1 (every Session is Relayed); later $0.01/GB, 20% |
| First-month discount | 30% off |
| Referral Share | 10% |

## Risks
1. **Expensive countries do not sell**: prices in the UK and the US are still several times those of an ordinary VPN. Buyers will be few and specific.
2. **Many Boxes, unsold Seats**: a Host who cannot cover their broadband bill will leave. The pace of selling Boxes must follow the number of Guests.
3. **Faked traffic**: the Platform cannot see Direct traffic. It relies on each end reporting separately and then cross-checking the two.
4. **Websites flag the IP**: too many people on one IP. The Load limit of 5 is a starting value and depends on measurements from Seed Boxes.

## What we do not do
- Publish Box IP addresses.
- Sell egress in bulk by traffic to data-collection companies.
- Advertise "bypassing age verification".
- Retain a law firm or spend large amounts on legal work (see docs/decisions.md).
