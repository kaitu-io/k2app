import { decrypt as _decrypt, loadJsonp } from './antiblock-crypto';
import { brandConfig } from '../brands';

export const DEFAULT_ENTRY = 'https://k2.52j.me';
export const DECRYPTION_KEY =
  '9e3573184d5e5b3034a087c33fa2cdb76bd0126238ed08f54d1de8c6ae0eb4ba';

// Re-export decrypt so existing importers that pull it from antiblock.ts
// continue to work without any change to their import paths.
export { decrypt } from './antiblock-crypto';

// 品牌派生的入口配置 CDN 镜像（Happy Eyeballs 竞速）。空数组 = 该品牌无需
// 入口伪装（fetchEntryFromCDN 对空列表返回 null → resolveEntry 回落 DEFAULT_ENTRY）。
// 具体镜像列表见各品牌 brands/<brand>/index.ts 的 antiblockCdnSources
// （kaitu 的列表原样保留了 main 分支 fix/disable-antiblock-relay 的镜像扩容
// + zzko→Bunny 替换，见该文件顶部注释）。
export const CDN_SOURCES: readonly string[] = brandConfig.antiblockCdnSources;

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
