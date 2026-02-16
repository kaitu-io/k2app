import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';

describe('Dark Theme CSS Variables', () => {
  it('test_dark_theme_css_variables — app.css defines --color-bg-default, --color-primary, --color-text-primary CSS custom properties', () => {
    const cssPath = resolve(__dirname, '../../app.css');
    const cssContent = readFileSync(cssPath, 'utf-8');

    // Verify that the CSS file defines the required design token custom properties
    // These should be in a @theme block or :root selector
    expect(cssContent).toMatch(/--color-bg-default/);
    expect(cssContent).toMatch(/--color-primary/);
    expect(cssContent).toMatch(/--color-text-primary/);
  });
});
