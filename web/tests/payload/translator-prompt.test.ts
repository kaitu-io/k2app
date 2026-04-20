import { describe, it, expect } from 'vitest'
import { buildTranslationPrompt } from '@/payload/translator/customPrompt'

describe('buildTranslationPrompt', () => {
  it('includes locale descriptors for zh-TW', () => {
    const p = buildTranslationPrompt({
      localeFrom: 'zh-CN', localeTo: 'zh-TW', texts: ['土豆'],
    })
    expect(p).toMatch(/Traditional Chinese.*Taiwan/i)
  })

  it('forbids markdown code fences', () => {
    const p = buildTranslationPrompt({
      localeFrom: 'zh-CN', localeTo: 'en-US', texts: ['hi'],
    })
    expect(p).toMatch(/do not.*code fence|no.*markdown/i)
  })

  it('instructs to preserve brand terms', () => {
    const p = buildTranslationPrompt({
      localeFrom: 'zh-CN', localeTo: 'ja', texts: ['test'],
    })
    expect(p).toContain('Kaitu')
    expect(p).toContain('k2cc')
  })

  it('outputs a JSON array only, same length as input', () => {
    const p = buildTranslationPrompt({
      localeFrom: 'zh-CN', localeTo: 'en-US', texts: ['a', 'b', 'c'],
    })
    expect(p).toMatch(/JSON array/i)
    expect(p).toContain('"a"')
    expect(p).toContain('"c"')
  })
})
