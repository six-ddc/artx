import { useEffect, useRef, type RefObject } from 'react';
import { ART_PROTOCOL, isArtMessage } from '@/lib/protocol';
import type { DiffOpsMsg, FromFrame, HighlightMsg, ModeMsg, ScrollToMsg, ToFrame } from '@/lib/protocol';

// Omit<ToFrame, 'art'> would collapse to `keyof` the *intersection* of all
// union members (only `type` is common) and silently drop mode/aids/aid.
// Spell the union out explicitly so each variant keeps its own fields.
type OutgoingToFrame =
  | Omit<ModeMsg, 'art'>
  | Omit<HighlightMsg, 'art'>
  | Omit<ScrollToMsg, 'art'>
  | Omit<DiffOpsMsg, 'art'>;

/**
 * Shell-side postMessage send/receive (§7.5).
 *
 * Pitfall 1 (verbatim from the protocol comment): the iframe uses
 * sandbox="allow-scripts" without allow-same-origin, so its origin is
 * always the literal string "null". Validating a message **must** use
 * event.source === iframeEl.contentWindow — never compare event.origin.
 *
 * Pitfall 2: every message must reach the handler exactly once, so this is
 * a direct callback, NOT a `lastMessage` state. Firefox delivers a burst of
 * postMessage events (ready + size, or size + scroll) inside one task, and
 * React batches the setStates — a state-shaped bridge then keeps only the
 * final message of the burst. That silently ate the frame's `ready` (the
 * shell never acked, the reviewer retried for 5s) and, when a size landed
 * mid-burst, the height update itself — the Firefox-only
 * "artifact cut off at half height after reload" bug.
 */
export function useFrameBridge(
  iframeRef: RefObject<HTMLIFrameElement | null>,
  onMessage: (msg: FromFrame) => void,
) {
  // Always call the latest render's closure without re-subscribing.
  const handlerRef = useRef(onMessage);
  useEffect(() => {
    handlerRef.current = onMessage;
  });

  useEffect(() => {
    function onWindowMessage(e: MessageEvent) {
      const frame = iframeRef.current;
      if (!frame || e.source !== frame.contentWindow) return;
      if (!isArtMessage(e.data)) return;
      handlerRef.current(e.data as FromFrame);
    }
    window.addEventListener('message', onWindowMessage);
    return () => window.removeEventListener('message', onWindowMessage);
  }, [iframeRef]);

  function postToFrame(msg: OutgoingToFrame): void {
    const win = iframeRef.current?.contentWindow;
    win?.postMessage({ art: ART_PROTOCOL, ...msg } as ToFrame, '*');
  }

  return { postToFrame };
}
