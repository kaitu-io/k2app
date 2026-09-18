// Tests for the Web OTA anti-rollback gate (scripts/ci/web-ota-gate.mjs).
//
// Three layers, because the gate's failure modes live in three different
// places:
//   1. the POLICY is a pure function and is asserted exhaustively as a table —
//      every relation × allow_rollback. Changing the policy has to mean editing
//      this table on purpose.
//   2. ANCESTRY runs against a real throwaway git repo, not a mocked git. A
//      mocked `merge-base --is-ancestor` would just re-state the assumption
//      being tested.
//   3. PROVENANCE READS use an injected exec so the fail-closed split (404 vs
//      any other failure) can be driven, including the shapes that must NOT
//      read as "clean": credential errors, truncated bodies, a body with no
//      usable commit.
//
// The gate has ONE mode now: every publish is a human explicitly asking for one
// (a `webapp/*` tag or a workflow_dispatch). There is no app-release linkage —
// so an unsafe relation REFUSES (loud red) unless allow_rollback says otherwise,
// and a gate that cannot be evaluated at all fails closed. (The linkage "skip
// instead of fail an app release" branch was removed with the v* linkage.)
//
// Run: node --test scripts/ci/web-ota-gate.test.mjs

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import {
  RELATIONS,
  ProvenanceUnreadable,
  classifyRelation,
  combineRelations,
  decide,
  evaluate,
  manifestKey,
  probeSiblingManifest,
  provenanceKey,
  readProvenance,
} from './web-ota-gate.mjs';

// ---------------------------------------------------------------- policy ---

// action per (relation, allowRollback), as [allowRollback=false, =true].
// Exhaustive on purpose.
const POLICY = {
  'not-enforced': ['publish', 'publish'],
  absent: ['publish', 'publish'],
  identical: ['publish', 'publish'], // an explicit publish always re-delivers
  forward: ['publish', 'publish'],
  downgrade: ['refuse', 'publish'],
  diverged: ['refuse', 'publish'],
  unresolvable: ['refuse', 'publish'],
};

test('policy table is exhaustive over RELATIONS × allow_rollback', () => {
  assert.deepEqual(Object.keys(POLICY).sort(), [...RELATIONS].sort());
  for (const relation of RELATIONS) {
    for (const [i, allowRollback] of [false, true].entries()) {
      const got = decide({ relation, allowRollback });
      assert.equal(got.action, POLICY[relation][i], `${relation} / allowRollback=${allowRollback}`);
      assert.ok(got.reason.length > 20, 'every decision must carry an actionable reason');
    }
  }
});

test('an unsafe relation refuses loudly unless allow_rollback is set', () => {
  for (const relation of ['downgrade', 'diverged', 'unresolvable']) {
    assert.equal(decide({ relation }).action, 'refuse', relation);
    assert.equal(decide({ relation, allowRollback: true }).action, 'publish', relation);
  }
});

test('the gate never silently does nothing — no decision is a skip', () => {
  // With the linkage removed there is no "skip an app release" outcome. Every
  // publish either happens or is refused; nothing is a quiet no-op.
  for (const relation of RELATIONS) {
    for (const allowRollback of [false, true]) {
      assert.notEqual(decide({ relation, allowRollback }).action, 'skip', `${relation}/${allowRollback}`);
    }
  }
});

test('only a deliberate backwards publish notifies a human; routine decisions do not', () => {
  assert.equal(decide({ relation: 'downgrade', allowRollback: true }).notify, true);
  assert.equal(decide({ relation: 'diverged', allowRollback: true }).notify, true);
  assert.equal(decide({ relation: 'unresolvable', allowRollback: true }).notify, true);
  // A refusal is already loud (red run) — it does not need a second page.
  assert.equal(decide({ relation: 'downgrade' }).notify, false);
  assert.equal(decide({ relation: 'forward' }).notify, false);
  assert.equal(decide({ relation: 'identical' }).notify, false);
  assert.equal(decide({ relation: 'absent' }).notify, false);
});

test('bootstrap (no provenance) passes but is never silent', () => {
  const d = decide({ relation: 'absent' });
  assert.equal(d.action, 'publish');
  assert.equal(d.level, 'warning');
});

test('decide rejects unknown relations instead of defaulting to publish', () => {
  assert.throws(() => decide({ relation: 'sideways' }), /unknown relation/);
});

// ----------------------------------------------------------- combination ---

test('combineRelations takes the worst relation across brands', () => {
  assert.equal(combineRelations(['identical', 'identical']), 'identical');
  assert.equal(combineRelations(['identical', 'forward']), 'forward');
  assert.equal(combineRelations(['forward', 'forward']), 'forward');
  assert.equal(combineRelations(['forward', 'absent']), 'absent');
  assert.equal(combineRelations(['forward', 'downgrade']), 'downgrade');
  assert.equal(combineRelations(['downgrade', 'diverged']), 'diverged');
  assert.equal(combineRelations(['diverged', 'unresolvable']), 'unresolvable');
  // A brand skew (one brand identical, the other behind) must NOT dedup away:
  // that state comes from a half-failed publish and needs the forward publish.
  assert.equal(combineRelations(['identical', 'downgrade']), 'downgrade');
});

test('combineRelations refuses garbage rather than guessing', () => {
  assert.throws(() => combineRelations([]), /at least one/);
  assert.throws(() => combineRelations(['forward', 'nonsense']), /unknown relation/);
});

// -------------------------------------------------------------- ancestry ---

function gitRepo() {
  const dir = mkdtempSync(path.join(tmpdir(), 'web-ota-gate-'));
  const git = (...args) => {
    const r = spawnSync('git', args, { cwd: dir, encoding: 'utf8' });
    return { status: r.status ?? 1, stdout: r.stdout ?? '', stderr: r.stderr ?? '' };
  };
  git('init', '-q', '-b', 'main');
  git('config', 'user.email', 't@example.com');
  git('config', 'user.name', 'T');
  const commit = (msg) => {
    spawnSync('git', ['commit', '-q', '--allow-empty', '-m', msg], { cwd: dir, encoding: 'utf8' });
    return git('rev-parse', 'HEAD').stdout.trim();
  };
  return { dir, git, commit, exec: (cmd, args) => (cmd === 'git' ? git(...args) : { status: 127, stdout: '', stderr: 'unexpected cmd' }) };
}

test('classifyRelation against a real repo: identical / forward / downgrade / diverged', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const newer = repo.commit('newer');
    assert.equal(classifyRelation(newer, newer, repo), 'identical');
    assert.equal(classifyRelation(base, newer, repo), 'forward');
    assert.equal(classifyRelation(newer, base, repo), 'downgrade');

    repo.git('checkout', '-q', '-b', 'side', base);
    const side = repo.commit('side');
    assert.equal(classifyRelation(newer, side, repo), 'diverged');
    assert.equal(classifyRelation(side, newer, repo), 'diverged');
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('classifyRelation: absent live commit, and a live commit this clone lacks', () => {
  const repo = gitRepo();
  try {
    const head = repo.commit('base');
    assert.equal(classifyRelation(null, head, repo), 'absent');
    const ghost = 'a'.repeat(40);
    assert.equal(classifyRelation(ghost, head, repo), 'unresolvable');
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('classifyRelation throws when the PUBLISH commit is missing — a broken checkout is not a relation', () => {
  const repo = gitRepo();
  try {
    const head = repo.commit('base');
    assert.throws(() => classifyRelation(head, 'b'.repeat(40), repo), /not in this clone/);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

// ------------------------------------------------------------ provenance ---

const SHA_A = '1'.repeat(40);

function awsExec({ status, stdout = '', stderr = '' }) {
  return (cmd, args) => {
    assert.equal(cmd, 'aws');
    assert.deepEqual(args.slice(0, 2), ['s3', 'cp']);
    return { status, stdout, stderr };
  };
}

test('provenance key sits next to the manifest it describes', () => {
  assert.equal(provenanceKey('kaitu'), 'kaitu/web/latest-source.json');
  assert.equal(provenanceKey('overleap'), 'overleap/web/latest-source.json');
});

test('readProvenance: happy path', () => {
  const exec = awsExec({ status: 0, stdout: JSON.stringify({ commit: SHA_A, version: '0.4.10.1' }) });
  assert.deepEqual(readProvenance('kaitu', { bucket: 'b', exec }), {
    commit: SHA_A,
    version: '0.4.10.1',
  });
});

test('readProvenance: a genuine 404 is the ONLY tolerated failure', () => {
  for (const stderr of [
    'An error occurred (404) when calling the HeadObject operation: Not Found',
    'fatal error: An error occurred (NoSuchKey) when calling the GetObject operation',
    'An error occurred (404) ... Key "kaitu/web/latest-source.json" does not exist',
  ]) {
    assert.equal(readProvenance('kaitu', { bucket: 'b', exec: awsExec({ status: 1, stderr }) }), null, stderr);
  }
});

test('readProvenance is fail-closed on everything else', () => {
  const cases = [
    { status: 1, stderr: 'An error occurred (AccessDenied) when calling the GetObject operation' },
    { status: 1, stderr: 'Could not connect to the endpoint URL' },
    { status: 255, stderr: 'Unable to locate credentials' },
    { status: 127, stderr: 'spawnSync aws ENOENT' },
  ];
  for (const c of cases) {
    assert.throws(
      () => readProvenance('kaitu', { bucket: 'b', exec: awsExec(c) }),
      ProvenanceUnreadable,
      c.stderr,
    );
  }
});

test('readProvenance rejects a body that is not usable provenance', () => {
  for (const stdout of ['', '<html>403</html>', '{"commit":null}', '{"commit":"abc"}', '{"commit":"' + 'Z'.repeat(40) + '"}']) {
    assert.throws(
      () => readProvenance('kaitu', { bucket: 'b', exec: awsExec({ status: 0, stdout }) }),
      ProvenanceUnreadable,
      JSON.stringify(stdout),
    );
  }
});

// -------------------------------------------------------------- evaluate ---

test('evaluate: beta and namespaced publishes are out of scope', () => {
  const boom = () => {
    throw new Error('must not touch S3 or git');
  };
  for (const opts of [
    { channel: 'beta', namespace: '' },
    { channel: 'stable', namespace: 'uat' },
  ]) {
    const r = evaluate({
      brands: ['kaitu'],
      bucket: 'b',
      publishCommit: SHA_A,
      allowRollback: false,
      exec: boom,
      ...opts,
    });
    assert.equal(r.relation, 'not-enforced');
    assert.equal(r.action, 'publish');
  }
});

test('evaluate: end to end over both brands against a real repo', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const exec = (cmd, args) => {
      if (cmd === 'git') return repo.exec(cmd, args);
      // both brands' provenance points at the older commit
      return { status: 0, stdout: JSON.stringify({ commit: base, version: '0.4.10.1' }) };
    };
    const fwd = evaluate({
      brands: ['kaitu', 'overleap'],
      bucket: 'b',
      publishCommit: head,
      allowRollback: false,
      channel: 'stable',
      namespace: '',
      exec,
    });
    assert.equal(fwd.relation, 'forward');
    assert.equal(fwd.action, 'publish');
    assert.equal(fwd.perBrand.overleap.commit, base);

    // The incident shape: live is NEWER than the ref being published. An
    // explicit publish REFUSES it (loud red) rather than moving stable back.
    const back = evaluate({
      brands: ['kaitu', 'overleap'],
      bucket: 'b',
      publishCommit: base,
      allowRollback: false,
      channel: 'stable',
      namespace: '',
      exec: (cmd, args) =>
        cmd === 'git' ? repo.exec(cmd, args) : { status: 0, stdout: JSON.stringify({ commit: head }) },
    });
    assert.equal(back.relation, 'downgrade');
    assert.equal(back.action, 'refuse');
    assert.equal(back.notify, false);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('evaluate: per-brand — publishing ONE brand only reads that brand', () => {
  // Independent brand control means a single-brand tag must not read (or be
  // gated by) the other brand's provenance.
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const seen = [];
    const exec = (cmd, args) => {
      if (cmd === 'git') return repo.exec(cmd, args);
      seen.push(args[2]); // the s3:// uri
      return { status: 0, stdout: JSON.stringify({ commit: base }) };
    };
    const r = evaluate({
      brands: ['overleap'],
      bucket: 'b',
      publishCommit: head,
      allowRollback: false,
      channel: 'stable',
      namespace: '',
      exec,
    });
    assert.equal(r.relation, 'forward');
    assert.deepEqual(Object.keys(r.perBrand), ['overleap']);
    assert.ok(seen.every((uri) => uri.includes('/overleap/')), `only overleap reads: ${seen}`);
    assert.ok(!seen.some((uri) => uri.includes('/kaitu/')), `kaitu must not be read: ${seen}`);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('evaluate: an unreadable brand aborts the whole evaluation (no partial pass)', () => {
  assert.throws(
    () =>
      evaluate({
        brands: ['kaitu', 'overleap'],
        bucket: 'b',
        publishCommit: SHA_A,
        allowRollback: true,
        channel: 'stable',
        namespace: '',
        exec: awsExec({ status: 1, stderr: 'Unable to locate credentials' }),
      }),
    ProvenanceUnreadable,
  );
});

// ------------------------------------------- bootstrap vs. reading nothing ---
//
// `aws s3 cp` reports a missing BUCKET with the same stderr as a missing KEY, so
// "provenance 404" is ambiguous on its own. These tests pin the disambiguation:
// the sibling manifest that every client polls.

/** aws stub keyed by S3 URI, so the two probes can answer differently. */
function keyedAws(byUri) {
  return (cmd, args) => {
    assert.equal(cmd, 'aws');
    const uri = args[2];
    const r = byUri[uri];
    if (!r) throw new Error(`test stub has no answer for ${uri}`);
    return { status: r.status, stdout: r.stdout ?? '', stderr: r.stderr ?? '' };
  };
}

const NOT_FOUND = {
  status: 1,
  // Verbatim aws-cli 2.34 text, captured from the real bucket — the whole point
  // of this fixture is that it is identical for a missing key and a missing bucket.
  stderr:
    'download failed: s3://d0.all7.cc/kaitu/web/latest-source.json to - An error occurred (404) when calling the HeadObject operation: Not Found',
};

test('manifest key sits next to the provenance key', () => {
  assert.equal(manifestKey('kaitu'), 'kaitu/web/latest.json');
  assert.equal(path.dirname(manifestKey('kaitu')), path.dirname(provenanceKey('kaitu')));
});

test('probeSiblingManifest: present / absent / unreadable', () => {
  const uri = 's3://b/kaitu/web/latest.json';
  assert.equal(probeSiblingManifest('kaitu', { bucket: 'b', exec: keyedAws({ [uri]: { status: 0, stdout: '{}' } }) }), true);
  assert.equal(probeSiblingManifest('kaitu', { bucket: 'b', exec: keyedAws({ [uri]: NOT_FOUND }) }), false);
  assert.throws(
    () => probeSiblingManifest('kaitu', { bucket: 'b', exec: keyedAws({ [uri]: { status: 1, stderr: 'Unable to locate credentials' } }) }),
    ProvenanceUnreadable,
  );
});

test('evaluate: provenance absent but manifest present = the expected bootstrap', () => {
  const repo = gitRepo();
  try {
    const head = repo.commit('base');
    const exec = (cmd, args) =>
      cmd === 'git'
        ? repo.exec(cmd, args)
        : keyedAws({
            's3://b/kaitu/web/latest-source.json': NOT_FOUND,
            's3://b/kaitu/web/latest.json': { status: 0, stdout: '{"version":"0.4.10.21546118"}' },
          })(cmd, args);
    const r = evaluate({
      brands: ['kaitu'], bucket: 'b', publishCommit: head,
      allowRollback: false, channel: 'stable', namespace: '', exec,
    });
    assert.equal(r.relation, 'absent');
    assert.equal(r.action, 'publish');
    assert.equal(r.perBrand.kaitu.bootstrap, 'expected');
    assert.deepEqual(r.suspicious, []);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('evaluate: NEITHER provenance nor manifest readable is flagged, not quietly passed', () => {
  const repo = gitRepo();
  try {
    const head = repo.commit('base');
    const exec = (cmd, args) =>
      cmd === 'git'
        ? repo.exec(cmd, args)
        : keyedAws({
            's3://b/kaitu/web/latest-source.json': NOT_FOUND,
            's3://b/kaitu/web/latest.json': NOT_FOUND,
          })(cmd, args);
    const r = evaluate({
      brands: ['kaitu'], bucket: 'b', publishCommit: head,
      allowRollback: false, channel: 'stable', namespace: '', exec,
    });
    // Still allowed — a brand that has never published is legitimate — but the
    // caller gets told, so "absent" can never be mistaken for "verified clean".
    assert.equal(r.action, 'publish');
    assert.equal(r.perBrand.kaitu.bootstrap, 'suspicious');
    assert.deepEqual(r.suspicious, ['kaitu']);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('evaluate: a recorded provenance never triggers the sibling probe', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const exec = (cmd, args) =>
      cmd === 'git'
        ? repo.exec(cmd, args)
        : keyedAws({
            's3://b/kaitu/web/latest-source.json': { status: 0, stdout: JSON.stringify({ commit: base }) },
            // no answer registered for the manifest: the stub throws if probed
          })(cmd, args);
    const r = evaluate({
      brands: ['kaitu'], bucket: 'b', publishCommit: head,
      allowRollback: false, channel: 'stable', namespace: '', exec,
    });
    assert.equal(r.relation, 'forward');
    assert.equal(r.perBrand.kaitu.bootstrap, null);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

// ------------------------------------------------------------------- CLI ----
//
// The CLI layer is where a refusal becomes an EXIT CODE (1 = red run). The
// decision-table tests above cannot see it, so these drive the real process
// with a stubbed `aws` on PATH.

const GATE = path.join(path.dirname(fileURLToPath(import.meta.url)), 'web-ota-gate.mjs');

/** Run the gate CLI with a stub `aws` first on PATH. Returns {status, out}. */
function runCli({ awsScript, args, cwd }) {
  const bin = mkdtempSync(path.join(tmpdir(), 'gate-bin-'));
  const outFile = path.join(bin, 'gh-output');
  writeFileSync(path.join(bin, 'aws'), awsScript);
  chmodSync(path.join(bin, 'aws'), 0o755);
  writeFileSync(outFile, '');
  const r = spawnSync('node', [GATE, ...args], {
    encoding: 'utf8',
    cwd: cwd ?? process.cwd(),
    env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, GITHUB_OUTPUT: outFile },
  });
  const out = Object.fromEntries(
    readFileSync(outFile, 'utf8').split('\n').filter(Boolean).map((l) => {
      const i = l.indexOf('=');
      return [l.slice(0, i), l.slice(i + 1)];
    }),
  );
  rmSync(bin, { recursive: true, force: true });
  return { status: r.status, stdout: r.stdout ?? '', out };
}

const AWS_NO_CREDS = '#!/bin/bash\necho "Unable to locate credentials" >&2\nexit 255\n';
const SOME_SHA = 'd'.repeat(40);

test('CLI: a publish that cannot evaluate the gate fails closed (RED)', () => {
  const r = runCli({
    awsScript: AWS_NO_CREDS,
    args: ['--publish-commit', SOME_SHA, '--channel', 'stable', '--namespace', ''],
  });
  assert.equal(r.status, 1);
  assert.equal(r.out.proceed, 'false');
  assert.equal(r.out.relation, 'unevaluable');
  assert.match(r.stdout, /::error::/);
});

test('CLI: allow_rollback does NOT override blindness', () => {
  const r = runCli({
    awsScript: AWS_NO_CREDS,
    args: ['--publish-commit', SOME_SHA, '--allow-rollback', 'true', '--channel', 'stable', '--namespace', ''],
  });
  assert.equal(r.status, 1, 'allow_rollback is intent about content, not permission to publish blind');
});

test('CLI: a short/absent publish commit is refused before anything else happens', () => {
  for (const sha of ['', 'abc123']) {
    const r = runCli({ awsScript: AWS_NO_CREDS, args: ['--publish-commit', sha] });
    assert.equal(r.status, 1, JSON.stringify(sha));
  }
});

test('CLI: identical provenance re-delivers (an explicit publish is never a no-op)', () => {
  const repo = gitRepo();
  try {
    const head = repo.commit('base');
    const r = runCli({
      awsScript: `#!/bin/bash\necho '{"commit":"${head}","version":"0.4.10.1"}'\n`,
      args: ['--publish-commit', head, '--channel', 'stable', '--namespace', ''],
      cwd: repo.dir,
    });
    assert.equal(r.status, 0);
    assert.equal(r.out.proceed, 'true', 'explicit republish, not a dedup skip');
    assert.equal(r.out.relation, 'identical');
    assert.equal(r.out.notify, 'false', 'a routine republish must not page anyone');
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('CLI: --brands accepts a single brand and gates only it', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const r = runCli({
      // Any provenance read returns the older commit → forward roll for overleap.
      awsScript: `#!/bin/bash\necho '{"commit":"${base}"}'\n`,
      args: ['--brands', 'overleap', '--publish-commit', head, '--channel', 'stable', '--namespace', ''],
      cwd: repo.dir,
    });
    assert.equal(r.status, 0);
    assert.equal(r.out.proceed, 'true');
    assert.equal(r.out.relation, 'forward');
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('CLI: a downgrade exits 1 and names the escape hatch', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const r = runCli({
      awsScript: `#!/bin/bash\necho '{"commit":"${head}","version":"0.4.10.9"}'\n`,
      args: ['--publish-commit', base, '--channel', 'stable', '--namespace', ''],
      cwd: repo.dir,
    });
    assert.equal(r.status, 1);
    assert.equal(r.out.relation, 'downgrade');
    assert.match(r.stdout, /allow_rollback/);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('CLI: a downgrade WITH allow_rollback publishes and pages a human', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const r = runCli({
      awsScript: `#!/bin/bash\necho '{"commit":"${head}","version":"0.4.10.9"}'\n`,
      args: ['--publish-commit', base, '--allow-rollback', 'true', '--channel', 'stable', '--namespace', ''],
      cwd: repo.dir,
    });
    assert.equal(r.status, 0);
    assert.equal(r.out.proceed, 'true');
    assert.equal(r.out.relation, 'downgrade');
    assert.equal(r.out.notify, 'true', 'a deliberate backwards publish must be visible');
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});
