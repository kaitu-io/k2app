// Auto-generated namespace index
// DO NOT EDIT - run 'node scripts/i18n/split-namespaces.js webapp' to regenerate

export const namespaces = ["common","nav","auth","account","dashboard","purchase","invite","retailer","wallet","startup","theme","ticket","feedback"] as const;
export type Namespace = typeof namespaces[number];
export const defaultNamespace: Namespace = 'common';

export const namespaceMapping: Record<string, Namespace> = {
  "common": "common",
  "status": "common",
  "errors": "common",
  "loadingAndEmpty": "common",
  "messages": "common",
  "brand": "common",
  "features": "common",
  "navigation": "nav",
  "appBar": "nav",
  "appBarConnector": "nav",
  "appBarMembership": "nav",
  "layout": "nav",
  "auth": "auth",
  "updateEmail": "auth",
  "account": "account",
  "devices": "account",
  "proHistory": "account",
  "memberManagement": "account",
  "dashboard": "dashboard",
  "troubleshooting": "dashboard",
  "versionComparison": "dashboard",
  "purchase": "purchase",
  "plan": "purchase",
  "memberSelection": "purchase",
  "deviceInstall": "purchase",
  "invite": "invite",
  "inviteCodeList": "invite",
  "retailer": "retailer",
  "retailerStats": "retailer",
  "retailerRule": "retailer",
  "wallet": "wallet",
  "startup": "startup",
  "serviceNotInstalled": "startup",
  "upgradeRequired": "startup",
  "app": "startup",
  "theme": "theme",
  "ticket": "ticket",
  "faq": "ticket",
  "feedback": "feedback"
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
