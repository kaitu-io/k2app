# Technology selection

*2026-11-10, third version. Direction for version 1 is decided (D-30, D-33, D-34): every Session is Relayed through the existing Nodes on the k2 protocol; the Box connects to Nodes with k2; Overleap is its own business with its own app.*

Status of each line: **Decided** (by David, see `decisions.md`), **Proposed** (my pick, waiting for David), or **Spike** (must be measured before it counts). Nothing here has been built or measured.

Evidence in `research/`:
- `k2-reverse-exit-feasibility-2026-11-10.md` — can a Node forward streams to a Box; what must change.
- `k2-code-evaluation-2026-11-10.md` — state of the existing code.
- `networking-stack-comparison-2026-11-10.md` — kernel WireGuard, Tailscale, NetBird (kept for version 2).
- `china-path-cc-kernel-nat-2026-11-10.md` — crossing the firewall, congestion control, kernels, NAT levers.
- `wyse-3040-linux-2026-11-10.md` — the hardware.

## 1. How the direction was reached
| Considered | Outcome |
|---|---|
| Kernel WireGuard on the Box with our own hole punching | Fastest data path, but plain WireGuard does not cross the Great Firewall, and Boxes in mainland China need the camouflaged transport on the Box itself. Moved to version 2 as the candidate for Direct Sessions |
| Tailscale's stack, NetBird | Not chosen: user-space speed risk and closed GUIs; AGPL server |
| A separate Overleap container on the Nodes, joined at the network layer, no change to k2 | Could not map a Guest to a Box without either one server instance per Box or a second tunnel inside the app |
| Selling through Access Partners | Dropped: Overleap sells on its own (D-33) |
| **Relay mode added to the k2 protocol** | **Decided (D-34).** Design: `relay.md` |

## 2. Connections
| Area | Pick | Status |
|---|---|---|
| Guest ↔ Node | Existing k2 protocol and client, unchanged | Decided |
| Box ↔ Node | k2 protocol, Box dials out (Box Link) | Decided |
| Relay mode | New Box mode, Box Link registry, Node-opened relayed streams; see `relay.md` | Proposed |
| Hole punching, Direct Sessions | Not in version 1 | Decided |
| Congestion control | Existing controller on Guest ↔ Node; a gentler profile on Box Links | Spike |

## 3. The Box
| Area | Pick | Status |
|---|---|---|
| Hardware | Dell Wyse 3040, x86-64, 2 GB RAM, 8 GB eMMC | Decided |
| Development target | PVE virtual machines sized like the Box | Decided (D-28) |
| Base OS | Debian 13 minimal | Proposed |
| Kernel | Linux 6.18 LTS, shipped inside the Image so it can be changed by an Update | Decided (D-29) |
| Boot loader | GRUB on the EFI removable-media path, slot state in `grubenv` | Spike |
| Update engine | RAUC, A/B Slots, signed bundles | Spike |
| Image build | mkosi + one assembly script | Proposed |
| Disk layout (8 GB) | ESP 256 MB · Slot A 1.5 GB · Slot B 1.5 GB · data about 4.5 GB | Proposed |
| Root file system | Read-only squashfs; tmpfs overlay; `noatime` ext4 data | Proposed |
| Box Agent | One Go program: Box Links, exit handler, per-Seat metering and Throttle, Egress Policy, Sealed Connection Records, Telemetry | Proposed |
| Host Priority | Kernel traffic shaper (`tc` with CAKE) under the Box Agent; Host chooses a share ratio (default 70%) | Spike |
| Gateway Mode | The existing Linux gateway (`k2r`), unchanged | Proposed |
| Box identity and Claim | Ed25519 key generated on first boot; QR code with a one-time claim code | Proposed |
| Logs and Telemetry | journald in memory; small batch to Center every minute | Proposed |
| Support Session | Outbound, started by the Host, auto-expiring | Proposed |

## 4. Platform
| Area | Pick | Status | Note |
|---|---|---|---|
| Guest app | The existing client built with `BRAND=overleap` | Proposed | Already builds; this is Overleap's own app |
| Control plane | The existing Center, Overleap brand, with new Overleap modules: Marketplace, Seats, ledgers, Claim, Box Link assignment | Proposed | "Use the existing architecture behind the scenes" (D-33) |
| Nodes | The existing roughly 40 Nodes | Decided | Relay bandwidth is a Platform cost |
| Formulas and parameters | One Go package, integer arithmetic; a config table | Proposed | Constitution art. 15–17 |
| Site, Marketplace, Host console | The existing overleap.io Next.js site, extended | Proposed | |

## 5. Money, identity, disclosure
| Area | Pick | Status |
|---|---|---|
| Collecting | Stripe Billing, web checkout, cards only (Visa, Mastercard, UnionPay). Alipay and WeChat Pay forbid VPN-type businesses and do not work with Billing (`research/stripe-china-collection-2026-11-11.md`) | Decided (D-21) |
| Identity | No vendor. Usage Undertaking at purchase; Identity Submission (document and live photo) built by us, Sealed at upload | Decided (D-39) |
| Connection Records | Written on the Node side and the Box side, with the Guest's Source Address; a hash of each batch goes to the Transparency Log | Decided (D-39) · design in `relay.md` |
| Payouts | Stripe stablecoin payouts in USDC where the recipient country is supported. Mainland China is not on Stripe's list (`research/stripe-china-collection-2026-11-11.md`): Hosts there are paid by our own USDC transfer, as a supplier payment | Decided (D-21, D-40) |
| Chain | Base, if major exchanges accept USDC deposits on it | Decided (D-21) · Spike |
| Sealing | Public-key encryption to the Committee key (X25519, `age` format) | Proposed |
| Committee key | Shamir 2-of-3; rebuilt offline for a Disclosure; new key each quarter | Decided (D-21) |
| Transparency Log | Safe 2-of-3 multisig on Base recording hashes | Proposed |

## 6. Where the code lives

**Decided (D-35): code and documents both live in `k2app/overleap/`.** The table records why.

| | In k2app (`k2app/overleap/`) | In a separate repository |
|---|---|---|
| Protocol changes | Made where the protocol lives; one change, one review, one test run | Impossible here; they must be made in k2app anyway |
| Box Agent | Imports the transport and exit handler directly | Needs k2 as an outside dependency. Its forked QUIC and smux libraries are wired in with `replace` lines that do not carry over to another repository, so every one must be copied and kept in step |
| Tests | The existing pre-merge suites and contract tests cover Node, Box and app together | Two repositories to keep in step for every protocol change |
| App, Center, site | Already there and already build for the Overleap brand | Would have to be copied or duplicated |
| Separation as a business | Unaffected: brand, app, accounts, pricing and documents stay separate | Same |
| Cost | The Box lives beside code written for a different product; k2app's rules apply (worktrees, its review gate) | — |

Layout inside k2app:
```
k2app/overleap/
├── CLAUDE.md      English
├── CONSTITUTION.md
├── docs/          product, business, design
├── research/      evidence (may be Chinese)
├── box/           Box Agent (Go)
├── image/         Image build: mkosi config, partition layout, RAUC bundle
└── ota/           Update channels and Rollout tooling
k2app/k2/          relay mode: transport, server, new Box entry point
k2app/api/         Overleap modules in Center
```

## 7. Spikes, in order
1. **Relay prototype.** QUIC only, TCP only: Guest → Node → Box on a real mainland China home line. Throughput with several Guests; forced TCP fallback; Box unplugged. (`relay.md`)
2. **Bootable Image on a PVE virtual machine.** Debian 13, kernel 6.18, read-only root, the Box Agent starting on boot.
3. **Safe Updates.** RAUC with GRUB: 10 A/B switches and one broken Image that must roll back.
4. **Host Priority.** The shaper holding the Host's share at full Uplink; CPU cost.
5. **Real hardware.** On a Wyse 3040: headless CPU frequency, 100 reboots, 10 power cuts, 72 hours under load, throughput of the Box Agent.
6. **Accounts** (no hardware): Stripe stablecoin payout fee and limits; whether Stripe accepts our business type; USDC-on-Base deposit support.

## 8. Open questions for David
1. `relay.md`: the direction is agreed (D-36); the details are refined through the first prototype.
2. How a Guest in mainland China pays when they have no usable card (see `open-questions.md`).
3. The k2app repository is public on GitHub. Which of these documents may be committed and pushed there?
