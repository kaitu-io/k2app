/**
 * Feedback Store - 反馈工单未读计数管理
 *
 * 职责：
 * - 轮询未读工单回复数
 * - 登录时启动轮询，登出时停止
 * - 窗口不可见时暂停，可见时立即刷新并恢复
 */

import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import { cloudApi } from '../services/cloud-api';
import type { UnreadCount } from '../services/api-types';

const POLL_INTERVAL = 60_000; // 60s

interface FeedbackState {
  unreadCount: number;
  _pollTimer: ReturnType<typeof setInterval> | null;
  _visibilityHandler: (() => void) | null;
  _polling: boolean; // whether polling is logically active (auth'd)
  fetchUnread: () => Promise<void>;
  startPolling: () => void;
  stopPolling: () => void;
}

export const useFeedbackStore = create<FeedbackState>()(
  subscribeWithSelector((set, get) => ({
    unreadCount: 0,
    _pollTimer: null,
    _visibilityHandler: null,
    _polling: false,

    fetchUnread: async () => {
      const response = await cloudApi.get<UnreadCount>('/api/user/tickets/unread');
      if (response.code === 0 && response.data) {
        set({ unreadCount: response.data.unread });
      }
    },

    startPolling: () => {
      const state = get();
      if (state._polling) return;

      // Fetch immediately
      state.fetchUnread();

      // Start interval
      const timer = setInterval(() => { get().fetchUnread(); }, POLL_INTERVAL);

      // Visibility handler: pause when hidden, resume + fetch when visible
      const visibilityHandler = () => {
        const s = get();
        if (!s._polling) return;

        if (document.hidden) {
          // Pause: clear timer
          if (s._pollTimer) {
            clearInterval(s._pollTimer);
            set({ _pollTimer: null });
          }
        } else {
          // Resume: fetch immediately + restart timer
          s.fetchUnread();
          if (!s._pollTimer) {
            const newTimer = setInterval(() => { get().fetchUnread(); }, POLL_INTERVAL);
            set({ _pollTimer: newTimer });
          }
        }
      };
      document.addEventListener('visibilitychange', visibilityHandler);

      set({ _pollTimer: timer, _visibilityHandler: visibilityHandler, _polling: true });
    },

    stopPolling: () => {
      const state = get();
      if (state._pollTimer) {
        clearInterval(state._pollTimer);
      }
      if (state._visibilityHandler) {
        document.removeEventListener('visibilitychange', state._visibilityHandler);
      }
      set({ _pollTimer: null, _visibilityHandler: null, _polling: false, unreadCount: 0 });
    },
  }))
);
