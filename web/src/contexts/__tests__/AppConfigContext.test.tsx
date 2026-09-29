import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';

const getAppConfig = vi.fn();
vi.mock('@/lib/api', () => ({ api: { getAppConfig: (...a: unknown[]) => getAppConfig(...a) } }));

import { AppConfigProvider, useAppConfig } from '../AppConfigContext';

function Probe() {
  const { appConfig, isLoading } = useAppConfig();
  return <div data-testid="state">{isLoading ? 'loading' : JSON.stringify(appConfig)}</div>;
}

afterEach(() => vi.restoreAllMocks());

describe('AppConfigProvider', () => {
  // iOS Safari with "Block All Cookies" throws SecurityError on every localStorage
  // call. The catch block used to call localStorage.removeItem again, rethrowing out
  // of the effect and unmounting the whole tree (Sentry JAVASCRIPT-NEXTJS-6).
  it('survives localStorage throwing SecurityError and still loads from the API', async () => {
    const denied = () => { throw new DOMException('The operation is insecure.', 'SecurityError'); };
    vi.spyOn(localStorage, 'getItem').mockImplementation(denied);
    vi.spyOn(localStorage, 'setItem').mockImplementation(denied);
    vi.spyOn(localStorage, 'removeItem').mockImplementation(denied);
    vi.spyOn(console, 'error').mockImplementation(() => {});
    getAppConfig.mockResolvedValue({ ok: 1 });

    render(<AppConfigProvider><Probe /></AppConfigProvider>);

    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('{"ok":1}'));
  });
});
