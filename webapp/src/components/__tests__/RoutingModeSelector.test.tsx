/**
 * RoutingModeSelector — country-picker brand gate (Stage 1: Kaitu = China-only).
 *
 * Brand-adaptive: green under both `vitest run` (kaitu) and
 * `K2_BRAND=overleap vitest run`. The country picker only makes sense for the
 * multi-country brand (Overleap); Kaitu is China-market (region always cn), so
 * the picker is hidden and smart-bypass reads as "中国直连".
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { getCurrentAppConfig } from '../../config/apps';
import { useConfigStore } from '../../stores/config.store';
import RoutingModeSelector from '../RoutingModeSelector';

const MULTI_COUNTRY = getCurrentAppConfig().features.multiCountryRouting === true;

describe('RoutingModeSelector — country picker brand gate', () => {
  beforeEach(() => {
    // Smart-bypass preset (proxy + direct) — the only mode where the country
    // picker could show. If the gate leaked, it would appear here.
    useConfigStore.setState({ defaultVia: 'proxy', countryVia: 'direct', country: 'cn', autoDetect: false });
  });

  it('renders the preset radios for every brand', () => {
    render(<RoutingModeSelector />);
    expect(screen.getByTestId('routing-preset-global')).toBeInTheDocument();
    expect(screen.getByTestId('routing-preset-bypass')).toBeInTheDocument();
  });

  it('shows the country picker only for the multi-country brand', () => {
    render(<RoutingModeSelector />);
    if (MULTI_COUNTRY) {
      expect(screen.getByTestId('country-select')).toBeInTheDocument();
    } else {
      expect(screen.queryByTestId('country-select')).not.toBeInTheDocument();
    }
  });
});
