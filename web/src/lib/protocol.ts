// postMessage protocol between the shell page and the reviewer script inside the sandboxed iframe.
//
// Frozen contract: both sides of W-web (the React shell, the vanilla reviewer script) depend on it.
//
// Two hard constraints:
//  1. The reviewer script stays **vanilla, zero dependencies** — React only
//     lives in the shell app, never enters the sandbox. This file is types
//     only (no runtime code survives compilation), so the reviewer can
//     `import type` from it.
//  2. The iframe uses sandbox="allow-scripts" and **does not grant**
//     allow-same-origin, so its origin is the literal string "null". When
//     the shell validates a message it **must** use
//     `event.source === iframeEl.contentWindow` for authentication —
//     never `event.origin === location.origin` (that's never true here).

/** Protocol version marker. Any message missing the `art` field is ignored — the page may carry someone else's postMessage traffic. */
export const ART_PROTOCOL = 1;

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

interface Base {
  art: typeof ART_PROTOCOL;
}

// --- iframe → shell ---------------------------------------------------------

/** Script is ready; the shell only starts sending commands after receiving this. */
export interface ReadyMsg extends Base {
  type: 'ready';
  href: string;
  /** Count of elements in the document that already carry data-aid, used to flag "aid not yet injected". */
  aidCount: number;
}

/** In review mode, the mouse hovered over an anchorable element. The shell draws a highlight box from this. */
export interface HoverMsg extends Base {
  type: 'hover';
  aid: string | null;
  rect: Rect | null;
  tag: string;
}

/** In review mode, an element was clicked/picked. The shell pops up the comment box. */
export interface PickMsg extends Base {
  type: 'pick';
  aid: string;
  rect: Rect;
  tag: string;
  /** Plain-text summary of the element, up to 200 chars, shown in the comment box as the anchor target. */
  text: string;
  /** Selected text within the element; present only when there's a selection, used to refine the anchor. */
  quote?: string;
}

/** Content height changed; the shell resizes the iframe accordingly. */
export interface SizeMsg extends Base {
  type: 'size';
  height: number;
}

/** Scroll position inside the iframe changed; the shell syncs the thread sidebar's current position from this. */
export interface ScrollMsg extends Base {
  type: 'scroll';
  top: number;
}

/** M2: a contenteditable edit was committed; the shell turns it into a POST that writes back to the source file. */
export interface EditMsg extends Base {
  type: 'edit';
  aid: string;
  html: string;
}

export type FromFrame = ReadyMsg | HoverMsg | PickMsg | SizeMsg | ScrollMsg | EditMsg;

// --- shell → iframe ---------------------------------------------------------

/** Switches interaction mode. browse = normal demo use; review = hover to outline, click to pick an aid; edit = M2. */
export interface ModeMsg extends Base {
  type: 'mode';
  mode: 'browse' | 'review' | 'edit';
}

/** Tells the reviewer which aids have comments attached, so it can give them a persistent marker. */
export interface HighlightMsg extends Base {
  type: 'highlight';
  aids: string[];
}

/** Scroll to a given aid and flash it briefly. */
export interface ScrollToMsg extends Base {
  type: 'scrollTo';
  aid: string;
}

export type ToFrame = ModeMsg | HighlightMsg | ScrollToMsg;

/** Type guard: whether an arbitrary message belongs to this protocol. */
export function isArtMessage(data: unknown): data is Base {
  return (
    typeof data === 'object' &&
    data !== null &&
    (data as Base).art === ART_PROTOCOL
  );
}
