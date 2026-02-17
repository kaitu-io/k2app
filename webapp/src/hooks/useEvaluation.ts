/**
 * useEvaluation Hook
 *
 * Evaluates tunnels using the Rust service's evaluate_tunnels action.
 * Replaces the deprecated useDiagnosis hook with a simpler, synchronous approach.
 *
 * Unlike diagnosis (which required traceroute and polling), evaluation uses
 * pre-computed route data and returns results immediately.
 */

import { useEffect, useRef, useCallback } from 'react';
import { useEvaluationStore } from '../stores/evaluation.store';
import type { Tunnel } from '../services/api-types';
import type { EvaluateTunnelsResponse, TunnelInput } from '../services/control-types';

interface UseEvaluationOptions {
  /** Tunnels to evaluate */
  tunnels: Tunnel[];
  /** Whether to auto-evaluate when tunnels change (default: false) */
  autoEvaluate?: boolean;
  /** Whether relay fallback is available (default: false) */
  hasRelays?: boolean;
}

interface UseEvaluationReturn {
  /** Manually trigger evaluation */
  evaluate: () => Promise<void>;
  /** Whether evaluation is currently running */
  isRunning: boolean;
  /** Get route quality score (0-5) for a domain */
  getRouteQuality: (domain: string) => number;
}

/**
 * Hook for evaluating tunnels and getting recommendations
 */
export function useEvaluation({
  tunnels,
  autoEvaluate = false,
  hasRelays = false,
}: UseEvaluationOptions): UseEvaluationReturn {
  const { status, setResult, setStatus, getRouteQuality } = useEvaluationStore();

  // Track if component is mounted
  const isMountedRef = useRef(true);
  // Track if evaluation has been triggered for current tunnels
  const hasEvaluatedRef = useRef(false);

  /**
   * Convert Tunnel objects to TunnelInput for the evaluate_tunnels action
   */
  const buildTunnelInputs = useCallback((tunnelList: Tunnel[]): TunnelInput[] => {
    return tunnelList
      .filter((tunnel) => tunnel.node?.ipv4) // Only include tunnels with valid IP
      .map((tunnel) => ({
        domain: tunnel.domain.toLowerCase(),
        node_load: tunnel.node?.load ?? 0,
        // These fields may not be available from API, use defaults
        traffic_usage_percent: 0,
        bandwidth_usage_percent: 0,
        upstream_route_type: null,
        downstream_route_type: null,
      }));
  }, []);

  /**
   * Evaluate tunnels
   */
  const evaluate = useCallback(async (): Promise<void> => {
    if (!window._k2) {
      console.warn('[useEvaluation] k2 not available');
      return;
    }

    if (tunnels.length === 0) {
      console.debug('[useEvaluation] No tunnels to evaluate');
      return;
    }

    const tunnelInputs = buildTunnelInputs(tunnels);
    if (tunnelInputs.length === 0) {
      console.debug('[useEvaluation] No tunnels with valid IP to evaluate');
      return;
    }

    setStatus('loading');
    console.debug('[useEvaluation] Evaluating %d tunnels', tunnelInputs.length);

    try {
      const response = await window._k2.run<EvaluateTunnelsResponse>('evaluate_tunnels', {
        tunnels: tunnelInputs,
        has_relays: hasRelays,
      });

      if (!isMountedRef.current) {
        return;
      }

      if (response.code === 0 && response.data) {
        console.debug('[useEvaluation] Evaluation completed, recommended: %s', response.data.recommended_domain);
        setResult(response.data);
      } else {
        console.warn('[useEvaluation] Evaluation failed:', response.message);
        setStatus('error');
      }
    } catch (error) {
      console.error('[useEvaluation] Error during evaluation:', error);
      if (isMountedRef.current) {
        setStatus('error');
      }
    }
  }, [tunnels, hasRelays, buildTunnelInputs, setResult, setStatus]);

  // Auto-evaluate when tunnels change (if enabled)
  useEffect(() => {
    if (autoEvaluate && tunnels.length > 0 && !hasEvaluatedRef.current) {
      hasEvaluatedRef.current = true;
      evaluate();
    }
  }, [autoEvaluate, tunnels.length, evaluate]);

  // Reset hasEvaluatedRef when tunnels change
  useEffect(() => {
    hasEvaluatedRef.current = false;
  }, [tunnels]);

  // Cleanup on unmount
  useEffect(() => {
    isMountedRef.current = true;

    return () => {
      isMountedRef.current = false;
    };
  }, []);

  return {
    evaluate,
    isRunning: status === 'loading',
    getRouteQuality,
  };
}
