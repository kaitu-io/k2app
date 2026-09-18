#!/usr/bin/env node
// Web OTA anti-rollback gate.
//
// WHY THIS EXISTS
// ---------------
// `{brand}/web/latest.json` is a single global mutable pointer, and the 4th
// version segment is TIME-BASED (scripts/ci/web-ota-manifest.mjs) — so a
// bundle built from an OLD commit still outranks whatever is live and wins.
// Two entry points can publish (a `webapp/*` tag — per brand — and a
// workflow_dispatch), and a tag can be cut from a release branch that lags
// main. Publishing that ref's webapp silently DOWNGRADES the UI of every OTA
// client of the brand(s) it targets, and nothing else in the pipeline noticed.
//
// Near-miss that produced this gate (2026-09-07): tag v0.4.10-overleap-mobile
// published dual-brand stable web OTA 0.4.10.21546118 from 96fe5032 via
// build-mobile.yml's app-release LINKAGE tail. Harmless only by timing — every
// newer webapp commit happened to land after it. Had the same tag been pushed
// five days later, it would have reverted the purchase-preview runaway fix
// (07fd51b4) for the entire installed base, invisibly. That linkage tail has
// since been removed — a web OTA now needs its own `webapp/*` tag — but the
// same lag hazard remains for a tag cut from a release branch, which is exactly
// what this gate still catches.
//
// DESIGN
// ------
// The gate lives at the ONE chokepoint every publish path goes through, so it
// covers current and future callers without enumerating them.
//
// It compares against the SOURCE COMMIT OF THE BUNDLE THAT IS ACTUALLY LIVE,
// not against main. main is a proxy that is wrong in both directions: it
// refuses legitimate forward rolls when live is older than both, and it misses
// a downgrade when live is ahead of main (a dispatch from a non-main ref).
// Provenance comes from a sidecar object next to the manifest that NO client
// reads (`latest-source.json`), so the manifest schema — and every shipped
// parser — is untouched.
//
// Provenance is read from S3, never from the CDN: CloudFront invalidation is
// asynchronous, so a CDN read right after a publish can still serve the
// previous provenance, which would turn the dedup decision into a coin flip.
//
// Fail-closed discipline: "cannot read provenance" and "provenance names a
// commit this clone does not have" are DIFFERENT from "no provenance yet", and
// neither is allowed to read as "clean". Only a genuine 404 (first publish
// after this gate landed, or a brand-new brand) is a pass-with-warning.

import { spawnSync } from 'node:child_process';
import { appendFileSync } from 'node:fs';

// Relation of (live bundle's source commit) → (commit being published).
export const RELATIONS = [
  'not-enforced', // beta / namespaced publish: no shipped stable audience to protect
  'absent', // no provenance recorded yet (bootstrap)
  'identical', // live bundle already came from this exact commit
  'forward', // live commit is an ancestor: a normal forward roll
  'downgrade', // publish commit is an ancestor of live: would move stable BACKWARDS
  'diverged', // neither is an ancestor of the other: partial downgrade, unreviewable
  'unresolvable', // provenance commit is not in this clone (deleted branch, shallow fetch)
];

// Combination severity across brands. Both brands always publish from the same
// commit, so a disagreement means a previous publish half-failed. Taking the
// worst relation heals that on the next forward publish instead of wedging the
// pipeline behind a human.
const SEVERITY = {
  identical: 0,
  forward: 0,
  'not-enforced': 0,
  absent: 1,
  downgrade: 2,
  diverged: 3,
  unresolvable: 4,
};

class ProvenanceUnreadable extends Error {}
export { ProvenanceUnreadable };

/** Default child-process runner. Injected in tests. */
export function defaultExec(cmd, args) {
  const r = spawnSync(cmd, args, { encoding: 'utf8' });
  if (r.error) return { status: 127, stdout: '', stderr: String(r.error.message) };
  return { status: r.status ?? 1, stdout: r.stdout ?? '', stderr: r.stderr ?? '' };
}

// A missing key is the ONLY tolerated failure. Everything else — credentials,
// network, a truncated body, a body that is not the expected shape — throws.
const MISSING_KEY = /\(404\)|NoSuchKey|Not Found|does not exist/i;

export function provenanceKey(brand) {
  return `${brand}/web/latest-source.json`;
}

// The manifest every client polls, next to the provenance file. Used only to
// tell the two meanings of "provenance is 404" apart.
export function manifestKey(brand) {
  return `${brand}/web/latest.json`;
}

// `aws s3 cp` reports a MISSING BUCKET with byte-identical stderr to a missing
// key ("An error occurred (404) when calling the HeadObject operation: Not
// Found" in both cases — verified against aws-cli 2.34). So a 404 alone cannot
// distinguish "first publish, nothing recorded yet" from "reading the wrong
// place entirely", and the second one must never read as the first.
//
// Two things keep that honest:
//   1. the bucket is ONE literal (WEB_OTA_BUCKET in publish-web-ota.yml) shared
//      by this gate and the upload step, so a wrong bucket cannot make the gate
//      blind while the publish still succeeds — the upload fails loudly too;
//   2. this probe: if the sibling manifest IS readable, the prefix is right and
//      a 404 on provenance really is the bootstrap case. If neither exists, the
//      publish is still allowed (a brand that has never published is legitimate)
//      but says so in as many words instead of implying "verified clean".
//
// Deliberately NOT `s3api head-bucket`: the CI IAM user is scoped to the brand
// object subtrees, so a bucket-level call can 403 on a perfectly healthy
// setup — a gate that fails closed on a missing permission it never needed
// would take down every publish.
export function probeSiblingManifest(brand, { bucket, exec = defaultExec }) {
  const key = manifestKey(brand);
  const r = exec('aws', ['s3', 'cp', `s3://${bucket}/${key}`, '-']);
  if (r.status === 0) return true;
  if (MISSING_KEY.test(r.stderr)) return false;
  throw new ProvenanceUnreadable(
    `cannot read s3://${bucket}/${key} (exit ${r.status}): ${r.stderr.trim() || '(no stderr)'}`,
  );
}

/**
 * Read one brand's live provenance from S3.
 * @returns {{commit: string, version?: string}|null} null when the key is absent.
 * @throws {ProvenanceUnreadable} on any other failure, including a malformed body.
 */
export function readProvenance(brand, { bucket, exec = defaultExec }) {
  const key = provenanceKey(brand);
  const r = exec('aws', ['s3', 'cp', `s3://${bucket}/${key}`, '-']);
  if (r.status !== 0) {
    if (MISSING_KEY.test(r.stderr)) return null;
    throw new ProvenanceUnreadable(
      `cannot read s3://${bucket}/${key} (exit ${r.status}): ${r.stderr.trim() || '(no stderr)'}`,
    );
  }
  let parsed;
  try {
    parsed = JSON.parse(r.stdout);
  } catch (e) {
    throw new ProvenanceUnreadable(`s3://${bucket}/${key} is not valid JSON: ${e.message}`);
  }
  if (!/^[0-9a-f]{40}$/.test(parsed?.commit ?? '')) {
    throw new ProvenanceUnreadable(
      `s3://${bucket}/${key} has no usable .commit (got: ${JSON.stringify(parsed?.commit)})`,
    );
  }
  return parsed;
}

/** git ancestry, with the runner injected so tests drive a real throwaway repo. */
export function classifyRelation(liveCommit, publishCommit, { exec = defaultExec } = {}) {
  if (liveCommit === null) return 'absent';
  if (exec('git', ['cat-file', '-e', `${liveCommit}^{commit}`]).status !== 0) return 'unresolvable';
  if (exec('git', ['cat-file', '-e', `${publishCommit}^{commit}`]).status !== 0) {
    throw new Error(`publish commit ${publishCommit} is not in this clone — checkout is broken`);
  }
  if (liveCommit === publishCommit) return 'identical';
  if (exec('git', ['merge-base', '--is-ancestor', liveCommit, publishCommit]).status === 0) {
    return 'forward';
  }
  if (exec('git', ['merge-base', '--is-ancestor', publishCommit, liveCommit]).status === 0) {
    return 'downgrade';
  }
  return 'diverged';
}

export function combineRelations(relations) {
  if (relations.length === 0) throw new Error('combineRelations needs at least one relation');
  for (const r of relations) {
    if (!RELATIONS.includes(r)) throw new Error(`unknown relation: ${r}`);
  }
  if (relations.every((r) => r === 'identical')) return 'identical';
  let worst = relations[0];
  for (const r of relations) {
    if (SEVERITY[r] > SEVERITY[worst]) worst = r;
  }
  // All-safe mixture (identical + forward) is a forward roll, not a dedup.
  return SEVERITY[worst] === 0 ? 'forward' : worst;
}

/**
 * The whole policy, as a pure function. Every publish is a human explicitly
 * asking for one (a `webapp/*` tag or a workflow_dispatch) — there is no
 * app-release linkage that a refusal would wrongly fail, so silence is never
 * the right answer. An unsafe relation REFUSES loudly, and `allow_rollback` is
 * the documented way to say "yes, go backwards" (spec §7 emergency republish).
 */
export function decide({ relation, allowRollback = false }) {
  if (!RELATIONS.includes(relation)) throw new Error(`unknown relation: ${relation}`);

  switch (relation) {
    case 'not-enforced':
      return {
        action: 'publish',
        level: 'info',
        notify: false,
        reason: 'gate does not apply to beta / namespaced publishes (no shipped stable audience)',
      };
    case 'absent':
      return {
        action: 'publish',
        level: 'warning',
        notify: false,
        reason:
          'no provenance recorded for the live bundle yet — publishing and recording it. ' +
          'Expected exactly once per brand, right after this gate landed.',
      };
    case 'forward':
      return {
        action: 'publish',
        level: 'info',
        notify: false,
        reason: 'live bundle was built from an ancestor of this commit — normal forward roll',
      };
    case 'identical':
      return {
        action: 'publish',
        level: 'info',
        notify: false,
        reason:
          'live bundle already came from this commit; republishing anyway because the ' +
          'publish was requested explicitly (a higher build number is the documented ' +
          'way to force re-delivery)',
      };
    case 'downgrade':
    case 'diverged':
    case 'unresolvable': {
      const what = {
        downgrade: 'this commit is an ANCESTOR of the live bundle, so publishing would move stable BACKWARDS',
        diverged:
          'this commit and the live bundle have diverged, so publishing would drop webapp work that is live',
        unresolvable:
          'the live bundle names a commit this clone does not have, so the comparison cannot be made',
      }[relation];
      if (allowRollback) {
        return {
          action: 'publish',
          level: 'warning',
          notify: true,
          reason: `${what} — publishing anyway because allow_rollback was set explicitly`,
        };
      }
      return {
        action: 'refuse',
        level: 'error',
        notify: false,
        reason:
          `${what}. Refusing. Either publish a descendant (merge/rebase onto main and retry), ` +
          'or re-run this workflow_dispatch with allow_rollback=true if moving stable ' +
          'backwards is the intent (spec §7 emergency republish).',
      };
    }
    default:
      throw new Error(`unhandled relation: ${relation}`);
  }
}

// ---------------------------------------------------------------- CLI ------

function arg(argv, name, fallback = '') {
  const i = argv.indexOf(`--${name}`);
  return i >= 0 && i + 1 < argv.length ? argv[i + 1] : fallback;
}

export function evaluate({
  brands,
  bucket,
  publishCommit,
  allowRollback,
  channel,
  namespace,
  exec = defaultExec,
}) {
  if (channel !== 'stable' || namespace !== '') {
    return { relation: 'not-enforced', perBrand: {}, ...decide({ relation: 'not-enforced', allowRollback }) };
  }
  const perBrand = {};
  for (const brand of brands) {
    const prov = readProvenance(brand, { bucket, exec });
    const entry = {
      commit: prov?.commit ?? null,
      version: prov?.version ?? null,
      relation: classifyRelation(prov?.commit ?? null, publishCommit, { exec }),
      bootstrap: null,
    };
    if (entry.relation === 'absent') {
      entry.bootstrap = probeSiblingManifest(brand, { bucket, exec }) ? 'expected' : 'suspicious';
    }
    perBrand[brand] = entry;
  }
  const relation = combineRelations(Object.values(perBrand).map((b) => b.relation));
  const suspicious = Object.entries(perBrand)
    .filter(([, b]) => b.bootstrap === 'suspicious')
    .map(([brand]) => brand);
  return { relation, perBrand, suspicious, ...decide({ relation, allowRollback }) };
}

function main(argv) {
  // --brands accepts a comma- OR whitespace-separated list, so the plan step's
  // space-separated `brands` output (scripts/ci/web-ota-plan.mjs) is passed
  // straight through, no bash reformatting.
  const brands = arg(argv, 'brands', 'kaitu overleap').split(/[,\s]+/).filter(Boolean);
  const bucket = arg(argv, 'bucket', 'd0.all7.cc');
  const publishCommit = arg(argv, 'publish-commit', process.env.GITHUB_SHA ?? '');
  const allowRollback = arg(argv, 'allow-rollback', 'false') === 'true';
  const channel = arg(argv, 'channel', 'stable');
  const namespace = arg(argv, 'namespace', '');
  const stage = arg(argv, 'stage', 'gate');

  if (!/^[0-9a-f]{40}$/.test(publishCommit)) {
    console.log(`::error::web-ota gate: --publish-commit must be a full sha, got "${publishCommit}"`);
    process.exit(1);
  }

  // Appended synchronously, never truncated: other steps write here too, and a
  // fire-and-forget async write would race process.exit() below.
  const gha = (file, text) => {
    if (process.env[file]) appendFileSync(process.env[file], text);
  };
  const emitOutputs = ({ proceed, relation, notify, reason }) =>
    gha(
      'GITHUB_OUTPUT',
      [
        `proceed=${proceed}`,
        `relation=${relation}`,
        `notify=${notify}`,
        // The reason travels into a bash string and then into a Slack JSON
        // payload; ${{ }} interpolation happens before bash parses the line, so a
        // quote / backtick / $ in the text would break the step rather than the
        // message. Sanitised at the source instead of at three call sites.
        `reason=${reason.replace(/\s+/g, ' ').replace(/"/g, "'").replace(/[`$\\]/g, '')}`,
        '',
      ].join('\n'),
    );

  let result;
  try {
    result = evaluate({ brands, bucket, publishCommit, allowRollback, channel, namespace });
  } catch (e) {
    // The gate could not be evaluated at all (S3 unreachable, credentials gone,
    // a body that is not provenance, a broken checkout). Fail closed: a publish
    // was explicitly requested and silence would be the wrong answer, so this
    // goes RED. `allow_rollback` must NEVER turn it into a pass — that flag
    // expresses intent about content, not permission to publish blind.
    //
    // `unevaluable` is deliberately NOT a RELATIONS member: no relation was
    // established. It exists only as an output value so the workflow and Slack
    // can tell this apart from a real `unresolvable` comparison.
    const reason = `cannot evaluate the gate: ${e.message}`;
    emitOutputs({ proceed: false, relation: 'unevaluable', notify: false, reason });
    console.log(`::error::web-ota gate: ${reason}`);
    process.exit(1);
  }

  const lines = [
    `### Web OTA gate (${stage})`,
    '',
    `- channel: \`${channel}\`${namespace ? ` namespace: \`${namespace}\`` : ''}${allowRollback ? ' (allow_rollback)' : ''}`,
    `- brands: \`${brands.join(', ')}\``,
    `- publishing: \`${publishCommit.slice(0, 12)}\``,
  ];
  for (const [brand, b] of Object.entries(result.perBrand)) {
    lines.push(
      `- live (${brand}): ${b.commit ? `\`${b.commit.slice(0, 12)}\`` : '_none recorded_'}` +
        `${b.version ? ` (v${b.version})` : ''} → \`${b.relation}\`` +
        `${b.bootstrap ? ` (sibling manifest ${b.bootstrap === 'expected' ? 'present' : 'ALSO MISSING'})` : ''}`,
    );
  }
  lines.push('', `**${result.action.toUpperCase()}** — ${result.reason}`);
  const summary = lines.join('\n');
  console.log(summary);

  gha('GITHUB_STEP_SUMMARY', `${summary}\n\n`);
  emitOutputs({
    proceed: result.action === 'publish',
    relation: result.relation,
    notify: result.notify,
    reason: result.reason,
  });

  if (result.suspicious?.length) {
    console.log(
      `::warning::web-ota gate: no provenance AND no live manifest under ` +
        `s3://${bucket}/{${result.suspicious.join(',')}}/web/ — that is expected only for a brand ` +
        `that has never published. If it is not, this gate is reading the wrong prefix and is ` +
        `protecting nothing; the upload step uses the same bucket literal, so check the run's ` +
        `S3 paths before trusting this publish.`,
    );
  }
  if (result.level === 'error') console.log(`::error::web-ota gate: ${result.reason}`);
  else if (result.level === 'warning') console.log(`::warning::web-ota gate: ${result.reason}`);

  process.exit(result.action === 'refuse' ? 1 : 0);
}

if (process.argv[1] && process.argv[1].endsWith('web-ota-gate.mjs')) main(process.argv.slice(2));
