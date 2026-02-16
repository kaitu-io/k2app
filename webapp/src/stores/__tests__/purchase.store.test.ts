import { describe, it, expect, vi, beforeEach } from 'vitest';
import { usePurchaseStore } from '../purchase.store';

vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getPlans: vi.fn(),
    previewOrder: vi.fn(),
    createOrder: vi.fn(),
  },
}));

import { cloudApi } from '../../api/cloud';

const mockPlans = [
  {
    id: 'plan-free',
    name: 'Free',
    description: 'Basic access',
    price: 0,
    period: 'monthly',
    features: ['1 device'],
  },
  {
    id: 'plan-pro',
    name: 'Pro',
    description: 'Full access',
    price: 9.99,
    period: 'monthly',
    features: ['5 devices', 'All servers'],
  },
  {
    id: 'plan-yearly',
    name: 'Pro Yearly',
    description: 'Full access yearly',
    price: 99.99,
    period: 'yearly',
    features: ['5 devices', 'All servers', 'Priority support'],
  },
];

describe('usePurchaseStore', () => {
  beforeEach(() => {
    usePurchaseStore.setState({
      plans: [],
      selectedPlanId: null,
      campaignCode: null,
      orderPreview: null,
      currentOrder: null,
      isLoading: false,
      error: null,
    });
    vi.clearAllMocks();
  });

  describe('loadPlans', () => {
    it('test_purchase_store_load_plans — loadPlans() calls cloudApi.getPlans(), stores list', async () => {
      vi.mocked(cloudApi.getPlans).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockPlans,
      });

      await usePurchaseStore.getState().loadPlans();

      expect(cloudApi.getPlans).toHaveBeenCalledOnce();

      const state = usePurchaseStore.getState();
      expect(state.plans).toHaveLength(3);
      expect(state.plans[0]).toEqual(mockPlans[0]);
      expect(state.plans[1]!.name).toBe('Pro');
      expect(state.isLoading).toBe(false);
    });
  });

  describe('selectPlan', () => {
    it('test_purchase_store_select_plan — selectPlan(id) sets selectedPlanId', () => {
      usePurchaseStore.getState().selectPlan('plan-pro');

      expect(usePurchaseStore.getState().selectedPlanId).toBe('plan-pro');
    });

    it('can change selection', () => {
      usePurchaseStore.getState().selectPlan('plan-pro');
      usePurchaseStore.getState().selectPlan('plan-yearly');

      expect(usePurchaseStore.getState().selectedPlanId).toBe('plan-yearly');
    });
  });

  describe('previewOrder', () => {
    it('test_purchase_store_preview_order — previewOrder() calls cloudApi.previewOrder()', async () => {
      usePurchaseStore.setState({ selectedPlanId: 'plan-pro' });

      const mockPreview = {
        planId: 'plan-pro',
        period: 'monthly',
        amount: 9.99,
        discount: 0,
        total: 9.99,
      };

      vi.mocked(cloudApi.previewOrder).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockPreview,
      });

      await usePurchaseStore.getState().previewOrder();

      expect(cloudApi.previewOrder).toHaveBeenCalledWith('plan-pro', expect.any(String));

      const state = usePurchaseStore.getState();
      expect(state.orderPreview).toEqual(mockPreview);
    });
  });

  describe('createOrder', () => {
    it('test_purchase_store_create_order — createOrder() calls cloudApi.createOrder()', async () => {
      usePurchaseStore.setState({ selectedPlanId: 'plan-pro' });

      const mockOrder = {
        id: 'order-123',
        planId: 'plan-pro',
        period: 'monthly',
        amount: 9.99,
        status: 'pending',
        createdAt: '2026-02-16T00:00:00Z',
      };

      vi.mocked(cloudApi.createOrder).mockResolvedValue({
        code: 0,
        message: 'ok',
        data: mockOrder,
      });

      await usePurchaseStore.getState().createOrder();

      expect(cloudApi.createOrder).toHaveBeenCalledWith('plan-pro', expect.any(String));

      const state = usePurchaseStore.getState();
      expect(state.currentOrder).toEqual(mockOrder);
    });
  });

  describe('setCampaignCode', () => {
    it('test_purchase_store_campaign_code — setCampaignCode(code) stores campaign code', () => {
      usePurchaseStore.getState().setCampaignCode('WELCOME2026');

      expect(usePurchaseStore.getState().campaignCode).toBe('WELCOME2026');
    });
  });
});
