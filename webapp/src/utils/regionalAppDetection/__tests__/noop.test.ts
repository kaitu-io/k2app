import { describe, it, expect } from 'vitest';
import { noopDetector } from '../noop';

describe('noopDetector', () => {
  it('has stable region marker', () => {
    expect(noopDetector.region).toBe('noop');
  });

  it('returns empty array regardless of input', () => {
    expect(noopDetector.detect([])).toEqual([]);
    expect(
      noopDetector.detect([
        { packageName: 'com.tencent.mm', label: 'WeChat' },
        { packageName: 'com.android.chrome', label: 'Chrome' },
      ]),
    ).toEqual([]);
  });

  it('returns a fresh array each call (no shared mutable state)', () => {
    const a = noopDetector.detect([]);
    const b = noopDetector.detect([]);
    a.push({ packageName: 'leak', label: 'leak', reasonKey: 'x' });
    expect(b).toEqual([]);
  });
});
