/**
 * ConnectionButton / CollapsibleConnectionSection — reconnecting reason
 *
 * While the engine holds a session it reports reconnecting + the reason
 * (101 offline / 106 no internet / 107 captive portal / 503 lines down).
 * The user sees that reason, not a bare "reconnecting". Without a reason
 * (iOS today) the generic copy stays.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/ConnectionButton.reconnectReason.test.tsx
 */
import { describe, it, expect, beforeAll, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import i18n, { i18nPromise } from '../../i18n/i18n';
import { ConnectionButton } from '../ConnectionButton';
import { RECONNECT_REASON_CATALOG } from '../../utils/errorCatalog';

const base = {
  serviceState: 'reconnecting' as const,
  hasTunnelSelected: true,
  tunnelName: 'Tokyo',
  onToggle: () => {},
};

function setShellVersion(version: string) {
  (window as any)._platform = { ...((window as any)._platform ?? {}), version };
}

describe('ConnectionButton reconnect reason', () => {
  const savedPlatform = (window as any)._platform;
  beforeAll(async () => { await i18nPromise; });
  afterEach(() => { (window as any)._platform = savedPlatform; });

  // ≥0.4.16 shell: its engine ran netcheck, so 503 means "line unreachable".
  for (const code of [101, 106, 107, 503]) {
    it(`reconnecting + ${code} shows the ${code} reason copy`, () => {
      setShellVersion('0.4.16');
      render(<ConnectionButton {...base} error={{ code, message: 'x' }} />);
      const text = i18n.t(RECONNECT_REASON_CATALOG[code].key);
      expect(text).not.toBe(RECONNECT_REASON_CATALOG[code].key.split(':')[1]); // resolved, not the raw key
      expect(screen.getByText(text)).toBeInTheDocument();
    });
  }

  // ≤0.4.15 shell (web OTA): its engine reports the user's own dead network as
  // 503 too, so a reconnecting 503 keeps the generic copy (engineJudgesNetwork).
  it('old shell: reconnecting + 503 shows only the generic copy', () => {
    setShellVersion('0.4.15');
    render(<ConnectionButton {...base} error={{ code: 503, message: 'x' }} />);
    expect(screen.getByText(i18n.t('common:status.reconnecting'))).toBeInTheDocument();
    expect(screen.queryByText(i18n.t(RECONNECT_REASON_CATALOG[503].key))).not.toBeInTheDocument();
  });

  it('reconnecting without error shows only the generic copy', () => {
    render(<ConnectionButton {...base} />);
    expect(screen.getByText(i18n.t('common:status.reconnecting'))).toBeInTheDocument();
    for (const e of Object.values(RECONNECT_REASON_CATALOG)) {
      expect(screen.queryByText(i18n.t(e.key))).not.toBeInTheDocument();
    }
  });

  it('a code with no reason entry (570) shows no reason line', () => {
    render(<ConnectionButton {...base} error={{ code: 570, message: 'x' }} />);
    for (const e of Object.values(RECONNECT_REASON_CATALOG)) {
      expect(screen.queryByText(i18n.t(e.key))).not.toBeInTheDocument();
    }
  });
});
