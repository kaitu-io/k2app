#!/usr/bin/env node

const fs = require('fs');
const path = require('path');

/**
 * Generate releases data from releases/ directory
 *
 * This script reads all version files from releases/ and generates:
 *   - web/public/releases.json  (primary: includes downloads + channel info)
 *   - web/public/changelog.json (backward compat copy)
 *   - web/public/changelog.md   (concatenated markdown)
 *
 * Usage:
 *   node web/scripts/generate-changelog.js  (from repo root)
 *   node scripts/generate-changelog.js     (from web/)
 */

const CDN_PRIMARY_BASE = 'https://dl.kaitu.io/kaitu';
const CDN_BACKUP_BASE = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu';
const CDN_PRIMARY = `${CDN_PRIMARY_BASE}/desktop`;
const CDN_BACKUP = `${CDN_BACKUP_BASE}/desktop`;
const CDN_PRIMARY_ANDROID = `${CDN_PRIMARY_BASE}/android`;
const CDN_BACKUP_ANDROID = `${CDN_BACKUP_BASE}/android`;
// iOS is App Store only — version-independent.
const IOS_APPSTORE_URL = 'https://apps.apple.com/app/id6448744655';

// Per-platform first version that has a published artifact on the CDN. Platforms
// added later (Linux tarball, Android APK) start showing later than Windows/macOS.
const MIN_VERSION = {
  desktop: { major: 0, minor: 3, patch: 22 },
  linux: { major: 0, minor: 4, patch: 3 },
  android: { major: 0, minor: 4, patch: 1 },
  ios: { major: 0, minor: 4, patch: 1 },
};

function parseVersion(filename) {
  // Extract version from filename (e.g., "v0.3.18.md" or "v0.4.0-beta.1.md")
  const match = filename.match(/^v(\d+)\.(\d+)\.(\d+)(?:-([a-z0-9.]+))?\.md$/);
  if (!match) return null;

  return {
    major: parseInt(match[1]),
    minor: parseInt(match[2]),
    patch: parseInt(match[3]),
    prerelease: match[4] || null,
    string: match[4]
      ? `${match[1]}.${match[2]}.${match[3]}-${match[4]}`
      : `${match[1]}.${match[2]}.${match[3]}`
  };
}

function compareVersions(a, b) {
  // Sort in descending order (newest first)
  if (a.major !== b.major) return b.major - a.major;
  if (a.minor !== b.minor) return b.minor - a.minor;
  if (a.patch !== b.patch) return b.patch - a.patch;
  // Pre-release versions sort before their release (0.4.0-beta.1 < 0.4.0)
  if (a.prerelease && !b.prerelease) return 1;
  if (!a.prerelease && b.prerelease) return -1;
  if (a.prerelease && b.prerelease) return b.prerelease.localeCompare(a.prerelease);
  return 0;
}

function parseMarkdownSections(content) {
  // Parse markdown content into sections
  const sections = {
    newFeatures: [],
    bugFixes: [],
    improvements: [],
    infrastructure: [],
    breakingChanges: []
  };

  const lines = content.split('\n');
  let currentSection = null;

  for (const line of lines) {
    const trimmed = line.trim();

    // Detect section headers
    if (trimmed.startsWith('## New Features')) {
      currentSection = 'newFeatures';
    } else if (trimmed.startsWith('## Bug Fixes')) {
      currentSection = 'bugFixes';
    } else if (trimmed.startsWith('## Improvements')) {
      currentSection = 'improvements';
    } else if (trimmed.startsWith('## Infrastructure')) {
      currentSection = 'infrastructure';
    } else if (trimmed.startsWith('## Breaking Changes')) {
      currentSection = 'breakingChanges';
    } else if (trimmed.startsWith('- ') && currentSection) {
      // Add bullet point to current section
      sections[currentSection].push(trimmed.substring(2));
    }
  }

  return sections;
}

function isVersionGte(v, min) {
  if (v.major !== min.major) return v.major > min.major;
  if (v.minor !== min.minor) return v.minor > min.minor;
  return v.patch >= min.patch;
}

function generateChangelog() {
  const releasesDir = path.join(__dirname, '../releases');
  const mdOutputPath = path.join(__dirname, '../public/changelog.md');
  const releasesOutputPath = path.join(__dirname, '../public/releases.json');
  const changelogOutputPath = path.join(__dirname, '../public/changelog.json');

  console.log('==> 📝 Generating CHANGELOG from releases/');

  // Check if releases directory exists
  if (!fs.existsSync(releasesDir)) {
    console.error('❌ Error: client/releases/ directory not found');
    process.exit(1);
  }

  // Read all release files
  const files = fs.readdirSync(releasesDir)
    .filter(f => f.startsWith('v') && f.endsWith('.md'));

  if (files.length === 0) {
    console.warn('⚠️  Warning: No release files found in client/releases/');
    process.exit(0);
  }

  // Parse and sort versions
  const versions = files
    .map(file => {
      const version = parseVersion(file);
      if (!version) {
        console.warn(`   ⚠️  Skipping invalid filename: ${file}`);
        return null;
      }
      return { file, version };
    })
    .filter(v => v !== null)
    .sort((a, b) => compareVersions(a.version, b.version));

  console.log(`   Found ${versions.length} valid release files`);

  // Generate CHANGELOG markdown
  let mdOutput = '# Changelog\n\n';
  mdOutput += 'All notable changes to Kaitu will be documented in this file.\n\n';
  mdOutput += '---\n\n';

  // Prepare JSON data
  const jsonData = {
    generated: new Date().toISOString(),
    latestBeta: null,
    latestStable: null,
    versions: []
  };

  // Process each version
  for (const { file, version } of versions) {
    const filePath = path.join(releasesDir, file);
    const content = fs.readFileSync(filePath, 'utf8');

    // Extract date from frontmatter
    const dateMatch = content.match(/date:\s*(\d{4}-\d{2}-\d{2})/);
    const date = dateMatch ? dateMatch[1] : 'Unknown';
    // Optional `noDownloads: true` flag for releases that don't ship app binaries
    // (e.g. router-only releases where the install one-liner is in the body).
    const noDownloads = /noDownloads:\s*true/i.test(content);

    // Strip YAML frontmatter
    let withoutFrontmatter = content;
    if (content.startsWith('---')) {
      withoutFrontmatter = content.replace(/^---[\s\S]*?---\n/, '');
    }

    // Add to markdown
    mdOutput += `## [${version.string}] - ${date}\n\n`;
    mdOutput += withoutFrontmatter.trim() + '\n\n';
    mdOutput += '---\n\n';

    // Parse sections for JSON
    const sections = parseMarkdownSections(withoutFrontmatter);

    // Determine channel and per-platform download availability
    const channel = version.prerelease ? 'beta' : 'stable';
    const hasDesktop = !noDownloads && isVersionGte(version, MIN_VERSION.desktop);
    const hasLinux = !noDownloads && isVersionGte(version, MIN_VERSION.linux);
    const hasAndroid = !noDownloads && isVersionGte(version, MIN_VERSION.android);
    const hasIOS = !noDownloads && isVersionGte(version, MIN_VERSION.ios);
    const hasDownloads = hasDesktop || hasLinux || hasAndroid || hasIOS;

    const downloads = {};
    if (hasDesktop) {
      downloads.windows = `${CDN_PRIMARY}/${version.string}/Kaitu_${version.string}_x64.exe`;
      downloads.windowsBackup = `${CDN_BACKUP}/${version.string}/Kaitu_${version.string}_x64.exe`;
      downloads.macos = `${CDN_PRIMARY}/${version.string}/Kaitu_${version.string}_universal.pkg`;
      downloads.macosBackup = `${CDN_BACKUP}/${version.string}/Kaitu_${version.string}_universal.pkg`;
    }
    if (hasLinux) {
      downloads.linux = `${CDN_PRIMARY}/${version.string}/Kaitu_${version.string}_linux_amd64.tar.gz`;
      downloads.linuxBackup = `${CDN_BACKUP}/${version.string}/Kaitu_${version.string}_linux_amd64.tar.gz`;
    }
    if (hasAndroid) {
      downloads.android = `${CDN_PRIMARY_ANDROID}/${version.string}/Kaitu-${version.string}.apk`;
      downloads.androidBackup = `${CDN_BACKUP_ANDROID}/${version.string}/Kaitu-${version.string}.apk`;
    }
    if (hasIOS) {
      downloads.ios = IOS_APPSTORE_URL;
    }

    // Track latest beta and stable
    if (channel === 'beta' && !jsonData.latestBeta) {
      jsonData.latestBeta = version.string;
    }
    if (channel === 'stable' && !jsonData.latestStable) {
      jsonData.latestStable = version.string;
    }

    // Add to JSON
    jsonData.versions.push({
      version: version.string,
      date: date,
      content: withoutFrontmatter.trim(),
      sections: sections,
      channel,
      hasDownloads,
      downloads,
    });

    console.log(`   ✅ Added v${version.string} (${date})`);
  }

  // Ensure output directory exists
  const outputDir = path.dirname(mdOutputPath);
  if (!fs.existsSync(outputDir)) {
    fs.mkdirSync(outputDir, { recursive: true });
  }

  // Write markdown file
  fs.writeFileSync(mdOutputPath, mdOutput, 'utf8');

  // Write releases.json (primary output with downloads)
  fs.writeFileSync(releasesOutputPath, JSON.stringify(jsonData, null, 2), 'utf8');

  // Write changelog.json (backward compat — same data)
  fs.writeFileSync(changelogOutputPath, JSON.stringify(jsonData, null, 2), 'utf8');

  console.log(`\n==> ✅ Generated releases data with ${versions.length} versions`);
  console.log(`   Latest beta: ${jsonData.latestBeta || 'none'}`);
  console.log(`   Latest stable: ${jsonData.latestStable || 'none'}`);
  console.log(`   Markdown: ${mdOutputPath}`);
  console.log(`   Releases JSON: ${releasesOutputPath}`);
  console.log(`   Changelog JSON: ${changelogOutputPath} (backward compat)`);
}

// Main execution
if (require.main === module) {
  try {
    generateChangelog();
  } catch (error) {
    console.error('\n==> ❌ Failed to generate changelog:', error.message);
    process.exit(1);
  }
}

module.exports = { generateChangelog };
