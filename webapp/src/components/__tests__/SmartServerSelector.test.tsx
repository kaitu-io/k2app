import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { SmartServerSelector } from '../SmartServerSelector';

// --- Mock connection store ---
const mockState = {
  serverMode: 'manual' as 'manual' | 'self_hosted' | 'k2sub',
  setServerMode: vi.fn(async () => {}),
};

vi.mock('../../stores/connection.store', () => ({
  useConnectionStore: (selector: (s: typeof mockState) => unknown) => selector(mockState),
}));

const manualContent = <div data-testid="manual-content">manual</div>;
const selfHostedContent = <div data-testid="selfhosted-content">self-hosted</div>;
const k2subContent = <div data-testid="k2sub-content">k2sub</div>;

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
        k2subContent={k2subContent}
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
        k2subContent={k2subContent}
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
        k2subContent={k2subContent}
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
        k2subContent={k2subContent}
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
        k2subContent={k2subContent}
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

// ==================== Platform-aware tabs ====================

describe('SmartServerSelector — platform-aware tabs', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockState.serverMode = 'manual';
    mockState.setServerMode = vi.fn(async () => {});
  });

  afterEach(() => {
    delete (window as any)._platform;
  });

  it('gateway mounts both k2sub content and manual content (manual hidden — needed for CloudTunnelList side effects)', () => {
    (window as any)._platform = { platformType: 'gateway' };
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        k2subContent={k2subContent}
      />
    );
    // K2sub content is in DOM (visible in gateway mode).
    expect(screen.getByTestId('k2sub-content')).toBeInTheDocument();
    // Manual content is also in DOM (hidden via CSS) — CloudTunnelList must keep
    // running so its onTunnelsLoaded feeds K2sub's country list.
    expect(screen.getByTestId('manual-content')).toBeInTheDocument();
  });

  it('non-gateway shows manual tab content (not k2sub)', () => {
    (window as any)._platform = { platformType: 'desktop' };
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        k2subContent={k2subContent}
      />
    );
    expect(screen.getByTestId('manual-content')).toBeInTheDocument();
    expect(screen.queryByTestId('k2sub-content')).not.toBeInTheDocument();
  });

  it('gateway hides manual refresh button even when callback provided', () => {
    (window as any)._platform = { platformType: 'gateway' };
    const onManualRefresh = vi.fn();
    render(
      <SmartServerSelector
        isInteractive
        manualContent={manualContent}
        selfHostedContent={selfHostedContent}
        k2subContent={k2subContent}
        onManualRefresh={onManualRefresh}
      />
    );
    expect(screen.queryByTestId('manual-refresh-button')).not.toBeInTheDocument();
  });
});
