#!/usr/bin/env node

const fs = require('fs');
const path = require('path');

/**
 * Generate CHANGELOG.md from client/releases/ directory
 *
 * This script reads all version files from client/releases/ and generates
 * a single CHANGELOG.md file for the web dashboard.
 *
 * Usage:
 *   node scripts/generate-changelog.js
 *
 * Output:
 *   web/public/changelog.md
 */

function parseVersion(filename) {
  // Extract version from filename (e.g., "v0.3.18.md" -> [0, 3, 18])
  const match = filename.match(/^v(\d+)\.(\d+)\.(\d+)\.md$/);
  if (!match) return null;

  return {
    major: parseInt(match[1]),
    minor: parseInt(match[2]),
    patch: parseInt(match[3]),
    string: `${match[1]}.${match[2]}.${match[3]}`
  };
}

function compareVersions(a, b) {
  // Sort in descending order (newest first)
  if (a.major !== b.major) return b.major - a.major;
  if (a.minor !== b.minor) return b.minor - a.minor;
  return b.patch - a.patch;
}

function parseMarkdownSections(content) {
  // Parse markdown content into sections
  const sections = {
    newFeatures: [],
    bugFixes: [],
    improvements: [],
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
    } else if (trimmed.startsWith('## Breaking Changes')) {
      currentSection = 'breakingChanges';
    } else if (trimmed.startsWith('- ') && currentSection) {
      // Add bullet point to current section
      sections[currentSection].push(trimmed.substring(2));
    }
  }

  return sections;
}

function generateChangelog() {
  const releasesDir = path.join(__dirname, '../releases');
  const mdOutputPath = path.join(__dirname, '../web/public/changelog.md');
  const jsonOutputPath = path.join(__dirname, '../web/public/changelog.json');

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
    versions: []
  };

  // Process each version
  for (const { file, version } of versions) {
    const filePath = path.join(releasesDir, file);
    const content = fs.readFileSync(filePath, 'utf8');

    // Extract date from frontmatter
    const dateMatch = content.match(/date:\s*(\d{4}-\d{2}-\d{2})/);
    const date = dateMatch ? dateMatch[1] : 'Unknown';

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

    // Add to JSON
    jsonData.versions.push({
      version: version.string,
      date: date,
      content: withoutFrontmatter.trim(),
      sections: sections
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

  // Write JSON file
  fs.writeFileSync(jsonOutputPath, JSON.stringify(jsonData, null, 2), 'utf8');

  console.log(`\n==> ✅ Generated changelog with ${versions.length} versions`);
  console.log(`   Markdown: ${mdOutputPath}`);
  console.log(`   JSON: ${jsonOutputPath}`);
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
