import { describe, expect, it } from 'vitest';
import { encodeSource, parseSourcepos, sliceSourceBytes } from './source-bytes';

// data-sourcepos ranges are BYTE offsets; JS strings are UTF-16. These tests
// pin the slicing math on multibyte content — the exact bug class that would
// silently hand the editor (and the write-back) the wrong slice.
describe('sliceSourceBytes', () => {
  const src = '# 标题 🎯\n\n第一段 `code` 内容。\n\n- 项目 🀄\n';
  const bytes = encodeSource(src);

  function byteOffsetOf(sub: string): number {
    const idx = src.indexOf(sub);
    return encodeSource(src.slice(0, idx)).length;
  }

  it('recovers a CJK+emoji heading by byte range', () => {
    const start = byteOffsetOf('# 标题 🎯');
    const end = start + encodeSource('# 标题 🎯').length;
    expect(sliceSourceBytes(bytes, start, end)).toBe('# 标题 🎯');
  });

  it('recovers a paragraph after multibyte content, unshifted', () => {
    const para = '第一段 `code` 内容。';
    const start = byteOffsetOf(para);
    expect(sliceSourceBytes(bytes, start, start + encodeSource(para).length)).toBe(para);
  });

  it('round-trips the whole document', () => {
    expect(sliceSourceBytes(bytes, 0, bytes.length)).toBe(src);
  });

  it('rejects ranges outside the buffer', () => {
    expect(sliceSourceBytes(bytes, 0, bytes.length + 1)).toBeNull();
    expect(sliceSourceBytes(bytes, -1, 3)).toBeNull();
    expect(sliceSourceBytes(bytes, 5, 3)).toBeNull();
  });
});

describe('parseSourcepos', () => {
  it('parses a well-formed range', () => {
    expect(parseSourcepos('120:245')).toEqual({ start: 120, end: 245 });
  });

  it('rejects malformed and inverted values', () => {
    expect(parseSourcepos(undefined)).toBeNull();
    expect(parseSourcepos('abc')).toBeNull();
    expect(parseSourcepos('12')).toBeNull();
    expect(parseSourcepos('20:10')).toBeNull();
    expect(parseSourcepos('-3:10')).toBeNull();
  });
});
