/**
 * kaitu-mail — OpenClaw plugin
 * Provides email tools scoped to a per-profile himalaya account.
 *
 * Config (plugins.entries.kaitu-mail.config):
 *   account: "marketing-kaitu" | "support-kaitu" | ...
 *
 * Tools registered (all optional, must be in agent allowlist):
 *   mail_list    — list emails, filtered by account's email address
 *   mail_read    — read full email by ID
 *   mail_send    — send email from this account
 *   mail_search  — search emails, filtered by account
 *   mail_folders — list folders
 *   mail_move    — move email to folder
 */

import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// Derive email address from account name: "marketing-kaitu" → "marketing@kaitu.io"
function accountToEmail(account) {
  const m = account.match(/^(.+)-kaitu$/);
  return m ? `${m[1]}@kaitu.io` : account;
}

// himalaya CLI: subcommand-level -a flag (v1 syntax)
async function hima(account, ...args) {
  const insertAt = args.length >= 2 ? 2 : 1;
  const fullArgs = [...args.slice(0, insertAt), "-a", account, "-o", "json", ...args.slice(insertAt)];
  const { stdout, stderr } = await execFileAsync("himalaya", fullArgs);
  if (stderr && !stdout) throw new Error(stderr.trim());
  return JSON.parse(stdout || "[]");
}

async function himaRaw(account, ...args) {
  const insertAt = args.length >= 2 ? 2 : 1;
  const fullArgs = [...args.slice(0, insertAt), "-a", account, ...args.slice(insertAt)];
  const { stdout, stderr } = await execFileAsync("himalaya", fullArgs);
  if (stderr && !stdout) throw new Error(stderr.trim());
  return stdout;
}

// Filter envelopes to only those addressed to this account's email
// himalaya may return `to` as an object or an array depending on version
function filterByTo(envelopes, email) {
  if (!email) return envelopes;
  return envelopes.filter(e => {
    const raw = e.to || [];
    const toList = Array.isArray(raw) ? raw : [raw];
    const addrs = toList.map(a => (a.addr || a.email || a.address || "").toLowerCase());
    return addrs.some(addr => addr.includes(email.toLowerCase()));
  });
}

function ok(text) {
  return { content: [{ type: "text", text: typeof text === "string" ? text : JSON.stringify(text, null, 2) }] };
}
function err(msg) {
  return { content: [{ type: "text", text: `Error: ${msg}` }], isError: true };
}

const plugin = {
  id: "kaitu-mail",
  name: "Kaitu Mail",

  register(api) {
    const cfg = api.pluginConfig;
    if (!cfg?.account) {
      api.log?.warn?.("[kaitu-mail] No account configured. Set plugins.entries.kaitu-mail.config.account");
      return;
    }

    const ACCOUNT = cfg.account;
    const EMAIL = accountToEmail(ACCOUNT);

    const toolOpts = { optional: true };

    // ── mail_list ────────────────────────────────────────────────────────────
    api.registerTool({
      name: "mail_list",
      label: "List emails",
      description: `List emails for ${EMAIL}. Results filtered to this account's address. Params: folder (default INBOX), limit (default 20), page (default 1).`,
      parameters: {
        type: "object",
        properties: {
          folder: { type: "string" },
          limit: { type: "number" },
          page: { type: "number" },
        },
      },
      async execute(_id, params) {
        try {
          const { folder = "INBOX", limit = 20, page = 1 } = params || {};
          const envelopes = await hima(ACCOUNT, "envelope", "list",
            "--folder", folder,
            "--max-width", "0",
            "--page-size", String(limit),
            "--page", String(page)
          );
          const filtered = filterByTo(Array.isArray(envelopes) ? envelopes : [], EMAIL);
          const result = filtered.map(e => ({
            id: e.id, subject: e.subject, from: e.from, to: e.to, date: e.date, flags: e.flags,
          }));
          return ok(result);
        } catch (e) { return err(e.message); }
      },
    }, toolOpts);

    // ── mail_read ────────────────────────────────────────────────────────────
    api.registerTool({
      name: "mail_read",
      label: "Read email",
      description: `Read full content of an email by ID (from mail_list). Account: ${EMAIL}.`,
      parameters: {
        type: "object",
        properties: {
          id: { type: "string" },
          folder: { type: "string" },
        },
        required: ["id"],
      },
      async execute(_id, params) {
        try {
          const { id, folder = "INBOX" } = params;
          const text = await himaRaw(ACCOUNT, "message", "read", "--folder", folder, id);
          return ok(text);
        } catch (e) { return err(e.message); }
      },
    }, toolOpts);

    // ── mail_send ────────────────────────────────────────────────────────────
    api.registerTool({
      name: "mail_send",
      label: "Send email",
      description: `Send email from ${EMAIL}. The From field is set automatically.`,
      parameters: {
        type: "object",
        properties: {
          to: { type: "string", description: "Recipient(s), comma-separated" },
          subject: { type: "string" },
          body: { type: "string" },
          cc: { type: "string" },
          reply_to: { type: "string" },
        },
        required: ["to", "subject", "body"],
      },
      async execute(_id, params) {
        try {
          const { to, subject, body, cc, reply_to } = params;
          let mml = `From: ${EMAIL}\nTo: ${to}\nSubject: ${subject}\n`;
          if (cc) mml += `Cc: ${cc}\n`;
          if (reply_to) mml += `Reply-To: ${reply_to}\n`;
          mml += `\n${body}`;

          await new Promise((resolve, reject) => {
            const proc = execFile("himalaya", ["message", "send", "-a", ACCOUNT], (e, _out, serr) => {
              if (e) reject(new Error(serr || e.message));
              else resolve();
            });
            proc.stdin.write(mml);
            proc.stdin.end();
          });
          return ok(`Email sent to ${to}`);
        } catch (e) { return err(e.message); }
      },
    }, toolOpts);

    // ── mail_search ──────────────────────────────────────────────────────────
    api.registerTool({
      name: "mail_search",
      label: "Search emails",
      description: `Search emails for ${EMAIL} by keyword. Results filtered to this account.`,
      parameters: {
        type: "object",
        properties: {
          query: { type: "string" },
          folder: { type: "string" },
          limit: { type: "number" },
        },
        required: ["query"],
      },
      async execute(_id, params) {
        try {
          const { query, folder = "INBOX", limit = 20 } = params;
          const envelopes = await hima(ACCOUNT, "envelope", "list",
            "--folder", folder,
            "--query", query,
            "--max-width", "0",
            "--page-size", String(limit)
          );
          const filtered = filterByTo(Array.isArray(envelopes) ? envelopes : [], EMAIL);
          return ok(filtered.map(e => ({ id: e.id, subject: e.subject, from: e.from, to: e.to, date: e.date })));
        } catch (e) { return err(e.message); }
      },
    }, toolOpts);

    // ── mail_folders ─────────────────────────────────────────────────────────
    api.registerTool({
      name: "mail_folders",
      label: "List mail folders",
      description: `List all IMAP folders for ${EMAIL}.`,
      parameters: { type: "object", properties: {} },
      async execute(_id, _params) {
        try {
          const folders = await hima(ACCOUNT, "folder", "list");
          return ok(folders);
        } catch (e) { return err(e.message); }
      },
    }, toolOpts);

    // ── mail_move ────────────────────────────────────────────────────────────
    api.registerTool({
      name: "mail_move",
      label: "Move email",
      description: `Move an email to a different folder. Account: ${EMAIL}.`,
      parameters: {
        type: "object",
        properties: {
          id: { type: "string" },
          from_folder: { type: "string" },
          to_folder: { type: "string" },
        },
        required: ["id", "from_folder", "to_folder"],
      },
      async execute(_id, params) {
        try {
          const { id, from_folder, to_folder } = params;
          await himaRaw(ACCOUNT, "message", "move", "--folder", from_folder, id, to_folder);
          return ok(`Moved ${id} → ${to_folder}`);
        } catch (e) { return err(e.message); }
      },
    }, toolOpts);
  },
};

export default plugin;
