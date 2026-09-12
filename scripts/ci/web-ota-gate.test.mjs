// Tests for the Web OTA anti-rollback gate (scripts/ci/web-ota-gate.mjs).
//
// Three layers, because the gate's failure modes live in three different
// places:
//   1. the POLICY is a pure function and is asserted exhaustively as a table —
//      every relation × mode × allow_rollback. Changing the policy has to mean
//      editing this table on purpose.
//   2. ANCESTRY runs against a real throwaway git repo, not a mocked git. A
//      mocked `merge-base --is-ancestor` would just re-state the assumption
//      being tested.
//   3. PROVENANCE READS use an injected exec so the fail-closed split (404 vs
//      any other failure) can be driven, including the shapes that must NOT
//      read as "clean": credential errors, truncated bodies, a body with no
//      usable commit.
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
  MODES,
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

// action per (relation, mode, allowRollback). Exhaustive on purpose.
const POLICY = {
  'not-enforced': { linkage: ['publish', 'publish'], explicit: ['publish', 'publish'] },
  absent: { linkage: ['publish', 'publish'], explicit: ['publish', 'publish'] },
  identical: { linkage: ['skip', 'skip'], explicit: ['publish', 'publish'] },
  forward: { linkage: ['publish', 'publish'], explicit: ['publish', 'publish'] },
  // [allowRollback=false, allowRollback=true]
  downgrade: { linkage: ['skip', 'skip'], explicit: ['refuse', 'publish'] },
  diverged: { linkage: ['skip', 'skip'], explicit: ['refuse', 'publish'] },
  unresolvable: { linkage: ['skip', 'skip'], explicit: ['refuse', 'publish'] },
};

test('policy table is exhaustive over RELATIONS × MODES × allow_rollback', () => {
  assert.deepEqual(Object.keys(POLICY).sort(), [...RELATIONS].sort());
  for (const relation of RELATIONS) {
    for (const mode of MODES) {
      for (const [i, allowRollback] of [false, true].entries()) {
        const got = decide({ relation, mode, allowRollback });
        assert.equal(
          got.action,
          POLICY[relation][mode][i],
          `${relation} / ${mode} / allowRollback=${allowRollback}`,
        );
        assert.ok(got.reason.length > 20, 'every decision must carry an actionable reason');
      }
    }
  }
});

test('a linkage publish never refuses — an app release is never failed over a web concern', () => {
  for (const relation of RELATIONS) {
    assert.notEqual(decide({ relation, mode: 'linkage' }).action, 'refuse', relation);
  }
});

test('an explicit publish never silently does nothing on an unsafe relation', () => {
  for (const relation of ['downgrade', 'diverged', 'unresolvable']) {
    assert.equal(decide({ relation, mode: 'explicit' }).action, 'refuse', relation);
  }
});

test('every unsafe skip/override notifies a human; routine decisions do not', () => {
  assert.equal(decide({ relation: 'downgrade', mode: 'linkage' }).notify, true);
  assert.equal(decide({ relation: 'diverged', mode: 'linkage' }).notify, true);
  assert.equal(decide({ relation: 'unresolvable', mode: 'linkage' }).notify, true);
  assert.equal(decide({ relation: 'downgrade', mode: 'explicit', allowRollback: true }).notify, true);
  assert.equal(decide({ relation: 'identical', mode: 'linkage' }).notify, false);
  assert.equal(decide({ relation: 'forward', mode: 'linkage' }).notify, false);
  assert.equal(decide({ relation: 'absent', mode: 'explicit' }).notify, false);
});

test('bootstrap (no provenance) passes but is never silent', () => {
  const d = decide({ relation: 'absent', mode: 'linkage' });
  assert.equal(d.action, 'publish');
  assert.equal(d.level, 'warning');
});

test('decide rejects unknown relations and modes instead of defaulting to publish', () => {
  assert.throws(() => decide({ relation: 'sideways', mode: 'linkage' }), /unknown relation/);
  assert.throws(() => decide({ relation: 'forward', mode: 'yolo' }), /unknown mode/);
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
      mode: 'linkage',
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
      mode: 'linkage',
      allowRollback: false,
      channel: 'stable',
      namespace: '',
      exec,
    });
    assert.equal(fwd.relation, 'forward');
    assert.equal(fwd.action, 'publish');
    assert.equal(fwd.perBrand.overleap.commit, base);

    // The incident shape: live is NEWER than the ref being published.
    const back = evaluate({
      brands: ['kaitu', 'overleap'],
      bucket: 'b',
      publishCommit: base,
      mode: 'linkage',
      allowRollback: false,
      channel: 'stable',
      namespace: '',
      exec: (cmd, args) =>
        cmd === 'git' ? repo.exec(cmd, args) : { status: 0, stdout: JSON.stringify({ commit: head }) },
    });
    assert.equal(back.relation, 'downgrade');
    assert.equal(back.action, 'skip');
    assert.equal(back.notify, true);
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
        mode: 'explicit',
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
      brands: ['kaitu'], bucket: 'b', publishCommit: head, mode: 'linkage',
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
      brands: ['kaitu'], bucket: 'b', publishCommit: head, mode: 'explicit',
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
      brands: ['kaitu'], bucket: 'b', publishCommit: head, mode: 'linkage',
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
// The CLI layer is where the mode asymmetry becomes an EXIT CODE, and an exit
// code is what decides whether an app release goes red. The decision table
// tests above cannot see it, so these drive the real process with a stubbed
// `aws` on PATH.

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

test('CLI: a linkage publish that cannot evaluate the gate SKIPS instead of failing the app release', () => {
  const r = runCli({
    awsScript: AWS_NO_CREDS,
    args: ['--publish-commit', SOME_SHA, '--mode', 'linkage', '--channel', 'stable', '--namespace', ''],
  });
  assert.equal(r.status, 0, 'must not fail the calling release');
  assert.equal(r.out.proceed, 'false', 'must not publish blind');
  assert.equal(r.out.relation, 'unevaluable');
  assert.equal(r.out.notify, 'true', 'a silent skip would hide an S3 outage');
  assert.match(r.stdout, /::warning::/);
});

test('CLI: an explicit publish that cannot evaluate the gate goes RED', () => {
  const r = runCli({
    awsScript: AWS_NO_CREDS,
    args: ['--publish-commit', SOME_SHA, '--mode', 'explicit', '--channel', 'stable', '--namespace', ''],
  });
  assert.equal(r.status, 1);
  assert.equal(r.out.proceed, 'false');
  assert.match(r.stdout, /::error::/);
});

test('CLI: allow_rollback does NOT override blindness', () => {
  const r = runCli({
    awsScript: AWS_NO_CREDS,
    args: ['--publish-commit', SOME_SHA, '--mode', 'explicit', '--allow-rollback', 'true', '--channel', 'stable', '--namespace', ''],
  });
  assert.equal(r.status, 1, 'allow_rollback is intent about content, not permission to publish blind');
});

test('CLI: a short/absent publish commit is refused before anything else happens', () => {
  for (const sha of ['', 'abc123']) {
    const r = runCli({ awsScript: AWS_NO_CREDS, args: ['--publish-commit', sha, '--mode', 'linkage'] });
    assert.equal(r.status, 1, JSON.stringify(sha));
  }
});

test('CLI: identical provenance makes a linkage publish a clean no-op (the bare v* dedup)', () => {
  const repo = gitRepo();
  try {
    const head = repo.commit('base');
    const r = runCli({
      awsScript: `#!/bin/bash\necho '{"commit":"${head}","version":"0.4.10.1"}'\n`,
      args: ['--publish-commit', head, '--mode', 'linkage', '--channel', 'stable', '--namespace', ''],
      cwd: repo.dir,
    });
    assert.equal(r.status, 0);
    assert.equal(r.out.proceed, 'false');
    assert.equal(r.out.relation, 'identical');
    assert.equal(r.out.notify, 'false', 'a routine dedup must not page anyone');
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});

test('CLI: a downgrade on the explicit path exits 1 and names the escape hatch', () => {
  const repo = gitRepo();
  try {
    const base = repo.commit('base');
    const head = repo.commit('head');
    const r = runCli({
      awsScript: `#!/bin/bash\necho '{"commit":"${head}","version":"0.4.10.9"}'\n`,
      args: ['--publish-commit', base, '--mode', 'explicit', '--channel', 'stable', '--namespace', ''],
      cwd: repo.dir,
    });
    assert.equal(r.status, 1);
    assert.equal(r.out.relation, 'downgrade');
    assert.match(r.stdout, /allow_rollback/);
  } finally {
    rmSync(repo.dir, { recursive: true, force: true });
  }
});
