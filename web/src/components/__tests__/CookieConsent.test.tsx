import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import CookieConsent, { COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION } from '../CookieConsent';

const OPTOUT = '/api/px/optout';

describe('CookieConsent', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    window.localStorage.clear();
    window.history.pushState({}, '', '/zh-CN');
    fetchMock = vi.fn(() => Promise.resolve(new Response(null)));
    vi.stubGlobal('fetch', fetchMock);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  const show = () => {
    render(<CookieConsent />);
    act(() => { vi.advanceTimersByTime(1600); });
  };
  const settle = () => act(() => { vi.advanceTimersByTime(400); });

  it('decline asks the server to opt this browser out of the statistics', () => {
    show();
    fireEvent.click(screen.getByText('discovery.cookieConsent.decline'));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(OPTOUT, { credentials: 'same-origin', redirect: 'manual' });
    settle();
    expect(window.localStorage.getItem(COOKIE_CONSENT_KEY)).toBe(COOKIE_CONSENT_VERSION);
    expect(screen.queryByText('discovery.cookieConsent.decline')).toBeNull();
  });

  it('a failing opt-out request never surfaces', () => {
    fetchMock.mockImplementation(() => Promise.reject(new Error('offline')));
    show();
    expect(() => fireEvent.click(screen.getByText('discovery.cookieConsent.decline'))).not.toThrow();
    settle();
    expect(screen.queryByText('discovery.cookieConsent.decline')).toBeNull();
  });

  it('accept does not opt out', () => {
    show();
    fireEvent.click(screen.getByText('discovery.cookieConsent.accept'));
    settle();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(window.localStorage.getItem(COOKIE_CONSENT_KEY)).toBe(COOKIE_CONSENT_VERSION);
  });

  it('a visitor who answered the previous version sees the new text once', () => {
    expect(COOKIE_CONSENT_VERSION).not.toBe('1');
    window.localStorage.setItem(COOKIE_CONSENT_KEY, '1');
    show();
    expect(screen.getByText('discovery.cookieConsent.details')).toBeTruthy();
  });

  it('stays hidden once the current version is answered', () => {
    window.localStorage.setItem(COOKIE_CONSENT_KEY, COOKIE_CONSENT_VERSION);
    show();
    expect(screen.queryByText('discovery.cookieConsent.details')).toBeNull();
  });

  it('stays hidden in embed mode', () => {
    window.history.pushState({}, '', '/zh-CN/releases?embed=true');
    show();
    expect(screen.queryByText('discovery.cookieConsent.details')).toBeNull();
  });
});
