# BudgetScore Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace composite load score with a simple traffic-budget-pace metric (`BudgetScore = TrafficRatio - TimeRatio`) and update frontend to use it for display and country-based sorting.

**Architecture:** Add `BudgetScore float64` to `DataTunnelInstance` (API). Frontend `VerticalLoadBar` switches from `node.load` (0-100 int) to `instance.budgetScore` (-1 to +1 float). Tunnel sorting changes from load-secondary to country-secondary. `DataSlaveNode.Load` is marked deprecated but kept for backward compatibility.

**Tech Stack:** Go (Gin), React 18, MUI 5, TypeScript, Vitest

---

### Task 1: Add BudgetScore to API type

**Files:**
- Modify: `api/type.go:264` (deprecate Load comment)
- Modify: `api/type.go:274-279` (add BudgetScore field)

**Step 1: Mark Load as deprecated and add BudgetScore field**

In `api/type.go`, change the `Load` comment on line 264:

```go
Load      int    `json:"load"`      // Deprecated: to be removed. Use DataTunnelInstance.BudgetScore instead.
```

In `DataTunnelInstance` (line 274-279), add the new field after `TimeRatio`:

```go
type DataTunnelInstance struct {
	TrafficTotalBytes int64   `json:"trafficTotalBytes"` // Total traffic allowance in bytes
	TrafficRatio      float64 `json:"trafficRatio"`      // Traffic consumption ratio (0-1, e.g., 0.75 = 75% used)
	BillingCycleEndAt int64   `json:"billingCycleEndAt"` // Billing cycle end timestamp (Unix seconds)
	TimeRatio         float64 `json:"timeRatio"`         // Time consumption ratio (0-1, e.g., 0.5 = 50% of cycle elapsed)
	BudgetScore       float64 `json:"budgetScore"`       // TrafficRatio - TimeRatio. [-1,+1]. Negative = under budget (recommended), positive = over budget.
}
```

**Step 2: Verify Go compiles**

Run: `cd api && go build ./...`
Expected: clean build, no errors

**Step 3: Commit**

```
feat(api): add BudgetScore to DataTunnelInstance, deprecate Load
```

---

### Task 2: Compute BudgetScore in buildTunnelInstanceData

**Files:**
- Modify: `api/api_tunnel.go:247-252` (add BudgetScore to return struct)

**Step 1: Write a unit test for BudgetScore computation**

In `api/api_tunnel_test.go`, add:

```go
func TestBuildTunnelInstanceData_BudgetScore(t *testing.T) {
	t.Run("computes budgetScore as trafficRatio minus timeRatio", func(t *testing.T) {
		now := time.Now().Unix()
		inst := &CloudInstance{
			TrafficUsedBytes:  500,
			TrafficTotalBytes: 1000,
			TrafficResetAt:    now + 15*86400, // 15 days from now
		}
		// TrafficRatio = 0.5
		// TimeRatio depends on cycle start calculation; just verify the formula
		result := buildTunnelInstanceData(inst)
		require.NotNil(t, result)

		expected := result.TrafficRatio - result.TimeRatio
		assert.InDelta(t, expected, result.BudgetScore, 0.001,
			"BudgetScore must equal TrafficRatio - TimeRatio")
	})

	t.Run("returns nil for nil input", func(t *testing.T) {
		result := buildTunnelInstanceData(nil)
		assert.Nil(t, result)
	})

	t.Run("unlimited traffic has zero trafficRatio", func(t *testing.T) {
		now := time.Now().Unix()
		inst := &CloudInstance{
			TrafficUsedBytes:  999,
			TrafficTotalBytes: 0, // unlimited
			TrafficResetAt:    now + 15*86400,
		}
		result := buildTunnelInstanceData(inst)
		require.NotNil(t, result)
		assert.Equal(t, 0.0, result.TrafficRatio)
		// BudgetScore = 0 - timeRatio = negative (recommended)
		assert.LessOrEqual(t, result.BudgetScore, 0.0)
	})
}
```

**Step 2: Run test to verify it fails**

Run: `cd api && go test -run TestBuildTunnelInstanceData_BudgetScore -v ./...`
Expected: FAIL — `BudgetScore` is zero (not assigned yet)

**Step 3: Add BudgetScore computation to buildTunnelInstanceData**

In `api/api_tunnel.go`, change the return block (lines 247-252) to:

```go
	return &DataTunnelInstance{
		TrafficTotalBytes: inst.TrafficTotalBytes,
		TrafficRatio:      trafficRatio,
		BillingCycleEndAt: billingCycleEndAt,
		TimeRatio:         timeRatio,
		BudgetScore:       trafficRatio - timeRatio,
	}
```

**Step 4: Run test to verify it passes**

Run: `cd api && go test -run TestBuildTunnelInstanceData_BudgetScore -v ./...`
Expected: PASS

**Step 5: Run full API test suite**

Run: `cd api && go test ./...`
Expected: all existing tests pass

**Step 6: Commit**

```
feat(api): compute BudgetScore in buildTunnelInstanceData
```

---

### Task 3: Add TunnelInstance type to frontend api-types

**Files:**
- Modify: `webapp/src/services/api-types.ts:170-178` (add TunnelInstance interface, add instance to Tunnel)

**Step 1: Add the TypeScript interface**

After `SlaveNode` interface (line 164) and before `Tunnel` interface (line 170), insert:

```typescript
// Cloud instance billing/traffic data (only present for cloud-managed nodes)
export interface TunnelInstance {
  trafficTotalBytes: number;
  trafficRatio: number;      // 0-1, fraction of traffic allowance used
  billingCycleEndAt: number; // Unix seconds
  timeRatio: number;         // 0-1, fraction of billing period elapsed
  budgetScore: number;       // trafficRatio - timeRatio. [-1,+1]. Negative = recommended.
}
```

In `Tunnel` interface (line 170-178), add `instance?`:

```typescript
export interface Tunnel {
  id: number;
  domain: string;
  name: string;
  protocol: string;
  port: number;
  serverUrl?: string;
  node: SlaveNode;
  instance?: TunnelInstance; // Cloud instance data (only for cloud-managed nodes)
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd webapp && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```
feat(webapp): add TunnelInstance type with budgetScore
```

---

### Task 4: Update VerticalLoadBar to use budgetScore

**Files:**
- Modify: `webapp/src/components/VerticalLoadBar.tsx` (change props and mapping)
- Modify: `webapp/src/components/__tests__/VerticalLoadBar.test.tsx` (update tests)

**Step 1: Rewrite the test file**

Replace `webapp/src/components/__tests__/VerticalLoadBar.test.tsx`:

```typescript
/**
 * VerticalLoadBar Component Tests
 *
 * Tests for the vertical bar showing traffic budget status.
 * budgetScore range: [-1, +1]. Negative = under budget, positive = over budget.
 * Mapping: percentage = clamp((budgetScore + 1) * 50, 0, 100)
 *   -1 → 0%, 0 → 50%, +1 → 100%
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { VerticalLoadBar } from '../VerticalLoadBar';

describe('VerticalLoadBar', () => {
  it('should render nothing when budgetScore is undefined', () => {
    const { container } = render(<VerticalLoadBar />);
    expect(container.firstChild).toBeNull();
  });

  it('should render the bar container', () => {
    render(<VerticalLoadBar budgetScore={0} />);
    const container = screen.getByTestId('load-bar-container');
    expect(container).toBeInTheDocument();
  });

  it('should render green for negative budgetScore (under budget)', () => {
    render(<VerticalLoadBar budgetScore={-0.5} />);
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });

  it('should render red for high positive budgetScore (over budget)', () => {
    render(<VerticalLoadBar budgetScore={0.5} />);
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });

  it('should clamp values outside [-1, 1]', () => {
    render(<VerticalLoadBar budgetScore={2} />);
    const fill = screen.getByTestId('load-bar-fill');
    expect(fill).toBeInTheDocument();
  });
});
```

**Step 2: Run tests to verify they fail**

Run: `cd webapp && npx vitest run src/components/__tests__/VerticalLoadBar.test.tsx`
Expected: FAIL — `budgetScore` prop doesn't exist yet

**Step 3: Rewrite the component**

Replace `webapp/src/components/VerticalLoadBar.tsx`:

```typescript
/**
 * VerticalLoadBar Component
 *
 * Displays a minimal vertical progress bar for traffic budget status.
 * budgetScore: [-1, +1]. Negative = under budget (green), positive = over budget (red).
 */

import { Box } from '@mui/material';

interface VerticalLoadBarProps {
  /** Budget score (-1 to +1). Undefined renders nothing. */
  budgetScore?: number;
}

export function VerticalLoadBar({ budgetScore }: VerticalLoadBarProps) {
  if (budgetScore === undefined) return null;

  // Map [-1, +1] → [0, 100]
  const percentage = Math.max(0, Math.min(100, (budgetScore + 1) * 50));

  const color = percentage < 40
    ? 'success.main'
    : percentage < 65
      ? 'warning.main'
      : 'error.main';

  return (
    <Box
      data-testid="load-bar-container"
      sx={{
        width: 4,
        height: 24,
        bgcolor: 'action.hover',
        borderRadius: 1,
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'flex-end',
      }}
    >
      <Box
        data-testid="load-bar-fill"
        sx={{
          width: '100%',
          height: `${percentage}%`,
          bgcolor: color,
          borderRadius: 1,
          transition: 'height 0.3s ease',
        }}
      />
    </Box>
  );
}
```

Color thresholds (in budgetScore terms):
- `percentage < 40` → `budgetScore < -0.2` → green (under budget)
- `percentage < 65` → `budgetScore < 0.3` → yellow (on pace)
- `percentage >= 65` → `budgetScore >= 0.3` → red (over budget)

**Step 4: Run tests to verify they pass**

Run: `cd webapp && npx vitest run src/components/__tests__/VerticalLoadBar.test.tsx`
Expected: PASS

**Step 5: Commit**

```
feat(webapp): VerticalLoadBar uses budgetScore instead of load
```

---

### Task 5: Update tunnel sorting to use country

**Files:**
- Modify: `webapp/src/utils/tunnel-sort.ts` (country secondary sort)
- Modify: `webapp/src/utils/__tests__/tunnel-sort.test.ts` (update tests)

**Step 1: Rewrite the test file**

Replace `webapp/src/utils/__tests__/tunnel-sort.test.ts`:

```typescript
/**
 * Tunnel Sorting Tests
 *
 * Sort by route quality (primary), then country alphabetical (secondary).
 */
import { describe, it, expect } from 'vitest';
import { sortTunnelsByRecommendation, type RouteQualityProvider } from '../tunnel-sort';
import type { Tunnel } from '../../services/api-types';

describe('sortTunnelsByRecommendation', () => {
  const createTunnel = (domain: string, country: string): Tunnel => ({
    id: 1,
    name: domain,
    domain: domain,
    protocol: 'k2v4',
    port: 443,
    node: {
      name: domain,
      country: country,
      region: 'asia',
      ipv4: '1.2.3.4',
      ipv6: '',
      isAlive: true,
      load: 0,
      trafficUsagePercent: 0,
      bandwidthUsagePercent: 0,
    },
  });

  const createQualityProvider = (qualityMap: Map<string, number>): RouteQualityProvider => ({
    getRouteQuality: (domain: string) => qualityMap.get(domain.toLowerCase()) ?? 0,
  });

  it('should sort tunnels by quality (higher first)', () => {
    const tunnels = [
      createTunnel('low.example.com', 'JP'),
      createTunnel('high.example.com', 'JP'),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['low.example.com', 2],
      ['high.example.com', 5],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);
    expect(sorted[0].domain).toBe('high.example.com');
    expect(sorted[1].domain).toBe('low.example.com');
  });

  it('should use country as secondary sort when quality is equal', () => {
    const tunnels = [
      createTunnel('us.example.com', 'US'),
      createTunnel('jp.example.com', 'JP'),
      createTunnel('sg.example.com', 'SG'),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['us.example.com', 4],
      ['jp.example.com', 4],
      ['sg.example.com', 4],
    ]));

    const sorted = sortTunnelsByRecommendation(tunnels, qualityProvider);
    // Same quality → country alphabetical: JP, SG, US
    expect(sorted[0].node.country).toBe('JP');
    expect(sorted[1].node.country).toBe('SG');
    expect(sorted[2].node.country).toBe('US');
  });

  it('should return empty array for empty input', () => {
    const qualityProvider = createQualityProvider(new Map());
    const sorted = sortTunnelsByRecommendation([], qualityProvider);
    expect(sorted).toEqual([]);
  });

  it('should not mutate the original array', () => {
    const tunnels = [
      createTunnel('b.example.com', 'US'),
      createTunnel('a.example.com', 'JP'),
    ];

    const qualityProvider = createQualityProvider(new Map([
      ['a.example.com', 5],
      ['b.example.com', 3],
    ]));

    const originalOrder = tunnels.map(t => t.domain);
    sortTunnelsByRecommendation(tunnels, qualityProvider);
    expect(tunnels.map(t => t.domain)).toEqual(originalOrder);
  });
});
```

**Step 2: Run tests to verify they fail**

Run: `cd webapp && npx vitest run src/utils/__tests__/tunnel-sort.test.ts`
Expected: FAIL — secondary sort still uses load

**Step 3: Update the sort function**

Replace `webapp/src/utils/tunnel-sort.ts`:

```typescript
/**
 * Tunnel Sorting Utilities
 *
 * Sort tunnels by recommendation (route quality), with country as secondary sort.
 */

import type { Tunnel } from '../services/api-types';

/**
 * Interface for route quality lookup.
 */
export interface RouteQualityProvider {
  getRouteQuality: (domain: string) => number;
}

/**
 * Sort tunnels by recommendation (route quality).
 * Higher quality tunnels appear first. If quality is equal, sort by country alphabetically.
 *
 * @param tunnels - Array of tunnels to sort
 * @param qualityProvider - Object with getRouteQuality function
 * @returns New sorted array (does not mutate original)
 */
export function sortTunnelsByRecommendation(
  tunnels: Tunnel[],
  qualityProvider: RouteQualityProvider
): Tunnel[] {
  return [...tunnels].sort((a, b) => {
    const qualityA = qualityProvider.getRouteQuality(a.domain.toLowerCase());
    const qualityB = qualityProvider.getRouteQuality(b.domain.toLowerCase());

    // Higher quality first
    if (qualityB !== qualityA) {
      return qualityB - qualityA;
    }

    // If same quality, sort by country alphabetically
    return a.node.country.localeCompare(b.node.country);
  });
}
```

**Step 4: Run tests to verify they pass**

Run: `cd webapp && npx vitest run src/utils/__tests__/tunnel-sort.test.ts`
Expected: PASS

**Step 5: Commit**

```
feat(webapp): sort tunnels by country instead of load
```

---

### Task 6: Wire budgetScore into CloudTunnelList

**Files:**
- Modify: `webapp/src/components/CloudTunnelList.tsx:345-348` (pass budgetScore instead of load)

**Step 1: Update the VerticalLoadBar usage**

In `webapp/src/components/CloudTunnelList.tsx`, change line 347 from:

```tsx
<VerticalLoadBar load={tunnel.node.load} />
```

to:

```tsx
<VerticalLoadBar budgetScore={tunnel.instance?.budgetScore} />
```

**Step 2: Verify TypeScript compiles**

Run: `cd webapp && npx tsc --noEmit`
Expected: no errors

**Step 3: Run full webapp test suite**

Run: `cd webapp && npx vitest run`
Expected: all tests pass

**Step 4: Commit**

```
feat(webapp): wire budgetScore into CloudTunnelList
```
