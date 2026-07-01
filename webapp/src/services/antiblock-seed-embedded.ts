import type { NodeEntry } from './node-descriptor';

// Build-embedded floor seed. Regenerated at build time (Task 6) with real
// camouflage-relay nodes. The empty stub is the offline floor: it always
// exists, never throws, and guarantees a default entry URL even with zero
// network and an empty node pool.
export const EMBEDDED_SEED: {
  cursor: number;
  entries: string[];
  nodes: NodeEntry[];
} = {
  cursor: 0,
  entries: ['https://k2.52j.me'],
  nodes: [],
};
