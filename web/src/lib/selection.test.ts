import { describe, expect, it } from 'vitest';
import { selectionInputFromRange } from './selection';

describe('selectionInputFromRange', () => {
  it('collects block_start/end + exact/before/after within a single block', () => {
    document.body.innerHTML = '<p data-sourcepos="100:150">Hello world there</p>';
    const p = document.querySelector('p')!;
    const text = p.firstChild!;

    const range = document.createRange();
    range.setStart(text, 6); // after "Hello "
    range.setEnd(text, 11); // "world"

    const result = selectionInputFromRange(range);

    expect(result).toEqual({
      block_start: 100,
      block_end: 150,
      exact: 'world',
      before: 'Hello ',
      after: ' there',
    });
  });

  it('resolves the nearest data-sourcepos ancestor, not just a direct parent', () => {
    document.body.innerHTML =
      '<p data-sourcepos="0:20">Hello <strong>bold world</strong> there</p>';
    const strong = document.querySelector('strong')!;
    const text = strong.firstChild!;

    const range = document.createRange();
    range.setStart(text, 5); // "world"
    range.setEnd(text, 10);

    const result = selectionInputFromRange(range);

    expect(result?.block_start).toBe(0);
    expect(result?.block_end).toBe(20);
    expect(result?.exact).toBe('world');
    expect(result?.before.endsWith('bold ')).toBe(true);
    expect(result?.after).toBe(' there');
  });

  it('shrinks a cross-block selection to the start-side block', () => {
    document.body.innerHTML =
      '<p data-sourcepos="0:10">First para</p><p data-sourcepos="10:22">Second para</p>';
    const p1 = document.querySelectorAll('p')[0]!;
    const p2 = document.querySelectorAll('p')[1]!;

    const range = document.createRange();
    range.setStart(p1.firstChild!, 6); // "para" inside "First para"
    range.setEnd(p2.firstChild!, 6); // partway into "Second"

    const result = selectionInputFromRange(range);

    expect(result?.block_start).toBe(0);
    expect(result?.block_end).toBe(10);
    expect(result?.exact).toBe('para');
    expect(result?.before).toBe('First ');
    // The selection was shrunk to the start-side block, which has no more text left for "after".
    expect(result?.after).toBe('');
  });

  it('returns null for a collapsed selection', () => {
    document.body.innerHTML = '<p data-sourcepos="0:5">Hi</p>';
    const p = document.querySelector('p')!;
    const range = document.createRange();
    range.setStart(p.firstChild!, 1);
    range.setEnd(p.firstChild!, 1);

    expect(selectionInputFromRange(range)).toBeNull();
  });

  it('returns null when no ancestor carries data-sourcepos', () => {
    document.body.innerHTML = '<p>No sourcepos here</p>';
    const p = document.querySelector('p')!;
    const range = document.createRange();
    range.setStart(p.firstChild!, 0);
    range.setEnd(p.firstChild!, 2);

    expect(selectionInputFromRange(range)).toBeNull();
  });

  it('caps before/after context at 64 characters', () => {
    const long = 'x'.repeat(100);
    document.body.innerHTML = `<p data-sourcepos="0:250">${long}MID${long}</p>`;
    const p = document.querySelector('p')!;
    const text = p.firstChild!;

    const range = document.createRange();
    range.setStart(text, 100);
    range.setEnd(text, 103); // "MID"

    const result = selectionInputFromRange(range);

    expect(result?.exact).toBe('MID');
    expect(result?.before).toBe('x'.repeat(64));
    expect(result?.after).toBe('x'.repeat(64));
  });
});
