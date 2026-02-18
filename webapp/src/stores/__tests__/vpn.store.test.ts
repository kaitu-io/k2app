/**
 * VPN Store 单元测试
 *
 * 测试内容：
 * - 状态管理 (setStatus)
 * - 乐观更新 (setOptimisticState)
 * - 状态派生 (通过 useVPNStatus hook)
 * - 状态防抖机制
 *
 * 注意: 认证错误 (401/402) 现在通过 k2apiEvents 统一处理
 * 相关测试在 initializeVPNStore 集成测试中
 *
 * 运行: yarn test src/stores/__tests__/vpn.store.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useVPNStore, useVPNStatus } from '../vpn.store';
import { act, renderHook } from '@testing-library/react';
import type { StatusResponseData, ControlError } from '../../services/vpn-types';

describe('VPN Store', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // 重置 store 状态
    useVPNStore.setState({
      status: null,
      localState: null,
      serviceConnected: true,
      serviceFailedSince: null,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  // ==================== 基础状态管理测试 ====================

  describe('Basic State Management', () => {
    it('初始状态应该是 null', () => {
      const state = useVPNStore.getState();
      expect(state.status).toBeNull();
      expect(state.localState).toBeNull();
    });

    it('setStatus 应该更新状态', () => {
      const status: StatusResponseData = {
        running: true,
        state: 'connected',
        startAt: Date.now(),
      };

      act(() => {
        useVPNStore.getState().setStatus(status);
      });

      expect(useVPNStore.getState().status).toEqual(status);
    });

    it('useVPNStatus hook 应该返回正确的 serviceState', () => {
      const status: StatusResponseData = {
        running: true,
        state: 'connected',
        startAt: Date.now(),
      };

      act(() => {
        useVPNStore.getState().setStatus(status);
      });

      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.serviceState).toBe('connected');
    });

    it('status 为 null 时 serviceState 应该返回 disconnected', () => {
      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.serviceState).toBe('disconnected');
    });
  });

  // ==================== 乐观更新测试 ====================

  describe('Optimistic Updates', () => {
    it('setOptimisticState 应该设置本地状态', () => {
      act(() => {
        useVPNStore.getState().setOptimisticState('connecting');
      });

      expect(useVPNStore.getState().localState).toBe('connecting');
    });

    it('乐观状态应该优先于后端状态', () => {
      const status: StatusResponseData = {
        running: false,
        state: 'disconnected',
        startAt: 0,
      };

      act(() => {
        useVPNStore.getState().setStatus(status);
        useVPNStore.getState().setOptimisticState('connecting');
      });

      // 本地状态优先
      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.serviceState).toBe('connecting');
    });

    it('setOptimisticState(null) 应该清除本地状态', () => {
      act(() => {
        useVPNStore.getState().setOptimisticState('connecting');
      });

      expect(useVPNStore.getState().localState).toBe('connecting');

      act(() => {
        useVPNStore.getState().setOptimisticState(null);
      });

      expect(useVPNStore.getState().localState).toBeNull();
    });

    it('乐观状态应该在 5 秒后自动清除（超时保护）', () => {
      act(() => {
        useVPNStore.getState().setOptimisticState('connecting');
      });

      expect(useVPNStore.getState().localState).toBe('connecting');

      // 前进 5 秒
      act(() => {
        vi.advanceTimersByTime(5000);
      });

      expect(useVPNStore.getState().localState).toBeNull();
    });

    it('重新设置乐观状态应该重置超时', () => {
      act(() => {
        useVPNStore.getState().setOptimisticState('connecting');
      });

      // 前进 3 秒
      act(() => {
        vi.advanceTimersByTime(3000);
      });

      expect(useVPNStore.getState().localState).toBe('connecting');

      // 重新设置
      act(() => {
        useVPNStore.getState().setOptimisticState('disconnecting');
      });

      // 再前进 3 秒（从重新设置开始计算）
      act(() => {
        vi.advanceTimersByTime(3000);
      });

      // 应该还存在
      expect(useVPNStore.getState().localState).toBe('disconnecting');

      // 再前进 2 秒（总共 5 秒）
      act(() => {
        vi.advanceTimersByTime(2000);
      });

      expect(useVPNStore.getState().localState).toBeNull();
    });
  });

  // ==================== 状态派生测试 ====================

  describe('Derived State', () => {
    it('isConnected 应该在 connected 状态时返回 true', () => {
      act(() => {
        useVPNStore.getState().setStatus({
          running: true,
          state: 'connected',
          startAt: Date.now(),
        });
      });

      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.isConnected).toBe(true);
      expect(result.current.isDisconnected).toBe(false);
    });

    it('isDisconnected 应该在 disconnected 状态时返回 true', () => {
      act(() => {
        useVPNStore.getState().setStatus({
          running: false,
          state: 'disconnected',
          startAt: 0,
        });
      });

      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.isConnected).toBe(false);
      expect(result.current.isDisconnected).toBe(true);
    });

    it('isTransitioning 应该在过渡状态时返回 true', () => {
      const transitionalStates = ['connecting', 'reconnecting', 'disconnecting'] as const;

      for (const state of transitionalStates) {
        act(() => {
          useVPNStore.getState().setStatus({
            running: false,
            state,
            startAt: 0,
          });
        });

        const { result } = renderHook(() => useVPNStatus());
        expect(result.current.isTransitioning).toBe(true);
      }
    });

    it('isServiceRunning 应该在 VPN 运行相关状态时返回 true', () => {
      const runningStates = ['connected', 'connecting', 'reconnecting', 'error'] as const;

      for (const state of runningStates) {
        act(() => {
          useVPNStore.getState().setStatus({
            running: false,
            state,
            startAt: 0,
          });
        });

        const { result } = renderHook(() => useVPNStatus());
        expect(result.current.isServiceRunning).toBe(true);
      }

      // disconnected 时应该返回 false
      act(() => {
        useVPNStore.getState().setStatus({
          running: false,
          state: 'disconnected',
          startAt: 0,
        });
      });

      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.isServiceRunning).toBe(false);
    });

    it('error 应该返回状态中的错误信息', () => {
      const error: ControlError = {
        code: 401,
        message: 'Unauthorized',
      };

      act(() => {
        useVPNStore.getState().setStatus({
          running: false,
          state: 'error',
          startAt: 0,
          error,
        });
      });

      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.error).toEqual(error);
    });

    it('error 应该在没有错误时返回 null', () => {
      act(() => {
        useVPNStore.getState().setStatus({
          running: true,
          state: 'connected',
          startAt: Date.now(),
        });
      });

      const { result } = renderHook(() => useVPNStatus());
      expect(result.current.error).toBeNull();
    });
  });

  // ==================== useVPNStatus Hook 测试 ====================

  describe('useVPNStatus Hook', () => {
    it('应该返回所有状态和方法', () => {
      const { result } = renderHook(() => useVPNStatus());

      expect(result.current).toHaveProperty('serviceState');
      expect(result.current).toHaveProperty('error');
      expect(result.current).toHaveProperty('isConnected');
      expect(result.current).toHaveProperty('isDisconnected');
      expect(result.current).toHaveProperty('isConnecting');
      expect(result.current).toHaveProperty('isReconnecting');
      expect(result.current).toHaveProperty('isDisconnecting');
      expect(result.current).toHaveProperty('isError');
      expect(result.current).toHaveProperty('isTransitioning');
      expect(result.current).toHaveProperty('isServiceRunning');
      expect(result.current).toHaveProperty('setOptimisticState');
    });

    it('应该响应状态变化', () => {
      const { result } = renderHook(() => useVPNStatus());

      expect(result.current.serviceState).toBe('disconnected');

      act(() => {
        useVPNStore.getState().setStatus({
          running: true,
          state: 'connected',
          startAt: Date.now(),
        });
      });

      expect(result.current.serviceState).toBe('connected');
      expect(result.current.isConnected).toBe(true);
    });

    it('setOptimisticState 应该工作', () => {
      const { result } = renderHook(() => useVPNStatus());

      act(() => {
        result.current.setOptimisticState('connecting');
      });

      // 由于 hook 使用 getter 函数，需要直接检查 store 状态
      expect(useVPNStore.getState().localState).toBe('connecting');

      // Check derived state through the hook
      const { result: result2 } = renderHook(() => useVPNStatus());
      expect(result2.current.serviceState).toBe('connecting');
      expect(result2.current.isConnecting).toBe(true);
    });
  });

  // ==================== 状态转换验证测试 ====================

  describe('State Transition Validation', () => {
    // 这些测试验证 isValidStateTransition 的逻辑
    // 该函数是私有的，通过行为来测试

    it('connecting -> connected 应该是有效转换', () => {
      // 设置乐观状态
      act(() => {
        useVPNStore.getState().setOptimisticState('connecting');
      });

      expect(useVPNStore.getState().localState).toBe('connecting');

      // 模拟后端状态变为 connected
      act(() => {
        useVPNStore.getState().setStatus({
          running: true,
          state: 'connected',
          startAt: Date.now(),
        });
      });

      // 注意：这里只是设置了 status，但 localState 的清除
      // 是在 initializeVPNStore 的 onStatusChange 回调中处理的
      // 这个测试主要验证状态设置本身
      expect(useVPNStore.getState().status?.state).toBe('connected');
    });

    it('disconnecting -> disconnected 应该是有效转换', () => {
      act(() => {
        useVPNStore.getState().setOptimisticState('disconnecting');
      });

      expect(useVPNStore.getState().localState).toBe('disconnecting');

      act(() => {
        useVPNStore.getState().setStatus({
          running: false,
          state: 'disconnected',
          startAt: 0,
        });
      });

      expect(useVPNStore.getState().status?.state).toBe('disconnected');
    });
  });

  // ==================== 边界情况测试 ====================

  describe('Edge Cases', () => {
    it('status 为 null 时所有布尔派生状态应该是安全的', () => {
      const { result } = renderHook(() => useVPNStatus());

      expect(result.current.isConnected).toBe(false);
      expect(result.current.isDisconnected).toBe(true); // 默认 disconnected
      expect(result.current.isConnecting).toBe(false);
      expect(result.current.isReconnecting).toBe(false);
      expect(result.current.isDisconnecting).toBe(false);
      expect(result.current.isError).toBe(false);
      expect(result.current.isTransitioning).toBe(false);
      expect(result.current.isServiceRunning).toBe(false);
      expect(result.current.error).toBeNull();
    });

    it('快速连续设置乐观状态应该只保留最后一个', () => {
      act(() => {
        useVPNStore.getState().setOptimisticState('connecting');
        useVPNStore.getState().setOptimisticState('disconnecting');
        useVPNStore.getState().setOptimisticState('reconnecting');
      });

      expect(useVPNStore.getState().localState).toBe('reconnecting');
    });

    it('setStatus(null) 应该清除状态', () => {
      act(() => {
        useVPNStore.getState().setStatus({
          running: true,
          state: 'connected',
          startAt: Date.now(),
        });
      });

      expect(useVPNStore.getState().status).not.toBeNull();

      act(() => {
        useVPNStore.getState().setStatus(null);
      });

      expect(useVPNStore.getState().status).toBeNull();
    });
  });
});
