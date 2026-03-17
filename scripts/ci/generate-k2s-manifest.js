#!/usr/bin/env node
// Generate k2s latest.json manifest files from CHECKSUMS.txt.
//
// Usage:
//   node scripts/ci/generate-k2s-manifest.js <version> <checksums-file> <out-dir>
//
// Outputs:
//   <out-dir>/k2s-cloudfront.latest.json  (CloudFront URLs)
//   <out-dir>/k2s-d0.latest.json          (d0 direct URLs)

const fs = require('fs');
const path = require('path');

const CLOUDFRONT = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2';
const D0 = 'https://d0.all7.cc/kaitu/k2';
const PLATFORMS = ['linux-amd64', 'linux-arm64'];

const [version, checksumsFile, outDir] = process.argv.slice(2);

if (!version || !checksumsFile || !outDir) {
  console.error('Usage: generate-k2s-manifest.js <version> <checksums-file> <out-dir>');
  process.exit(1);
}

// Parse CHECKSUMS.txt: "hash  filename" per line
const checksums = {};
const lines = fs.readFileSync(checksumsFile, 'utf-8').trim().split('\n');
for (const line of lines) {
  const [hash, ...rest] = line.split(/\s+/);
  const filename = rest.join('').trim();
  if (hash && filename) {
    checksums[filename] = hash;
  }
}

function buildManifest(baseUrl) {
  const binaries = {};
  const cs = {};
  for (const platform of PLATFORMS) {
    const filename = `k2s-${platform}`;
    binaries[platform] = `${baseUrl}/${version}/${filename}`;
    const hash = checksums[filename];
    if (!hash) {
      console.error(`Missing checksum for ${filename} in ${checksumsFile}`);
      process.exit(1);
    }
    cs[platform] = `sha256:${hash}`;
  }
  return { version, binaries, checksums: cs };
}

fs.mkdirSync(outDir, { recursive: true });

const cfManifest = buildManifest(CLOUDFRONT);
const d0Manifest = buildManifest(D0);

const cfPath = path.join(outDir, 'k2s-cloudfront.latest.json');
const d0Path = path.join(outDir, 'k2s-d0.latest.json');

fs.writeFileSync(cfPath, JSON.stringify(cfManifest, null, 2) + '\n');
fs.writeFileSync(d0Path, JSON.stringify(d0Manifest, null, 2) + '\n');

console.log(`Generated ${cfPath}`);
console.log(`Generated ${d0Path}`);
console.log(JSON.stringify(cfManifest, null, 2));
