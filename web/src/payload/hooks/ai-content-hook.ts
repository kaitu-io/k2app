/**
 * AI Content Hook for Payload CMS
 *
 * This hook uses AI (Claude/GPT) to automatically generate:
 * - Article summaries
 * - SEO titles and descriptions
 * - URL slugs
 * - (Optional) Translations for localized fields
 */

interface AIMetadataInput {
  content: unknown // Lexical editor content
  title: string
  locale: string
}

interface AIMetadataOutput {
  summary?: string
  seoTitle?: string
  seoDescription?: string
  slug?: string
}

/**
 * Extract plain text from Lexical editor content
 */
function extractTextFromLexical(content: unknown): string {
  if (!content || typeof content !== 'object') return ''

  const root = (content as { root?: unknown }).root
  if (!root || typeof root !== 'object') return ''

  const children = (root as { children?: unknown[] }).children
  if (!Array.isArray(children)) return ''

  const extractText = (nodes: unknown[]): string => {
    return nodes
      .map((node) => {
        if (!node || typeof node !== 'object') return ''
        const n = node as { type?: string; text?: string; children?: unknown[] }

        if (n.type === 'text' && typeof n.text === 'string') {
          return n.text
        }
        if (Array.isArray(n.children)) {
          return extractText(n.children)
        }
        return ''
      })
      .join(' ')
  }

  return extractText(children).trim()
}

/**
 * Generate article metadata using AI
 */
export async function generateArticleMetadata(
  input: AIMetadataInput
): Promise<AIMetadataOutput> {
  const apiKey = process.env.AI_API_KEY
  const apiProvider = process.env.AI_PROVIDER || 'anthropic' // 'anthropic' or 'openai'

  if (!apiKey) {
    console.warn('AI_API_KEY not configured, skipping AI metadata generation')
    return {}
  }

  const textContent = extractTextFromLexical(input.content)

  if (!textContent || textContent.length < 50) {
    // Not enough content to generate meaningful metadata
    return {}
  }

  const prompt = `You are a content editor assistant. Analyze the following article and generate metadata.

Title: ${input.title}
Locale: ${input.locale}
Content: ${textContent.substring(0, 3000)}

Please respond with a JSON object containing:
1. "summary": A 2-3 sentence summary of the article (in the same language as the content)
2. "seoTitle": An SEO-optimized title, max 60 characters
3. "seoDescription": An SEO meta description, max 160 characters
4. "slug": A URL-friendly slug in English/pinyin, lowercase, hyphens only, max 50 chars

Respond ONLY with valid JSON, no markdown code blocks.`

  try {
    let result: AIMetadataOutput = {}

    if (apiProvider === 'anthropic') {
      const response = await fetch('https://api.anthropic.com/v1/messages', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': apiKey,
          'anthropic-version': '2023-06-01',
        },
        body: JSON.stringify({
          model: process.env.AI_MODEL || 'claude-3-haiku-20240307',
          max_tokens: 500,
          messages: [
            {
              role: 'user',
              content: prompt,
            },
          ],
        }),
      })

      if (!response.ok) {
        throw new Error(`Anthropic API error: ${response.status}`)
      }

      const data = await response.json() as {
        content?: Array<{ type: string; text: string }>
      }
      const text = data.content?.[0]?.text || '{}'
      result = JSON.parse(text) as AIMetadataOutput
    } else if (apiProvider === 'openai' || apiProvider === 'deepseek') {
      // DeepSeek uses OpenAI-compatible API
      const apiEndpoint = apiProvider === 'deepseek'
        ? 'https://api.deepseek.com/v1/chat/completions'
        : 'https://api.openai.com/v1/chat/completions'
      const defaultModel = apiProvider === 'deepseek' ? 'deepseek-chat' : 'gpt-4o-mini'

      const response = await fetch(apiEndpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          model: process.env.AI_MODEL || defaultModel,
          messages: [
            {
              role: 'user',
              content: prompt,
            },
          ],
          max_tokens: 500,
        }),
      })

      if (!response.ok) {
        throw new Error(`${apiProvider} API error: ${response.status}`)
      }

      const data = await response.json() as {
        choices?: Array<{ message?: { content: string } }>
      }
      const text = data.choices?.[0]?.message?.content || '{}'
      result = JSON.parse(text) as AIMetadataOutput
    }

    return result
  } catch (error) {
    console.error('AI metadata generation failed:', error)
    return {}
  }
}

/**
 * Translate Lexical rich text content while preserving structure
 */
export async function translateLexicalContent(
  content: unknown,
  sourceLocale: string,
  targetLocale: string
): Promise<unknown> {
  if (!content || typeof content !== 'object') return content

  const apiKey = process.env.AI_API_KEY
  if (!apiKey || process.env.AI_ENABLED !== 'true') {
    return content
  }

  // Extract all text nodes for batch translation
  const textNodes: { path: string; text: string }[] = []

  const collectTextNodes = (node: unknown, path: string) => {
    if (!node || typeof node !== 'object') return
    const n = node as Record<string, unknown>

    if (n.type === 'text' && typeof n.text === 'string' && n.text.trim()) {
      textNodes.push({ path, text: n.text })
    }
    if (Array.isArray(n.children)) {
      n.children.forEach((child, i) => collectTextNodes(child, `${path}.children[${i}]`))
    }
    if (n.root && typeof n.root === 'object') {
      collectTextNodes(n.root, `${path}.root`)
    }
  }

  collectTextNodes(content, '')

  if (textNodes.length === 0) return content

  // Batch translate all text
  const textsToTranslate = textNodes.map(n => n.text)
  const translatedTexts = await translateTextBatch(textsToTranslate, sourceLocale, targetLocale)

  // Deep clone and replace text nodes
  const result = JSON.parse(JSON.stringify(content))

  const setByPath = (obj: unknown, path: string, value: string) => {
    const parts = path.split(/\.|\[|\]/).filter(Boolean)
    let current = obj as Record<string, unknown>
    for (let i = 0; i < parts.length - 1; i++) {
      current = current[parts[i]] as Record<string, unknown>
    }
    current[parts[parts.length - 1]] = value
  }

  textNodes.forEach((node, i) => {
    if (translatedTexts[i]) {
      setByPath(result, `${node.path}.text`, translatedTexts[i])
    }
  })

  return result
}

/**
 * Batch translate multiple texts efficiently
 */
async function translateTextBatch(
  texts: string[],
  sourceLocale: string,
  targetLocale: string
): Promise<string[]> {
  const apiKey = process.env.AI_API_KEY
  const apiProvider = process.env.AI_PROVIDER || 'deepseek'

  if (!apiKey || texts.length === 0) return texts

  const localeNames: Record<string, string> = {
    'zh-CN': 'Simplified Chinese',
    'zh-TW': 'Traditional Chinese (Taiwan)',
    'zh-HK': 'Traditional Chinese (Hong Kong)',
    'en-US': 'English (US)',
    'en-GB': 'English (UK)',
    'en-AU': 'English (Australia)',
    'ja': 'Japanese',
  }

  // Use JSON array for batch translation
  const prompt = `Translate the following texts from ${localeNames[sourceLocale] || sourceLocale} to ${localeNames[targetLocale] || targetLocale}.

Input (JSON array):
${JSON.stringify(texts)}

Respond with ONLY a JSON array of translated strings in the same order. No explanations.`

  try {
    let translatedTexts: string[] = texts

    if (apiProvider === 'anthropic') {
      const response = await fetch('https://api.anthropic.com/v1/messages', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': apiKey,
          'anthropic-version': '2023-06-01',
        },
        body: JSON.stringify({
          model: process.env.AI_MODEL || 'claude-3-haiku-20240307',
          max_tokens: 4000,
          messages: [{ role: 'user', content: prompt }],
        }),
      })

      if (response.ok) {
        const data = await response.json() as { content?: Array<{ text: string }> }
        const text = data.content?.[0]?.text || '[]'
        translatedTexts = JSON.parse(text)
      }
    } else if (apiProvider === 'openai' || apiProvider === 'deepseek') {
      const apiEndpoint = apiProvider === 'deepseek'
        ? 'https://api.deepseek.com/v1/chat/completions'
        : 'https://api.openai.com/v1/chat/completions'
      const defaultModel = apiProvider === 'deepseek' ? 'deepseek-chat' : 'gpt-4o-mini'

      const response = await fetch(apiEndpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          model: process.env.AI_MODEL || defaultModel,
          messages: [{ role: 'user', content: prompt }],
          max_tokens: 4000,
        }),
      })

      if (response.ok) {
        const data = await response.json() as { choices?: Array<{ message?: { content: string } }> }
        const text = data.choices?.[0]?.message?.content || '[]'
        translatedTexts = JSON.parse(text)
      }
    }

    return translatedTexts.length === texts.length ? translatedTexts : texts
  } catch (error) {
    console.error('Batch translation failed:', error)
    return texts
  }
}

/**
 * Translate plain text content to another locale using AI
 */
export async function translateContent(
  content: string,
  sourceLocale: string,
  targetLocale: string
): Promise<string> {
  const apiKey = process.env.AI_API_KEY
  const apiProvider = process.env.AI_PROVIDER || 'anthropic'

  if (!apiKey) {
    console.warn('AI_API_KEY not configured, skipping translation')
    return content
  }

  const localeNames: Record<string, string> = {
    'zh-CN': 'Simplified Chinese',
    'zh-TW': 'Traditional Chinese (Taiwan)',
    'zh-HK': 'Traditional Chinese (Hong Kong)',
    'en-US': 'English (US)',
    'en-GB': 'English (UK)',
    'en-AU': 'English (Australia)',
    'ja': 'Japanese',
  }

  const prompt = `Translate the following text from ${localeNames[sourceLocale] || sourceLocale} to ${localeNames[targetLocale] || targetLocale}. Preserve the meaning and tone. Only output the translated text, nothing else.

Text: ${content}`

  try {
    if (apiProvider === 'anthropic') {
      const response = await fetch('https://api.anthropic.com/v1/messages', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': apiKey,
          'anthropic-version': '2023-06-01',
        },
        body: JSON.stringify({
          model: process.env.AI_MODEL || 'claude-3-haiku-20240307',
          max_tokens: 2000,
          messages: [
            {
              role: 'user',
              content: prompt,
            },
          ],
        }),
      })

      if (!response.ok) {
        throw new Error(`Anthropic API error: ${response.status}`)
      }

      const data = await response.json() as {
        content?: Array<{ type: string; text: string }>
      }
      return data.content?.[0]?.text || content
    } else if (apiProvider === 'openai' || apiProvider === 'deepseek') {
      // DeepSeek uses OpenAI-compatible API
      const apiEndpoint = apiProvider === 'deepseek'
        ? 'https://api.deepseek.com/v1/chat/completions'
        : 'https://api.openai.com/v1/chat/completions'
      const defaultModel = apiProvider === 'deepseek' ? 'deepseek-chat' : 'gpt-4o-mini'

      const response = await fetch(apiEndpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          model: process.env.AI_MODEL || defaultModel,
          messages: [
            {
              role: 'user',
              content: prompt,
            },
          ],
          max_tokens: 2000,
        }),
      })

      if (!response.ok) {
        throw new Error(`${apiProvider} API error: ${response.status}`)
      }

      const data = await response.json() as {
        choices?: Array<{ message?: { content: string } }>
      }
      return data.choices?.[0]?.message?.content || content
    }

    return content
  } catch (error) {
    console.error('Translation failed:', error)
    return content
  }
}
