/**
 * T2 - Terminal Dark Theme System
 * Source-level verification tests that confirm theme configuration is correct.
 * These tests read file contents rather than rendering DOM, because theming
 * is CSS-level and applied at build/runtime via next-themes class injection.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';

// Resolve paths relative to web/ root
// __dirname = web/src/lib/__tests__  →  ../../../ = web/
const webRoot = resolve(__dirname, '../../../');

function readWebFile(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), 'utf-8');
}

describe('T2 - Terminal Dark Theme System', () => {
  describe('test_theme_provider_forces_dark', () => {
    it('EmbedThemeProvider should set defaultTheme to dark', () => {
      const source = readWebFile('src/components/providers/EmbedThemeProvider.tsx');

      // Must contain defaultTheme="dark" or defaultTheme={'dark'}
      const hasDarkDefault =
        source.includes('defaultTheme="dark"') ||
        source.includes("defaultTheme={'dark'}") ||
        source.includes('defaultTheme={`dark`}');

      expect(hasDarkDefault).toBe(true);
    });

    it('EmbedThemeProvider should NOT use enableSystem', () => {
      const source = readWebFile('src/components/providers/EmbedThemeProvider.tsx');

      expect(source).not.toContain('enableSystem');
    });
  });

  describe('test_header_no_theme_toggle', () => {
    it('Header should NOT import or render ThemeToggle', () => {
      const source = readWebFile('src/components/Header.tsx');

      expect(source).not.toContain('ThemeToggle');
    });
  });

  describe('test_css_variables_terminal_dark', () => {
    it('globals.css :root should define --background as #0a0a0f (terminal dark)', () => {
      const source = readWebFile('src/app/globals.css');

      // The :root block must contain the terminal dark background value
      // Accept either hex (#0a0a0f) or equivalent hsl/oklch representations
      const hasTerminalBackground =
        source.includes('#0a0a0f') ||
        source.includes('hsl(240, 6%, 4%)') ||
        source.includes('hsl(240,6%,4%)');

      expect(hasTerminalBackground).toBe(true);
    });

    it('globals.css :root should define --primary as #00ff88 (terminal green)', () => {
      const source = readWebFile('src/app/globals.css');

      const hasTerminalPrimary =
        source.includes('#00ff88') ||
        source.includes('hsl(150, 100%, 50%)') ||
        source.includes('hsl(150,100%,50%)');

      expect(hasTerminalPrimary).toBe(true);
    });

    it('globals.css :root should define --foreground as #e0e0e0 (terminal foreground)', () => {
      const source = readWebFile('src/app/globals.css');

      const hasTerminalForeground =
        source.includes('#e0e0e0') ||
        source.includes('hsl(0, 0%, 88%)') ||
        source.includes('hsl(0,0%,88%)');

      expect(hasTerminalForeground).toBe(true);
    });

    it('globals.css :root should define --card as #111118 (terminal card)', () => {
      const source = readWebFile('src/app/globals.css');

      const hasTerminalCard =
        source.includes('#111118') ||
        source.includes('hsl(240, 6%, 7%)') ||
        source.includes('hsl(240,6%,7%)');

      expect(hasTerminalCard).toBe(true);
    });
  });

  describe('ThemeToggle component removed', () => {
    it('ThemeToggle.tsx standalone file should not exist', () => {
      let fileExists = true;
      try {
        readWebFile('src/components/ThemeToggle.tsx');
      } catch {
        fileExists = false;
      }

      expect(fileExists).toBe(false);
    });
  });

  describe('JetBrains Mono font in locale layout', () => {
    it('locale layout.tsx should import JetBrains Mono', () => {
      const source = readWebFile('src/app/[locale]/layout.tsx');

      expect(source).toContain('JetBrains_Mono');
    });

    it('locale layout.tsx should expose --font-mono CSS variable', () => {
      const source = readWebFile('src/app/[locale]/layout.tsx');

      expect(source).toContain('--font-mono');
    });
  });
});
