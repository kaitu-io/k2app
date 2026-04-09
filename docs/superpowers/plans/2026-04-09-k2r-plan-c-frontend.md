# Plan C: Frontend — Implementation Plan (v4: Unified Tiers)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unified purchase page showing all plan tiers (个人版→专业版→家庭版→旗舰版). Router badge on higher tiers. Dynamic membership benefits. Router device management page for gateway. Gateway updater. Website install router tab. Admin plans with MaxDevice/MaxRouterDevice.

**Architecture:** One purchase page, all tiers. Plans with `maxLanClient != 0` show router badge. Benefits component reads selected plan's quotas. No product tabs. No product_type param.

**Tech Stack:** React 18, MUI 5, i18next (webapp); Next.js 15, shadcn/ui, next-intl (web)

**Dependencies:** Plan A must be deployed first (Plan.MaxDevice/MaxRouterDevice, User fields).

**Confidence: 9/10** — Purchase page is existing code, changes are additive.
**Risk: 2/10** — No breaking changes to purchase flow. Plan fetch unchanged. Just add fields to display.

---

## Task 1: Webapp API Types

**Files:** Modify: `webapp/src/services/api-types.ts`

- [ ] **Step 1: Add maxDevice and maxRouterDevice to Plan interface**

In `api-types.ts` (line ~134), add to `Plan`:

```typescript
export interface Plan {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
  maxDevice: number;        // NEW: app 设备数量（不含路由器）
  maxRouterDevice: number;  // NEW: 路由器登录数量上限 (0=不支持)
  maxLanClient: number;     // NEW: LAN 接入数量上限 (0=不支持, -1=无限)
}
```

- [ ] **Step 2: Commit**

```bash
git add webapp/src/services/api-types.ts
git commit -m "feat(webapp): add Plan.maxDevice/maxRouterDevice to API types"
```

---

## Task 2: Webapp Purchase Page — Tier Display

**Files:** Modify: `webapp/src/pages/Purchase.tsx`

- [ ] **Step 1: Add router badge to plan cards**

Find where plan cards are rendered. After the price display, add a router badge for plans with router access:

```tsx
{plan.maxLanClient !== 0 && (
  <Chip
    icon={<RouterIcon />}
    label={
      plan.maxLanClient === -1
        ? t('purchase:purchase.features.routerDeviceUnlimited')
        : t('purchase:purchase.features.routerDeviceAccess', { count: plan.maxLanClient })
    }
    size="small"
    color="primary"
    variant="outlined"
    sx={{ mt: 0.5 }}
  />
)}
```

Import at top: `import { Router as RouterIcon } from '@mui/icons-material';`

- [ ] **Step 2: Show device count on all plan cards**

Each card should show `maxDevice` count:

```tsx
<Typography variant="caption" color="text.secondary">
  {t('purchase:purchase.features.multiDevice', { count: plan.maxDevice })}
</Typography>
```

- [ ] **Step 3: Gateway upgrade banner**

When on gateway (`platformType === 'gateway'`), if user's plan has no router access, show banner:

```tsx
{window._platform?.platformType === 'gateway' && userProfile && userProfile.maxLanClient === 0 && (
  <Box sx={{ mb: 2, p: 2, borderRadius: 2, bgcolor: 'warning.main', color: 'warning.contrastText' }}>
    <Typography variant="body2" sx={{ fontWeight: 600 }}>
      {t('purchase:purchase.upgradeForRouter')}
    </Typography>
  </Box>
)}
```

Auto-select first plan with router access when on gateway:

```typescript
// In the plan selection auto-select logic:
const isGateway = window._platform?.platformType === 'gateway';
const defaultPlan = isGateway
  ? plans.find(p => p.maxLanClient !== 0 && p.highlight) || plans.find(p => p.maxLanClient !== 0)
  : plans.find(p => p.highlight);
setSelectedPlan(defaultPlan?.pid || plans[0]?.pid || '');
```

- [ ] **Step 4: Commit**

```bash
git add webapp/src/pages/Purchase.tsx
git commit -m "feat(webapp): purchase page tier display with router badge + gateway upgrade banner"
```

---

## Task 3: Webapp MembershipBenefits — Dynamic

**Files:** Modify: `webapp/src/components/MembershipBenefits.tsx`

- [ ] **Step 1: Accept selected plan props and show dynamic benefits**

```tsx
import { Box, Typography, Stack } from '@mui/material';
import {
  Devices as DevicesIcon, Public as GlobalIcon, RocketLaunch as ZeroMaintenanceIcon,
  AutorenewOutlined as OptimizationIcon, SupportAgent as SupportIcon,
  Router as RouterIcon, Wifi as WifiIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

interface Props {
  maxDevice?: number;
  maxRouterDevice?: number;
  maxLanClient?: number;
}

export default function MembershipBenefits({ maxDevice = 5, maxRouterDevice = 0, maxLanClient = 0 }: Props) {
  const { t } = useTranslation();
  const hasRouter = maxLanClient !== 0;

  const benefits = [
    { key: 'multiDevice', icon: DevicesIcon, color: '#2196f3', value: maxDevice },
    ...(hasRouter ? [
      {
        key: 'routerDeviceAccess',
        icon: RouterIcon,
        color: '#00bcd4',
        value: maxLanClient === -1 ? null : maxLanClient, // null = unlimited
      },
      { key: 'transparentProxy', icon: WifiIcon, color: '#4caf50' },
    ] : []),
    { key: 'globalNodes', icon: GlobalIcon, color: hasRouter ? '#ff9800' : '#4caf50' },
    { key: 'zeroMaintenance', icon: ZeroMaintenanceIcon, color: '#ff9800' },
    { key: 'continuousOptimization', icon: OptimizationIcon, color: '#7c4dff' },
    { key: 'prioritySupport', icon: SupportIcon, color: '#9c27b0' },
  ];

  return (
    <Box sx={{ mb: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
        {t('purchase:purchase.memberBenefits')}
      </Typography>
      <Stack spacing={1}>
        {benefits.map(({ key, icon: Icon, color, value }) => (
          <Box key={key} sx={{ display: 'flex', alignItems: 'center', gap: 1.5, py: 0.8, px: 1.5, borderRadius: 1.5, bgcolor: 'action.hover' }}>
            <Icon sx={{ color, fontSize: 22 }} />
            <Box sx={{ flex: 1 }}>
              <Typography variant="body2" sx={{ fontWeight: 500, lineHeight: 1.4 }} component="span">
                {value != null ? (
                  <>
                    <Box component="span" sx={{ fontSize: '1.3rem', fontWeight: 800, color: 'primary.main', mr: 0.3 }}>
                      {value}
                    </Box>
                    {t(`purchase:purchase.features.${key}`, { count: '' }).replace(/^\s+/, '')}
                  </>
                ) : key === 'routerDeviceAccess' ? (
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
        ))}
      </Stack>
    </Box>
  );
}
```

- [ ] **Step 2: Update caller in Purchase.tsx**

Pass selected plan's quotas to MembershipBenefits:

```tsx
const selectedPlanObj = plans.find(p => p.pid === selectedPlan);

<MembershipBenefits
  maxDevice={selectedPlanObj?.maxDevice}
  maxRouterDevice={selectedPlanObj?.maxRouterDevice}
  maxLanClient={selectedPlanObj?.maxLanClient}
/>
```

- [ ] **Step 3: Commit**

```bash
git add webapp/src/components/MembershipBenefits.tsx webapp/src/pages/Purchase.tsx
git commit -m "feat(webapp): dynamic membership benefits based on selected plan tier"
```

---

## Task 4: Webapp i18n

**Files:** Modify/Create: `webapp/src/i18n/locales/*/purchase.json` + `*/routerDevice.json`

- [ ] **Step 1: Add keys to zh-CN purchase.json**

In features object:

```json
"routerDeviceAccess": "{{count}} 台路由器接入设备",
"routerDeviceAccessDesc": "全家共享一键上网",
"routerDeviceUnlimited": "不限路由器接入设备",
"transparentProxy": "透明代理无需配置",
"transparentProxyDesc": "LAN 设备自动代理，无需逐台配置"
```

Add outside features:

```json
"upgradeForRouter": "当前套餐不支持路由器，请升级到家庭版或旗舰版"
```

- [ ] **Step 2: Add keys to en-US purchase.json**

```json
"routerDeviceAccess": "{{count}} router devices",
"routerDeviceAccessDesc": "Whole family shares one connection",
"routerDeviceUnlimited": "Unlimited router devices",
"transparentProxy": "Transparent proxy, zero config",
"transparentProxyDesc": "LAN devices proxied automatically",
"upgradeForRouter": "Current plan doesn't include router access. Upgrade to Family or Ultimate."
```

- [ ] **Step 3: Repeat for 5 remaining locales** (ja, zh-TW, zh-HK, en-AU, en-GB)

- [ ] **Step 4: Create routerDevice.json for all 7 locales**

zh-CN (`webapp/src/i18n/locales/zh-CN/routerDevice.json`):

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

Register `'routerDevice'` namespace in i18n config.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/i18n/
git commit -m "feat(webapp): i18n for tiered purchase + routerDevice namespace (7 locales)"
```

---

## Task 5: Webapp Router Device Management Page

**Files:** Create: `webapp/src/pages/RouterDevices.tsx`, Modify: `webapp/src/App.tsx`

Same implementation as Plan C v1 Task 5. Complete `RouterDevices.tsx` (~250 lines) with:
- Mode toggle (open/allowlist)
- Quota display
- Online/offline device lists
- Allow/remove/remark actions
- DeviceCard sub-component
- RemarkDialog sub-component

Route registration in App.tsx:

```tsx
{window._platform?.platformType === 'gateway' && (
  <Route path="router-devices" element={<RouterDevices />} />
)}
```

gateway-k2.ts action mapping:

```typescript
case 'router-devices-list': return fetchJSON('GET', '/api/router-devices');
case 'router-devices-allow': return fetchJSON('POST', '/api/router-devices/allow', params);
case 'router-devices-remove': return fetchJSON('POST', '/api/router-devices/remove', params);
case 'router-devices-mode': return fetchJSON('POST', '/api/router-devices/mode', params);
```

**Full code: see Plan C v1 Task 5** (unchanged — the RouterDevices page doesn't depend on product model)

- [ ] **Commit**

```bash
git add webapp/src/pages/RouterDevices.tsx webapp/src/App.tsx webapp/src/services/gateway-k2.ts
git commit -m "feat(webapp): router device management page + gateway-k2 action mapping"
```

---

## Task 6: Webapp Gateway Updater + Conditional Rendering

**Files:** Modify: `webapp/src/services/gateway-k2.ts`, various components

- [ ] **Step 1: Implement IUpdater in gateway-k2.ts**

Same as Plan C v1 Task 6 — `gatewayUpdater` object implementing `IUpdater`, calling `/api/updater/check` and `/api/updater/apply`.

- [ ] **Step 2: Conditional rendering guards**

Hide on gateway: service reinstall, ADB helper, proxy mode, TUN mode.
Show on gateway: router devices nav, LAN/DNS settings.

- [ ] **Commit**

```bash
git add webapp/src/
git commit -m "feat(webapp): gateway updater + conditional rendering"
```

---

## Task 7: Website — Unified Tier Purchase + Install Router Tab

**Files:** Modify: `web/src/lib/api.ts`, `web/src/app/[locale]/purchase/PurchaseClient.tsx`, `web/src/app/[locale]/install/`, `web/messages/`

- [ ] **Step 1: Update web Plan type**

In `web/src/lib/api.ts`, add to `Plan` interface:

```typescript
export interface Plan {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
  maxDevice: number;
  maxRouterDevice: number;
  maxLanClient: number;
}
```

`getPlans()` stays unchanged (no product_type param needed).

- [ ] **Step 2: Add router badge to PurchaseClient plan cards**

Same pattern as webapp: plans with `maxLanClient !== 0` show router badge. No tabs needed.

- [ ] **Step 3: Add router tab to install page**

Same as Plan C v1 Task 9 — `RouterPanel` component with one-line install command and architecture list.

- [ ] **Step 4: Add i18n keys to web messages**

All 7 locales: router device labels in `purchase.json`, router install text in `install.json`.

- [ ] **Step 5: Commit**

```bash
git add web/src/ web/messages/
git commit -m "feat(web): unified tier purchase + install page router tab"
```

---

## Task 8: Admin Plans Management

**Files:** Modify: `web/src/app/(manager)/manager/plans/page.tsx`

- [ ] **Step 1: Add MaxDevice and MaxRouterDevice columns + form fields**

Table columns: add `MaxDevice` (number) and `MaxRouterDevice` (number, show "∞" for -1, "—" for 0).
Create/edit form: add two number inputs.

- [ ] **Commit**

```bash
git add web/src/app/(manager)/manager/plans/
git commit -m "feat(web): admin plans with MaxDevice/MaxRouterDevice columns"
```

---

## Self-Review

| Spec Requirement | Task |
|-----------------|------|
| 5.2 Frontend Plan type | Task 1 |
| 6.1-6.3 Purchase page tiers | Task 2 |
| 6.4 Dynamic benefits | Task 3 |
| 6.5 Gateway entry point | Task 2 Step 3 |
| 7.7 Router device management UI | Task 5 |
| 7.5 Webapp updater | Task 6 |
| 11 Conditional rendering | Task 6 |
| 12.1 Install router tab | Task 7 |
| 12.2 Purchase unified tiers | Task 7 |
| 13.1 Admin plans | Task 8 |
| i18n | Task 4 + Task 7 |
