// Auto-generated namespace index
// DO NOT EDIT - run 'node scripts/i18n/split-namespaces.js web' to regenerate

export const namespaces = ["common","nav","hero","auth","discovery","purchase","wallet","campaigns","admin","invite","install","theme","changelog","k2"] as const;
export type Namespace = typeof namespaces[number];
export const defaultNamespace: Namespace = 'common';

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
  "changelog": "changelog"
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
