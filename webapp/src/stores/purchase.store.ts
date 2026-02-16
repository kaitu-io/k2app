import { create } from 'zustand';
import { cloudApi } from '../api/cloud';
import type { Plan, Order } from '../api/types';

export interface OrderPreview {
  planId: string;
  period: string;
  amount: number;
  discount?: number;
  total: number;
}

export interface PurchaseStore {
  plans: Plan[];
  selectedPlanId: string | null;
  campaignCode: string | null;
  orderPreview: OrderPreview | null;
  currentOrder: Order | null;
  isLoading: boolean;
  error: string | null;

  loadPlans: () => Promise<void>;
  selectPlan: (id: string) => void;
  setCampaignCode: (code: string) => void;
  previewOrder: () => Promise<void>;
  createOrder: () => Promise<void>;
}

export const usePurchaseStore = create<PurchaseStore>((set, get) => ({
  plans: [],
  selectedPlanId: null,
  campaignCode: null,
  orderPreview: null,
  currentOrder: null,
  isLoading: false,
  error: null,

  loadPlans: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.getPlans();
      const plans = (resp.data ?? []) as Plan[];
      set({ plans, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to load plans',
      });
    }
  },

  selectPlan: (id: string) => {
    set({ selectedPlanId: id });
  },

  setCampaignCode: (code: string) => {
    set({ campaignCode: code });
  },

  previewOrder: async () => {
    const { selectedPlanId, plans } = get();
    if (!selectedPlanId) return;

    const plan = plans.find((p) => p.id === selectedPlanId);
    const period = plan?.period ?? 'monthly';

    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.previewOrder(selectedPlanId, period);
      const preview = resp.data as OrderPreview;
      set({ orderPreview: preview, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to preview order',
      });
    }
  },

  createOrder: async () => {
    const { selectedPlanId, plans } = get();
    if (!selectedPlanId) return;

    const plan = plans.find((p) => p.id === selectedPlanId);
    const period = plan?.period ?? 'monthly';

    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.createOrder(selectedPlanId, period);
      const order = resp.data as Order;
      set({ currentOrder: order, isLoading: false });
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to create order',
      });
    }
  },
}));
