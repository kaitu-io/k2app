import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect } from 'vitest';
import PrivateNodePanel from './PrivateNodePanel';
import type { PrivateNodeSubscriptionView } from '../services/api-types';

function makeNode(over: Partial<PrivateNodeSubscriptionView> = {}): PrivateNodeSubscriptionView {
  return {
    id: 1, status: 'active', isServiceable: true, region: 'japan', ipType: 'non_residential',
    trafficTotalBytes: 1000, trafficUsedBytes: 960, purchasedAt: 0, expiresAt: 1893456000,
    graceUntil: 0, suspendUntil: 0, planLabel: 'JP', quotaExhausted: true, quotaResetAt: 1893456000,
    ...over,
  };
}

describe('PrivateNodePanel quota exhausted', () => {
  it('renders the worded exhausted state when quotaExhausted', () => {
    render(<MemoryRouter><PrivateNodePanel node={makeNode()} /></MemoryRouter>);
    expect(screen.getByTestId('private-node-quota-exhausted')).toBeInTheDocument();
  });

  it('does not render exhausted state when not exhausted', () => {
    render(<MemoryRouter><PrivateNodePanel node={makeNode({ quotaExhausted: false, trafficUsedBytes: 100 })} /></MemoryRouter>);
    expect(screen.queryByTestId('private-node-quota-exhausted')).toBeNull();
  });

  it('does not render exhausted state while provisioning', () => {
    render(<MemoryRouter><PrivateNodePanel node={makeNode({ status: 'provisioning', quotaExhausted: false })} /></MemoryRouter>);
    expect(screen.queryByTestId('private-node-quota-exhausted')).toBeNull();
  });
});
