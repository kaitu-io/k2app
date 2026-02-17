/**
 * Evaluation Store
 *
 * Manages tunnel evaluation state using the Rust service's evaluate_tunnels action.
 * Replaces the deprecated diagnosis system with a simpler, more reliable approach.
 */

import { create } from 'zustand';
import type { EvaluateTunnelsResponse, EvaluatedTunnelOutput } from '../services/control-types';

export type EvaluationStatus = 'idle' | 'loading' | 'completed' | 'error';

interface EvaluationState {
  // Status
  status: EvaluationStatus;

  // Results
  evaluatedTunnels: EvaluatedTunnelOutput[];
  tunnelResults: Map<string, EvaluatedTunnelOutput>;
  recommendedDomain: string | null;
  shouldUseRelay: boolean;
  relayReason: string | null;

  // Timestamp
  evaluatedAt: number | null;

  // Actions
  setResult: (response: EvaluateTunnelsResponse) => void;
  setStatus: (status: EvaluationStatus) => void;
  getRouteQuality: (domain: string) => number;
  reset: () => void;
}

const initialState = {
  status: 'idle' as EvaluationStatus,
  evaluatedTunnels: [] as EvaluatedTunnelOutput[],
  tunnelResults: new Map<string, EvaluatedTunnelOutput>(),
  recommendedDomain: null as string | null,
  shouldUseRelay: false,
  relayReason: null as string | null,
  evaluatedAt: null as number | null,
};

/**
 * Convert route quality string to numeric score (0-5)
 * Used for star ratings in the UI
 */
function routeQualityToScore(quality: string): number {
  switch (quality) {
    case 'excellent':
      return 5;
    case 'good':
      return 4;
    case 'fair':
      return 3;
    case 'poor':
      return 2;
    case 'unknown':
      return 1;
    default:
      return 0;
  }
}

export const useEvaluationStore = create<EvaluationState>((set, get) => ({
  ...initialState,

  setResult: (response: EvaluateTunnelsResponse) => {
    const tunnelResultsMap = new Map<string, EvaluatedTunnelOutput>();
    for (const tunnel of response.evaluated_tunnels) {
      tunnelResultsMap.set(tunnel.domain.toLowerCase(), tunnel);
    }

    set({
      status: 'completed',
      evaluatedTunnels: response.evaluated_tunnels,
      tunnelResults: tunnelResultsMap,
      recommendedDomain: response.recommended_domain || null,
      shouldUseRelay: response.should_use_relay,
      relayReason: response.relay_reason || null,
      evaluatedAt: Date.now(),
    });
  },

  setStatus: (status: EvaluationStatus) => {
    set({ status });
  },

  getRouteQuality: (domain: string): number => {
    const tunnel = get().tunnelResults.get(domain.toLowerCase());
    if (!tunnel) return 0;
    return routeQualityToScore(tunnel.route_quality);
  },

  reset: () => {
    set({
      ...initialState,
      tunnelResults: new Map<string, EvaluatedTunnelOutput>(),
    });
  },
}));
