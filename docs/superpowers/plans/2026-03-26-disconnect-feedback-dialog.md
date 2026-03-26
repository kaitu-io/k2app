# Disconnect Feedback Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a mandatory feedback dialog after user-initiated disconnect. "不好" triggers auto ticket + log upload to collect diagnostic data from users with bad experiences.

**Architecture:** Add `pendingFeedback` flag + `lastConnectionInfo` snapshot to connection store. New `DisconnectFeedbackDialog` component handles UI and submission. Dashboard renders the dialog. All submission logic (uploadLogs, ticket, device-log, feedback-notify) extracted from SubmitTicket.tsx patterns.

**Tech Stack:** React 18, MUI 5 Dialog, Zustand, cloudApi, i18next (feedback namespace)

**Spec:** `docs/superpowers/specs/2026-03-26-disconnect-feedback-dialog-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `webapp/src/stores/connection.store.ts` | Modify | Add `connectedAt`, `pendingFeedback`, `lastConnectionInfo`, snapshot logic in `disconnect()` |
| `webapp/src/components/DisconnectFeedbackDialog.tsx` | Create | Dialog UI + submission logic (uploadLogs, ticket, device-log, feedback-notify) |
| `webapp/src/pages/Dashboard.tsx` | Modify | Render `<DisconnectFeedbackDialog />` |
| `webapp/src/i18n/locales/*/feedback.json` (7 files) | Modify | Add `disconnectFeedback.*` keys |

---

### Task 1: Add i18n keys to all 7 locale files

**Files:**
- Modify: `webapp/src/i18n/locales/zh-CN/feedback.json`
- Modify: `webapp/src/i18n/locales/en-US/feedback.json`
- Modify: `webapp/src/i18n/locales/ja/feedback.json`
- Modify: `webapp/src/i18n/locales/zh-TW/feedback.json`
- Modify: `webapp/src/i18n/locales/zh-HK/feedback.json`
- Modify: `webapp/src/i18n/locales/en-AU/feedback.json`
- Modify: `webapp/src/i18n/locales/en-GB/feedback.json`

Add 4 new keys under existing `feedback` object in each file. New keys nested under `disconnectFeedback`:

- [ ] **Step 1: Add keys to zh-CN/feedback.json**

Add after the last key in the `feedback` object (before the closing `}`):

```json
    "disconnectFeedback": {
      "title": "本次连接体验如何？",
      "good": "好",
      "bad": "不好",
      "thankYou": "感谢反馈，正在上传日志..."
    }
```

- [ ] **Step 2: Add keys to en-US/feedback.json**

```json
    "disconnectFeedback": {
      "title": "How was your connection?",
      "good": "Good",
      "bad": "Bad",
      "thankYou": "Thanks for your feedback, uploading logs..."
    }
```

- [ ] **Step 3: Add keys to ja/feedback.json**

```json
    "disconnectFeedback": {
      "title": "接続の品質はいかがでしたか？",
      "good": "良い",
      "bad": "悪い",
      "thankYou": "フィードバックありがとうございます。ログをアップロード中..."
    }
```

- [ ] **Step 4: Add keys to zh-TW/feedback.json**

```json
    "disconnectFeedback": {
      "title": "本次連線體驗如何？",
      "good": "好",
      "bad": "不好",
      "thankYou": "感謝回饋，正在上傳日誌..."
    }
```

- [ ] **Step 5: Add keys to zh-HK/feedback.json**

```json
    "disconnectFeedback": {
      "title": "本次連線體驗如何？",
      "good": "好",
      "bad": "唔好",
      "thankYou": "感謝回饋，正在上傳日誌..."
    }
```

- [ ] **Step 6: Add keys to en-AU/feedback.json**

```json
    "disconnectFeedback": {
      "title": "How was your connection?",
      "good": "Good",
      "bad": "Bad",
      "thankYou": "Thanks for your feedback, uploading logs..."
    }
```

- [ ] **Step 7: Add keys to en-GB/feedback.json**

```json
    "disconnectFeedback": {
      "title": "How was your connection?",
      "good": "Good",
      "bad": "Bad",
      "thankYou": "Thanks for your feedback, uploading logs..."
    }
```

- [ ] **Step 8: Commit**

```bash
git add webapp/src/i18n/locales/*/feedback.json
git commit -m "feat: add disconnect feedback dialog i18n keys"
```

---

### Task 2: Modify connection.store.ts — add feedback state + snapshot

**Files:**
- Modify: `webapp/src/stores/connection.store.ts:35-50` (types)
- Modify: `webapp/src/stores/connection.store.ts:94-101` (initial state)
- Modify: `webapp/src/stores/connection.store.ts:121-141` (connect — add connectedAt)
- Modify: `webapp/src/stores/connection.store.ts:210-227` (disconnect — snapshot + flag)
- Modify: `webapp/src/stores/connection.store.ts:294-316` (lifecycle — clear pendingFeedback on non-user idle)

**Critical detail — read before implementing:** The `disconnect()` method currently does `set((s) => ({ connectedTunnel: null, ... }))` on line 220. The snapshot of `connectedTunnel` must happen BEFORE this `set()`. We read from `get()` first, compute the snapshot, then include it in the same `set()` call.

**Critical detail — timing:** Per spec, the dialog must show AFTER disconnect completes (VPN reaches `idle`), not immediately when `disconnect()` is called. So `disconnect()` sets `feedbackRequested = true` + saves `lastConnectionInfo`, and the existing VPN idle subscription promotes it to `pendingFeedback = true` when idle is reached. This ensures the dialog only appears after the disconnect animation completes.

**Critical detail — idle subscription:** Line 302-307 clears `connectedTunnel` when VPN reaches idle. We extend this to also check `feedbackRequested` and promote it to `pendingFeedback`.

- [ ] **Step 1: Add `LastConnectionInfo` type and extend `ConnectionState`**

After the `ActiveTunnel` interface (line 33), add:

```typescript
export interface LastConnectionInfo {
  domain: string;
  name: string;
  country: string;
  source: 'cloud' | 'self_hosted';
  durationSec: number;
  ruleMode: string;
  os: string;
  appVersion: string;
}
```

Add 4 new fields to `ConnectionState` interface (after `connectEpoch: number;`):

```typescript
  connectedAt: number | null;
  feedbackRequested: boolean;
  pendingFeedback: boolean;
  lastConnectionInfo: LastConnectionInfo | null;
```

- `feedbackRequested`: set by `disconnect()`, consumed by idle subscription
- `pendingFeedback`: set by idle subscription, consumed by dialog component

Add 1 new action to `ConnectionActions` interface:

```typescript
  clearPendingFeedback: () => void;
```

- [ ] **Step 2: Add initial state values**

In the store creation (line 94-100), add after `connectEpoch: 0,`:

```typescript
  connectedAt: null,
  feedbackRequested: false,
  pendingFeedback: false,
  lastConnectionInfo: null,
```

Add action implementation after the `disconnect` method:

```typescript
  clearPendingFeedback: () => {
    set({ pendingFeedback: false });
  },
```

- [ ] **Step 3: Record `connectedAt` in `connect()`**

Line 141 currently: `set({ connectedTunnel: activeTunnel, connectEpoch: myEpoch });`

Change to: `set({ connectedTunnel: activeTunnel, connectEpoch: myEpoch, connectedAt: Date.now() });`

- [ ] **Step 4: Snapshot connection info in `disconnect()` before clearing**

Replace the current `disconnect` method (lines 210-227) with:

```typescript
  disconnect: async () => {
    const vpnState = useVPNMachineStore.getState().state;
    if (vpnState === 'disconnecting' || vpnState === 'idle') {
      console.warn('[Connection] disconnect: rejected (vpnState=' + vpnState + ')');
      return;
    }

    // Snapshot connection info BEFORE clearing connectedTunnel
    const { connectedTunnel, connectedAt } = get();
    const isAuthenticated = useAuthStore.getState().isAuthenticated;
    let lastConnectionInfo: LastConnectionInfo | null = null;

    if (connectedTunnel && isAuthenticated) {
      const { ruleMode } = useConfigStore.getState();
      const durationSec = connectedAt
        ? Math.round((Date.now() - connectedAt) / 1000)
        : 0;
      lastConnectionInfo = {
        domain: connectedTunnel.domain,
        name: connectedTunnel.name,
        country: connectedTunnel.country,
        source: connectedTunnel.source,
        durationSec,
        ruleMode,
        os: window._platform?.os || 'unknown',
        appVersion: window._platform?.version || '0.0.0',
      };
    }

    console.info('[Connection] disconnect: bumping epoch, dispatching USER_DISCONNECT');
    set((s) => ({
      connectedTunnel: null,
      connectedAt: null,
      connectEpoch: s.connectEpoch + 1,
      // Mark feedback as requested — promoted to pendingFeedback when VPN reaches idle
      feedbackRequested: !!lastConnectionInfo,
      lastConnectionInfo,
    }));
    vpnDispatch('USER_DISCONNECT');
    try {
      await window._k2.run('down');
    } catch (err) {
      console.error('[Connection] disconnect failed:', err);
    }
  },
```

- [ ] **Step 5: Add `useAuthStore` import**

At the top of the file, add to the imports:

```typescript
import { useAuthStore } from './auth.store';
```

- [ ] **Step 6: Update idle subscription to promote feedbackRequested → pendingFeedback**

The VPN idle subscription (line 302-307) currently clears `connectedTunnel`. Extend it to also promote the feedback flag:

Replace the idle block:
```typescript
      if (state === 'idle') {
        const { connectedTunnel } = useConnectionStore.getState();
        if (connectedTunnel) {
          console.info('[Connection] VPN idle — clearing connectedTunnel');
          useConnectionStore.setState({ connectedTunnel: null });
        }
      }
```

With:
```typescript
      if (state === 'idle') {
        const { connectedTunnel, feedbackRequested } = useConnectionStore.getState();
        const updates: Record<string, any> = {};
        if (connectedTunnel) {
          console.info('[Connection] VPN idle — clearing connectedTunnel');
          updates.connectedTunnel = null;
        }
        if (feedbackRequested) {
          console.info('[Connection] VPN idle — promoting feedbackRequested → pendingFeedback');
          updates.feedbackRequested = false;
          updates.pendingFeedback = true;
        }
        if (Object.keys(updates).length > 0) {
          useConnectionStore.setState(updates);
        }
      }
```

This ensures the dialog only opens after VPN fully reaches idle state, matching the spec timing requirement.

- [ ] **Step 7: Run TypeScript check**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 8: Commit**

```bash
git add webapp/src/stores/connection.store.ts
git commit -m "feat: add disconnect feedback state to connection store"
```

---

### Task 3: Create DisconnectFeedbackDialog component

**Files:**
- Create: `webapp/src/components/DisconnectFeedbackDialog.tsx`

**Reference:** Pattern for uploadLogs + ticket + device-log + feedback-notify from `webapp/src/pages/SubmitTicket.tsx:94-144` and `webapp/src/pages/SubmitTicket.tsx:172-188`.

**Key design decisions:**
- Component reads `pendingFeedback` from connection store — only renders Dialog when `true`
- On mount (when pendingFeedback becomes true), immediately calls `clearPendingFeedback()` so the flag is consumed exactly once
- Dialog has its own `open` state for the actual MUI Dialog visibility
- "好" → close dialog
- "不好" → close dialog, show toast, fire-and-forget background submission
- Submission never blocks UI, never shows errors to user

- [ ] **Step 1: Create the component file**

Create `webapp/src/components/DisconnectFeedbackDialog.tsx`:

```tsx
/**
 * DisconnectFeedbackDialog — mandatory post-disconnect quality dialog
 *
 * Shown once after each user-initiated disconnect (authenticated only).
 * "不好" auto-submits a ticket + uploads logs for diagnostics.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogActions,
  Button,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore, type LastConnectionInfo } from '../stores/connection.store';
import { useAlertStore } from '../stores/alert.store';
import { cloudApi } from '../services/cloud-api';
import { getDeviceUdid } from '../services/device-udid';

function generateFeedbackId(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function formatConnectionInfo(info: LastConnectionInfo): string {
  return [
    `Server: ${info.name} (${info.domain})`,
    `Region: ${info.country || 'unknown'}`,
    `Type: ${info.source}`,
    `Duration: ${info.durationSec}s`,
    `Rule: ${info.ruleMode}`,
    `OS: ${info.os}`,
    `Version: ${info.appVersion}`,
  ].join('\n');
}

async function submitNegativeFeedback(info: LastConnectionInfo): Promise<void> {
  const feedbackId = generateFeedbackId();
  let s3Keys: Array<{ name: string; s3Key: string }> = [];

  // Step 1: Upload logs (best-effort)
  if (window._platform?.uploadLogs) {
    try {
      const result = await window._platform.uploadLogs({
        email: null,
        reason: 'disconnect_feedback_bad',
        platform: window._platform.os,
        version: window._platform.version,
        feedbackId,
      });
      if (result.success && result.s3Keys?.length) {
        s3Keys = result.s3Keys;
      }
    } catch (err) {
      console.warn('[DisconnectFeedback] uploadLogs failed:', err);
    }
  }

  // Step 2: Submit ticket (proceeds even if logs failed)
  try {
    await cloudApi.post('/api/user/ticket', {
      content: `[Auto] User reported bad connection experience after disconnect.\n\n${formatConnectionInfo(info)}`,
      feedbackId,
      os: info.os,
      app_version: info.appVersion,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] ticket submission failed:', err);
  }

  // Step 3: Register log metadata (only if logs were uploaded)
  if (s3Keys.length > 0) {
    try {
      const udid = await getDeviceUdid();
      await cloudApi.post('/api/user/device-log', {
        udid,
        feedbackId,
        s3Keys,
        reason: 'disconnect_feedback_bad',
        meta: {
          os: info.os,
          appVersion: info.appVersion,
          channel: window._platform?.updater?.channel ?? 'stable',
        },
      });
    } catch (err) {
      console.warn('[DisconnectFeedback] device-log registration failed:', err);
    }
  }

  // Step 4: Slack notification
  try {
    await cloudApi.post('/api/user/feedback-notify', {
      reason: 'disconnect_feedback_bad',
      platform: info.os,
      version: info.appVersion,
      feedbackId,
      s3Keys,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] feedback-notify failed:', err);
  }
}

export function DisconnectFeedbackDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const connectionInfoRef = useRef<LastConnectionInfo | null>(null);

  const pendingFeedback = useConnectionStore((s) => s.pendingFeedback);
  const lastConnectionInfo = useConnectionStore((s) => s.lastConnectionInfo);
  const clearPendingFeedback = useConnectionStore((s) => s.clearPendingFeedback);
  const showAlert = useAlertStore((s) => s.showAlert);

  // When pendingFeedback becomes true, consume it and open dialog
  useEffect(() => {
    if (pendingFeedback) {
      connectionInfoRef.current = lastConnectionInfo;
      clearPendingFeedback();
      setOpen(true);
    }
  }, [pendingFeedback, lastConnectionInfo, clearPendingFeedback]);

  const handleGood = useCallback(() => {
    setOpen(false);
    connectionInfoRef.current = null;
  }, []);

  const handleBad = useCallback(() => {
    setOpen(false);
    const info = connectionInfoRef.current;
    connectionInfoRef.current = null;

    if (info) {
      showAlert(t('feedback:feedback.disconnectFeedback.thankYou'), 'info');
      // Fire-and-forget
      submitNegativeFeedback(info).catch((err) => {
        console.error('[DisconnectFeedback] submission error:', err);
      });
    }
  }, [showAlert, t]);

  return (
    <Dialog
      open={open}
      disableEscapeKeyDown
      onClose={(_event, reason) => {
        // Block backdrop click — force user to choose
        if (reason === 'backdropClick') return;
      }}
      PaperProps={{
        sx: { minWidth: 280, textAlign: 'center' },
      }}
    >
      <DialogTitle sx={{ pb: 1 }}>
        {t('feedback:feedback.disconnectFeedback.title')}
      </DialogTitle>
      <DialogActions sx={{ justifyContent: 'center', pb: 2, gap: 2 }}>
        <Button
          variant="outlined"
          onClick={handleGood}
          sx={{ minWidth: 80 }}
        >
          {t('feedback:feedback.disconnectFeedback.good')}
        </Button>
        <Button
          variant="contained"
          color="error"
          onClick={handleBad}
          sx={{ minWidth: 80 }}
        >
          {t('feedback:feedback.disconnectFeedback.bad')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
```

- [ ] **Step 2: Run TypeScript check**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 3: Commit**

```bash
git add webapp/src/components/DisconnectFeedbackDialog.tsx
git commit -m "feat: create DisconnectFeedbackDialog component"
```

---

### Task 4: Render DisconnectFeedbackDialog in Dashboard

**Files:**
- Modify: `webapp/src/pages/Dashboard.tsx:1-2` (import)
- Modify: `webapp/src/pages/Dashboard.tsx:628` (render before closing `</DashboardContainer>`)

- [ ] **Step 1: Add import**

Add at the end of the import block (after the `cacheStore` import on line 40):

```typescript
import { DisconnectFeedbackDialog } from '../components/DisconnectFeedbackDialog';
```

- [ ] **Step 2: Render the dialog**

Before the closing `</DashboardContainer>` tag (line 628), add:

```tsx
      <DisconnectFeedbackDialog />
```

The dialog renders via a MUI `Dialog` portal, so its position in the JSX tree doesn't affect layout.

- [ ] **Step 3: Run TypeScript check**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors

- [ ] **Step 4: Commit**

```bash
git add webapp/src/pages/Dashboard.tsx
git commit -m "feat: render DisconnectFeedbackDialog in Dashboard"
```

---

### Task 5: Manual verification

- [ ] **Step 1: Start dev server**

Run: `cd /Users/david/projects/kaitu-io/k2app && make dev-standalone`

- [ ] **Step 2: Verify no build errors**

Expected: Vite dev server starts without errors.

- [ ] **Step 3: Test in browser — happy path**

1. Open app in browser
2. Login (must be authenticated for dialog to show)
3. Select a tunnel and connect
4. Click disconnect
5. Wait for disconnect to complete (idle state)
6. **Expected:** Dialog appears with title "本次连接体验如何？" and two buttons
7. Click "好"
8. **Expected:** Dialog closes, no toast, no network requests

- [ ] **Step 4: Test in browser — bad feedback path**

1. Connect again
2. Disconnect
3. Dialog appears
4. Click "不好"
5. **Expected:** Dialog closes, toast "感谢反馈，正在上传日志..." appears
6. Check browser Network tab: POST to `/api/user/ticket` should fire (may fail on standalone — that's expected)

- [ ] **Step 5: Test anti-duplicate — rapid disconnect**

1. Connect
2. Click disconnect rapidly (double-click)
3. **Expected:** Only one dialog appears

- [ ] **Step 6: Test unauthenticated — dialog suppressed**

1. Logout
2. If a self-hosted tunnel is configured, connect and disconnect
3. **Expected:** No dialog appears

- [ ] **Step 7: Run existing tests**

Run: `cd webapp && npx vitest run`
Expected: All existing tests pass (no regressions).
