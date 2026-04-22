import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { ProbeChip } from '../ProbeChip';
import type { ProbeResult } from '../../services/api-types';

const now = new Date().toISOString();

function mk(overrides: Partial<ProbeResult> = {}): ProbeResult {
  return {
    url: 'k2v5://u:t@a:443',
    avgRttMs: 0, minRttMs: 0, maxRttMs: 0, jitterMs: 0, lossRate: 0,
    reachable: true, echoSupported: true, probeScore: 0.5, measuredAt: now,
    ...overrides,
  };
}

describe('ProbeChip', () => {
  it('shows Skeleton when loading', () => {
    const { container } = render(<ProbeChip result={null} loading={true} />);
    expect(container.querySelector('.MuiSkeleton-root')).toBeTruthy();
  });

  it('renders RTT for normal result', () => {
    render(<ProbeChip result={mk({ avgRttMs: 42, probeScore: 0.7 })} loading={false} />);
    expect(screen.getByText(/42\s*ms/)).toBeTruthy();
  });

  it('renders loss % when lossRate >= 0.05', () => {
    render(<ProbeChip result={mk({ avgRttMs: 60, lossRate: 0.12, probeScore: 0.35 })} loading={false} />);
    expect(screen.getByText(/12%/)).toBeTruthy();
  });

  it('hides loss % when lossRate < 0.05', () => {
    render(<ProbeChip result={mk({ avgRttMs: 60, lossRate: 0.01, probeScore: 0.6 })} loading={false} />);
    expect(screen.queryByText(/%/)).toBeNull();
  });

  it('renders ? for echo-unsupported', () => {
    const { container } = render(<ProbeChip result={mk({ echoSupported: false, probeScore: -1 })} loading={false} />);
    expect(container.textContent).toContain('?');
  });

  it('renders — for unreachable', () => {
    const { container } = render(<ProbeChip result={mk({ reachable: false, probeScore: 0 })} loading={false} />);
    expect(container.textContent).toContain('—');
  });

  it('renders nothing when result=null and not loading', () => {
    const { container } = render(<ProbeChip result={null} loading={false} />);
    expect(container.firstChild).toBeNull();
  });
});
