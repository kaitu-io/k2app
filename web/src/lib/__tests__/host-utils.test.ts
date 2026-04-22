import { describe, it, expect } from 'vitest';
import { isProductionHost } from '../host-utils';

describe('isProductionHost', () => {
  it('returns true for kaitu.io', () => {
    expect(isProductionHost('kaitu.io')).toBe(true);
  });

  it('returns true for www.kaitu.io', () => {
    expect(isProductionHost('www.kaitu.io')).toBe(true);
  });

  it('returns true for overleap.io', () => {
    expect(isProductionHost('overleap.io')).toBe(true);
  });

  it('returns true for www.overleap.io', () => {
    expect(isProductionHost('www.overleap.io')).toBe(true);
  });

  it('strips port when matching', () => {
    expect(isProductionHost('kaitu.io:3000')).toBe(true);
  });

  it('is case-insensitive', () => {
    expect(isProductionHost('OVERLEAP.io')).toBe(true);
  });

  it('returns false for localhost', () => {
    expect(isProductionHost('localhost')).toBe(false);
  });

  it('returns false for localhost with port', () => {
    expect(isProductionHost('localhost:3000')).toBe(false);
  });

  it('returns false for 127.0.0.1 with port', () => {
    expect(isProductionHost('127.0.0.1:3000')).toBe(false);
  });

  it('returns false for Amplify main branch preview', () => {
    expect(isProductionHost('main.d3q8wll74rs94h.amplifyapp.com')).toBe(false);
  });

  it('returns false for Amplify PR preview', () => {
    expect(isProductionHost('pr-7.d3q8wll74rs94h.amplifyapp.com')).toBe(false);
  });

  it('returns false for null', () => {
    expect(isProductionHost(null)).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isProductionHost(undefined)).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isProductionHost('')).toBe(false);
  });
});
