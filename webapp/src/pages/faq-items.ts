export const FAQ_KEYS = [
  "connection",
  "appNotWorking",
  "nodeChoice",
  "wifiSwitch",
  "deviceRemoved",
  "updateIssue",
  "loginFailed",
  "allNationConnect",
  "chinaAppStore",
] as const;

export type FaqKey = (typeof FAQ_KEYS)[number];
