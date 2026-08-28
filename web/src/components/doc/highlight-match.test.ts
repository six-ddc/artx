import { describe, expect, it } from 'vitest';
import { findQuoteSpan } from './HighlightLayer';

// anchor.exact is a markdown SOURCE excerpt; the haystack is rendered DOM
// text. The span must land on original haystack offsets.
describe('findQuoteSpan', () => {
  it('finds a verbatim quote directly', () => {
    expect(findQuoteSpan('abc def', 'c d')).toEqual([2, 5]);
  });

  it('matches a quote containing inline-code backticks', () => {
    const dom = '答案在 internal/lockfile：具体协议';
    const src = '答案在 `internal/lockfile`：';
    const span = findQuoteSpan(dom, src);
    expect(span).not.toBeNull();
    expect(dom.slice(span![0], span![1])).toBe('答案在 internal/lockfile：');
  });

  it('matches across soft line breaks and blockquote prefixes', () => {
    const dom = '本文基于 six-ddc/artx 源码（cmd/artx + internal/*）整理， 所有行为都可以对应到具体位置。';
    const src = '源码（`cmd/artx` + `internal/*`）整理，\n> 所有行为都可以对应到具体位置。';
    const span = findQuoteSpan(dom, src);
    expect(span).not.toBeNull();
    expect(dom.slice(span![0], span![1])).toBe('源码（cmd/artx + internal/*）整理， 所有行为都可以对应到具体位置。');
  });

  it('strips strong markers from the needle but keeps literal asterisks in the haystack', () => {
    const dom = '核心是文件锁即存活证明这一点';
    const src = '核心是**文件锁即存活证明**这一点';
    expect(findQuoteSpan(dom, src)).not.toBeNull();
  });

  it('returns null when the text truly is not there', () => {
    expect(findQuoteSpan('完全无关的内容', '`不存在的引文`')).toBeNull();
  });
});
