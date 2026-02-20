# Release Notes Format

This directory contains version-specific release notes for Kaitu.

## Single Source of Truth

All release notes are stored here and used for:
- **Desktop updater notifications** - `change-desktop-version.js` reads individual files
- **Web dashboard changelog** - `generate-changelog.js` creates JSON for list view
- **Client webapp changelog** - iframe loads markdown from web dashboard
- Future use cases (RSS, API, etc.)

## File Naming Convention

Each version should have its own file:

```
client/releases/
├── v0.3.20.md
├── v0.3.19.md
├── v0.3.18.md
└── ...
```

## File Format

Use this template for new releases:

```markdown
---
version: 0.3.20
date: 2026-01-27
---

## New Features
- **Feature name**: Brief description of what was added
- **Another feature**: Why this matters to users

## Bug Fixes
- Fixed issue with X that caused Y
- Resolved problem where Z would fail

## Improvements
- Enhanced performance of A
- Optimized B for better user experience

## Breaking Changes (if any)
- Changed X behavior - users need to do Y
```

## Guidelines

### Writing Style
- **User-focused**: Write for end users, not developers
- **Clear**: Avoid technical jargon when possible
- **Concise**: One line per change, bullet points only
- **Action-oriented**: Start with verbs (Added, Fixed, Enhanced, etc.)

### Categories

Use these standard categories:
- **New Features**: Wholly new functionality
- **Bug Fixes**: Fixes for existing issues
- **Improvements**: Enhancements to existing features
- **Breaking Changes**: Changes that require user action

### What to Include
✅ User-visible changes
✅ Bug fixes that affected user experience
✅ Performance improvements users will notice
✅ Security updates (when appropriate to disclose)

### What to Exclude
❌ Internal refactoring
❌ Dependency updates (unless they fix bugs)
❌ Code cleanup that doesn't affect users
❌ Development-only changes

## Automation

### Generating Changelog Files

Run this to generate both markdown and JSON:

```bash
node scripts/generate-changelog.js
```

This generates:
- `web/public/changelog.md` - Complete markdown (for client webapp iframe)
- `web/public/changelog.json` - Structured JSON (for web dashboard list view)

Automatically run during:
- `make build-desktop`
- `make deploy-web`

### Desktop Version Change

When deploying a new desktop version:

```bash
node scripts/change-desktop-version.js 0.3.20
```

This reads `client/releases/v0.3.20.md` and embeds it in `latest.json`.

## Multi-Language Support (Future)

To support multiple languages, use this structure:

```
client/releases/
├── v0.3.20.md          # English (default)
├── v0.3.20.zh-CN.md    # Simplified Chinese
├── v0.3.20.ja.md       # Japanese
└── ...
```

The scripts will automatically detect and use locale-specific files.

## Examples

See `v0.3.18.md` for a good example of the format.
