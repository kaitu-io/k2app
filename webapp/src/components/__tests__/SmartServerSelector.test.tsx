import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { SmartServerSelector } from '../SmartServerSelector';

// --- Mock connection store ---
const mockState = {
  serverMode: 'manual' as 'manual' | 'self_hosted',
  setServerMode: vi.fn(async () => {}),
};

vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (selector: (s: typeof mockState) => unknown) => selector(mockState),
}));

const manualContent = <div data-testid="manual-content">manual</div>;
const selfHostedContent = <div data-testid="selfhosted-content">self-hosted</div>;

describe('SmartServerSelector — manual refresh button', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockState.serverMode = 'manual';
    mockState.setServerMode = vi.fn(async () => {});
  });

  it('renders the refresh button on manual tab when onManualRefresh is provided', () => {
    const onManualRefresh = vi.fn();
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        onManualRefresh={onManualRefresh}
      />
    );
    expect(screen.getByTestId('manual-refresh-button')).toBeInTheDocument();
  });

  it('does not render the refresh button when onManualRefresh is omitted', () => {
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
      />
    );
    expect(screen.queryByTestId('manual-refresh-button')).not.toBeInTheDocument();
  });

  it('hides the refresh button on the self_hosted tab even when callback is provided', () => {
    mockState.serverMode = 'self_hosted';
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        onManualRefresh={vi.fn()}
      />
    );
    expect(screen.queryByTestId('manual-refresh-button')).not.toBeInTheDocument();
  });

  it('invokes onManualRefresh when clicked', () => {
    const onManualRefresh = vi.fn();
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        onManualRefresh={onManualRefresh}
      />
    );
    fireEvent.click(screen.getByTestId('manual-refresh-button'));
    expect(onManualRefresh).toHaveBeenCalledTimes(1);
  });

  it('disables the button while manualRefreshing is true', () => {
    const onManualRefresh = vi.fn();
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        onManualRefresh={onManualRefresh}
        manualRefreshing
      />
    );
    const btn = screen.getByTestId('manual-refresh-button') as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    fireEvent.click(btn);
    expect(onManualRefresh).not.toHaveBeenCalled();
  });
});
