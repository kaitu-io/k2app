import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import LanguageDialog from '../LanguageDialog';
import { languages } from '../../i18n/i18n';
import { brandConfig } from '../../brands';

vi.mock('react-i18next', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-i18next')>()),
  useTranslation: () => ({ t: (key: string) => key }),
}));

const setup = (value = brandConfig.defaultLocale) => {
  const onSelect = vi.fn();
  const onClose = vi.fn();
  render(<LanguageDialog open value={value} onSelect={onSelect} onClose={onClose} />);
  const options = () => within(screen.getByRole('list')).getAllByRole('button');
  const search = screen.getByLabelText('account:account.searchLanguage');
  return { onSelect, onClose, options, search };
};

describe('LanguageDialog', () => {
  // setup.ts restores all mocks after each test, which strips the
  // getComputedStyle implementation MUI's Modal needs to mount.
  beforeEach(() => {
    window.getComputedStyle = (() => ({ paddingRight: '0px', getPropertyValue: () => '' })) as any;
  });

  it('lists exactly the languages the brand offers, current one first', () => {
    const { options } = setup('ja');
    expect(options()).toHaveLength(brandConfig.locales.length);
    expect(options()[0]).toHaveTextContent(languages.ja.nativeName);
  });

  it('filters by English name and reports the pick', () => {
    const { options, search, onSelect } = setup();
    fireEvent.change(search, { target: { value: 'japanese' } });
    expect(options()).toHaveLength(1);
    fireEvent.click(options()[0]);
    expect(onSelect).toHaveBeenCalledWith('ja');
  });

  it('shows the empty state when nothing matches', () => {
    const { search } = setup();
    fireEvent.change(search, { target: { value: 'klingon' } });
    expect(screen.queryByRole('list')).toBeNull();
    expect(screen.getByText('account:account.noLanguageFound')).toBeInTheDocument();
  });
});
