// reviewer script: the comment-collection script injected into the sandboxed iframe.
//
// R2 (hard rule): stays vanilla, zero dependencies. React only lives in the
// shell app and never enters the sandbox. Only `import type` from
// protocol.ts is allowed (erased at compile time, no runtime code left
// behind); importing any value from that file (including ART_PROTOCOL,
// isArtMessage) is forbidden — the equivalent runtime logic is redeclared
// locally here instead.
//
// Pitfall 1 (verbatim from the protocol's comment): the iframe uses
// sandbox="allow-scripts" without allow-same-origin, so this document's
// origin is the literal string "null". This script never compares origin
// when sending or receiving messages; it only trusts the fact that "the
// message came from window.parent" (the parent page symmetrically
// validates with event.source === iframeEl.contentWindow).

import type {
  EditMsg,
  FromFrame,
  HoverMsg,
  ModeMsg,
  PickMsg,
  Rect,
  ReadyMsg,
  ScrollMsg,
  SizeMsg,
  ToFrame,
} from '../lib/protocol';

/** Must stay equal to protocol.ts's ART_PROTOCOL value; can't import the value from there. */
const ART_VERSION = 1 as const;

const AID_ATTR = 'data-aid';
const HIGHLIGHT_CLASS = 'art-reviewer-highlight';
const FLASH_CLASS = 'art-reviewer-flash';
const OUTLINE_CLASS = 'art-reviewer-editing';

type Mode = ModeMsg['mode'];

function isToFrameMessage(data: unknown): data is ToFrame {
  return (
    typeof data === 'object' &&
    data !== null &&
    (data as { art?: unknown }).art === ART_VERSION &&
    typeof (data as { type?: unknown }).type === 'string'
  );
}

type OutgoingMessage =
  | Omit<ReadyMsg, 'art'>
  | Omit<HoverMsg, 'art'>
  | Omit<PickMsg, 'art'>
  | Omit<SizeMsg, 'art'>
  | Omit<ScrollMsg, 'art'>
  | Omit<EditMsg, 'art'>;

function post(msg: OutgoingMessage): void {
  if (window.parent === window) return; // Not inside an iframe; silently skip
  window.parent.postMessage({ art: ART_VERSION, ...msg } as FromFrame, '*');
}

function toRect(el: Element): Rect {
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, w: r.width, h: r.height };
}

function textSummary(el: Element): string {
  const text = (el.textContent ?? '').replace(/\s+/g, ' ').trim();
  return text.length > 200 ? text.slice(0, 200) : text;
}

function selectionQuoteWithin(el: Element): string | undefined {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return undefined;
  const range = sel.getRangeAt(0);
  if (!el.contains(range.commonAncestorContainer)) return undefined;
  const quote = range.toString();
  return quote.length > 0 ? quote : undefined;
}

function injectStyle(): void {
  const style = document.createElement('style');
  style.textContent = `
    .${HIGHLIGHT_CLASS} { outline: 2px solid rgba(234, 179, 8, 0.9); outline-offset: 2px; }
    .${FLASH_CLASS} { animation: art-reviewer-flash-kf 1.1s ease-out; }
    .${OUTLINE_CLASS} { outline: 2px dashed rgba(37, 99, 235, 0.9); outline-offset: 2px; }
    @keyframes art-reviewer-flash-kf {
      0% { background-color: rgba(234, 179, 8, 0.45); }
      100% { background-color: transparent; }
    }
  `;
  document.head.appendChild(style);
}

class Reviewer {
  private mode: Mode = 'browse';
  private lastHoverAid: string | null = null;
  private highlighted = new Set<string>();
  private editingEl: HTMLElement | null = null;

  init(): void {
    injectStyle();
    window.addEventListener('message', (e) => this.handleMessage(e));
    document.addEventListener('mouseover', (e) => this.handleMouseOver(e));
    document.addEventListener('mouseout', (e) => this.handleMouseOut(e));
    document.addEventListener('click', (e) => this.handleClick(e), true);
    window.addEventListener('scroll', () => this.scheduleScroll(), { passive: true });

    const ro = new ResizeObserver(() => this.sendSize());
    ro.observe(document.documentElement);
    window.addEventListener('load', () => this.sendSize());

    this.sendReady();
    this.sendSize();
  }

  private sendReady(): void {
    const msg: Omit<ReadyMsg, 'art'> = {
      type: 'ready',
      href: window.location.href,
      aidCount: document.querySelectorAll(`[${AID_ATTR}]`).length,
    };
    post(msg);
  }

  private sendSize(): void {
    const msg: Omit<SizeMsg, 'art'> = {
      type: 'size',
      height: document.documentElement.scrollHeight,
    };
    post(msg);
  }

  private scrollTicking = false;
  private scheduleScroll(): void {
    if (this.scrollTicking) return;
    this.scrollTicking = true;
    requestAnimationFrame(() => {
      const msg: Omit<ScrollMsg, 'art'> = { type: 'scroll', top: window.scrollY };
      post(msg);
      this.scrollTicking = false;
    });
  }

  private handleMessage(e: MessageEvent): void {
    if (e.source !== window.parent) return;
    if (!isToFrameMessage(e.data)) return;
    const msg = e.data;
    switch (msg.type) {
      case 'mode':
        this.setMode(msg.mode);
        break;
      case 'highlight':
        this.setHighlight(msg.aids);
        break;
      case 'scrollTo':
        this.scrollToAid(msg.aid);
        break;
    }
  }

  private setMode(mode: Mode): void {
    if (this.editingEl && mode !== 'edit') {
      this.stopEditing(this.editingEl);
    }
    this.mode = mode;
    if (mode === 'browse') {
      this.lastHoverAid = null;
      const msg: Omit<HoverMsg, 'art'> = { type: 'hover', aid: null, rect: null, tag: '' };
      post(msg);
    }
  }

  private setHighlight(aids: string[]): void {
    const next = new Set(aids);
    for (const aid of this.highlighted) {
      if (next.has(aid)) continue;
      const el = this.findByAid(aid);
      el?.classList.remove(HIGHLIGHT_CLASS);
    }
    for (const aid of next) {
      const el = this.findByAid(aid);
      el?.classList.add(HIGHLIGHT_CLASS);
    }
    this.highlighted = next;
  }

  private scrollToAid(aid: string): void {
    const el = this.findByAid(aid);
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.classList.add(FLASH_CLASS);
    window.setTimeout(() => el.classList.remove(FLASH_CLASS), 1200);
  }

  private findByAid(aid: string): HTMLElement | null {
    return document.querySelector<HTMLElement>(`[${AID_ATTR}="${CSS.escape(aid)}"]`);
  }

  private handleMouseOver(e: MouseEvent): void {
    if (this.mode !== 'review') return;
    const target = e.target as Element | null;
    const el = target?.closest<HTMLElement>(`[${AID_ATTR}]`) ?? null;
    const aid = el?.getAttribute(AID_ATTR) ?? null;
    if (aid === this.lastHoverAid) return;
    this.lastHoverAid = aid;
    const msg: Omit<HoverMsg, 'art'> = el
      ? { type: 'hover', aid, rect: toRect(el), tag: el.tagName.toLowerCase() }
      : { type: 'hover', aid: null, rect: null, tag: '' };
    post(msg);
  }

  private handleMouseOut(e: MouseEvent): void {
    if (this.mode !== 'review') return;
    if (e.relatedTarget !== null) return; // Still inside the document; let the next mouseover handle it
    if (this.lastHoverAid === null) return;
    this.lastHoverAid = null;
    const msg: Omit<HoverMsg, 'art'> = { type: 'hover', aid: null, rect: null, tag: '' };
    post(msg);
  }

  private handleClick(e: MouseEvent): void {
    if (this.mode !== 'review' && this.mode !== 'edit') return;
    const target = e.target as Element | null;
    const el = target?.closest<HTMLElement>(`[${AID_ATTR}]`) ?? null;
    if (!el) return;

    e.preventDefault();
    e.stopPropagation();

    const aid = el.getAttribute(AID_ATTR);
    if (!aid) return;

    if (this.mode === 'edit') {
      // Don't send pick in edit mode: the shell would only read it as a
      // signal to "open the comment popover", and the popover's autoFocus
      // input would steal focus, blurring the contenteditable element we're
      // about to focus() and breaking editing outright.
      this.startEditing(el, aid);
      return;
    }

    const msg: Omit<PickMsg, 'art'> = {
      type: 'pick',
      aid,
      rect: toRect(el),
      tag: el.tagName.toLowerCase(),
      text: textSummary(el),
      quote: selectionQuoteWithin(el),
    };
    post(msg);
  }

  private startEditing(el: HTMLElement, aid: string): void {
    if (this.editingEl && this.editingEl !== el) {
      this.stopEditing(this.editingEl);
    }
    this.editingEl = el;
    el.setAttribute('contenteditable', 'true');
    el.classList.add(OUTLINE_CLASS);
    el.focus();

    const onBlur = () => {
      el.removeEventListener('blur', onBlur);
      this.commitEdit(el, aid);
    };
    el.addEventListener('blur', onBlur);
  }

  private stopEditing(el: HTMLElement): void {
    el.removeAttribute('contenteditable');
    el.classList.remove(OUTLINE_CLASS);
    if (this.editingEl === el) this.editingEl = null;
  }

  private commitEdit(el: HTMLElement, aid: string): void {
    this.stopEditing(el);
    const msg: Omit<EditMsg, 'art'> = { type: 'edit', aid, html: el.innerHTML };
    post(msg);
  }
}

function boot(): void {
  new Reviewer().init();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot);
} else {
  boot();
}
