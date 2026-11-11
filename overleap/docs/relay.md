# Relay: design proposal

*2026-11-10. **Proposal, waiting for David.** Nothing is built. Based on a read-only study of the k2 code: `research/k2-reverse-exit-feasibility-2026-11-10.md` (file and line references are there). Effort figures are that study's estimates. Decisions: D-30, D-33, D-34.*

## Goal
A Guest uses the existing app. Their traffic leaves the internet from a Box in someone's home. The Box may be anywhere, including mainland China, and never accepts an inbound connection.

```
Guest app ──k2──> Node ──k2 (Box Link, opened by the Box)──> Box ──> home broadband ──> destination
```

Version 1 has no hole punching: every Session is Relayed through a Node.

## Why this shape
- One protocol for every case: Guest in China and Box abroad, Guest abroad and Box in China, both abroad.
- The Box only dials out, so it needs no router setup and always connects.
- Camouflage and the custom congestion controller are used as they are, on the legs they were built for.
- The Guest app needs no change: which Box a Guest exits through is decided on the server side.

## What exists and what is missing (from the code study)

| Exists | Missing |
|---|---|
| Stream framing with a type byte and a destination `host:port` | A way for a Node to open a proxy stream towards a connected peer (only one server-opened stream type exists today) |
| Authentication of a connection against Center | A connection mode that marks the peer as a Box, and a registry of linked Boxes on the Node |
| An exit handler: dials destinations, blocks reserved addresses and plaintext BitTorrent, meters per device | An outbound abstraction on the Node; today it dials the destination directly in two fixed places |
| Wrappers that put either role on an existing QUIC connection | Guest identity on each relayed stream, so the Box can meter and record per Guest |

## Protocol additions

1. **Box mode.** A new connection mode in the metadata stream. The Box proves its identity with its own key; Center confirms the Box is Claimed. A Box-mode connection never gets normal exit rights on the Node.
2. **Box Link registry.** The Node keeps a table: Box → live Box Link.
3. **Relayed streams.** Two new stream types, opened by the Node towards the Box: relayed TCP and relayed UDP. Header: Seat identifier, then the destination exactly as the Guest sent it.
4. **Exit selection.** When a Guest connects, Center's verdict names the Box for that Seat. The Guest cannot choose or forge it. For such a Guest the Node never dials a destination itself.
5. **Fail closed.** If the Box Link is down, the Guest's streams fail. Traffic never falls back to the Node's own datacenter address.
6. **Which Node.** A Box keeps Box Links to two or three Nodes near it. Center tells the Guest app which Nodes those are. No Node-to-Node forwarding in version 1.

## On the Box
One program, built from the existing transport and exit handler:
- dials Nodes and keeps the Box Links alive;
- accepts relayed streams and dials the destination from the home connection;
- per Seat: counts traffic, enforces the Data Allowance and the Throttle;
- applies the Egress Policy;
- writes a Sealed Connection Record for each stream (Seat, time, Destination);
- answers the Guest's DNS itself, so that records carry domain names and not only IP addresses.

## Connection Records on both sides (D-39)
The Box is in the Host's hands, so a record written only there cannot prove the traffic was not the Host's own. The Node is the second witness:
- the Node writes a Sealed Connection Record for each relayed stream: Member, Seat, Box, the Guest's Source Address, time, Destination;
- the Box writes its own, as above;
- a hash of each batch from both sides goes to the Transparency Log, so that records cannot be added or altered afterwards.

The Node still never writes a Destination anywhere else (Constitution art. 10). Batch size, the hashing scheme and what happens when the two sides disagree are open details.

Host Priority is enforced below the program with the kernel's traffic shaper, so the Host's own traffic always wins whatever the program does.

## Where limits are applied
The rule: stop excess traffic at the first hop of each direction, so it never uses a leg that someone pays for.

| Direction | Path | Enforced at | Why |
|---|---|---|---|
| Download (most traffic) | destination → Box → Node → Guest | **The Box**, before data enters the Box Link | Saves the Host's Uplink and the Node's bandwidth at once |
| Upload | Guest → Node → Box → destination | **The Node**, as data arrives from the Guest | Saves Node → Box bandwidth and the Host's downlink |

- For TCP, slowing the reader pushes back on the sender through flow control; nothing is thrown away.
- UDP has no push-back, so excess packets are dropped at that same first hop.
- The Box is the authority on a Seat's Data Allowance and Throttle (it must work when Center is unreachable). The Node applies the same per-Seat ceiling as a second guard, so a faulty Box cannot make the Node carry more.
- This applies to the Throttle (256 kbps after the Data Allowance) and to the per-Seat speed ceiling alike.

## Risks the design must answer

| Risk | Proposed answer | Status |
|---|---|---|
| Guests on one Box slow each other down: they share one connection and its flow-control window | One Box Link per active Seat instead of one per Box | To prototype |
| A Box in China may be stuck on the TCP fallback, which stacks TCP on TCP | Measure first; if bad, keep several fallback connections and spread streams across them | To measure |
| The congestion controller now sends from the Box up a home uplink, a direction it was never tuned for | A separate, gentler profile for Box Links, capped by the measured Uplink share | To measure |
| A Guest's traffic exits through the wrong Box | Mapping comes only from Center's verdict; test that a forged value is ignored | Design rule |
| Nodes log every destination in plaintext today | Relayed streams must not be logged on the Node; a test guards it (Constitution art. 10) | Required before launch |
| Box Link drops | Streams in flight end; the Box redials; the app shows the Box as offline rather than failing silently | To prototype |
| UDP: nested datagram size | Lower the inner packet size for relayed UDP; push oversized datagrams to the stream path | To prototype |

## Effort (the study's estimate, not measured)
- Node and transport changes: about 300–500 lines plus tests.
- Box program: about 200–400 lines on top of existing packages.
- Total: medium, about 2–4 weeks. A QUIC-only, TCP-only demonstration is possible in days.

## First prototype
QUIC only, TCP only: Guest → Node → a Box on a real mainland China home line. Measure:
1. throughput with several Guests downloading at once, and how much they slow each other;
2. behaviour when forced onto the TCP fallback;
3. what the Guest sees when the Box is unplugged.

## Not in version 1
Direct Sessions and hole punching; kernel WireGuard on the Box; Node-to-Node forwarding.
