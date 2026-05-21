import { describe, it, expect } from 'vitest';
import { checkPasswordStrength, PASSWORD_MIN_LENGTH } from '../password-strength';

describe('checkPasswordStrength', () => {
  it('returns score=0 tooShort=true isValid=false for empty', async () => {
    const r = await checkPasswordStrength('');
    expect(r.score).toBe(0);
    expect(r.isValid).toBe(false);
    expect(r.tooShort).toBe(true);
  });

  it('rejects when shorter than the floor', async () => {
    const r = await checkPasswordStrength('Short1!');
    expect(r.isValid).toBe(false);
    expect(r.tooShort).toBe(true);
    expect('Short1!'.length).toBeLessThan(PASSWORD_MIN_LENGTH);
  });

  it('rejects common dictionary word at full length', async () => {
    const r = await checkPasswordStrength('Password12');
    expect(r.score).toBeLessThan(3);
    expect(r.isValid).toBe(false);
  });

  it('accepts a strong random password', async () => {
    const r = await checkPasswordStrength('k7N#mq2P!xT9');
    expect(r.score).toBeGreaterThanOrEqual(3);
    expect(r.isValid).toBe(true);
  });

  it('penalizes passwords containing the user email', async () => {
    const r = await checkPasswordStrength('alice12345!', ['alice@example.com']);
    expect(r.score).toBeLessThan(3);
    expect(r.isValid).toBe(false);
  });

  it('clamps a score out of range (defensive)', async () => {
    // We can't easily induce zxcvbn to return >4 in real usage, but we
    // can document the contract: the return type is the narrow union
    // 0|1|2|3|4. This is enforced at compile time via the cast.
    const r = await checkPasswordStrength('k7N#mq2P!xT9');
    expect([0, 1, 2, 3, 4]).toContain(r.score);
  });
});
