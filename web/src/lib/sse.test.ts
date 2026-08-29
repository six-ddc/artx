import { describe, expect, it } from 'vitest';
import { invalidationKeysForEvent } from './sse';

describe('invalidationKeysForEvent', () => {
  it('maps `comments` to invalidate(["comments", doc])', () => {
    expect(invalidationKeysForEvent('comments', { doc: 'a7f3k2', threads: ['c7k2f9'] })).toEqual([
      ['comments', 'a7f3k2'],
    ]);
  });

  it('maps `doc` (content) to invalidate(["doc", doc]) + history + diff', () => {
    expect(invalidationKeysForEvent('doc', { doc: 'a7f3k2', kind: 'content' })).toEqual([
      ['doc', 'a7f3k2'],
      ['history', 'a7f3k2'],
      ['diff', 'a7f3k2'],
    ]);
  });

  it('maps `doc` (remap) to invalidate(["doc", doc]) + history + diff AND ["comments", doc]', () => {
    expect(
      invalidationKeysForEvent('doc', { doc: 'a7f3k2', kind: 'remap', remaps: 3, orphans: 1 }),
    ).toEqual([
      ['doc', 'a7f3k2'],
      ['history', 'a7f3k2'],
      ['diff', 'a7f3k2'],
      ['comments', 'a7f3k2'],
    ]);
  });

  it('maps `doc` (aid/remove) to invalidate(["doc", doc]) + history + diff', () => {
    expect(invalidationKeysForEvent('doc', { doc: 'x', kind: 'aid' })).toEqual([
      ['doc', 'x'],
      ['history', 'x'],
      ['diff', 'x'],
    ]);
    expect(invalidationKeysForEvent('doc', { doc: 'x', kind: 'remove' })).toEqual([
      ['doc', 'x'],
      ['history', 'x'],
      ['diff', 'x'],
    ]);
  });

  it('maps `docs` to invalidate(["docs"])', () => {
    expect(invalidationKeysForEvent('docs', {})).toEqual([['docs']]);
  });

  it('maps `ping` to no invalidation', () => {
    expect(invalidationKeysForEvent('ping', {})).toEqual([]);
  });
});
