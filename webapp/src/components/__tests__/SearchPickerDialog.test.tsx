import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import SearchPickerDialog from '../SearchPickerDialog';

/** jsdom has no visualViewport — stand one in so the keyboard can be simulated. */
function installViewport(height: number) {
  const listeners = new Set<() => void>();
  const vv = {
    height,
    addEventListener: (_: string, fn: () => void) => listeners.add(fn),
    removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
  };
  Object.defineProperty(window, 'visualViewport', { value: vv, configurable: true });
  return {
    resize(h: number) {
      vv.height = h;
      act(() => listeners.forEach((fn) => fn()));
    },
  };
}

const renderPicker = () =>
  render(
    <SearchPickerDialog
      open
      title="t"
      searchLabel="search"
      emptyText="none"
      value={null}
      getOptions={() => [{ id: 'a', primary: 'A' }]}
      onClose={vi.fn()}
      onSelect={vi.fn()}
    />,
  );

const paper = () => document.querySelector('.MuiDialog-paper') as HTMLElement;

describe('SearchPickerDialog — stays above the on-screen keyboard', () => {
  beforeEach(() => {
    window.getComputedStyle = (() => ({ paddingRight: '0px', getPropertyValue: () => '' })) as any;
  });
  afterEach(() => {
    Object.defineProperty(window, 'visualViewport', { value: undefined, configurable: true });
  });

  // The bug this exists for: the keyboard covers the bottom of the layout
  // viewport without shrinking it, so a list sized against the full height
  // has rows nobody can reach. The cap must follow the VISIBLE height.
  it('caps its height to the visible viewport and re-caps when the keyboard opens', () => {
    const viewport = installViewport(800);
    renderPicker();
    expect(paper().style.maxHeight || getComputedStyleMax(paper())).toContain('800px');

    viewport.resize(460); // keyboard up
    expect(getComputedStyleMax(paper())).toContain('460px');
    expect(getComputedStyleMax(paper())).not.toContain('800px');
  });

  it('is pinned to the top rather than centred', () => {
    installViewport(800);
    renderPicker();
    const container = document.querySelector('.MuiDialog-container') as HTMLElement;
    expect(cssFor(container)).toContain('align-items:flex-start');
  });

  it('focuses the search box on open', () => {
    installViewport(800);
    renderPicker();
    expect(screen.getByLabelText('search')).toHaveFocus();
  });
});

/** Emotion writes sx into <style> rules, not inline — read them back by class. */
function cssFor(el: HTMLElement): string {
  const classes = Array.from(el.classList);
  let out = '';
  for (const sheet of Array.from(document.styleSheets)) {
    for (const rule of Array.from(sheet.cssRules)) {
      const text = rule.cssText.replace(/\s+/g, '');
      if (classes.some((c) => text.includes('.' + c))) out += text;
    }
  }
  return out;
}
function getComputedStyleMax(el: HTMLElement): string {
  const m = cssFor(el).match(/max-height:[^;]+/g);
  return m ? m[m.length - 1] : '';
}
