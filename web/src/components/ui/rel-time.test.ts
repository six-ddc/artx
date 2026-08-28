import { describe, expect, it } from 'vitest';
import { relText } from './rel-time';

const MINUTE = 60_000;
const HOUR = 3_600_000;
const DAY = 86_400_000;
// A fixed "now" mid-year so same-year/other-year cases are unambiguous.
const NOW = new Date('2026-08-28T12:00:00Z').getTime();

describe('relText', () => {
  it('never counts seconds: everything under a minute is "just now"', () => {
    expect(relText(NOW - 1_000, NOW).text).toBe('just now');
    expect(relText(NOW - 59_000, NOW).text).toBe('just now');
  });

  it('a slightly-future timestamp (clock skew) stays "just now" instead of breaking', () => {
    const r = relText(NOW + 30_000, NOW);
    expect(r.text).toBe('just now');
    expect(r.nextIn).toBeGreaterThan(0);
  });

  it('ticks m → h → d as the diff grows', () => {
    expect(relText(NOW - MINUTE, NOW).text).toBe('1m ago');
    expect(relText(NOW - 59 * MINUTE, NOW).text).toBe('59m ago');
    expect(relText(NOW - HOUR, NOW).text).toBe('1h ago');
    expect(relText(NOW - 23 * HOUR, NOW).text).toBe('23h ago');
    expect(relText(NOW - DAY, NOW).text).toBe('1d ago');
    expect(relText(NOW - 6 * DAY, NOW).text).toBe('6d ago');
  });

  it('schedules the next refresh exactly at the boundary where the text changes', () => {
    // 90s ago shows "1m ago"; it becomes "2m ago" in 30s.
    expect(relText(NOW - 90_000, NOW).nextIn).toBe(30_000);
    // 59s ago flips to "1m ago" in 1s.
    expect(relText(NOW - 59_000, NOW).nextIn).toBe(1_000);
  });

  it('switches to an absolute date past 7 days and stops refreshing', () => {
    const r = relText(NOW - 8 * DAY, NOW);
    expect(r.nextIn).toBeNull();
    expect(r.text).toMatch(/Aug/);
    expect(r.text).not.toMatch(/2026/); // same year → no year shown
  });

  it('includes the year for dates from another year', () => {
    const r = relText(new Date('2024-03-05T00:00:00Z').getTime(), NOW);
    expect(r.nextIn).toBeNull();
    expect(r.text).toMatch(/2024/);
  });
});
