import { useEffect, useState, type RefObject } from 'react';
import { ART_PROTOCOL, isArtMessage } from '@/lib/protocol';
import type { FromFrame, HighlightMsg, ModeMsg, ScrollToMsg, ToFrame } from '@/lib/protocol';

// Omit<ToFrame, 'art'> would collapse to `keyof` the *intersection* of all
// union members (only `type` is common) and silently drop mode/aids/aid.
// Spell the union out explicitly so each variant keeps its own fields.
type OutgoingToFrame = Omit<ModeMsg, 'art'> | Omit<HighlightMsg, 'art'> | Omit<ScrollToMsg, 'art'>;

/**
 * Shell-side postMessage send/receive (§7.5).
 *
 * Pitfall 1 (verbatim from the protocol comment): the iframe uses
 * sandbox="allow-scripts" without allow-same-origin, so its origin is
 * always the literal string "null". Validating a message **must** use
 * event.source === iframeEl.contentWindow — never compare event.origin.
 */
export function useFrameBridge(iframeRef: RefObject<HTMLIFrameElement | null>) {
  const [lastMessage, setLastMessage] = useState<FromFrame | null>(null);

  useEffect(() => {
    function onMessage(e: MessageEvent) {
      const frame = iframeRef.current;
      if (!frame || e.source !== frame.contentWindow) return;
      if (!isArtMessage(e.data)) return;
      setLastMessage(e.data as FromFrame);
    }
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [iframeRef]);

  function postToFrame(msg: OutgoingToFrame): void {
    const win = iframeRef.current?.contentWindow;
    win?.postMessage({ art: ART_PROTOCOL, ...msg } as ToFrame, '*');
  }

  return { lastMessage, postToFrame };
}
