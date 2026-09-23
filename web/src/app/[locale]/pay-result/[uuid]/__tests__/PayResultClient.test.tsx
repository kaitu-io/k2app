import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import PayResultClient from '../PayResultClient';

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string, values?: Record<string, unknown>) =>
    values ? `${key}:${JSON.stringify(values)}` : key,
}));
vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));
vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => ({ showNavigation: false, showFooter: false }),
}));

describe('PayResultClient', () => {
  it('renders order uuid and the two exits without any network call', () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch');
    render(<PayResultClient orderUuid="abc-123" />);
    expect(screen.getByText('title')).toBeInTheDocument();
    expect(screen.getByText('abc-123')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'account' })).toHaveAttribute('href', '/account');
    expect(screen.getByRole('link', { name: 'install' })).toHaveAttribute('href', '/install');
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
