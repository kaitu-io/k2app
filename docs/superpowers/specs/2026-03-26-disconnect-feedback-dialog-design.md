# Disconnect Feedback Dialog Design

## Goal

Collect logs from users experiencing connection quality issues. After each user-initiated disconnect, show a mandatory feedback dialog. Users who report "不好" automatically trigger a ticket submission + log upload, giving the team diagnostic data without requiring the user to navigate to a separate feedback page.

## Trigger Conditions

- **Only on user-initiated disconnect** (clicking the disconnect button)
- **Not triggered by**: server switch, network error, auto-disconnect, or any non-user action
- **Timing**: After disconnect completes (VPN state reaches `idle`)
- **No duplicate popups**: The dialog shows at most once per disconnect action

## UI

MUI Dialog, minimal:

- Title: "本次连接体验如何？" (i18n)
- Two buttons: 「好」 「不好」
- No close button, no backdrop dismiss (forced choice)
- No text input, no additional options

## Behavior

### "好" Path

Close dialog. No API calls.

### "不好" Path

1. Close dialog immediately
2. Show toast: "感谢反馈，正在上传日志..." (i18n)
3. Fire-and-forget background submission:
   - Generate `feedbackId` (UUID v4)
   - Call `window._platform.uploadLogs()` (if available)
   - `POST /api/user/ticket` with auto-generated content containing connection info
   - `POST /api/user/device-log` to register log metadata
   - `POST /api/user/feedback-notify` for Slack alert
4. On standalone (no `uploadLogs`), only submit ticket without logs

### Error Handling

- If `uploadLogs` fails, ticket is still submitted (without log references / empty `s3Keys`)
- If ticket submission fails, silently log error — no user-facing error display

### Unauthenticated Users

Dialog is **suppressed** for unauthenticated users. The ticket endpoints require auth, and collecting logs without user identity has limited diagnostic value. The dialog only shows when `useAuthStore.getState().isAuthenticated` is true.

## Connection Info Snapshot

At `disconnect()` call time, snapshot must be taken from `get()` **before** the `set({ connectedTunnel: null })` call that clears the tunnel. The snapshot is stored in `lastConnectionInfo`:

| Field | Source |
|-------|--------|
| server domain | `connectedTunnel.domain` |
| server name | `connectedTunnel.name` |
| country/region | `connectedTunnel.country` |
| source type | `connectedTunnel.source` (cloud/self-hosted) |
| connected at | `connectedAt` (stored in connection store at connect time as `Date.now()`) |
| duration (seconds) | `(Date.now() - connectedAt) / 1000` |
| rule mode | `configStore.ruleMode` |
| OS | `window._platform.os` |
| app version | `window._platform.version` |

This snapshot is consumed by the dialog when submitting the ticket, then discarded.

## Anti-Duplicate Mechanism

`connection.store.ts` manages a `pendingFeedback: boolean` flag:

1. `disconnect()` sets `pendingFeedback = true` (only when triggered by user action)
2. `DisconnectFeedbackDialog` reads `pendingFeedback` — renders only when `true`
3. On dialog mount, immediately sets `pendingFeedback = false`
4. Any button click closes the dialog

This ensures:
- Multiple `idle` state transitions don't re-trigger the dialog
- Fast double-clicks on disconnect don't cause duplicate popups
- The flag is consumed exactly once

## New Files

- `webapp/src/components/DisconnectFeedbackDialog.tsx` — Dialog component + submission logic

## Modified Files

- `webapp/src/stores/connection.store.ts` — Add `pendingFeedback`, `lastConnectionInfo`, snapshot in `disconnect()`
- `webapp/src/pages/Dashboard.tsx` — Render `<DisconnectFeedbackDialog />`
- `webapp/src/i18n/locales/*/feedback.json` (7 locales) — Dialog text

## Cross-Platform

| Platform | uploadLogs | Ticket | Behavior |
|----------|-----------|--------|----------|
| Desktop (Tauri) | Yes | Yes | Full: logs + ticket |
| Mobile (Capacitor) | Yes | Yes | Full: logs + ticket |
| Standalone (Web) | No | Yes | Ticket only, no logs |

## Existing Code Reused

- `SubmitTicket.tsx` submission pattern (feedbackId generation, uploadLogs call, device-log + feedback-notify endpoints)
- `connection.store.ts` disconnect orchestration
- `useAlertStore` for toast notifications
- `cloudApi` for API calls
- `getDeviceUdid()` for device identification
