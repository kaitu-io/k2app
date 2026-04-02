/**
 * Feedback Store - 反馈工单未读计数管理
 *
 * 职责：
 * - 轮询未读工单回复数
 * - 登录时启动轮询，登出时停止
 */

import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import { cloudApi } from '../services/cloud-api';
import type { UnreadCount } from '../services/api-types';

interface FeedbackState {
  unreadCount: number;
  _pollTimer: ReturnType<typeof setInterval> | null;
  fetchUnread: () => Promise<void>;
  startPolling: () => void;
  stopPolling: () => void;
  decrementUnread: (count: number) => void;
}

export const useFeedbackStore = create<FeedbackState>()(
  subscribeWithSelector((set, get) => ({
    unreadCount: 0,
    _pollTimer: null,

    fetchUnread: async () => {
      const response = await cloudApi.get<UnreadCount>('/api/user/tickets/unread');
      if (response.code === 0 && response.data) {
        set({ unreadCount: response.data.unread });
      }
    },

    startPolling: () => {
      const state = get();
      if (state._pollTimer) return;
      state.fetchUnread();
      const timer = setInterval(() => { get().fetchUnread(); }, 60_000);
      set({ _pollTimer: timer });
    },

    stopPolling: () => {
      const timer = get()._pollTimer;
      if (timer) {
        clearInterval(timer);
        set({ _pollTimer: null, unreadCount: 0 });
      }
    },

    decrementUnread: (count: number) => {
      set((state) => ({ unreadCount: Math.max(0, state.unreadCount - count) }));
    },
  }))
);
