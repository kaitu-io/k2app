/**
 * Evaluation Store Unit Tests
 *
 * Tests for the evaluation store which manages tunnel evaluation state
 * using the Rust service's evaluate_tunnels action.
 */

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useEvaluationStore } from '../evaluation.store';
import type { EvaluateTunnelsResponse, EvaluatedTunnelOutput } from '../../services/control-types';

describe('EvaluationStore', () => {
  beforeEach(() => {
    // Reset store state before each test
    useEvaluationStore.getState().reset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('initial state', () => {
    it('should have idle status initially', () => {
      const { result } = renderHook(() => useEvaluationStore());

      expect(result.current.status).toBe('idle');
      expect(result.current.evaluatedTunnels).toEqual([]);
      expect(result.current.recommendedDomain).toBeNull();
      expect(result.current.shouldUseRelay).toBe(false);
    });
  });

  describe('setResult', () => {
    it('should update state with evaluation results', () => {
      const { result } = renderHook(() => useEvaluationStore());

      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          {
            domain: 'us1.example.com',
            final_score: 85.5,
            route_quality: 'excellent',
            is_overloaded: false,
          },
          {
            domain: 'jp1.example.com',
            final_score: 72.0,
            route_quality: 'good',
            is_overloaded: false,
          },
        ],
        recommended_domain: 'us1.example.com',
        should_use_relay: false,
        relay_reason: null,
      };

      act(() => {
        result.current.setResult(mockResponse);
      });

      expect(result.current.status).toBe('completed');
      expect(result.current.evaluatedTunnels).toHaveLength(2);
      expect(result.current.recommendedDomain).toBe('us1.example.com');
      expect(result.current.shouldUseRelay).toBe(false);
    });

    it('should store evaluated tunnels in a Map keyed by domain', () => {
      const { result } = renderHook(() => useEvaluationStore());

      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          {
            domain: 'tokyo.example.com',
            final_score: 90.0,
            route_quality: 'excellent',
            is_overloaded: false,
          },
        ],
        recommended_domain: 'tokyo.example.com',
        should_use_relay: false,
        relay_reason: null,
      };

      act(() => {
        result.current.setResult(mockResponse);
      });

      // Should be able to look up by domain
      const storedResult = result.current.tunnelResults.get('tokyo.example.com');
      expect(storedResult).toBeDefined();
      expect(storedResult?.final_score).toBe(90.0);
      expect(storedResult?.route_quality).toBe('excellent');
    });

    it('should handle relay fallback response', () => {
      const { result } = renderHook(() => useEvaluationStore());

      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          {
            domain: 'poor.example.com',
            final_score: 25.0,
            route_quality: 'poor',
            is_overloaded: false,
          },
        ],
        recommended_domain: 'poor.example.com',
        should_use_relay: true,
        relay_reason: 'All routes have poor quality',
      };

      act(() => {
        result.current.setResult(mockResponse);
      });

      expect(result.current.shouldUseRelay).toBe(true);
      expect(result.current.relayReason).toBe('All routes have poor quality');
    });
  });

  describe('getRouteQuality', () => {
    it('should return route quality score (0-5) for a domain', () => {
      const { result } = renderHook(() => useEvaluationStore());

      const mockResponse: EvaluateTunnelsResponse = {
        evaluated_tunnels: [
          { domain: 'excellent.com', final_score: 90, route_quality: 'excellent', is_overloaded: false },
          { domain: 'good.com', final_score: 70, route_quality: 'good', is_overloaded: false },
          { domain: 'fair.com', final_score: 50, route_quality: 'fair', is_overloaded: false },
          { domain: 'poor.com', final_score: 30, route_quality: 'poor', is_overloaded: false },
          { domain: 'unknown.com', final_score: 20, route_quality: 'unknown', is_overloaded: false },
        ],
        recommended_domain: 'excellent.com',
        should_use_relay: false,
        relay_reason: null,
      };

      act(() => {
        result.current.setResult(mockResponse);
      });

      // Route quality mapping: excellent=5, good=4, fair=3, poor=2, unknown=1
      expect(result.current.getRouteQuality('excellent.com')).toBe(5);
      expect(result.current.getRouteQuality('good.com')).toBe(4);
      expect(result.current.getRouteQuality('fair.com')).toBe(3);
      expect(result.current.getRouteQuality('poor.com')).toBe(2);
      expect(result.current.getRouteQuality('unknown.com')).toBe(1);
      expect(result.current.getRouteQuality('nonexistent.com')).toBe(0);
    });
  });

  describe('setStatus', () => {
    it('should update status', () => {
      const { result } = renderHook(() => useEvaluationStore());

      act(() => {
        result.current.setStatus('loading');
      });

      expect(result.current.status).toBe('loading');
    });
  });

  describe('reset', () => {
    it('should reset all state to initial values', () => {
      const { result } = renderHook(() => useEvaluationStore());

      // First set some state
      act(() => {
        result.current.setResult({
          evaluated_tunnels: [{ domain: 'test.com', final_score: 80, route_quality: 'good', is_overloaded: false }],
          recommended_domain: 'test.com',
          should_use_relay: true,
          relay_reason: 'test',
        });
      });

      // Then reset
      act(() => {
        result.current.reset();
      });

      expect(result.current.status).toBe('idle');
      expect(result.current.evaluatedTunnels).toEqual([]);
      expect(result.current.tunnelResults.size).toBe(0);
      expect(result.current.recommendedDomain).toBeNull();
      expect(result.current.shouldUseRelay).toBe(false);
      expect(result.current.relayReason).toBeNull();
    });
  });
});
