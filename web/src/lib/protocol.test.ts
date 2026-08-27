import { describe, expect, it } from 'vitest';
import { ART_PROTOCOL, isArtMessage } from './protocol';

describe('isArtMessage', () => {
  it('returns false when the art field is missing', () => {
    expect(isArtMessage({ type: 'ready', href: '/', aidCount: 0 })).toBe(false);
  });

  it('returns false for non-object payloads', () => {
    expect(isArtMessage(null)).toBe(false);
    expect(isArtMessage(undefined)).toBe(false);
    expect(isArtMessage('hello')).toBe(false);
    expect(isArtMessage(42)).toBe(false);
  });

  it('returns false when art does not match the protocol version', () => {
    expect(isArtMessage({ art: 2, type: 'ready' })).toBe(false);
  });

  it('returns true for a well-formed message', () => {
    expect(isArtMessage({ art: ART_PROTOCOL, type: 'ready', href: '/', aidCount: 0 })).toBe(true);
  });
});
