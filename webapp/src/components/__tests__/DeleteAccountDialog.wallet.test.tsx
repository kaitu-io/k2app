/**
 * DeleteAccountDialog: the "you lose your wallet balance" line is a kaitu-only
 * surface (features.wallet). A brand without a wallet must not mention one.
 * Toggles the live brandConfig flag so both branches run under either build brand.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';

vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

import DeleteAccountDialog from '../DeleteAccountDialog';
import { brandConfig } from '../../brands';

const original = brandConfig.features.wallet;
afterEach(() => {
  (brandConfig.features as { wallet: boolean }).wallet = original;
});

function renderDialog() {
  return render(
    <I18nextProvider i18n={i18n}>
      <DeleteAccountDialog open user={null} loading={false} error={null} onClose={vi.fn()} onConfirm={vi.fn()} />
    </I18nextProvider>
  );
}

describe('DeleteAccountDialog wallet line', () => {
  it('is shown when the brand has a wallet', () => {
    (brandConfig.features as { wallet: boolean }).wallet = true;
    renderDialog();
    expect(screen.getByText(i18n.t('account:account.deleteLoseWallet'))).toBeTruthy();
  });

  it('is hidden when the brand has no wallet', () => {
    (brandConfig.features as { wallet: boolean }).wallet = false;
    renderDialog();
    expect(screen.queryByText(i18n.t('account:account.deleteLoseWallet'))).toBeNull();
  });
});
