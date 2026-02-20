/**
 * Stdout redaction module.
 *
 * Filters sensitive patterns from stdout before returning to Claude.
 * This is a pure function with no side effects and no external dependencies.
 */

/**
 * Regex patterns used for redaction, applied in order.
 *
 * Pattern 1: Any KEY_NAME=value where KEY_NAME contains SECRET, KEY, PASSWORD, or TOKEN
 *   - Captures the key name in group 1 so we preserve it in the replacement.
 *   - The value is consumed up to (but not including) whitespace or end-of-string.
 *
 * Pattern 2: Standalone 64-character lowercase hex strings.
 *   - Must be surrounded by word boundaries so partial matches (e.g. 128-char hex)
 *     are not split into two replacements.
 *   - Uses a word boundary (\b) rather than lookahead/lookbehind for simplicity.
 */
const REDACTION_PATTERNS: ReadonlyArray<{ pattern: RegExp; replacement: string }> = [
  {
    // Matches: SOME_SECRET_KEY=<value>, DB_PASSWORD=<value>, API_TOKEN=<value>, etc.
    // Key name must consist of uppercase letters, digits, or underscores.
    // Value runs until whitespace or end-of-string (greedy, non-whitespace chars).
    pattern: /([A-Z0-9_]*(?:SECRET|KEY|PASSWORD|TOKEN)[A-Z0-9_]*)=\S+/g,
    replacement: '$1=[REDACTED]',
  },
  {
    // Matches a standalone 64-character hex string (exactly 64 hex digits, bounded
    // by a non-hex character or string boundary on each side).
    pattern: /(?<![0-9a-fA-F])[0-9a-fA-F]{64}(?![0-9a-fA-F])/g,
    replacement: '[REDACTED]',
  },
]

/**
 * Redacts sensitive patterns from stdout text before returning to Claude.
 *
 * Patterns redacted:
 * - Any `KEY_NAME=value` where KEY_NAME contains SECRET, KEY, PASSWORD, or TOKEN
 * - Standalone 64-character lowercase hex strings (e.g. SHA-256 hashes used as secrets)
 *
 * Normal output (log lines, JSON without sensitive keys, short identifiers) is
 * returned unchanged.
 *
 * @param text - Raw stdout string to filter.
 * @returns The redacted string with sensitive values replaced by `[REDACTED]`.
 */
export function redactStdout(text: string): string {
  let result = text
  for (const { pattern, replacement } of REDACTION_PATTERNS) {
    // Reset lastIndex before each use since the patterns have the /g flag.
    pattern.lastIndex = 0
    result = result.replace(pattern, replacement)
  }
  return result
}
