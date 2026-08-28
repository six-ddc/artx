// Byte-accurate source slicing for the md block editor. data-sourcepos
// ranges are BYTE offsets into the source file (Go side, mdsrc), while JS
// strings are UTF-16 — every slice must round-trip through an encoded view
// or CJK/emoji content shifts the boundaries.

export interface SourceposRange {
  start: number;
  end: number;
}

export function parseSourcepos(raw: string | undefined): SourceposRange | null {
  if (!raw) return null;
  const [s, e] = raw.split(':');
  const start = Number.parseInt(s ?? '', 10);
  const end = Number.parseInt(e ?? '', 10);
  if (Number.isNaN(start) || Number.isNaN(end) || start < 0 || end < start) return null;
  return { start, end };
}

export function encodeSource(raw: string): Uint8Array {
  return new TextEncoder().encode(raw);
}

/** Decodes bytes[start:end); null when the range falls outside the buffer. */
export function sliceSourceBytes(bytes: Uint8Array, start: number, end: number): string | null {
  if (start < 0 || end < start || end > bytes.length) return null;
  return new TextDecoder().decode(bytes.subarray(start, end));
}
