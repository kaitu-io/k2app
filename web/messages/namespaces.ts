// Auto-generated namespace index
// DO NOT EDIT - run 'node scripts/i18n/split-namespaces.js web' to regenerate

export const namespaces = ["common","nav","hero","auth","discovery","purchase","wallet","campaigns","admin","invite","install","theme","changelog","releases","routers","k2","guide-parents","errors","licenseKeys","survey","account","landing","download","help"] as const;
export type Namespace = typeof namespaces[number];
export const defaultNamespace: Namespace = 'common';

/**
 * Namespace 按品牌划分（spec 2026-09-04-overleap-site-decoupling §5.1）。
 * `request.ts` 只为当前构建品牌加载它自己的集合；一个品牌独有的 namespace 只在该品牌的
 * locale 目录里有文件（开途 = zh-*，Overleap = en-* + ja），tests/messages-parity.test.ts
 * 按品牌逐 namespace 比对 key 集。
 *
 * SHARED 里的 `admin` 是因为共享的 Header（账户按钮）与 ChangePasswordDialog 用到
 * `admin.account.*`——不是 Overleap 有后台。
 */
export const SHARED_NAMESPACES = ["common","nav","auth","purchase","account","discovery","errors","k2","admin"] as const satisfies readonly Namespace[];
export const BRAND_NAMESPACES = {
  kaitu: [...SHARED_NAMESPACES, "hero","install","wallet","campaigns","invite","theme","changelog","releases","routers","guide-parents","licenseKeys","survey"],
  overleap: [...SHARED_NAMESPACES, "landing","download","help"],
} as const satisfies Record<string, readonly Namespace[]>;

export const namespaceMapping: Record<string, Namespace> = {
  "step1": "common",
  "step2": "common",
  "step3": "common",
  "colon": "common",
  "slash": "common",
  "dollarSign": "common",
  "common": "common",
  "plan": "common",
  "nav": "nav",
  "footer": "nav",
  "languageDetection": "nav",
  "hero": "hero",
  "security": "hero",
  "download": "hero",
  "routers": "hero",
  "faq": "hero",
  "login": "auth",
  "discovery": "discovery",
  "privacy": "discovery",
  "terms": "discovery",
  "cookieConsent": "discovery",
  "purchase": "purchase",
  "refund": "purchase",
  "error403": "purchase",
  "purchaseStep3": "purchase",
  "wallet": "wallet",
  "campaigns": "campaigns",
  "edm": "campaigns",
  "admin": "admin",
  "retailer": "admin",
  "retailerRules": "admin",
  "users": "admin",
  "tasks": "admin",
  "account": "admin",
  "inviteLanding": "invite",
  "install": "install",
  "theme": "theme",
  "opensource": "theme",
  "changelog": "changelog",
  "releases": "releases",
  "guide-parents": "guide-parents",
  "errors": "errors",
  "licenseKeys": "licenseKeys",
  "survey": "survey"
};

// Lazy load namespace for a specific language
export async function loadNamespace(lang: string, ns: Namespace): Promise<Record<string, unknown>> {
  return import(`./${lang}/${ns}.json`);
}

// Load all namespaces for a language
export async function loadAllNamespaces(lang: string): Promise<Record<Namespace, Record<string, unknown>>> {
  const results = await Promise.all(
    namespaces.map(async (ns) => {
      const data = await loadNamespace(lang, ns);
      return [ns, data] as const;
    })
  );
  return Object.fromEntries(results) as Record<Namespace, Record<string, unknown>>;
}
