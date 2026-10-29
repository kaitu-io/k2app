# kaitu.io funnel event parameters (moved from `web/CLAUDE.md`)

Event names are locked by `web/tests/funnel-contract.test.ts` against `contracts/api-contract.json`.
The `source` values below are free strings in `src/lib/funnel.ts` (`opts.source` → query param `s`);
nothing enforces them, so keep this list in sync when you add an emit site.

## `install_click` `source`

| Value | Meaning |
|---|---|
| `button` | Main download button or store link |
| `backup` | Backup download link |
| `auto` | Automatic download when the countdown ends. This is the main desktop conversion. It fires only when the download really starts, at most once per mount, and not when the countdown is cancelled. |
| `cli` | Install command copied; `plan` = the platform tab it was on |
| `ad` | Download button on the X / ad landing page `/go/<code>` |

## `qr_view`

The ad landing page sends this once after mount when it shows the QR code on desktop. In the funnel
it sits beside `install_click` as the "clicked get / saw the QR code" step.
