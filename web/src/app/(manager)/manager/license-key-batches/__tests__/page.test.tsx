/**
 * 授权码批次页的品牌维度：列表跟随全局品牌筛选；创建必须显式选品牌（无默认值）。
 */
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ManagerBrandProvider, MANAGER_BRAND_STORAGE_KEY } from '@/components/manager/brand';

const mockList = vi.fn();
const mockStats = vi.fn();
const mockCreate = vi.fn();

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      listLicenseKeyBatches: (...a: unknown[]) => mockList(...a),
      getLicenseKeyBatchStats: (...a: unknown[]) => mockStats(...a),
      createLicenseKeyBatch: (...a: unknown[]) => mockCreate(...a),
    },
  };
});

import LicenseKeyBatchesPage from '../page';

function renderPage() {
  return render(
    <ManagerBrandProvider>
      <LicenseKeyBatchesPage />
    </ManagerBrandProvider>,
  );
}

describe('license-key-batches brand', () => {
  beforeEach(() => {
    window.localStorage.clear();
    mockList.mockReset().mockResolvedValue({
      items: [{ id: 1, name: 'b1', sourceTag: '', recipientMatcher: 'all', planDays: 30, quantity: 10, expiresAt: 0, note: '', createdByUserId: 1, redeemedCount: 0, expiredCount: 0, createdAt: 0, brand: 'overleap' }],
      total: 1,
    });
    mockStats.mockReset().mockResolvedValue([]);
    mockCreate.mockReset().mockResolvedValue(undefined);
  });

  it('list follows the global brand filter', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'overleap');
    renderPage();
    await waitFor(() => expect(mockList).toHaveBeenCalled());
    expect(mockList.mock.calls[0][0]).toMatchObject({ brand: 'overleap' });
    expect(mockList).toHaveBeenCalledTimes(1); // no extra "all brands" fetch before the stored filter is read
  });

  it('list sends no brand when filter is all', async () => {
    renderPage();
    await waitFor(() => expect(mockList).toHaveBeenCalled());
    expect(mockList.mock.calls[0][0].brand).toBeUndefined();
  });

  it('create requires an explicit brand', async () => {
    renderPage();
    await waitFor(() => expect(mockList).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: /创建批次/ }));
    fireEvent.change(screen.getByPlaceholderText('Apr Twitter 投放'), { target: { value: 'x' } });
    const submit = screen.getByRole('button', { name: '提交审批' }) as HTMLButtonElement;
    expect((screen.getByLabelText('归属品牌') as HTMLSelectElement).value).toBe('');
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('归属品牌'), { target: { value: 'kaitu' } });
    expect(submit.disabled).toBe(false);
    fireEvent.click(submit);
    await waitFor(() => expect(mockCreate).toHaveBeenCalled());
    expect(mockCreate.mock.calls[0][0]).toMatchObject({ name: 'x', brand: 'kaitu' });
  });
});
