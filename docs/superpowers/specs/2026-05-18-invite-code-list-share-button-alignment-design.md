# 邀请码列表页分享按钮交互对齐 InviteHub

**Date**: 2026-05-18
**Scope**: `webapp/` — `MyInviteCodeList` 页面的「分享」「复制链接」按钮交互

## 背景

`webapp/src/pages/MyInviteCodeList.tsx` 列表里每行都有「分享」和「复制链接」按钮。当前点击会先弹出 `ExpirationSelectorPopover`，让用户在 1 / 7 / 30 / 365 天里选有效期，然后才执行操作。

`webapp/src/pages/InviteHub.tsx`（分享页）则没有这个选择步骤——分享按钮直接走 `shareInviteCode(invite)`（默认 7 天 signed link + 系统分享/剪贴板）；复制链接按钮直接复制 `${baseURL}/s/{code}` 推广链。

用户反馈：列表页 popover 多余且显示有问题，要求两个按钮的交互对齐 InviteHub，统一去掉「多少天」picker。

## 目标

列表页两个按钮变成「点一下立即执行」，行为与 InviteHub 完全一致：

| 按钮 | 行为 |
|------|------|
| 分享 | 调 `shareInviteCode(row)` → 默认 7 天 signed link → 系统分享 sheet（移动）或复制到剪贴板（桌面） |
| 复制链接 | 复制 `${baseURL}/s/{code}` 推广链（无 token、不过期），toast `invite:invite.promotionLinkCopied` |

## 改动清单

### 1. `webapp/src/hooks/useInviteCodeActions.ts`

- **新增** `copyPromotionLink(code: string)` helper，复刻 InviteHub 的 `handleCopyPromotionLink` 逻辑：
  - 通过 `useAppConfig()` 拿 `appConfig?.appLinks?.baseURL`，fallback `'https://kaitu.io'`。
  - 调 `window._platform!.writeClipboard?.(\`${baseURL}/s/${code}\`)`。
  - 成功 toast：`invite:invite.promotionLinkCopied`；失败 toast：`invite:invite.copyFailedPermission`。
- **删除** `shareInviteCodeWithExpiration`、`copyShareLinkWithExpiration`（仅 `MyInviteCodeList` 用，删完无引用）。
- 从 `return` 对象里同步移除上述两个 key，加入 `copyPromotionLink`。
- Hook 里新增 `useAppConfig()` import。

### 2. `webapp/src/pages/MyInviteCodeList.tsx`

- **删除 state**：`popoverAnchorEl`、`popoverAction`、`selectedInviteCode`。
- **删除 handler**：`handleExpirationSelect`。
- **改写 handler**：
  - `handleShareClick(row)` → 直接 `await shareInviteCode(row)`。
  - `handleCopyLinkClick(row)` → 直接 `await copyPromotionLink(row.code)`。
  - 移除两个 button onClick 里的 `event` 参数（不再用 anchor）。
- **更新 import**：从 `useInviteCodeActions` 拿 `shareInviteCode`、`copyPromotionLink`，移除 `shareInviteCodeWithExpiration`、`copyShareLinkWithExpiration`。
- **删除 JSX**：`<ExpirationSelectorPopover ... />` 整段及其 import。

### 3. `webapp/src/components/ExpirationSelectorPopover.tsx`

- **删除整个文件**。confirmed via `grep -rn "ExpirationSelectorPopover" webapp/src` —— 唯一调用方是 `MyInviteCodeList`。

### 4. i18n（不在本次范围）

以下 key 在本次改动后变孤儿，本 PR **不删**（避免触动 7 个 locale 文件、增大 review 面）：

- `invite:invite.expiration.1day`
- `invite:invite.expiration.7days`
- `invite:invite.expiration.30days`
- `invite:invite.expiration.365days`
- `invite:invite.selectExpiration`
- `invite:invite.expirationSecurityHint`

后续可以单开一个 i18n cleanup PR 统一删。

## 不改动的内容

- `InviteHub.tsx`：本次不重构。它内部仍保留 `baseURL` / `promotionLink` 局部变量（用于在 `<Paper>` 里显示推广链文本），因此 `handleCopyPromotionLink` 也保留——只是行为现在和新的 hook helper 等价。后续如要 DRY，可以单独 PR 把 InviteHub 也切到 `copyPromotionLink`。
- `useShareLink` hook：默认 `expiresInDays = 7` 已经满足需求，不动。
- 后端 `/api/invite/my-codes/{code}/share-link` 接口的 `expiresInDays` query 参数：依然支持，本次 webapp 只是不主动指定。

## 验证

启 `cd webapp && yarn dev`，进 `/invite-codes`：

1. 至少需要 1 个邀请码，否则页面是空状态。
2. **桌面**：点「分享」→ 无 popover，toast「分享内容已复制」+ 剪贴板里是 InviteHub 同款多行文案（含 7 天 signed link）。
3. **桌面**：点「复制链接」（链接图标）→ 无 popover，toast「推广链接已复制」，剪贴板是 `https://kaitu.io/s/{CODE}`。
4. **移动**（iOS / Android）：点「分享」→ 弹系统分享 sheet（无 popover）；点「复制链接」→ toast「推广链接已复制」。
5. 编辑备注按钮 / 返回按钮 / 列表加载 / 空态 不受影响。
6. `cd webapp && npx tsc --noEmit` 通过。
7. `cd webapp && yarn build` 通过。

## 不需要新增的内容

- `MyInviteCodeList` 当前没有单元/E2E 测试，本次不补——属于一次按钮线路的拆除，行为完全复用已经在 InviteHub 路径上稳定运行的 `shareInviteCode` + 新的 `copyPromotionLink`（后者是 `handleCopyPromotionLink` 的搬家）。
- 不需要 feature flag / 灰度——纯前端 UI 改动，回退就是 revert commit。
