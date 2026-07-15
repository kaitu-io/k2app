/**
 * Recommended router hardware for running k2r (k2r gateway).
 *
 * Static hardware specs live here; locale-aware copy (tagline / pros / cons /
 * fit) lives in messages/{locale}/routers.json under `hardware.{id}.*`.
 *
 * Purchase links are organized as a per-platform map so we can layer in
 * Amazon / Lazada / AliExpress / JD URLs over time without re-shaping the
 * data model. The UI picks the platform priority based on the active locale.
 */

export type HardwareTier = 'budget' | 'mainstream' | 'premium';
export type HardwareType = 'wifi-router' | 'soft-router';
export type PurchasePlatform =
  | 'taobao'
  | 'jd'
  | 'amazon'
  | 'lazada'
  | 'aliexpress';

export interface PurchaseLinks {
  taobao?: string;
  jd?: string;
  amazon?: string;
  lazada?: string;
  aliexpress?: string;
}

export interface RouterHardware {
  id: string;
  type: HardwareType;
  tier: HardwareTier;
  /** Approximate street price in CNY at time of writing */
  priceMin: number;
  priceMax: number;
  /** Hardware specs — locale-independent */
  soc: string;
  ram: string;
  flash: string;
  /** Wi-Fi spec; absent for soft routers (no built-in radio) */
  wifi?: string;
  ports: string;
  /** Estimated single-core QUIC throughput when running k2r */
  k2rThroughput: string;
  purchaseLinks: PurchaseLinks;
  image: string;
}

export const RECOMMENDED_HARDWARE: RouterHardware[] = [
  {
    id: 'redmi-ax6s',
    type: 'wifi-router',
    tier: 'budget',
    priceMin: 200,
    priceMax: 260,
    soc: 'MT7622B (A53×2 1.35GHz)',
    ram: '256MB',
    flash: '128MB',
    wifi: 'WiFi6 AX1800',
    ports: '1×千兆 WAN + 3×千兆 LAN',
    k2rThroughput: '400-500 Mbps',
    purchaseLinks: {
      taobao: 'https://s.taobao.com/search?q=红米AX6S+RB03',
      jd: 'https://search.jd.com/Search?keyword=红米AX6S',
    },
    image: '/images/routers/recommended/redmi-ax6s.jpg',
  },
  {
    id: 'xiaomi-ax3000t',
    type: 'wifi-router',
    tier: 'mainstream',
    priceMin: 230,
    priceMax: 300,
    soc: 'MT7981B (A53×2 1.3GHz)',
    ram: '256MB',
    flash: '128MB',
    wifi: 'WiFi6 AX3000',
    ports: '1×千兆 WAN + 3×千兆 LAN',
    k2rThroughput: '500-650 Mbps',
    purchaseLinks: {
      taobao: 'https://s.taobao.com/search?q=小米AX3000T+RA82',
      jd: 'https://search.jd.com/Search?keyword=小米AX3000T',
    },
    image: '/images/routers/recommended/xiaomi-ax3000t.jpg',
  },
  {
    id: 'glinet-flint2',
    type: 'wifi-router',
    tier: 'premium',
    priceMin: 1000,
    priceMax: 1500,
    soc: 'Filogic 880 / MT7986A (A53×4 2.0GHz)',
    ram: '1GB DDR4',
    flash: '8GB EMMC',
    wifi: 'WiFi6 AX6000',
    ports: '1×2.5G WAN + 1×2.5G LAN + 4×千兆 LAN',
    k2rThroughput: '1 Gbps+',
    purchaseLinks: {
      taobao: 'https://s.taobao.com/search?q=GL.iNet+Flint+2+MT6000',
      amazon: 'https://www.amazon.com/s?k=GL.iNet+Flint+2+MT6000',
      aliexpress: 'https://www.aliexpress.com/wholesale?SearchText=GL.iNet+Flint+2',
    },
    image: '/images/routers/recommended/glinet-flint2.jpg',
  },
  {
    id: 'j4125-router',
    type: 'soft-router',
    tier: 'mainstream',
    priceMin: 400,
    priceMax: 600,
    soc: 'Intel Celeron J4125 (4×2.0GHz, AES-NI)',
    ram: '4-8GB DDR4',
    flash: '64GB SSD',
    ports: '4×Intel i211/i225 千兆',
    k2rThroughput: '~1 Gbps',
    purchaseLinks: {
      taobao: 'https://s.taobao.com/search?q=J4125+软路由+4网口',
      jd: 'https://search.jd.com/Search?keyword=J4125+软路由',
    },
    image: '/images/routers/recommended/j4125-router.jpg',
  },
  {
    id: 'n100-router',
    type: 'soft-router',
    tier: 'premium',
    priceMin: 800,
    priceMax: 1200,
    soc: 'Intel N100 (4×3.4GHz Boost, AES-NI)',
    ram: '8-16GB DDR5',
    flash: '256GB NVMe',
    ports: '4×Intel i226 2.5G',
    k2rThroughput: '2.5 Gbps+',
    purchaseLinks: {
      taobao: 'https://s.taobao.com/search?q=N100+软路由+4网口+2.5G',
      jd: 'https://search.jd.com/Search?keyword=N100+软路由',
      aliexpress: 'https://www.aliexpress.com/wholesale?SearchText=N100+mini+pc+4x2.5G',
    },
    image: '/images/routers/recommended/n100-router.jpg',
  },
];

/**
 * Locale → preferred purchase platform order.
 * UI renders the first available link as primary; remaining as secondary chips.
 */
export const PLATFORM_PRIORITY_BY_LOCALE: Record<string, PurchasePlatform[]> = {
  'zh-CN': ['taobao', 'jd', 'aliexpress', 'amazon', 'lazada'],
  'zh-TW': ['taobao', 'aliexpress', 'amazon', 'lazada', 'jd'],
  'zh-HK': ['taobao', 'aliexpress', 'amazon', 'lazada', 'jd'],
  'en-US': ['amazon', 'aliexpress', 'taobao', 'lazada', 'jd'],
  'en-GB': ['amazon', 'aliexpress', 'taobao', 'lazada', 'jd'],
  'en-AU': ['amazon', 'aliexpress', 'taobao', 'lazada', 'jd'],
  ja: ['amazon', 'aliexpress', 'taobao', 'lazada', 'jd'],
};

export function orderedPurchaseLinks(
  links: PurchaseLinks,
  locale: string,
): { platform: PurchasePlatform; url: string }[] {
  const priority = PLATFORM_PRIORITY_BY_LOCALE[locale] ?? PLATFORM_PRIORITY_BY_LOCALE['zh-CN'];
  return priority
    .map((platform) => ({ platform, url: links[platform] }))
    .filter((x): x is { platform: PurchasePlatform; url: string } => Boolean(x.url));
}
