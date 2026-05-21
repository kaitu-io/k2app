import { describe, it, expect } from 'vitest';
import { isValidEmail } from '../email';

describe('isValidEmail', () => {
  it.each([
    ['a@b.co', true],
    ['user.name+tag@example.com', true],
    // trim() only gates the non-empty check; the regex still runs on the
    // unstripped string, so internal/edge whitespace fails the regex.
    ['  a@b.co  ', false],
    ['nope', false],
    ['', false],
    ['@b.co', false],
    ['a@', false],
    ['a@b', false],
  ])('isValidEmail(%j) === %s', (input, expected) => {
    expect(isValidEmail(input)).toBe(expected);
  });
});
