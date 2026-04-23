const LOCALE_DESC: Record<string, string> = {
  'zh-CN': 'Simplified Chinese (Mainland China)',
  'zh-TW': 'Traditional Chinese (Taiwan — use Taiwan vocabulary and idioms, e.g. 馬鈴薯 for potato, 網路 for internet)',
  'zh-HK': 'Traditional Chinese (Hong Kong — use Hong Kong vocabulary, e.g. 薯仔 for potato, 互聯網 for internet)',
  'en-US': 'English (United States)',
  'en-GB': 'English (United Kingdom)',
  'en-AU': 'English (Australia)',
  'ja': 'Japanese',
}

export type PromptArgs = {
  localeFrom: string
  localeTo: string
  texts: string[]
}

export const buildTranslationPrompt = ({ localeFrom, localeTo, texts }: PromptArgs): string => {
  const from = LOCALE_DESC[localeFrom] ?? localeFrom
  const to = LOCALE_DESC[localeTo] ?? localeTo

  return [
    `You are a professional translator for a VPN product website operated under two brands: Overleap (overseas) and 开途 / Kaitu (China).`,
    `Translate the strings from ${from} to ${to}.`,
    `Preserve exactly: URLs, email addresses, placeholder tokens like {name} or %s, and brand terms: "Overleap", "Kaitu", "开途", "k2", "k2cc".`,
    `Tone: concise, technical-marketing, natural for native readers.`,
    ``,
    `CRITICAL OUTPUT RULES:`,
    `- Respond with ONLY a JSON array of translated strings, same length and order as the input.`,
    `- Do NOT wrap the response in markdown code fences (no \`\`\`json).`,
    `- Do NOT include any commentary, preamble, or trailing text.`,
    `- The response must parse with JSON.parse() on the first try.`,
    ``,
    `Input array (${texts.length} item${texts.length === 1 ? '' : 's'}):`,
    JSON.stringify(texts),
  ].join('\n')
}
