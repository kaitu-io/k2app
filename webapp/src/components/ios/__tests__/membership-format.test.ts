import { describe, it, expect } from 'vitest';
import {
  daysRemaining,
  expiryUrgency,
  formatExpiryDate,
  urgencyColor,
} from '../membership-format';

const DAY = 86400;
const NOW = 1_700_000_000;

describe('membership-format', () => {
  describe('daysRemaining', () => {
    it('rounds up partial days', () => {
      expect(daysRemaining(NOW + 5 * DAY + 100, NOW)).toBe(6);
      expect(daysRemaining(NOW + 1, NOW)).toBe(1);
    });
    it('returns 0 for expired / zero / past', () => {
      expect(daysRemaining(NOW - DAY, NOW)).toBe(0);
      expect(daysRemaining(0, NOW)).toBe(0);
      expect(daysRemaining(NOW, NOW)).toBe(0);
    });
  });

  describe('expiryUrgency', () => {
    it('critical at <= 3 days', () => {
      expect(expiryUrgency(0)).toBe('critical');
      expect(expiryUrgency(3)).toBe('critical');
    });
    it('warning at 4..7 days', () => {
      expect(expiryUrgency(4)).toBe('warning');
      expect(expiryUrgency(7)).toBe('warning');
    });
    it('normal beyond 7 days', () => {
      expect(expiryUrgency(8)).toBe('normal');
      expect(expiryUrgency(365)).toBe('normal');
    });
  });

  describe('urgencyColor', () => {
    it('maps urgency to MUI palette color', () => {
      expect(urgencyColor('critical')).toBe('error');
      expect(urgencyColor('warning')).toBe('warning');
      expect(urgencyColor('normal')).toBe('success');
    });
  });

  describe('formatExpiryDate', () => {
    it('returns empty string for 0 / negative (caller skips render)', () => {
      expect(formatExpiryDate(0)).toBe('');
      expect(formatExpiryDate(-1)).toBe('');
    });
    it('formats a real timestamp to a non-empty localized string', () => {
      expect(formatExpiryDate(NOW).length).toBeGreaterThan(0);
    });
  });
});
