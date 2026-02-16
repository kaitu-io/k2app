// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { Changelog } from '../Changelog';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'changelog.title': 'Changelog',
        'changelog.loading': 'Loading...',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

describe('Changelog', () => {
  afterEach(() => {
    cleanup();
  });

  it('test_changelog_iframe_loads — renders iframe with changelog URL and loading state', () => {
    render(<Changelog />);

    // Should have a loading indicator initially
    expect(screen.getByText('Loading...')).toBeInTheDocument();

    // Should render an iframe
    const iframe = screen.getByTestId('changelog-iframe') as HTMLIFrameElement;
    expect(iframe).toBeInTheDocument();
    expect(iframe.tagName).toBe('IFRAME');

    // iframe should have a src pointing to changelog
    expect(iframe.src).toContain('changelog');
  });
});
