# Plan C: Frontend — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Platform-aware purchase pages (webapp shows matching product, website shows tabs), router device management page, gateway conditional rendering, gateway updater integration, admin plan management, website install router tab.

**Architecture:** Webapp uses `window._platform.platformType` to determine product type. Website uses URL-driven tabs. Both fetch plans from `GET /api/plans?product_type=`. New `/router-devices` page in webapp (gateway only). i18n keys in all 7 locales.

**Tech Stack:** React 18, MUI 5, React Router 7, i18next (webapp); Next.js 15, shadcn/ui, next-intl (web); TypeScript

**Dependencies:** Plan A must be deployed first (Subscription model, Plan.productType/quota, Plans API filter).

**Spec:** `docs/superpowers/specs/2026-04-09-k2r-router-release-features-design.md` (Sections 3-4, 9-11)

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| **Webapp** | | |
| `webapp/src/services/api-types.ts` | Modify | Add Plan.productType/quota, Subscription type |
| `webapp/src/pages/Purchase.tsx` | Modify | Platform-based plan fetching, hide members for gateway |
| `webapp/src/components/MembershipBenefits.tsx` | Modify | Gateway-specific benefits |
| `webapp/src/pages/RouterDevices.tsx` | Create | Router device management page |
| `webapp/src/App.tsx` | Modify | Add /router-devices route (gateway only) |
| `webapp/src/services/gateway-k2.ts` | Modify | Add updater implementation |
| `webapp/src/i18n/locales/*/purchase.json` | Modify | Gateway feature text (7 locales) |
| `webapp/src/i18n/locales/*/routerDevice.json` | Create | New namespace (7 locales) |
| **Website** | | |
| `web/src/lib/api.ts` | Modify | getPlans() accepts product_type param |
| `web/src/app/[locale]/purchase/PurchaseClient.tsx` | Modify | Product tab switcher |
| `web/src/app/[locale]/install/InstallClient.tsx` | Modify | Router tab |
| `web/src/app/(manager)/manager/plans/page.tsx` | Modify | ProductType/Quota columns |
| `web/messages/*/purchase.json` | Modify | Gateway labels (7 locales) |

---

## Task 1: Webapp API Types

**Files:**
- Modify: `webapp/src/services/api-types.ts`

- [ ] **Step 1: Add productType and quota to Plan interface**

In `webapp/src/services/api-types.ts` (line ~134), add two fields to `Plan`:

```typescript
export interface Plan {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
  // new fields
  productType: 'personal' | 'gateway';
  quota: number; // personal=max devices, gateway=max router devices, 0=unlimited
}
```

- [ ] **Step 2: Add Subscription interface**

After the Plan interface, add:

```typescript
export interface Subscription {
  id: number;
  productType: 'personal' | 'gateway';
  planPid: string;
  expiredAt: number;
  quota: number;
}
```

- [ ] **Step 3: Commit**

```bash
git add webapp/src/services/api-types.ts
git commit -m "feat(webapp): add Plan.productType/quota and Subscription type"
```

---

## Task 2: Webapp Purchase Page — Platform-Based Plans

**Files:**
- Modify: `webapp/src/pages/Purchase.tsx`

- [ ] **Step 1: Add product_type to plan fetch**

In `Purchase.tsx`, find where plans are fetched (the `useEffect` or callback that calls `cloudApi`). Currently:

```typescript
const response = await cloudApi.get<{ items: Plan[] }>('/api/plans');
```

Replace with:

```typescript
const productType = window._platform?.platformType === 'gateway' ? 'gateway' : 'personal';
const response = await cloudApi.get<{ items: Plan[] }>(`/api/plans?product_type=${productType}`);
```

- [ ] **Step 2: Hide member selection for gateway plans**

Find the `MemberSelection` component render. Wrap it with a platform check:

```tsx
{window._platform?.platformType !== 'gateway' && (
  <MemberSelection
    selectedForMyself={selectedForMyself}
    onSelectForMyself={setSelectedForMyself}
    selectedMemberUUIDs={selectedMemberUUIDs}
    onSelectMemberUUIDs={setSelectedMemberUUIDs}
  />
)}
```

For gateway, force `selectedForMyself = true` and `selectedMemberUUIDs = []`.

- [ ] **Step 3: Group gateway plans by quota tier**

For gateway plans, the card layout groups by `quota`. Add grouping logic before rendering:

```typescript
const isGateway = window._platform?.platformType === 'gateway';

// Group plans by quota for gateway display
const planGroups = isGateway
  ? Object.entries(
      plans.reduce((acc, plan) => {
        const key = plan.quota === 0 ? 'unlimited' : `${plan.quota}`;
        (acc[key] = acc[key] || []).push(plan);
        return acc;
      }, {} as Record<string, Plan[]>)
    ).sort(([a], [b]) => {
      if (a === 'unlimited') return 1;
      if (b === 'unlimited') return -1;
      return Number(a) - Number(b);
    })
  : null;
```

Render gateway plans with tier headers:

```tsx
{planGroups ? (
  // Gateway: grouped by quota tier
  planGroups.map(([quota, tierPlans]) => (
    <Box key={quota} sx={{ mb: 3 }}>
      <Typography variant="subtitle2" sx={{ mb: 1, color: 'text.secondary', fontWeight: 600 }}>
        {quota === 'unlimited'
          ? t('purchase:purchase.features.routerDeviceUnlimited')
          : t('purchase:purchase.features.routerDeviceAccess', { count: quota })}
      </Typography>
      <Stack direction="row" spacing={1.5} sx={{ flexWrap: 'wrap' }}>
        {tierPlans.map(plan => (
          <PlanCard key={plan.pid} plan={plan} selected={selectedPlan === plan.pid} onSelect={setSelectedPlan} />
        ))}
      </Stack>
    </Box>
  ))
) : (
  // Personal: flat list (existing rendering)
  <Stack direction="row" spacing={1.5} sx={{ flexWrap: 'wrap' }}>
    {plans.map(plan => (
      <PlanCard key={plan.pid} plan={plan} selected={selectedPlan === plan.pid} onSelect={setSelectedPlan} />
    ))}
  </Stack>
)}
```

Note: `PlanCard` is the existing plan card component (or inline rendering). Adapt to the actual code structure.

- [ ] **Step 4: Verify compilation**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add webapp/src/pages/Purchase.tsx
git commit -m "feat(webapp): purchase page platform-based plan display with gateway tier grouping"
```

---

## Task 3: Webapp MembershipBenefits — Gateway Benefits

**Files:**
- Modify: `webapp/src/components/MembershipBenefits.tsx`

- [ ] **Step 1: Add gateway benefits**

Replace the existing `MembershipBenefits.tsx` (75 lines) with platform-aware version:

```tsx
import { Box, Typography, Stack } from '@mui/material';
import {
  Devices as DevicesIcon,
  Public as GlobalIcon,
  RocketLaunch as ZeroMaintenanceIcon,
  AutorenewOutlined as OptimizationIcon,
  SupportAgent as SupportIcon,
  Router as RouterIcon,
  Wifi as WifiIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

interface BenefitItem {
  key: string;
  icon: React.ElementType;
  color: string;
  count?: number;
}

const personalBenefits: BenefitItem[] = [
  { key: 'multiDevice', icon: DevicesIcon, color: '#2196f3', count: 5 },
  { key: 'globalNodes', icon: GlobalIcon, color: '#4caf50' },
  { key: 'zeroMaintenance', icon: ZeroMaintenanceIcon, color: '#ff9800' },
  { key: 'continuousOptimization', icon: OptimizationIcon, color: '#7c4dff' },
  { key: 'prioritySupport', icon: SupportIcon, color: '#9c27b0' },
];

const gatewayBenefits: BenefitItem[] = [
  { key: 'routerDeviceAccess', icon: RouterIcon, color: '#2196f3' },
  { key: 'transparentProxy', icon: WifiIcon, color: '#4caf50' },
  { key: 'globalNodes', icon: GlobalIcon, color: '#ff9800' },
  { key: 'continuousOptimization', icon: OptimizationIcon, color: '#7c4dff' },
  { key: 'prioritySupport', icon: SupportIcon, color: '#9c27b0' },
];

interface Props {
  quota?: number; // gateway: show quota in benefits
}

export default function MembershipBenefits({ quota }: Props) {
  const { t } = useTranslation();
  const isGateway = window._platform?.platformType === 'gateway';
  const benefits = isGateway ? gatewayBenefits : personalBenefits;

  return (
    <Box sx={{ mb: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
        {t('purchase:purchase.memberBenefits')}
      </Typography>
      <Stack spacing={1}>
        {benefits.map(({ key, icon: Icon, color, count }) => {
          // For gateway routerDeviceAccess, show quota
          const displayCount = key === 'routerDeviceAccess' && quota ? quota : count;
          return (
            <Box key={key} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, py: 0.8, px: 1.5, borderRadius: 1.5, bgcolor: 'action.hover' }}>
              <Icon sx={{ color, fontSize: 22 }} />
              <Box sx={{ flex: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 500, lineHeight: 1.4 }} component="span">
                  {displayCount ? (
                    <>
                      <Box component="span" sx={{ fontSize: '1.3rem', fontWeight: 800, color: 'primary.main', mr: 0.3 }}>
                        {displayCount}
                      </Box>
                      {t(`purchase:purchase.features.${key}`, { count: '' }).replace(/^\s+/, '')}
                    </>
                  ) : key === 'routerDeviceAccess' && quota === 0 ? (
                    t('purchase:purchase.features.routerDeviceUnlimited')
                  ) : (
                    t(`purchase:purchase.features.${key}`)
                  )}
                </Typography>
                <Typography variant="caption" color="text.secondary" component="span" sx={{ display: 'block', mt: 0.2 }}>
                  {t(`purchase:purchase.features.${key}Desc`)}
                </Typography>
              </Box>
            </Box>
          );
        })}
      </Stack>
    </Box>
  );
}
```

- [ ] **Step 2: Commit**

```bash
git add webapp/src/components/MembershipBenefits.tsx
git commit -m "feat(webapp): gateway-specific membership benefits (router device access, transparent proxy)"
```

---

## Task 4: Webapp i18n — Gateway Keys

**Files:**
- Modify: `webapp/src/i18n/locales/*/purchase.json` (7 locales)
- Create: `webapp/src/i18n/locales/*/routerDevice.json` (7 locales)

- [ ] **Step 1: Add gateway feature keys to zh-CN purchase.json**

In `webapp/src/i18n/locales/zh-CN/purchase.json`, add to the `features` object:

```json
    "routerDeviceAccess": "{{count}} 台设备接入",
    "routerDeviceAccessDesc": "全家共享一键上网",
    "routerDeviceUnlimited": "不限设备数量",
    "transparentProxy": "透明代理无需配置",
    "transparentProxyDesc": "LAN 设备自动代理，无需逐台配置"
```

- [ ] **Step 2: Add gateway feature keys to en-US purchase.json**

```json
    "routerDeviceAccess": "{{count}} devices connected",
    "routerDeviceAccessDesc": "Whole family shares one connection",
    "routerDeviceUnlimited": "Unlimited devices",
    "transparentProxy": "Transparent proxy, zero config",
    "transparentProxyDesc": "LAN devices proxied automatically"
```

- [ ] **Step 3: Repeat for remaining 5 locales** (ja, zh-TW, zh-HK, en-AU, en-GB)

Translate keys appropriately for each locale.

- [ ] **Step 4: Create routerDevice.json for zh-CN**

Create `webapp/src/i18n/locales/zh-CN/routerDevice.json`:

```json
{
  "routerDevice": {
    "title": "路由器设备管理",
    "modeOpen": "开放",
    "modeAllowlist": "白名单",
    "quota": "配额",
    "quotaDisplay": "{{used}}/{{max}}",
    "quotaUnlimited": "不限",
    "online": "在线设备",
    "offline": "离线设备（白名单中）",
    "allowed": "已允许",
    "notAllowed": "未允许",
    "allow": "允许",
    "remove": "移除",
    "remark": "备注",
    "addRemark": "添加备注",
    "editRemark": "修改备注",
    "quotaExceeded": "已达到设备配额上限",
    "confirmRemove": "确认从白名单中移除此设备？",
    "switchToAllowlist": "切换到白名单模式后，只有白名单中的设备可以接入",
    "switchToOpen": "切换到开放模式后，所有 LAN 设备均可接入",
    "noDevices": "暂无发现设备",
    "refreshing": "正在扫描..."
  }
}
```

- [ ] **Step 5: Create routerDevice.json for en-US + remaining 5 locales**

en-US version:

```json
{
  "routerDevice": {
    "title": "Router Device Management",
    "modeOpen": "Open",
    "modeAllowlist": "Allowlist",
    "quota": "Quota",
    "quotaDisplay": "{{used}}/{{max}}",
    "quotaUnlimited": "Unlimited",
    "online": "Online Devices",
    "offline": "Offline Devices (in allowlist)",
    "allowed": "Allowed",
    "notAllowed": "Not Allowed",
    "allow": "Allow",
    "remove": "Remove",
    "remark": "Remark",
    "addRemark": "Add remark",
    "editRemark": "Edit remark",
    "quotaExceeded": "Device quota exceeded",
    "confirmRemove": "Remove this device from the allowlist?",
    "switchToAllowlist": "In allowlist mode, only listed devices can connect through the router",
    "switchToOpen": "In open mode, all LAN devices can connect through the router",
    "noDevices": "No devices found",
    "refreshing": "Scanning..."
  }
}
```

- [ ] **Step 6: Register routerDevice namespace in i18n config**

Find the i18n initialization file and add `'routerDevice'` to the namespace list.

- [ ] **Step 7: Commit**

```bash
git add webapp/src/i18n/
git commit -m "feat(webapp): i18n keys for gateway purchase features + routerDevice namespace (7 locales)"
```

---

## Task 5: Webapp Router Device Management Page

**Files:**
- Create: `webapp/src/pages/RouterDevices.tsx`
- Modify: `webapp/src/App.tsx`

- [ ] **Step 1: Create RouterDevices.tsx**

```tsx
import { useState, useEffect, useCallback } from 'react';
import {
  Box, Typography, Stack, Card, Chip, Button, IconButton,
  ToggleButton, ToggleButtonGroup, TextField, Dialog, DialogTitle,
  DialogContent, DialogActions, CircularProgress,
} from '@mui/material';
import {
  Wifi as WifiIcon, WifiOff as WifiOffIcon,
  Check as CheckIcon, Block as BlockIcon,
  Delete as DeleteIcon, Edit as EditIcon, Refresh as RefreshIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useAlert } from '../stores';

interface RouterDeviceInfo {
  mac: string;
  ip: string;
  hostname: string;
  online: boolean;
  allowed: boolean;
  remark: string;
}

interface RouterDeviceListResponse {
  mode: 'open' | 'allowlist';
  maxRouterDevice: number;
  routerDevices: RouterDeviceInfo[];
}

export default function RouterDevices() {
  const { t } = useTranslation('routerDevice');
  const alert = useAlert();
  const [data, setData] = useState<RouterDeviceListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [remarkDialog, setRemarkDialog] = useState<{ mac: string; remark: string } | null>(null);

  const fetchDevices = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await window._k2.run<RouterDeviceListResponse>('router-devices-list');
      if (resp.code === 0 && resp.data) {
        setData(resp.data);
      }
    } catch (e) {
      console.error('Failed to fetch router devices', e);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchDevices(); }, [fetchDevices]);

  const handleModeChange = async (_: any, newMode: string) => {
    if (!newMode || !data) return;
    await window._k2.run('router-devices-mode', { mode: newMode });
    fetchDevices();
  };

  const handleAllow = async (mac: string) => {
    const resp = await window._k2.run('router-devices-allow', { mac, remark: '' });
    if (resp.code === 0) {
      fetchDevices();
    } else {
      alert.error(resp.message === 'quotaExceeded' ? t('quotaExceeded') : resp.message);
    }
  };

  const handleRemove = async (mac: string) => {
    await window._k2.run('router-devices-remove', { mac });
    fetchDevices();
  };

  if (loading && !data) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 8 }}>
        <CircularProgress size={32} />
      </Box>
    );
  }

  if (!data) return null;

  const online = data.routerDevices.filter(d => d.online);
  const offline = data.routerDevices.filter(d => !d.online && d.allowed);
  const allowedCount = data.routerDevices.filter(d => d.allowed).length;

  return (
    <Box sx={{ px: 2, py: 3, maxWidth: 600, mx: 'auto' }}>
      {/* Header */}
      <Stack direction="row" justifyContent="space-between" alignItems="center" sx={{ mb: 3 }}>
        <Typography variant="h6" sx={{ fontWeight: 700 }}>{t('title')}</Typography>
        <IconButton onClick={fetchDevices} size="small"><RefreshIcon /></IconButton>
      </Stack>

      {/* Mode toggle + Quota */}
      <Stack direction="row" justifyContent="space-between" alignItems="center" sx={{ mb: 3 }}>
        <ToggleButtonGroup
          value={data.mode}
          exclusive
          onChange={handleModeChange}
          size="small"
        >
          <ToggleButton value="open">{t('modeOpen')}</ToggleButton>
          <ToggleButton value="allowlist">{t('modeAllowlist')}</ToggleButton>
        </ToggleButtonGroup>

        <Chip
          label={`${t('quota')}: ${
            data.maxRouterDevice === 0
              ? t('quotaUnlimited')
              : t('quotaDisplay', { used: allowedCount, max: data.maxRouterDevice })
          }`}
          variant="outlined"
          size="small"
        />
      </Stack>

      {/* Online devices */}
      {online.length > 0 && (
        <>
          <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
            {t('online')} ({online.length})
          </Typography>
          <Stack spacing={1} sx={{ mb: 3 }}>
            {online.map(device => (
              <DeviceCard
                key={device.mac}
                device={device}
                mode={data.mode}
                onAllow={handleAllow}
                onRemove={handleRemove}
                onEditRemark={(mac, remark) => setRemarkDialog({ mac, remark })}
                t={t}
              />
            ))}
          </Stack>
        </>
      )}

      {/* Offline allowed devices */}
      {offline.length > 0 && (
        <>
          <Typography variant="subtitle2" color="text.secondary" sx={{ mb: 1 }}>
            {t('offline')} ({offline.length})
          </Typography>
          <Stack spacing={1}>
            {offline.map(device => (
              <DeviceCard
                key={device.mac}
                device={device}
                mode={data.mode}
                onAllow={handleAllow}
                onRemove={handleRemove}
                onEditRemark={(mac, remark) => setRemarkDialog({ mac, remark })}
                t={t}
              />
            ))}
          </Stack>
        </>
      )}

      {data.routerDevices.length === 0 && (
        <Typography color="text.secondary" sx={{ textAlign: 'center', py: 4 }}>
          {t('noDevices')}
        </Typography>
      )}

      {/* Remark edit dialog */}
      {remarkDialog && (
        <RemarkDialog
          mac={remarkDialog.mac}
          initialRemark={remarkDialog.remark}
          onClose={() => setRemarkDialog(null)}
          onSave={async (mac, remark) => {
            // Re-add with remark (remove + allow with remark)
            await window._k2.run('router-devices-remove', { mac });
            await window._k2.run('router-devices-allow', { mac, remark });
            setRemarkDialog(null);
            fetchDevices();
          }}
          t={t}
        />
      )}
    </Box>
  );
}

function DeviceCard({ device, mode, onAllow, onRemove, onEditRemark, t }: {
  device: RouterDeviceInfo;
  mode: string;
  onAllow: (mac: string) => void;
  onRemove: (mac: string) => void;
  onEditRemark: (mac: string, remark: string) => void;
  t: any;
}) {
  return (
    <Card variant="outlined" sx={{ p: 1.5 }}>
      <Stack direction="row" alignItems="center" spacing={1.5}>
        {device.online
          ? <WifiIcon sx={{ color: 'success.main', fontSize: 20 }} />
          : <WifiOffIcon sx={{ color: 'text.disabled', fontSize: 20 }} />
        }
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography variant="body2" sx={{ fontWeight: 600 }} noWrap>
            {device.hostname || device.mac}
            {device.remark && (
              <Typography component="span" variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                ({device.remark})
              </Typography>
            )}
          </Typography>
          <Typography variant="caption" color="text.secondary" noWrap>
            {device.mac}{device.ip && ` · ${device.ip}`}
          </Typography>
        </Box>

        {mode === 'allowlist' && (
          <>
            {device.allowed ? (
              <Stack direction="row" spacing={0.5}>
                <Chip label={t('allowed')} size="small" color="success" variant="outlined" icon={<CheckIcon />} />
                <IconButton size="small" onClick={() => onEditRemark(device.mac, device.remark)}>
                  <EditIcon fontSize="small" />
                </IconButton>
                <IconButton size="small" onClick={() => onRemove(device.mac)} color="error">
                  <DeleteIcon fontSize="small" />
                </IconButton>
              </Stack>
            ) : (
              <Button size="small" variant="outlined" onClick={() => onAllow(device.mac)}>
                {t('allow')}
              </Button>
            )}
          </>
        )}
      </Stack>
    </Card>
  );
}

function RemarkDialog({ mac, initialRemark, onClose, onSave, t }: {
  mac: string; initialRemark: string;
  onClose: () => void; onSave: (mac: string, remark: string) => void; t: any;
}) {
  const [remark, setRemark] = useState(initialRemark);
  return (
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>{t('editRemark')}</DialogTitle>
      <DialogContent>
        <TextField
          autoFocus fullWidth margin="dense" size="small"
          value={remark} onChange={e => setRemark(e.target.value)}
          placeholder={t('addRemark')}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t('common:common.cancel', 'Cancel')}</Button>
        <Button variant="contained" onClick={() => onSave(mac, remark)}>{t('common:common.save', 'Save')}</Button>
      </DialogActions>
    </Dialog>
  );
}
```

- [ ] **Step 2: Add route in App.tsx**

In `webapp/src/App.tsx`, inside the `<Route path="/" element={<Layout />}>` block, add:

```tsx
{window._platform?.platformType === 'gateway' && (
  <Route path="router-devices" element={<RouterDevices />} />
)}
```

Import at the top:

```typescript
import RouterDevices from './pages/RouterDevices';
```

- [ ] **Step 3: Add gateway-k2.ts router device actions**

The `RouterDevices.tsx` page calls `window._k2.run('router-devices-list')` etc. In `gateway-k2.ts`, these actions map to HTTP:

```typescript
// In gatewayK2.run():
case 'router-devices-list':
  return fetchJSON('GET', '/api/router-devices');
case 'router-devices-allow':
  return fetchJSON('POST', '/api/router-devices/allow', params);
case 'router-devices-remove':
  return fetchJSON('POST', '/api/router-devices/remove', params);
case 'router-devices-mode':
  return fetchJSON('POST', '/api/router-devices/mode', params);
```

Note: `gateway-k2.ts` exists on `feat/k2r-router-release` branch. Add these cases to the existing `run()` method.

- [ ] **Step 4: Verify compilation**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add webapp/src/pages/RouterDevices.tsx webapp/src/App.tsx webapp/src/services/gateway-k2.ts
git commit -m "feat(webapp): router device management page (/router-devices, gateway only)"
```

---

## Task 6: Webapp Gateway Updater

**Files:**
- Modify: `webapp/src/services/gateway-k2.ts`

- [ ] **Step 1: Implement IUpdater for gateway**

In `gateway-k2.ts`, add updater implementation that calls the gateway updater API:

```typescript
const gatewayUpdater: IUpdater = {
  isUpdateReady: false,
  updateInfo: null,
  isChecking: false,
  error: null,
  channel: 'stable' as const,

  async checkUpdateManual() {
    this.isChecking = true;
    this.error = null;
    try {
      const resp = await fetchJSON('POST', '/api/updater/check');
      if (resp.code === 0 && resp.data) {
        const info = resp.data as { hasUpdate: boolean; currentVersion: string; newVersion: string };
        if (info.hasUpdate) {
          this.isUpdateReady = true;
          this.updateInfo = {
            currentVersion: info.currentVersion,
            newVersion: info.newVersion,
          };
        }
        return info.hasUpdate ? info.newVersion : '';
      }
      return '';
    } catch (e: any) {
      this.error = e.message;
      return '';
    } finally {
      this.isChecking = false;
    }
  },

  async applyUpdateNow() {
    await fetchJSON('POST', '/api/updater/apply');
    // After apply, service restarts. SSE will disconnect.
    // Webapp should show "restarting" state and poll for reconnection.
  },
};
```

In the `injectGatewayGlobals()` function, set:

```typescript
window._platform.updater = gatewayUpdater;
```

- [ ] **Step 2: Commit**

```bash
git add webapp/src/services/gateway-k2.ts
git commit -m "feat(webapp): gateway IUpdater implementation (CDN check + apply via HTTP API)"
```

---

## Task 7: Webapp Conditional Rendering

**Files:**
- Modify: Various components

- [ ] **Step 1: Identify and guard gateway-hidden components**

Search for components that should be hidden on gateway:

```bash
cd webapp && grep -rn "reinstallService\|getPid\|adb-\|proxyMode\|tunMode" src/ --include="*.tsx" --include="*.ts"
```

For each found component, wrap with:

```tsx
{window._platform?.platformType !== 'gateway' && (
  // existing component
)}
```

- [ ] **Step 2: Show gateway-specific navigation**

In the navigation/tab bar component, add a "router devices" tab for gateway:

```tsx
{window._platform?.platformType === 'gateway' && (
  <Tab label={t('routerDevice:routerDevice.title')} value="/router-devices" />
)}
```

- [ ] **Step 3: Commit**

```bash
git add webapp/src/
git commit -m "feat(webapp): conditional rendering for gateway (hide desktop-only, show router features)"
```

---

## Task 8: Website Purchase Page — Product Tabs

**Files:**
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/app/[locale]/purchase/PurchaseClient.tsx`
- Modify: `web/messages/*/purchase.json` (7 locales)

- [ ] **Step 1: Update web api.ts getPlans to accept product_type**

In `web/src/lib/api.ts` (line ~1243), update:

```typescript
async getPlans(productType?: string, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<ListResult<Plan>> {
  const params = productType ? `?product_type=${productType}` : '';
  return this.request<ListResult<Plan>>(`/api/plans${params}`, options);
}
```

Update the `Plan` interface in the same file to add new fields:

```typescript
export interface Plan {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
  productType: 'personal' | 'gateway';
  quota: number;
}
```

- [ ] **Step 2: Add product tabs to PurchaseClient.tsx**

At the top of `PurchaseClient`, add state for product type:

```typescript
const searchParams = useSearchParams();
const [productTab, setProductTab] = useState<'personal' | 'gateway'>(
  searchParams.get('product') === 'gateway' ? 'gateway' : 'personal'
);
```

Update the plan fetch to use productTab:

```typescript
useEffect(() => {
  const fetchPlans = async () => {
    setPlansLoading(true);
    try {
      const result = await api.getPlans(productTab);
      setPlans(result.items || []);
      // Auto-select first highlighted plan
      const highlighted = (result.items || []).find(p => p.highlight);
      setSelectedPlan(highlighted?.pid || (result.items?.[0]?.pid ?? ''));
    } catch (err) {
      // ... error handling
    } finally {
      setPlansLoading(false);
    }
  };
  fetchPlans();
}, [productTab]);
```

Render tabs before the plan selection area:

```tsx
<div className="flex gap-2 mb-6">
  <button
    className={`px-4 py-2 rounded-lg font-medium transition-colors ${
      productTab === 'personal'
        ? 'bg-primary text-primary-foreground'
        : 'bg-muted text-muted-foreground hover:bg-muted/80'
    }`}
    onClick={() => {
      setProductTab('personal');
      window.history.replaceState(null, '', '?product=personal');
    }}
  >
    {t('purchase.productPersonal')}
  </button>
  <button
    className={`px-4 py-2 rounded-lg font-medium transition-colors ${
      productTab === 'gateway'
        ? 'bg-primary text-primary-foreground'
        : 'bg-muted text-muted-foreground hover:bg-muted/80'
    }`}
    onClick={() => {
      setProductTab('gateway');
      window.history.replaceState(null, '', '?product=gateway');
    }}
  >
    {t('purchase.productGateway')}
  </button>
</div>
```

Hide member selection when `productTab === 'gateway'`.

For gateway plans, add tier grouping (same logic as webapp Task 2 but using Tailwind/shadcn instead of MUI).

- [ ] **Step 3: Add i18n keys to web messages**

In `web/messages/zh-CN/purchase.json`, add:

```json
    "productPersonal": "个人版",
    "productGateway": "路由器版",
    "routerDeviceAccess": "最多 {count} 台设备接入",
    "routerDeviceUnlimited": "不限设备数量"
```

Repeat for all 7 locales.

- [ ] **Step 4: Commit**

```bash
git add web/src/ web/messages/
git commit -m "feat(web): purchase page product tabs (personal/gateway) with i18n"
```

---

## Task 9: Website Install Page — Router Tab

**Files:**
- Modify: `web/src/app/[locale]/install/InstallClient.tsx`
- Modify: `web/src/app/[locale]/install/platform-panels.tsx`

- [ ] **Step 1: Add Router to platform tabs**

In `InstallClient.tsx`, add `'router'` to the platform list. In the `PlatformTabBar` component, add a router icon + label.

- [ ] **Step 2: Create RouterPanel in platform-panels.tsx**

```tsx
export function RouterPanel({ t }: { t: any }) {
  return (
    <div className="space-y-6">
      <div className="rounded-lg bg-muted p-6">
        <h3 className="text-lg font-semibold mb-2">{t('install.router.title')}</h3>
        <p className="text-sm text-muted-foreground mb-4">{t('install.router.description')}</p>

        {/* One-line install command */}
        <div className="bg-background rounded-md p-4 font-mono text-sm">
          <code>wget -qO- https://kaitu.io/i/k2r | sh</code>
        </div>

        {/* Supported architectures */}
        <div className="mt-4 grid grid-cols-2 gap-2 text-sm">
          <div className="flex items-center gap-2">
            <span className="font-medium">aarch64</span>
            <span className="text-muted-foreground">ARM 64-bit (most routers)</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="font-medium">x86_64</span>
            <span className="text-muted-foreground">Intel/AMD (soft-router, NAS)</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="font-medium">armv7</span>
            <span className="text-muted-foreground">ARM 32-bit (older routers)</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="font-medium">mipsle</span>
            <span className="text-muted-foreground">MIPS (budget routers)</span>
          </div>
        </div>

        {/* Post-install */}
        <div className="mt-4 p-3 bg-primary/5 rounded-md text-sm">
          {t('install.router.postInstall')}
          <code className="ml-1 font-mono">http://&lt;router-ip&gt;:1779</code>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Add i18n keys for router install**

In `web/messages/zh-CN/install.json`:

```json
  "router": {
    "title": "路由器安装",
    "description": "支持 OpenWrt、软路由、NAS 等 Linux 设备。一键安装，浏览器管理。",
    "postInstall": "安装完成后，在浏览器访问"
  }
```

- [ ] **Step 4: Commit**

```bash
git add web/src/app/[locale]/install/ web/messages/
git commit -m "feat(web): install page router tab with one-line install command"
```

---

## Task 10: Admin Plans Management

**Files:**
- Modify: `web/src/app/(manager)/manager/plans/page.tsx`

- [ ] **Step 1: Add ProductType and Quota to plans table**

Add columns:
- `ProductType` — shown as badge (`personal` / `gateway`)
- `Quota` — number (with `0 = unlimited` display)

Add to create/edit dialog form:
- ProductType dropdown (`personal` / `gateway`)
- Quota number input

Add filter dropdown for product_type in the table header.

- [ ] **Step 2: Commit**

```bash
git add web/src/app/(manager)/manager/plans/
git commit -m "feat(web): admin plans management with ProductType/Quota columns + filter"
```

---

## Self-Review

| Spec Requirement | Task |
|-----------------|------|
| 3.3 Frontend Plan type | Task 1 |
| 3.4 Frontend Subscription type | Task 1 |
| 4.1 Platform-based plan display | Task 2 |
| 4.2 Website tab behavior | Task 8 |
| 4.3 Gateway plan card layout | Task 2 (tier grouping) |
| 4.4 Gateway membership benefits | Task 3 |
| 4.5 Gateway purchase flow | Task 2 (hide members) |
| 6.7 Router device management UI | Task 5 |
| 7.5 Webapp updater integration | Task 6 |
| 9 Conditional rendering | Task 7 |
| 10.1 Install page router tab | Task 9 |
| 10.2 Purchase page product tabs | Task 8 |
| 11.1 Admin plan management | Task 10 |
| i18n webapp | Task 4 |
| i18n website | Tasks 8, 9 |

**Type consistency:** `Plan` (with productType/quota) used consistently in webapp and web. `RouterDeviceInfo`, `RouterDeviceListResponse` match gateway API spec. `IUpdater` interface from `kaitu-core.ts` implemented in `gateway-k2.ts`.
