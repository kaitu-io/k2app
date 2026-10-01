/**
 * RoutingModeSelector — country-picker brand gate (Stage 1: Kaitu = China-only).
 *
 * Brand-adaptive: green under both `vitest run` (kaitu) and
 * `K2_BRAND=overleap vitest run`. The country picker only makes sense for the
 * multi-country brand (Overleap); Kaitu is China-market (region always cn), so
 * the picker is hidden and smart-bypass reads as "中国直连".
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { screen, fireEvent, within } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { getCurrentAppConfig } from '../../config/apps';
import { useConfigStore } from '../../stores/config.store';
import RoutingModeSelector from '../RoutingModeSelector';
import { brandConfig } from '../../brands';

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

  describe.runIf(MULTI_COUNTRY)('searchable country picker', () => {
    beforeEach(() => {
      // Popper reads overflow off getComputedStyle; the shared setup mock is
      // wiped by other suites' clearAllMocks, so give it a real shape here.
      const styles: Record<string, string> = {
        visibility: 'visible', display: 'block', opacity: '1',
        paddingRight: '0px', overflowY: 'auto', overflow: 'visible', position: 'static',
      };
      (window.getComputedStyle as unknown as { mockImplementation: (f: () => unknown) => void })
        .mockImplementation(() => new Proxy(styles, {
          get(target, prop) {
            if (prop === 'getPropertyValue') return (name: string) => target[name] || '';
            return typeof prop === 'string' ? target[prop] || '' : undefined;
          },
        }));
    });

    const openWith = (query: string) => {
      render(<RoutingModeSelector />);
      fireEvent.click(screen.getByTestId('country-select'));
      const input = screen.getByRole('textbox');
      fireEvent.change(input, { target: { value: query } });
      return input;
    };
    const optionCodes = () =>
      within(screen.getByRole('list')).getAllByRole('button').map((el) => el.getAttribute('data-testid'));

    it('lists the brand default country first when nothing is typed', () => {
      openWith('');
      expect(optionCodes()[0]).toBe(`country-option-${brandConfig.defaultRoutingCountry}`);
      expect(optionCodes().length).toBeGreaterThan(10);
    });

    it('narrows by English name regardless of UI language', () => {
      openWith('king');
      expect(optionCodes()).toEqual(['country-option-gb']);
    });

    it('narrows by the localized name (test locale is zh-CN)', () => {
      openWith('伊朗');
      expect(optionCodes()).toEqual(['country-option-ir']);
    });

    it('finds a country by its two-letter code', () => {
      openWith('TR');
      expect(optionCodes()).toContain('country-option-tr');
    });

    it('picking a result stores that country and turns auto-detect off', () => {
      openWith('king');
      fireEvent.click(screen.getByTestId('country-option-gb'));
      expect(useConfigStore.getState().country).toBe('gb');
      expect(useConfigStore.getState().autoDetect).toBe(false);
    });
  });
});
