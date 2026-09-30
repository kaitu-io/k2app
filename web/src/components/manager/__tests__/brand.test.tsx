/**
 * manager 全局品牌筛选：manager 只在 kaitu.io 一个入口，却要管两个品牌的数据。
 * 选择器是全局的（跨页保持，localStorage 持久化），页面经 useManagerBrand() 读取，
 * 选「全部」时不带 ?brand=（= API 不过滤）。
 */
import { render, screen, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  ManagerBrandProvider,
  ManagerBrandSelect,
  BrandPicker,
  BrandBadge,
  useManagerBrand,
  MANAGER_BRAND_STORAGE_KEY,
} from '../brand';
import { KAITU, OVERLEAP } from '@/lib/brands';

function Probe() {
  const { brand, brandParam } = useManagerBrand();
  return <div data-testid="probe">{`${brand}|${brandParam ?? "none"}`}</div>;
}

describe('ManagerBrandProvider', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('defaults to all (no brand param)', () => {
    render(<ManagerBrandProvider><ManagerBrandSelect /><Probe /></ManagerBrandProvider>);
    expect(screen.getByTestId('probe').textContent).toBe('all|none');
  });

  it('selecting a brand exposes it as the query param and persists it', () => {
    render(<ManagerBrandProvider><ManagerBrandSelect /><Probe /></ManagerBrandProvider>);
    fireEvent.change(screen.getByLabelText('品牌筛选'), { target: { value: 'overleap' } });
    expect(screen.getByTestId('probe').textContent).toBe('overleap|overleap');
    expect(window.localStorage.getItem(MANAGER_BRAND_STORAGE_KEY)).toBe('overleap');
  });

  it('restores a persisted choice', () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'kaitu');
    render(<ManagerBrandProvider><Probe /></ManagerBrandProvider>);
    expect(screen.getByTestId('probe').textContent).toBe('kaitu|kaitu');
  });

  it('ignores a garbage persisted value', () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'acme');
    render(<ManagerBrandProvider><Probe /></ManagerBrandProvider>);
    expect(screen.getByTestId('probe').textContent).toBe('all|none');
  });

  it('survives a throwing localStorage', () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('denied'); });
    const spy2 = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('denied'); });
    render(<ManagerBrandProvider><ManagerBrandSelect /><Probe /></ManagerBrandProvider>);
    expect(screen.getByTestId('probe').textContent).toBe('all|none');
    act(() => {
      fireEvent.change(screen.getByLabelText('品牌筛选'), { target: { value: 'kaitu' } });
    });
    expect(screen.getByTestId('probe').textContent).toBe('kaitu|kaitu');
    spy.mockRestore();
    spy2.mockRestore();
  });

  it('labels come from the brand registry', () => {
    render(<ManagerBrandProvider><ManagerBrandSelect /></ManagerBrandProvider>);
    const options = Array.from((screen.getByLabelText('品牌筛选') as HTMLSelectElement).options).map((o) => o.textContent);
    expect(options).toEqual(['全部品牌', KAITU.wordmark, OVERLEAP.wordmark]);
  });
});

describe('BrandPicker (create forms)', () => {
  it('has no preselected brand', () => {
    const onChange = vi.fn();
    render(<BrandPicker value="" onChange={onChange} />);
    const select = screen.getByLabelText('归属品牌') as HTMLSelectElement;
    expect(select.value).toBe('');
    fireEvent.change(select, { target: { value: 'overleap' } });
    expect(onChange).toHaveBeenCalledWith('overleap');
  });

  it('renders read-only when disabled (brand is immutable after create)', () => {
    render(<BrandPicker value="kaitu" onChange={() => {}} disabled />);
    expect((screen.getByLabelText('归属品牌') as HTMLSelectElement).disabled).toBe(true);
  });
});

describe('BrandBadge', () => {
  it('renders the registry wordmark', () => {
    render(<BrandBadge brand="overleap" />);
    expect(screen.getByText(OVERLEAP.wordmark)).toBeTruthy();
  });
  it('renders a dash for a missing brand', () => {
    render(<BrandBadge brand={undefined} />);
    expect(screen.getByText('—')).toBeTruthy();
  });
});
