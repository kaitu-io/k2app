import { brandConfig } from '../brands';

const COMMON_FAQ_KEYS = [
  'connection',
  'appNotWorking',
  'nodeChoice',
  'wifiSwitch',
  'deviceRemoved',
  'updateIssue',
  'loginFailed',
] as const;

export type FaqKey = string;

// 品牌专属故事 key 来自 brands/<id> 配置（kaitu: legacy ANC 客户端、中国区上架；
// 其文案在 brands/kaitu/locales/<lang>/ticket.json overlay，不打进其它品牌构建）。
export const FAQ_KEYS: readonly FaqKey[] = [
  ...COMMON_FAQ_KEYS,
  ...brandConfig.faqExtraKeys,
];
