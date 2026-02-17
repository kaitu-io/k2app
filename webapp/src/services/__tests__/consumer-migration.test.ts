/**
 * Consumer Migration Tests (T3 - RED phase)
 *
 * Verifies that all consumer files have been migrated:
 * - window._k2.updater -> window._platform.updater
 * - window._k2.platform.* -> window._platform.*
 * - window._k2.api.* -> cloudApi (via k2api)
 *
 * The grep-style test is the most important: it reads actual source files
 * and asserts zero occurrences of forbidden patterns.
 *
 * Run: cd webapp && npx vitest run --reporter=verbose src/services/__tests__/consumer-migration.test.ts
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import * as fs from 'fs';
import * as path from 'path';

// ==================== Test 1: useUpdater reads from _platform ====================

describe('useUpdater reads from window._platform', () => {
  beforeEach(() => {
    // Set up window._platform with a mock updater
    (window as any)._platform = {
      os: 'macos',
      isDesktop: true,
      isMobile: false,
      version: '0.4.0',
      storage: {
        get: vi.fn(),
        set: vi.fn(),
        remove: vi.fn(),
        has: vi.fn(),
        clear: vi.fn(),
        keys: vi.fn(),
      },
      getUdid: vi.fn().mockResolvedValue('test-udid'),
      updater: {
        isUpdateReady: true,
        updateInfo: {
          currentVersion: '0.4.0',
          newVersion: '0.5.0',
          releaseNotes: 'Test release',
        },
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn().mockResolvedValue(undefined),
        checkUpdateManual: vi.fn().mockResolvedValue('Update available'),
        onUpdateReady: vi.fn().mockReturnValue(() => {}),
      },
    };

    // Do NOT set window._k2.updater -- useUpdater should read from _platform
    (window as any)._k2 = {
      run: vi.fn().mockResolvedValue({ code: 0 }),
    };
  });

  afterEach(() => {
    delete (window as any)._platform;
    delete (window as any)._k2;
  });

  it('should read updater from window._platform, not window._k2', async () => {
    const { useUpdater } = await import('../../hooks/useUpdater');

    const { result } = renderHook(() => useUpdater());

    // useUpdater should see the updater from _platform
    expect(result.current.isAvailable).toBe(true);
    expect(result.current.isUpdateReady).toBe(true);
    expect(result.current.updateInfo).toEqual({
      currentVersion: '0.4.0',
      newVersion: '0.5.0',
      releaseNotes: 'Test release',
    });
  });

  it('should return unavailable when _platform has no updater', async () => {
    // Remove updater from _platform
    delete (window as any)._platform.updater;

    const { useUpdater } = await import('../../hooks/useUpdater');

    const { result } = renderHook(() => useUpdater());

    // Should be unavailable since _platform has no updater
    expect(result.current.isAvailable).toBe(false);
    expect(result.current.isUpdateReady).toBe(false);
  });
});

// ==================== Test 2: SubmitTicket uses _platform ====================

describe('SubmitTicket uses window._platform for platform capabilities', () => {
  it('should reference window._platform for uploadServiceLogs, not window._k2.platform', () => {
    const filePath = path.resolve(__dirname, '../../pages/SubmitTicket.tsx');
    const source = fs.readFileSync(filePath, 'utf-8');

    // Should NOT contain _k2.platform or _k2?.platform
    const k2PlatformMatches = source.match(/_k2[.?!]*\.platform/g) || [];
    expect(k2PlatformMatches).toEqual([]);

    // Should use _platform instead
    const platformMatches = source.match(/window\._platform/g) || [];
    expect(platformMatches.length).toBeGreaterThan(0);
  });
});

// ==================== Test 3: Dashboard uses _platform ====================

describe('Dashboard uses window._platform for platform capabilities', () => {
  it('should reference window._platform for uploadServiceLogs, not window._k2.platform', () => {
    const filePath = path.resolve(__dirname, '../../pages/Dashboard.tsx');
    const source = fs.readFileSync(filePath, 'utf-8');

    // Should NOT contain _k2.platform or _k2?.platform
    const k2PlatformMatches = source.match(/_k2[.?!]*\.platform/g) || [];
    expect(k2PlatformMatches).toEqual([]);

    // Should use _platform instead
    const platformMatches = source.match(/window\._platform/g) || [];
    expect(platformMatches.length).toBeGreaterThan(0);
  });
});

// ==================== Test 4: Codebase-wide forbidden pattern check ====================

describe('codebase-wide: no _k2.api, _k2.platform, or _k2.updater in source files', () => {
  /**
   * Recursively collect all .ts and .tsx files under a directory,
   * excluding __tests__ directories and .d.ts files.
   */
  function collectSourceFiles(dir: string): string[] {
    const results: string[] = [];
    const entries = fs.readdirSync(dir, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);

      if (entry.isDirectory()) {
        // Skip test directories, node_modules
        if (entry.name === '__tests__' || entry.name === 'node_modules' || entry.name === 'test') {
          continue;
        }
        results.push(...collectSourceFiles(fullPath));
      } else if (entry.isFile()) {
        // Only .ts and .tsx files, exclude .d.ts and .test. files
        if (
          (entry.name.endsWith('.ts') || entry.name.endsWith('.tsx')) &&
          !entry.name.endsWith('.d.ts') &&
          !entry.name.endsWith('.test.ts') &&
          !entry.name.endsWith('.test.tsx') &&
          !entry.name.endsWith('.spec.ts') &&
          !entry.name.endsWith('.spec.tsx')
        ) {
          results.push(fullPath);
        }
      }
    }

    return results;
  }

  /**
   * Check a source file for forbidden patterns.
   * Returns array of violations: { file, line, pattern, text }
   */
  function checkFile(
    filePath: string,
    forbiddenPatterns: RegExp[]
  ): { file: string; line: number; pattern: string; text: string }[] {
    const violations: { file: string; line: number; pattern: string; text: string }[] = [];
    const content = fs.readFileSync(filePath, 'utf-8');
    const lines = content.split('\n');

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];

      // Skip comment lines (single-line // comments and lines inside /* */ blocks)
      const trimmed = line.trim();
      if (trimmed.startsWith('//') || trimmed.startsWith('*') || trimmed.startsWith('/*')) {
        continue;
      }

      for (const pattern of forbiddenPatterns) {
        if (pattern.test(line)) {
          violations.push({
            file: path.relative(path.resolve(__dirname, '../..'), filePath),
            line: i + 1,
            pattern: pattern.source,
            text: line.trim(),
          });
        }
      }
    }

    return violations;
  }

  it('should have zero _k2.platform references in production source files', () => {
    const srcDir = path.resolve(__dirname, '../..');
    const sourceFiles = collectSourceFiles(srcDir);

    // Forbidden patterns: _k2.platform, _k2?.platform, _k2!.platform
    // These should all be migrated to _platform
    const forbiddenPatterns = [
      /_k2[?!]?\.platform\b/,
    ];

    const allViolations: { file: string; line: number; pattern: string; text: string }[] = [];

    for (const file of sourceFiles) {
      const violations = checkFile(file, forbiddenPatterns);
      allViolations.push(...violations);
    }

    if (allViolations.length > 0) {
      const summary = allViolations
        .map((v) => `  ${v.file}:${v.line} -> ${v.text}`)
        .join('\n');
      expect.fail(
        `Found ${allViolations.length} forbidden _k2.platform reference(s) in source files:\n${summary}\n\n` +
          'These should be migrated to window._platform.*'
      );
    }
  });

  it('should have zero _k2.updater references in production source files', () => {
    const srcDir = path.resolve(__dirname, '../..');
    const sourceFiles = collectSourceFiles(srcDir);

    // Forbidden: _k2.updater, _k2?.updater
    const forbiddenPatterns = [
      /_k2[?!]?\.updater\b/,
    ];

    const allViolations: { file: string; line: number; pattern: string; text: string }[] = [];

    for (const file of sourceFiles) {
      const violations = checkFile(file, forbiddenPatterns);
      allViolations.push(...violations);
    }

    if (allViolations.length > 0) {
      const summary = allViolations
        .map((v) => `  ${v.file}:${v.line} -> ${v.text}`)
        .join('\n');
      expect.fail(
        `Found ${allViolations.length} forbidden _k2.updater reference(s) in source files:\n${summary}\n\n` +
          'These should be migrated to window._platform.updater'
      );
    }
  });

  it('should have zero _k2.api references in production source files', () => {
    const srcDir = path.resolve(__dirname, '../..');
    const sourceFiles = collectSourceFiles(srcDir);

    // Forbidden: _k2.api (should use cloudApi via k2api module)
    // Note: _k2.api.exec is the old pattern
    const forbiddenPatterns = [
      /_k2[?!]?\.api\b/,
    ];

    const allViolations: { file: string; line: number; pattern: string; text: string }[] = [];

    for (const file of sourceFiles) {
      const violations = checkFile(file, forbiddenPatterns);
      allViolations.push(...violations);
    }

    if (allViolations.length > 0) {
      const summary = allViolations
        .map((v) => `  ${v.file}:${v.line} -> ${v.text}`)
        .join('\n');
      expect.fail(
        `Found ${allViolations.length} forbidden _k2.api reference(s) in source files:\n${summary}\n\n` +
          'These should be migrated to use cloudApi via k2api module'
      );
    }
  });
});
