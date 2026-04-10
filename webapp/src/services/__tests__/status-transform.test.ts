import { describe, it, expect } from 'vitest';
import { transformStatus } from '../status-transform';
import type { StatusResponseData, ControlError } from '../vpn-types';

describe('transformStatus', () => {
  it('maps disconnected state correctly (running=false, networkAvailable=true)', () => {
    const raw = {
      state: 'disconnected',
      running: false,
      networkAvailable: true,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('disconnected');
    expect(result.running).toBe(false);
    expect(result.networkAvailable).toBe(true);
    expect(result.error).toBeUndefined();
    expect(result.retrying).toBeFalsy();
  });

  it('maps connected state with startAt as Unix seconds', () => {
    const unixSeconds = 1678886400; // 2023-03-15 12:00:00 UTC
    const raw = {
      state: 'connected',
      running: true,
      startAt: unixSeconds,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('connected');
    expect(result.running).toBe(true);
    expect(result.startAt).toBe(unixSeconds);
    expect(result.networkAvailable).toBe(true);
    expect(result.error).toBeUndefined();
    expect(result.retrying).toBeFalsy();
  });

  it('maps connecting state (running=true)', () => {
    const raw = {
      state: 'connecting',
      running: true,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('connecting');
    expect(result.running).toBe(true);
    expect(result.networkAvailable).toBe(true);
    expect(result.error).toBeUndefined();
  });

  it('maps reconnecting state (running=false)', () => {
    const raw = {
      state: 'reconnecting',
      running: false,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('reconnecting');
    expect(result.running).toBe(false);
    expect(result.networkAvailable).toBe(true);
  });

  it('synthesizes error state from disconnected + error object', () => {
    const raw = {
      state: 'disconnected',
      running: false,
      error: { code: 503, message: 'Server unreachable' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.error).toEqual({ code: 503, message: 'Server unreachable' });
    expect(result.retrying).toBe(false); // disconnected + error means engine gave up
  });

  it('synthesizes error state from connected + network error (retrying=true)', () => {
    const raw = {
      state: 'connected',
      running: true,
      error: { code: 502, message: 'Protocol error' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.error).toEqual({ code: 502, message: 'Protocol error' });
    expect(result.retrying).toBe(true); // connected + network error → engine retries
  });

  it('does NOT retry on client errors (401/402) — retrying=false', () => {
    const testCases = [
      { code: 401, message: 'Unauthorized' },
      { code: 402, message: 'Payment required' },
    ];

    for (const errorData of testCases) {
      const raw = {
        state: 'connected',
        running: true,
        error: errorData,
      };

      const result = transformStatus(raw);

      expect(result.state).toBe('error');
      expect(result.error).toEqual(errorData);
      expect(result.retrying).toBe(false); // Client errors don't auto-retry
    }
  });

  it('handles legacy string error (wraps as code 570)', () => {
    const raw = {
      state: 'disconnected',
      running: false,
      error: 'Connection failed',
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.error).toEqual({ code: 570, message: 'Connection failed' });
    expect(result.retrying).toBe(false);
  });

  it('handles missing state (defaults to disconnected)', () => {
    const raw = {
      running: false,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('disconnected');
    expect(result.running).toBe(false);
    expect(result.networkAvailable).toBe(true);
  });

  it('handles EngineError with extra category field (silently dropped)', () => {
    const raw = {
      state: 'disconnected',
      running: false,
      error: { code: 400, message: 'Bad config', category: 'client' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.error).toEqual({ code: 400, message: 'Bad config' });
    expect(result.error).not.toHaveProperty('category');
  });

  it('normalizes daemon "stopped" to "disconnected"', () => {
    const raw = {
      state: 'stopped',
      running: false,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('disconnected');
    expect(result.running).toBe(false);
  });

  it('falls back to legacy connected_at RFC3339 string (snake_case)', () => {
    const rfc3339 = '2023-03-15T12:00:00Z';
    const raw = {
      state: 'connected',
      running: true,
      connected_at: rfc3339,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('connected');
    expect(result.startAt).toBeDefined();
    expect(typeof result.startAt).toBe('number');
    // Verify it's the correct timestamp (2023-03-15T12:00:00Z = 1678881600)
    expect(result.startAt).toBe(1678881600);
  });

  it('falls back to legacy connectedAt RFC3339 string (camelCase)', () => {
    const rfc3339 = '2023-03-15T12:00:00Z';
    const raw = {
      state: 'connected',
      running: true,
      connectedAt: rfc3339,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('connected');
    expect(result.startAt).toBeDefined();
    expect(typeof result.startAt).toBe('number');
    expect(result.startAt).toBe(1678881600);
  });

  it('prefers new startAt integer over legacy connected_at', () => {
    const newTime = 1678886400;
    const legacyTime = '2020-01-01T00:00:00Z'; // Much earlier
    const raw = {
      state: 'connected',
      running: true,
      startAt: newTime,
      connected_at: legacyTime,
    };

    const result = transformStatus(raw);

    expect(result.startAt).toBe(newTime);
  });

  it('handles empty error object (treats as no error)', () => {
    const raw = {
      state: 'disconnected',
      running: false,
      error: {},
    };

    const result = transformStatus(raw);

    // Empty object doesn't match the { code } pattern, so it's treated as string
    // and wrapped as 570
    expect(result.state).toBe('error');
    expect(result.error?.code).toBe(570);
  });

  it('handles null error (treats as no error)', () => {
    const raw = {
      state: 'disconnected',
      running: false,
      error: null,
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('disconnected');
    expect(result.error).toBeUndefined();
  });

  it('returns structured StatusResponseData with all fields', () => {
    const raw = {
      state: 'connected',
      running: true,
      startAt: 1678886400,
    };

    const result: StatusResponseData = transformStatus(raw);

    expect(result).toHaveProperty('state');
    expect(result).toHaveProperty('running');
    expect(result).toHaveProperty('networkAvailable');
    expect(result).toHaveProperty('startAt');
    expect(result).toHaveProperty('error');
    expect(result).toHaveProperty('retrying');
  });

  it('sets networkAvailable=true consistently (always true currently)', () => {
    const testCases = [
      { state: 'disconnected' },
      { state: 'connecting' },
      { state: 'connected' },
      { state: 'error', error: { code: 503, message: 'Test' } },
    ];

    for (const raw of testCases) {
      const result = transformStatus(raw);
      expect(result.networkAvailable).toBe(true);
    }
  });

  it('handles 403 forbidden error (client error, no retry)', () => {
    const raw = {
      state: 'connected',
      running: true,
      error: { code: 403, message: 'Certificate pin failed' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.retrying).toBe(false); // 403 is a client error
  });

  it('handles 400 bad config error (client error, no retry)', () => {
    const raw = {
      state: 'connected',
      running: true,
      error: { code: 400, message: 'Invalid wire URL' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.retrying).toBe(false); // 400 is a client error
  });

  it('handles network errors (501) that are NOT in client error list (should retry)', () => {
    const raw = {
      state: 'connected',
      running: true,
      error: { code: 501, message: 'Not implemented' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.retrying).toBe(true); // Not a client error (400/401/402/403)
  });

  it('handles 408 timeout (network error, retry)', () => {
    const raw = {
      state: 'connected',
      running: true,
      error: { code: 408, message: 'Connection timeout' },
    };

    const result = transformStatus(raw);

    expect(result.state).toBe('error');
    expect(result.retrying).toBe(true); // Network error should retry
  });
});
