import { describe, it, expect } from 'vitest';
import { canShip, canMint, isStuck, isBYO, STAGE_LABEL } from '../router-fulfillment-helpers';
import type { AdminRouterFulfillmentItem } from '@/lib/api';

function item(p: Partial<AdminRouterFulfillmentItem>): AdminRouterFulfillmentItem {
  return {
    id: 1, orderId: 1, hardwareSku: 'redmi-ax6s', stage: 'ready', shippedAt: 0, activatedAt: 0,
    credentialMinted: false, canMintCredential: true, createdAt: 0,
    userId: 1, email: 'a@b.c', subId: 1, note: '', updatedBy: '', updatedAt: 1_000_000,
    ...p,
  };
}

describe('router fulfillment helpers', () => {
  it('六个阶段都有中文名', () => {
    expect(Object.keys(STAGE_LABEL).sort()).toEqual(['expired', 'online', 'paid', 'provisioning', 'ready', 'shipped']);
  });
  it('isBYO 看 hardwareSku 是否为空', () => {
    expect(isBYO(item({ hardwareSku: '' }))).toBe(true);
    expect(isBYO(item({}))).toBe(false);
  });
  it('只有成品 ready 能发货', () => {
    expect(canShip(item({}))).toBe(true);
    expect(canShip(item({ hardwareSku: '' }))).toBe(false);
    expect(canShip(item({ stage: 'shipped' }))).toBe(false);
  });
  it('代铸需要后端允许且阶段在 ready/shipped/online', () => {
    expect(canMint(item({}))).toBe(true);
    expect(canMint(item({ stage: 'online' }))).toBe(true);
    expect(canMint(item({ stage: 'provisioning' }))).toBe(false);
    expect(canMint(item({ stage: 'expired' }))).toBe(false);
    expect(canMint(item({ canMintCredential: false }))).toBe(false);
  });
  it('卡住：未完成阶段超过 48 小时', () => {
    const now = 1_000_000 + 48 * 3600 + 1;
    expect(isStuck(item({ stage: 'ready' }), now)).toBe(true);
    expect(isStuck(item({ stage: 'shipped' }), now)).toBe(false);
    expect(isStuck(item({ stage: 'ready' }), 1_000_000 + 3600)).toBe(false);
  });
});
