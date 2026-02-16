import { create } from 'zustand';
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

export const usePurchaseStore = create<PurchaseStore>(() => ({
  plans: [],
  selectedPlanId: null,
  campaignCode: null,
  orderPreview: null,
  currentOrder: null,
  isLoading: false,
  error: null,

  loadPlans: async () => { throw new Error('Not implemented'); },
  selectPlan: () => { throw new Error('Not implemented'); },
  setCampaignCode: () => { throw new Error('Not implemented'); },
  previewOrder: async () => { throw new Error('Not implemented'); },
  createOrder: async () => { throw new Error('Not implemented'); },
}));
