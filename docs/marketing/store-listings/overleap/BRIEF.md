# Store listing localization brief (Overleap VPN)

Source copy (English): `source.json` in this directory. Read it first.

Produce ONE JSON file (path given in your task) shaped `{ "<langKey>": {name, subtitle, keywords, promo, playShort, body, iosSubscription, linkLabels:{privacy,terms}} }` for every language key assigned to you.

Field rules (limits are Unicode code points, hard limits — the stores reject longer values):
- `name` ≤30. Must contain the Latin word "Overleap". Normally "Overleap VPN"; you may append a short local descriptor only if it fits and reads naturally (e.g. "Overleap VPN – ..."). Never translate/transliterate "Overleap".
- `subtitle` ≤30. Localized equivalent of "No logs. Fast on any network." Short and natural beats literal.
- `keywords` ≤100 total, comma-separated, NO space after commas. These are App Store search keywords: choose the terms people in that language actually type when looking for this kind of app (local word for VPN/proxy/privacy/secure wifi/unblock/travel/IP etc., both native-script and common Latin forms such as "vpn"). Do not repeat words already in name or subtitle. Use as much of the 100 as you can. No competitor brand names, no trademarks.
- `promo` ≤170. Localized promotional text.
- `playShort` ≤80. Localized one-line summary.
- `body`: full translation of source `body`, keeping the structure (intro paragraph, heading lines in caps where the script has case, bullet lines starting with "• "). Keep "Overleap", "k2cc", "Encrypted Client Hello", "Wi-Fi" as is. Work the main local search terms in naturally (this text is indexed by Google Play) — do not keyword-stuff.
- `iosSubscription`: faithful translation of source `iosSubscription` (legal auto-renew disclosure; keep every fact: yearly, charged to Apple ID at confirmation, renews unless cancelled ≥24h before period end, charged within 24h before end, manage/cancel in App Store account settings). Product name "Overleap Basic" stays in Latin.
- `linkLabels`: local words for "Privacy Policy" and "Terms of Use" (labels only, no URLs).

Hard constraints:
- Outside `iosSubscription`, never mention Apple, iPhone, iPad, Android, Google Play or any other platform/store, nor any competitor.
- No claims that are not in the source (no "free", no "#1", no "fastest", no number of servers/countries, no streaming-service names, no mention of bypassing age verification or censorship of a specific country).
- Natural, idiomatic, native-quality language; formal-neutral register. Right-to-left languages: plain text, no direction marks.
- For regional variants (e.g. fr-CA, pt-PT, es-419) use that region's vocabulary; keywords should differ from the sibling variant where local usage differs.
- Plain text only: no markdown, no emoji, no HTML.

When done, run `node validate.mjs <your file>` in this directory and fix until it prints OK. Report only: the file path, the validator's final line, and any language where you are not confident in quality (one line each).
