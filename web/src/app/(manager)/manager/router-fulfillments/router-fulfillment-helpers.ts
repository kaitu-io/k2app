import type { AdminRouterFulfillmentItem, RouterStage } from '@/lib/api';

export const STAGE_LABEL: Record<RouterStage, string> = {
  paid: '已付款',
  provisioning: '线路开通中',
  ready: '线路就绪',
  shipped: '已发货',
  online: '已上线',
  expired: '已过期',
};

export const STAGE_VARIANT: Record<RouterStage, 'default' | 'secondary' | 'destructive' | 'outline'> = {
  paid: 'outline',
  provisioning: 'secondary',
  ready: 'default',
  shipped: 'secondary',
  online: 'default',
  expired: 'destructive',
};

export function isBYO(item: Pick<AdminRouterFulfillmentItem, 'hardwareSku'>): boolean {
  return item.hardwareSku === '';
}

// 发货按钮：成品、stage=ready
export function canShip(item: AdminRouterFulfillmentItem): boolean {
  return !isBYO(item) && item.stage === 'ready';
}

// 代铸按钮：stage ∈ ready/shipped/online 且后端判定可铸（持有可服务线路）。
// 「是否是该用户最新一条」由后端判定，前端不猜，被拒时显示后端错误。
export function canMint(item: AdminRouterFulfillmentItem): boolean {
  return item.canMintCredential && (item.stage === 'ready' || item.stage === 'shipped' || item.stage === 'online');
}

// 卡住：未完成阶段且超过 48 小时未更新
export function isStuck(item: AdminRouterFulfillmentItem, nowSec: number): boolean {
  return (item.stage === 'paid' || item.stage === 'provisioning' || item.stage === 'ready')
    && nowSec - item.updatedAt > 48 * 3600;
}
