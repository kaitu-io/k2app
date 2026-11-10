/**
 * Keep only the listed message paths. A path is `<file>` (a whole message file,
 * e.g. `chat`) or `<file>.<topKey>` (e.g. `nav.footer`) — see the "Message-file
 * shape trap" in web/CLAUDE.md for why that is the unit.
 *
 * Unknown paths are ignored (a locale may lack a file; request.ts already fell
 * back to zh-CN in that case, so the path exists).
 */
export function pickMessages(
  messages: Record<string, unknown>,
  paths: readonly string[]
): Record<string, unknown> {
  const out: Record<string, Record<string, unknown> | unknown> = {};
  for (const p of paths) {
    const [file, top] = p.split('.');
    const fileMessages = messages[file];
    if (fileMessages === undefined) continue;
    if (top === undefined) {
      out[file] = fileMessages;
      continue;
    }
    if (typeof fileMessages !== 'object' || fileMessages === null || !(top in fileMessages)) continue;
    const bucket = (out[file] ??= {}) as Record<string, unknown>;
    // A whole-file entry for the same file already holds everything.
    if (bucket === fileMessages) continue;
    bucket[top] = (fileMessages as Record<string, unknown>)[top];
  }
  return out;
}
