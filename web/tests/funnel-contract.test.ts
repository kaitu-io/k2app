/**
 * The web pixel's event vocabulary is locked to contracts/api-contract.json
 * (exported from the Go registry). A name added on one side only fails here.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';
import { WEB_FUNNEL_EVENTS } from '../src/lib/funnel';

const CONTRACT_PATH = path.resolve(__dirname, '../../contracts/api-contract.json');

describe('funnel events contract', () => {
  it('WEB_FUNNEL_EVENTS equals the contract events that list the web surface', () => {
    const contract = JSON.parse(readFileSync(CONTRACT_PATH, 'utf8')) as {
      funnelEvents: { name: string; surfaces: string[]; kind: string }[];
    };
    const fromContract = contract.funnelEvents.filter((e) => e.surfaces.includes('web')).map((e) => e.name).sort();
    expect([...WEB_FUNNEL_EVENTS].sort()).toEqual(fromContract);
  });
});
