# Connection Records, identity and the Committee

*Updated 2026-11-11 for D-39. Decisions: see docs/decisions.md. There is only one purpose: to protect the Host. When someone does something bad through a Host's network, it must be possible to show it was not the Host, and to give a lawful request something to follow. Research: research/kyc-conditional-decryption-2026-11-10.md, research/identity-without-documents-2026-11-10.md. Not legal advice.*

## Two levels (D-39)

| | Default | Second level |
|---|---|---|
| What the Guest gives | Nothing beyond an account and a payment. Accepts the Usage Undertaking when buying a Seat | An Identity Submission: an identity document and a photo taken live with the camera |
| When | Every Seat | To lift an Account Freeze; and before buying a Seat on a Box whose Host requires it |
| What protects the Host | Connection Records, the Usage Undertaking, Cut-off, Host Attestation | The same, plus a Sealed Identity |

No US federal or Wyoming law was found that requires a VPN or proxy service to identify its customers, so the level is ours to choose. What is mandatory is sanctions compliance: block sanctioned regions and keep records.

No third-party identity vendor is used. Stripe collects payment and plays no part in identity.

## Connection Records
- **What is recorded**: Member, Seat, Box, the Guest's Source Address, time, and the Destinations (domain and IP). The content of what was accessed is not recorded.
- **Why the Source Address**: it shows the traffic came from another household and another network, which is what answers "was the Guest really the Host?". It also gives a lawful request something to follow without the Guest handing over any document. Limit: a Guest who connects through another VPN shows that VPN's address.
- **Why domain and IP**: one Box carries several people at the same time, so time alone cannot tell them apart. A request that comes in says "at this moment, this website was accessed".
- **Two witnesses**: the Box is in the Host's hands, so the Node writes a record too. A hash of each batch from both sides goes to the Transparency Log, so records cannot be added or altered afterwards. Design: relay.md.
- **How it is stored**: Sealed with the Committee's public key at the moment of writing. The Platform alone cannot read it.
- **How long it is kept**: 24 months. Police and copyright holders often come only several months after the event.
- **Operational logs never contain domains or IPs** (see hardware/box.md). This kind of information travels only through this one path.

## The Usage Undertaking
- Accepted by the Guest when buying a Seat, for the country that Box is in: obey that country's law; if not, the Guest bears the liability and compensates the Host.
- It is made to the Platform, with the Host as beneficiary. Guest and Host never learn each other's name or address.
- What it does: shows the Host allowed lawful use only, and puts the Guest on notice.
- What it does not do: criminal liability cannot be moved by contract, and it does not cure a breach of the Host's ISP terms.
- The acceptance (time, version of the text, Seat) is stored with the account.

## The Identity Submission
1. The Guest uploads an identity document and takes a photo with the camera on the page. The photo must show a random code or gesture given at that moment, so an old picture cannot be reused.
2. Our code checks in memory that a face and the code are present, encrypts both files with the Committee's public key, and discards the originals.
3. The Platform stores only the ciphertext (the Sealed Identity). No face template is computed and no automatic face matching is done.
4. The document is not checked against any registry. We say "submitted", never "verified".

A Host has to receive Payouts and submit tax forms, so a Host is already identified to the Platform.

## Protections for the Host
1. **Real time**: a Host can perform a Cut-off on a Session. The Platform applies an Account Freeze to that account at the same time. This does not wait for the Committee. The freeze is lifted only after an Identity Submission and review.
2. **Host Attestation**: the Host enters a time in the app, and the Platform issues a signed Host Attestation: "this Box was carrying third-party traffic at that time". It does not say who, and it needs no Disclosure.
3. **Host's choice**: a Host can set a Box to accept only Guests with a Sealed Identity. The Listing shows this.

To find out who it was, the requesting party must go through legal process under the Charter below.

## What the Host must be told (Host Notice)
- By default Guests are not identity-checked.
- What we guarantee is a record showing the traffic was not the Host's, with a trail to the Guest's Source Address, and an Identity Submission after any Cut-off.
- Records do not stop an investigator from coming to the Host first; they are what the Host shows afterwards.

## The Committee
- **Three people each hold one key share; two must agree before anything can be unsealed.** One Keyholder from the Platform and two external Keyholders (for example, an independent person from the privacy field, and a representative chosen by the Host community).
- Keyholders are publicly named. No law firm is retained and no retainer is paid (see docs/decisions.md).
- The people are still to be decided.

## The Charter: respond only to Valid US Legal Process
The Committee answers only one question: someone asks for a Guest's identity or records; is it given or not.

| Who asks | What happens |
|---|---|
| An order, subpoena or search warrant issued by a US court | The Platform is a US company and complies with the law. Once the document is verified as genuine, the Disclosure is made |
| A foreign government writing directly | This has no binding force on a US company. Always declined, whatever the country |
| A foreign government going through the mutual legal assistance channel with the US | A US court reviews it under US law. If it passes, it becomes a US court document and is handled as in the first row |

- Whether the data "should be given" is judged by the US court. The Committee only verifies that the document is genuine and checks it against the Charter. This is why a small Committee can also be fast.
- Cases where someone was convicted for reasons such as belief do not get past the US court.
- Sanctioned countries (such as North Korea) cannot be served in the first place.

## Where the chain is used
- The contract enforces "two signatures required".
- The hash of every request and every vote is public forever (the Transparency Log). Anyone can check how many Disclosures have been made. A transparency report is published regularly.
- Community voting is used only for slow matters: changing the Charter, choosing the Host representative. Individual cases are not put to a vote: on-chain arbitration takes more than ten days on average, and the voters would see the facts of the case.

## What may be said externally
- **May say**: "Access records and any submitted identity documents are Sealed. The company alone cannot open them. Only the Committee can, under a US court document. Every Disclosure is publicly recorded."
- **Must not say**: "we do not record the websites you visit" (void), "nobody can find out", "even we don't know", "verified identity", "the Platform does not know who its Guests are".
- **Must be stated honestly**: a US court can order a Keyholder in the US to hand over their key share; Guests are not identity-checked by default; a submitted document passes through our code at upload and only the Sealed copy is kept; payment records are held unsealed by the payment processor, so the Platform can see who paid.
- Each sentence above may be published only after the matching implementation and test exist in the Overleap code.

## What to do without a legal budget
First read public sources ourselves. Before launch, ask a lawyer only once:
- Whether the structure in D-40 (the Platform sells the Seat as principal and pays Hosts in USDC as suppliers) keeps us outside money-transmitter rules; tax reporting for paying USDC across borders to individuals.
- Whether a pending US rule on identifying customers of infrastructure providers would cover us if finalised, and whether the default level (Connection Records and the Usage Undertaking, no identity check) is enough.
- Sanctions and export-control limits on serving Guests in mainland China.
- What limits US law puts on handing data to a foreign government.
- Whether the sentence "the company alone cannot open it" can be written into the privacy policy, and whether the Usage Undertaking is enforceable with the Host as beneficiary.

## What can be added later
- Checking a submitted document against an official source (a registry check or a passport chip), if Hosts ask for more than "submitted". One vendor option is recorded in research/identity-vendors-gemini-verify-2026-11-11.md.
- When on-chain threshold decryption networks are mature and clearly priced, move the release logic onto one. In 2026 there was a precedent of such a network shutting down, so we do not depend on them now.
