import { describe, expect, it } from 'vitest';
import { commitIdentity, formatDayLabel } from './utils';

describe('formatDayLabel', () => {
  // Fixed local reference: mid-afternoon, so day-boundary math is visible.
  const now = new Date(2026, 7, 28, 15, 0); // Aug 28 2026, 15:00 local

  it('labels the same local day Today', () => {
    expect(formatDayLabel(new Date(2026, 7, 28, 0, 5).toISOString(), now)).toBe('Today');
  });

  it('labels the previous local day Yesterday, even minutes before midnight', () => {
    expect(formatDayLabel(new Date(2026, 7, 27, 23, 50).toISOString(), now)).toBe('Yesterday');
  });

  it('labels older same-year dates without the year', () => {
    const label = formatDayLabel(new Date(2026, 7, 21, 9, 0).toISOString(), now);
    expect(label).toContain('21');
    expect(label).not.toContain('2026');
  });

  it('includes the year once it differs', () => {
    expect(formatDayLabel(new Date(2025, 11, 30, 9, 0).toISOString(), now)).toContain('2025');
  });

  it('passes an unparseable date through unchanged', () => {
    expect(formatDayLabel('not-a-date', now)).toBe('not-a-date');
  });
});

describe('commitIdentity', () => {
  it('maps the three fixed gitx authors', () => {
    expect(commitIdentity('artx-human')).toEqual({ kind: 'human', label: 'you' });
    expect(commitIdentity('artx-agent')).toEqual({ kind: 'agent', label: 'agent' });
    expect(commitIdentity('artx')).toEqual({ kind: 'artx', label: 'artx' });
  });

  it('falls back to artx for anything unexpected', () => {
    expect(commitIdentity('someone-else')).toEqual({ kind: 'artx', label: 'artx' });
  });
});
