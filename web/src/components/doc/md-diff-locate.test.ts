import { describe, expect, it } from 'vitest';
import { inlineSegments, locateAddedTexts, stripBlockMarkers } from './MdDiffCanvas';

// The word-level diff pipeline: added_texts are SOURCE segments (list
// bullets, heading hashes, inline syntax included), the search runs over
// the RENDERED DOM. These cases pin the normalization that bridges the two.

function ul(html: string): HTMLElement {
  const el = document.createElement('ul');
  el.innerHTML = html;
  return el;
}

describe('stripBlockMarkers', () => {
  it('strips list bullets, ordered numbers, heading hashes, blockquote arrows', () => {
    expect(stripBlockMarkers('- item text')).toBe('item text');
    expect(stripBlockMarkers('  * item')).toBe('item');
    expect(stripBlockMarkers('3. third')).toBe('third');
    expect(stripBlockMarkers('## Heading')).toBe('Heading');
    expect(stripBlockMarkers('> - nested quote item')).toBe('nested quote item');
  });

  it('leaves non-marker leading chars alone', () => {
    expect(stripBlockMarkers('-5 degrees')).toBe('-5 degrees');
  });
});

describe('inlineSegments', () => {
  it('splits on inline syntax and drops short fragments', () => {
    expect(inlineSegments('**Bold** and [link](url) tail')).toEqual([
      'Bold',
      'and',
      'link',
      'url',
      'tail',
    ]);
    expect(inlineSegments('a *b* c')).toEqual([]);
  });
});

describe('locateAddedTexts', () => {
  it('locates a newly added list item despite its source "- " prefix and trailing newline', () => {
    const el = ul(
      '<li>Compare from any historical version</li><li>Word-level highlights</li>',
    );
    const ranges = locateAddedTexts(el, ['- Compare from any historical version\n']);
    expect(ranges.length).toBe(1);
    expect(ranges[0]!.toString()).toBe('Compare from any historical version');
  });

  it('locates each line of a multi-line source segment', () => {
    const el = ul('<li>first new item</li><li>second new item</li>');
    const ranges = locateAddedTexts(el, ['- first new item\n- second new item\n']);
    expect(ranges.map((r) => r.toString())).toEqual(['first new item', 'second new item']);
  });

  it('falls back to inline sub-segments when a line mixes syntax the DOM lost (link URLs)', () => {
    const el = ul('<li>See the <a href="https://example.com">full guide</a> for details</li>');
    const ranges = locateAddedTexts(el, ['- See the [full guide](https://example.com) for details\n']);
    expect(ranges.length).toBeGreaterThan(0);
    expect(ranges.some((r) => r.toString() === 'full guide')).toBe(true);
  });

  it('returns nothing when the text truly is not in the block — the caller keeps the yellow wash', () => {
    const el = ul('<li>unrelated content</li>');
    expect(locateAddedTexts(el, ['- something else entirely\n'])).toEqual([]);
  });
});
