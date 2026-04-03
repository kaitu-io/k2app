import { describe, it, expect } from 'vitest';
import { suggestEmail } from '../email-suggest';

describe('suggestEmail', () => {
  // Chinese domain typos
  it('corrects qq.con to qq.com', () => {
    expect(suggestEmail('123@qq.con')).toBe('123@qq.com');
  });

  it('corrects qq.co to qq.com', () => {
    expect(suggestEmail('123@qq.co')).toBe('123@qq.com');
  });

  it('corrects qq.c0m to qq.com', () => {
    expect(suggestEmail('123@qq.c0m')).toBe('123@qq.com');
  });

  it('corrects qq.co m (with space) to qq.com', () => {
    expect(suggestEmail('123@qq.co m')).toBe('123@qq.com');
  });

  it('corrects qq.mcon to qq.com', () => {
    expect(suggestEmail('123@qq.mcon')).toBe('123@qq.com');
  });

  it('corrects qq.cpm to qq.com', () => {
    expect(suggestEmail('123@qq.cpm')).toBe('123@qq.com');
  });

  it('corrects 163.co to 163.com', () => {
    expect(suggestEmail('test@163.co')).toBe('test@163.com');
  });

  it('corrects 163.con to 163.com', () => {
    expect(suggestEmail('test@163.con')).toBe('test@163.com');
  });

  it('corrects 126.con to 126.com', () => {
    expect(suggestEmail('test@126.con')).toBe('test@126.com');
  });

  it('corrects wq.com to qq.com', () => {
    expect(suggestEmail('123@wq.com')).toBe('123@qq.com');
  });

  // Gmail typos (built-in to library)
  it('corrects gmai.com to gmail.com', () => {
    expect(suggestEmail('user@gmai.com')).toBe('user@gmail.com');
  });

  it('corrects gamil.com to gmail.com', () => {
    expect(suggestEmail('user@gamil.com')).toBe('user@gmail.com');
  });

  it('corrects gamail.com to gmail.com', () => {
    expect(suggestEmail('user@gamail.com')).toBe('user@gmail.com');
  });

  it('corrects gmail.cpm to gmail.com', () => {
    expect(suggestEmail('user@gmail.cpm')).toBe('user@gmail.com');
  });

  it('corrects gmail.vom to gmail.com', () => {
    expect(suggestEmail('user@gmail.vom')).toBe('user@gmail.com');
  });

  // Hotmail typos
  it('corrects hotmail.con to hotmail.com', () => {
    expect(suggestEmail('user@hotmail.con')).toBe('user@hotmail.com');
  });

  // Valid emails return null
  it('returns null for valid qq.com', () => {
    expect(suggestEmail('123@qq.com')).toBeNull();
  });

  it('returns null for valid gmail.com', () => {
    expect(suggestEmail('user@gmail.com')).toBeNull();
  });

  it('returns null for valid 163.com', () => {
    expect(suggestEmail('test@163.com')).toBeNull();
  });

  // Edge cases
  it('returns null for empty string', () => {
    expect(suggestEmail('')).toBeNull();
  });

  it('returns null for non-email string', () => {
    expect(suggestEmail('notanemail')).toBeNull();
  });

  it('handles qq.comm to qq.com', () => {
    expect(suggestEmail('123@qq.comm')).toBe('123@qq.com');
  });
});
