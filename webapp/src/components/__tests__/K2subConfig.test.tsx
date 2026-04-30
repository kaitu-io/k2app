/**
 * K2subConfig — Auto + per-country list shown in gateway-mode K2sub tab.
 *
 * Run: cd webapp && npx vitest run src/components/__tests__/K2subConfig.test.tsx
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { K2subConfig } from '../K2subConfig';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock('../../utils/country', () => ({
  getCountryName: (code: string) => code.toUpperCase(),
  getFlagIcon: () => null,
}));

const tunnels = [
  { id: 1, domain: 'a', name: 'A', protocol: 'k2v5', port: 443, recommendScore: 0.5,
    node: { country: 'jp', name: '', region: '', ipv4: '', ipv6: '', isAlive: true,
            load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 } },
  { id: 2, domain: 'b', name: 'B', protocol: 'k2v5', port: 443, recommendScore: 0.5,
    node: { country: 'jp', name: '', region: '', ipv4: '', ipv6: '', isAlive: true,
            load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 } },
  { id: 3, domain: 'c', name: 'C', protocol: 'k2v5', port: 443, recommendScore: 0.5,
    node: { country: 'us', name: '', region: '', ipv4: '', ipv6: '', isAlive: true,
            load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 } },
] as any;

describe('K2subConfig', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders Auto + one row per country (deduped, sorted by count desc)', () => {
    render(<K2subConfig tunnels={tunnels} subsCountry={null} setSubsCountry={() => {}} isInteractive />);
    // Auto row + jp + us → 3 radios
    expect(screen.getAllByRole('radio')).toHaveLength(3);
  });

  it('clicking a country row calls setSubsCountry with code', () => {
    const set = vi.fn();
    render(<K2subConfig tunnels={tunnels} subsCountry={null} setSubsCountry={set} isInteractive />);
    // First country listitem (most-frequent: jp)
    const items = screen.getAllByRole('listitem');
    fireEvent.click(items[1]); // [0] = Auto, [1] = jp
    expect(set).toHaveBeenCalledWith('jp');
  });

  it('clicking Auto row calls setSubsCountry(null)', () => {
    const set = vi.fn();
    render(<K2subConfig tunnels={tunnels} subsCountry="jp" setSubsCountry={set} isInteractive />);
    const items = screen.getAllByRole('listitem');
    fireEvent.click(items[0]);
    expect(set).toHaveBeenCalledWith(null);
  });

  it('disables interaction when isInteractive is false', () => {
    const set = vi.fn();
    render(<K2subConfig tunnels={tunnels} subsCountry={null} setSubsCountry={set} isInteractive={false} />);
    const items = screen.getAllByRole('listitem');
    fireEvent.click(items[0]);
    expect(set).not.toHaveBeenCalled();
  });
});
