/** FunnelPageViews: page_view on every route, plus the page-specific view events. */
import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';

const trackMock = vi.hoisted(() => vi.fn());
const pathname = vi.hoisted(() => ({ current: '/' }));
vi.mock('@/lib/funnel', () => ({ track: trackMock, externalReferrerHost: () => undefined }));
vi.mock('@/lib/embed', () => ({ isEmbeddedPage: () => false }));
vi.mock('@/i18n/routing', () => ({ usePathname: () => pathname.current }));

import FunnelPageViews from '../src/components/FunnelPageViews';

const events = () => trackMock.mock.calls.map((c) => c[0]);

describe('FunnelPageViews', () => {
  beforeEach(() => trackMock.mockClear());

  it.each([
    ['/', ['page_view']],
    ['/pricing', ['page_view', 'pricing_view']],
    ['/install', ['page_view', 'install_view']],
    ['/install/x', ['page_view']],
  ])('%s → %j', (path, want) => {
    pathname.current = path;
    render(<FunnelPageViews />);
    expect(events()).toEqual(want);
  });
});
