import { decrypt as _decrypt, loadJsonp } from './antiblock-crypto';

export const DEFAULT_ENTRY = 'https://k2.52j.me';
export const DECRYPTION_KEY =
  '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';

// Re-export decrypt so existing importers that pull it from antiblock.ts
// continue to work without any change to their import paths.
export { decrypt } from './antiblock-crypto';

// jsdelivr mirrors — 全部并行发起。首个成功解密的先返回（冷启动快），其余镜像
// 继续后台跑，按内容里的 ts 门控升级缓存（见下）。Same repo path, different edge
// networks for redundancy in blocked regions.
// 安全性：ui.js 载荷是 AES-256-GCM 加密+认证的，镜像只影响可用性，无法投毒
// （解密失败/篡改的候选被丢弃）。坏/慢镜像零成本。
// 缓存注意：jsDelivr 系全部忽略 query string（实测 2026-07），?bust= 无法穿透
// 边缘缓存；@dist 是 branch ref，官方边缘 ~12h 回源，第三方镜像 TTL 更长且不可控
// （曾实测 jsdmirror.cn 落后官方数天）。ts 标记 + max-ts 选取正是为吸收此镜像陈旧。
export const CDN_SOURCES = [
  // jsDelivr 官方边缘（cdn.jsdelivr.net 主域对 CN 已失效，但其余边缘域可用性各异）
  'https://cdn.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
  'https://fastly.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
  'https://testingcf.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
  'https://gcore.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
  // 网宿 CDNetworks 官方边缘 — 历史上的 CN 友好入口（2026-07 内容校验通过）
  'https://quantil.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js',
  // 国内第三方镜像（jsdMirror = 腾讯云 EdgeOne；zzko 面向 CN，海外探测不通属预期）
  'https://cdn.jsdmirror.com/gh/kaitu-io/ui-theme@dist/ui.js',
  'https://cdn.jsdmirror.cn/gh/kaitu-io/ui-theme@dist/ui.js',
  'https://jsd.onmicrosoft.cn/gh/kaitu-io/ui-theme@dist/ui.js',
  'https://jsd.cdn.zzko.cn/gh/kaitu-io/ui-theme@dist/ui.js',
  // statically.io — 独立于 jsDelivr 基础设施的 GitHub 代理，故障域隔离
  'https://cdn.statically.io/gh/kaitu-io/ui-theme@dist/ui.js',
];

// ---------------------------------------------------------------------------
// Config decode — {v,data} 信封 → {entries, ts}
// ---------------------------------------------------------------------------

const RECORD_KEY = 'k2_entry_cfg';
const JSONP_GLOBAL = '__k2ac';

interface AntiblockConfig {
  v: number;
  data: string;
}

interface EntryRecord {
  entries: string[];
  ts: number;
}

/** 解密并解析 ui.js/config.js 载荷 → {entries, ts}。缺 ts → 0（legacy）。
 *  v!==1 / 解密失败 / entries 空 → null。 */
async function decodeConfig(
  config: AntiblockConfig | null,
): Promise<EntryRecord | null> {
  if (!config || config.v !== 1 || typeof config.data !== 'string') return null;
  const plaintext = await _decrypt(config.data, DECRYPTION_KEY);
  if (!plaintext) return null;
  try {
    const parsed = JSON.parse(plaintext) as { entries?: string[]; ts?: number };
    if (!Array.isArray(parsed.entries) || parsed.entries.length === 0) return null;
    const ts = typeof parsed.ts === 'number' ? parsed.ts : 0;
    return { entries: parsed.entries, ts };
  } catch {
    return null;
  }
}

// ---------------------------------------------------------------------------
// 单一原子记录 —— {entries, ts} 存于一个键。entries 与 ts 同生共死：
// 一次 setItem 原子写、一次 getItem 原子读，绝不会 entries 更新而 ts 未更新。
// ---------------------------------------------------------------------------

function readRecord(): EntryRecord | null {
  try {
    const raw = localStorage.getItem(RECORD_KEY);
    if (!raw) return null;
    const p = JSON.parse(raw) as { entries?: unknown; ts?: unknown };
    if (Array.isArray(p.entries) && p.entries.length > 0 && typeof p.ts === 'number') {
      return { entries: p.entries as string[], ts: p.ts };
    }
  } catch {
    /* corrupt → treat as no cache */
  }
  return null;
}

// 全同步、内部禁止 await —— 单线程下每次调用原子执行到底，防并发 commit 读-改-写
// 竞态。陈旧镜像后台刷新回来的低 ts 永远盖不掉更新的记录。
function commitIfFresher(cfg: EntryRecord): void {
  try {
    if (cfg.entries.length === 0) return;
    const stored = readRecord();
    if (!stored || cfg.ts >= stored.ts) {
      localStorage.setItem(
        RECORD_KEY,
        JSON.stringify({ entries: cfg.entries, ts: cfg.ts }),
      );
    }
  } catch {
    /* best-effort */
  }
}

// ---------------------------------------------------------------------------
// CDN fetch —— 首个成功镜像先返回（冷启动快），其余镜像继续后台 ts 门控升级。
// ---------------------------------------------------------------------------

function fetchEntryFromCDN(): Promise<string[] | null> {
  if (CDN_SOURCES.length === 0) return Promise.resolve(null);
  return new Promise<string[] | null>((resolve) => {
    let remaining = CDN_SOURCES.length;
    let returned = false;
    const failIfLast = () => {
      if (--remaining === 0 && !returned) {
        returned = true;
        console.warn('[Antiblock] all CDN sources failed');
        resolve(null);
      }
    };
    for (const url of CDN_SOURCES) {
      loadJsonp(url, JSONP_GLOBAL)
        .then((config) => decodeConfig(config))
        .then((cfg) => {
          if (cfg) {
            commitIfFresher(cfg);
            if (!returned) {
              returned = true;
              // 返回记录中"已提交里最新"的 entries（可能已被更早到达的更高 ts 升级）。
              resolve(readRecord()?.entries ?? cfg.entries);
            }
            return;
          }
          failIfLast();
        })
        .catch(failIfLast);
    }
  });
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/** 最新已知 config 的完整有序 entry 列表（缓存 → CDN → 兜底）。 */
export async function resolveEntries(): Promise<string[]> {
  const cached = readRecord();
  if (cached) {
    refreshEntryInBackground();
    return cached.entries;
  }
  const entries = await fetchEntryFromCDN();
  if (!entries || entries.length === 0) {
    console.warn('[Antiblock] CDN failed, using default:', DEFAULT_ENTRY);
    return [DEFAULT_ENTRY];
  }
  return entries;
}

/** 主（最佳）entry —— 向后兼容的单串 API（buildSubsUrl 用）。 */
export async function resolveEntry(): Promise<string> {
  return (await resolveEntries())[0] ?? DEFAULT_ENTRY;
}

function refreshEntryInBackground(): void {
  fetchEntryFromCDN().catch(() => {});
}
