/**
 * useEvaluation Hook Unit Tests
 *
 * Tests for the evaluation hook which calls the Rust service's
 * evaluate_tunnels action and updates the evaluation store.
 */

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useEvaluation } from '../useEvaluation';
import { useEvaluationStore } from '../../stores/evaluation.store';
import type { Tunnel } from '../../services/api-types';
import type { EvaluateTunnelsResponse } from '../../services/control-types';

// Mock window._k2 (IK2Vpn: only has run())
const mockRun = vi.fn();

beforeEach(() => {
  // Reset store state
  useEvaluationStore.getState().reset();

  // Setup mock — new architecture: _k2 only has run()
  (window as any)._k2 = {
    run: mockRun,
  };
});

afterEach(() => {
  vi.clearAllMocks();
  delete (window as any)._k2;
});

// Helper to create mock tunnels
function createMockTunnel(domain: string, nodeLoad = 30): Tunnel {
  return {
    id: Math.random().toString(),
    name: `Tunnel ${domain}`,
    domain,
    protocol: 'k2v4',
    port: 443,
    user_id: 'user1',
    node_id: 'node1',
    node: {
      name: `Node ${domain}`,
      country: 'US',
      region: 'us-west',
      ipv4: '1.2.3.4',
      ipv6: '',
      is_alive: true,
      load: nodeLoad,
    },
  };
}

describe('useEvaluation', () => {
  describe('evaluate', () => {
    it('should call evaluate_tunnels action with correct parameters', async () => {
      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          { domain: 'us1.example.com', final_score: 85, route_quality: 'excellent', is_overloaded: false },
        ],
        recommended_domain: 'us1.example.com',
        should_use_relay: false,
        relay_reason: null,
      };

      mockRun.mockResolvedValueOnce({
        code: 0,
        message: 'success',
        data: mockResponse,
      });

      const tunnels = [createMockTunnel('us1.example.com', 30)];

      const { result } = renderHook(() => useEvaluation({ tunnels }));

      await act(async () => {
        await result.current.evaluate();
      });

      expect(mockRun).toHaveBeenCalledWith('evaluate_tunnels', {
        tunnels: expect.arrayContaining([
          expect.objectContaining({
            domain: 'us1.example.com',
            node_load: 30,
          }),
        ]),
        has_relays: false,
      });
    });

    it('should update store with evaluation results', async () => {
      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          { domain: 'tokyo.example.com', final_score: 90, route_quality: 'excellent', is_overloaded: false },
          { domain: 'seoul.example.com', final_score: 75, route_quality: 'good', is_overloaded: false },
        ],
        recommended_domain: 'tokyo.example.com',
        should_use_relay: false,
        relay_reason: null,
      };

      mockRun.mockResolvedValueOnce({
        code: 0,
        message: 'success',
        data: mockResponse,
      });

      const tunnels = [
        createMockTunnel('tokyo.example.com', 20),
        createMockTunnel('seoul.example.com', 40),
      ];

      const { result } = renderHook(() => useEvaluation({ tunnels }));

      await act(async () => {
        await result.current.evaluate();
      });

      // Check store was updated
      const store = useEvaluationStore.getState();
      expect(store.status).toBe('completed');
      expect(store.evaluatedTunnels).toHaveLength(2);
      expect(store.recommendedDomain).toBe('tokyo.example.com');
    });

    it('should set status to loading during evaluation', async () => {
      let resolvePromise: (value: any) => void;
      const pendingPromise = new Promise((resolve) => {
        resolvePromise = resolve;
      });

      mockRun.mockReturnValueOnce(pendingPromise);

      const tunnels = [createMockTunnel('us1.example.com')];
      const { result } = renderHook(() => useEvaluation({ tunnels }));

      // Start evaluation but don't await
      act(() => {
        result.current.evaluate();
      });

      // Status should be loading
      expect(useEvaluationStore.getState().status).toBe('loading');

      // Resolve the promise
      await act(async () => {
        resolvePromise!({
          code: 0,
          message: 'success',
          data: {
            evaluated_tunnels: [],
            recommended_domain: null,
            should_use_relay: false,
            relay_reason: null,
          },
        });
      });

      // Status should be completed
      expect(useEvaluationStore.getState().status).toBe('completed');
    });

    it('should set status to error on failure', async () => {
      mockRun.mockResolvedValueOnce({
        code: 500,
        message: 'Internal error',
        data: null,
      });

      const tunnels = [createMockTunnel('us1.example.com')];
      const { result } = renderHook(() => useEvaluation({ tunnels }));

      await act(async () => {
        await result.current.evaluate();
      });

      expect(useEvaluationStore.getState().status).toBe('error');
    });

    it('should not evaluate if tunnels is empty', async () => {
      const { result } = renderHook(() => useEvaluation({ tunnels: [] }));

      await act(async () => {
        await result.current.evaluate();
      });

      expect(mockRun).not.toHaveBeenCalled();
      expect(useEvaluationStore.getState().status).toBe('idle');
    });
  });

  describe('autoEvaluate', () => {
    it('should auto-evaluate when tunnels change and autoEvaluate is true', async () => {
      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          { domain: 'auto.example.com', final_score: 80, route_quality: 'good', is_overloaded: false },
        ],
        recommended_domain: 'auto.example.com',
        should_use_relay: false,
        relay_reason: null,
      };

      mockRun.mockResolvedValue({
        code: 0,
        message: 'success',
        data: mockResponse,
      });

      const tunnels = [createMockTunnel('auto.example.com')];

      renderHook(() => useEvaluation({ tunnels, autoEvaluate: true }));

      await waitFor(() => {
        expect(mockRun).toHaveBeenCalledWith('evaluate_tunnels', expect.any(Object));
      });
    });

    it('should not auto-evaluate when autoEvaluate is false', async () => {
      const tunnels = [createMockTunnel('no-auto.example.com')];

      renderHook(() => useEvaluation({ tunnels, autoEvaluate: false }));

      // Wait a bit to ensure no call is made
      await new Promise((resolve) => setTimeout(resolve, 100));

      expect(mockRun).not.toHaveBeenCalled();
    });
  });

  describe('getRouteQuality', () => {
    it('should return route quality score from store', async () => {
      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          { domain: 'excellent.com', final_score: 95, route_quality: 'excellent', is_overloaded: false },
          { domain: 'good.com', final_score: 75, route_quality: 'good', is_overloaded: false },
        ],
        recommended_domain: 'excellent.com',
        should_use_relay: false,
        relay_reason: null,
      };

      mockRun.mockResolvedValueOnce({
        code: 0,
        message: 'success',
        data: mockResponse,
      });

      const tunnels = [
        createMockTunnel('excellent.com'),
        createMockTunnel('good.com'),
      ];

      const { result } = renderHook(() => useEvaluation({ tunnels }));

      await act(async () => {
        await result.current.evaluate();
      });

      expect(result.current.getRouteQuality('excellent.com')).toBe(5);
      expect(result.current.getRouteQuality('good.com')).toBe(4);
      expect(result.current.getRouteQuality('nonexistent.com')).toBe(0);
    });
  });

  describe('isRunning', () => {
    it('should reflect loading status', async () => {
      let resolvePromise: (value: any) => void;
      const pendingPromise = new Promise((resolve) => {
        resolvePromise = resolve;
      });

      mockRun.mockReturnValueOnce(pendingPromise);

      const tunnels = [createMockTunnel('us1.example.com')];
      const { result } = renderHook(() => useEvaluation({ tunnels }));

      expect(result.current.isRunning).toBe(false);

      act(() => {
        result.current.evaluate();
      });

      expect(result.current.isRunning).toBe(true);

      await act(async () => {
        resolvePromise!({
          code: 0,
          message: 'success',
          data: {
            evaluated_tunnels: [],
            recommended_domain: null,
            should_use_relay: false,
            relay_reason: null,
          },
        });
      });

      expect(result.current.isRunning).toBe(false);
    });
  });

  describe('relay handling', () => {
    it('should pass has_relays parameter correctly', async () => {
      mockRun.mockResolvedValueOnce({
        code: 0,
        message: 'success',
        data: {
          evaluated_tunnels: [],
          recommended_domain: null,
          should_use_relay: false,
          relay_reason: null,
        },
      });

      const tunnels = [createMockTunnel('us1.example.com')];
      const { result } = renderHook(() => useEvaluation({ tunnels, hasRelays: true }));

      await act(async () => {
        await result.current.evaluate();
      });

      expect(mockRun).toHaveBeenCalledWith('evaluate_tunnels', expect.objectContaining({
        has_relays: true,
      }));
    });

    it('should store relay recommendation in store', async () => {
      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          { domain: 'poor.example.com', final_score: 20, route_quality: 'poor', is_overloaded: false },
        ],
        recommended_domain: 'poor.example.com',
        should_use_relay: true,
        relay_reason: 'All routes have poor quality',
      };

      mockRun.mockResolvedValueOnce({
        code: 0,
        message: 'success',
        data: mockResponse,
      });

      const tunnels = [createMockTunnel('poor.example.com')];
      const { result } = renderHook(() => useEvaluation({ tunnels, hasRelays: true }));

      await act(async () => {
        await result.current.evaluate();
      });

      const store = useEvaluationStore.getState();
      expect(store.shouldUseRelay).toBe(true);
      expect(store.relayReason).toBe('All routes have poor quality');
    });
  });
});
