/**
 * Antiblock relay kill-switch. 2026-07-17 关停：relay 节点对 CN 可达率仅 2/21，
 * embedded seed 大面积被 GFW 封（#3288 / #3289），relay-first 反而拖垮墙内用户的
 * API 可达性。传输回到 config.js 直连（antiblock.ts resolveEntry → fetch）。
 *
 * 恢复 relay：本行改回 true 重发版，并同步翻转 *.relay-disabled.test.ts 的预期。
 * relay 代码（TS/Go/native）全部保留，见 spec:
 * docs/superpowers/specs/2026-07-17-disable-relay-restore-direct-design.md
 */
export const RELAY_ENABLED = false;
