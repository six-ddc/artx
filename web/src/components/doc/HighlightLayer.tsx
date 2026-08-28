import { useEffect, useRef, type RefObject } from 'react';
import type { Thread } from '@/lib/types';

// Highlight echo (§7.4): ThreadAnchor.start/end are **source file** byte
// offsets, with no corresponding DOM node. We don't convert byte offsets —
// instead we find the data-sourcepos block covering that range and run a
// DOM text search (TreeWalker) for anchor.exact within it to paint the
// highlight; on a miss (including approx/orphan) we fall back to a faint
// border around the whole block.

const EXACT_CLASS = 'art-anchor-exact';
const APPROX_CLASS = 'art-anchor-approx';
const FOCUS_CLASS = 'art-anchor-focused';
const MARK_ATTR = 'data-art-thread';

interface BlockPos {
  start: number;
  end: number;
}

function parseSourcepos(el: HTMLElement): BlockPos | null {
  const raw = el.dataset.sourcepos;
  if (!raw) return null;
  const [startStr, endStr] = raw.split(':');
  if (startStr === undefined || endStr === undefined) return null;
  const start = Number.parseInt(startStr, 10);
  const end = Number.parseInt(endStr, 10);
  if (Number.isNaN(start) || Number.isNaN(end)) return null;
  return { start, end };
}

function findCoveringBlock(root: HTMLElement, start: number, end: number): HTMLElement | null {
  let best: HTMLElement | null = null;
  let bestSpan = Infinity;
  for (const el of root.querySelectorAll<HTMLElement>('[data-sourcepos]')) {
    const pos = parseSourcepos(el);
    if (!pos) continue;
    if (pos.start <= start && pos.end >= end) {
      const span = pos.end - pos.start;
      if (span < bestSpan) {
        bestSpan = span;
        best = el;
      }
    }
  }
  return best;
}

/**
 * Locates needle in full, returning [start, end) offsets into full, or null.
 *
 * Tries an exact indexOf first, then retries with markdown source syntax
 * normalized away on both sides. The retry is essential: anchor.exact is a
 * SOURCE FILE excerpt, so it carries inline-code backticks, `> ` blockquote
 * prefixes, `**` strong markers and hard newlines — none of which exist in
 * the rendered DOM text. Without normalization, any quote touching inline
 * markup silently degrades to the block-level dashed fallback and the
 * highlighter never appears.
 *
 * Exported for tests.
 */
export function findQuoteSpan(full: string, needle: string): [number, number] | null {
  const direct = full.indexOf(needle);
  if (direct !== -1) return [direct, direct + needle.length];

  // Normalize the haystack, keeping a map from each normalized char back to
  // its original index: whitespace runs collapse to one space, backticks
  // drop. (Literal `*` from code spans stays — only the paired `**` strong
  // marker is stripped from the needle below, where it cannot be literal.)
  const map: number[] = [];
  let norm = '';
  let pendingSpace = false;
  for (let i = 0; i < full.length; i++) {
    const ch = full[i]!;
    if (/\s/.test(ch)) {
      pendingSpace = norm.length > 0;
      continue;
    }
    if (ch === '`') continue;
    if (pendingSpace) {
      norm += ' ';
      map.push(i);
      pendingSpace = false;
    }
    norm += ch;
    map.push(i);
  }

  let needleNorm = '';
  {
    const preStripped = needle.replace(/^>[ \t]?/gm, '').replace(/\*\*/g, '');
    let sawChar = false;
    let space = false;
    for (const ch of preStripped) {
      if (/\s/.test(ch)) {
        space = sawChar;
        continue;
      }
      if (ch === '`') continue;
      if (space) {
        needleNorm += ' ';
        space = false;
      }
      needleNorm += ch;
      sawChar = true;
    }
  }
  if (!needleNorm) return null;

  const idx = norm.indexOf(needleNorm);
  if (idx === -1) return null;
  return [map[idx]!, map[idx + needleNorm.length - 1]! + 1];
}

/** Finds needle in the plain text of root's subtree, returning a Range that may span multiple text nodes. */
function findTextRange(root: HTMLElement, needle: string): Range | null {
  if (!needle) return null;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  const nodes: Text[] = [];
  let full = '';
  let n: Node | null;
  while ((n = walker.nextNode())) {
    const text = n as Text;
    nodes.push(text);
    full += text.data;
  }
  const span = findQuoteSpan(full, needle);
  if (!span) return null;
  const [idx, endIdx] = span;

  let pos = 0;
  let startNode: Text | null = null;
  let startOffset = 0;
  let endNode: Text | null = null;
  let endOffset = 0;
  for (const t of nodes) {
    const next = pos + t.data.length;
    if (!startNode && idx < next) {
      startNode = t;
      startOffset = idx - pos;
    }
    if (!endNode && endIdx <= next) {
      endNode = t;
      endOffset = endIdx - pos;
      break;
    }
    pos = next;
  }
  if (!startNode || !endNode) return null;

  const range = document.createRange();
  range.setStart(startNode, startOffset);
  range.setEnd(endNode, endOffset);
  return range;
}

/**
 * Wraps every text-node segment of range in its own mark element.
 * Range.surroundContents is useless here: it throws the moment the range
 * crosses an element boundary, and real anchors do that all the time (a
 * quote spanning inline-code spans, links, emphasis). Per-text-node marks
 * paint the same visual highlight and unwrap cleanly in clearMarks.
 */
function wrapRangeTextNodes(range: Range, makeMark: () => HTMLElement): HTMLElement[] {
  let startNode = range.startContainer;
  let endNode = range.endContainer;
  if (startNode.nodeType !== Node.TEXT_NODE || endNode.nodeType !== Node.TEXT_NODE) return [];
  const commonRoot =
    range.commonAncestorContainer.nodeType === Node.TEXT_NODE
      ? range.commonAncestorContainer.parentNode
      : range.commonAncestorContainer;
  if (!commonRoot) return [];

  let startText = startNode as Text;
  let endText = endNode as Text;
  if (startText === endText) {
    if (range.endOffset < endText.length) endText.splitText(range.endOffset);
    if (range.startOffset > 0) startText = startText.splitText(range.startOffset);
    endText = startText;
  } else {
    if (range.endOffset < endText.length) endText.splitText(range.endOffset);
    if (range.startOffset > 0) startText = startText.splitText(range.startOffset);
  }

  const walker = document.createTreeWalker(commonRoot, NodeFilter.SHOW_TEXT);
  const targets: Text[] = [];
  let inRange = false;
  let node: Node | null;
  while ((node = walker.nextNode())) {
    if (node === startText) inRange = true;
    if (inRange) targets.push(node as Text);
    if (node === endText) break;
  }

  const marks: HTMLElement[] = [];
  for (const t of targets) {
    if (!t.data || !t.parentNode) continue;
    const mark = makeMark();
    t.parentNode.insertBefore(mark, t);
    mark.appendChild(t);
    marks.push(mark);
  }
  return marks;
}

function clearMarks(root: HTMLElement): void {
  root.querySelectorAll(`mark[${MARK_ATTR}]`).forEach((mark) => {
    const parent = mark.parentNode;
    if (!parent) return;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parent.normalize();
  });
  root.querySelectorAll(`.${APPROX_CLASS}`).forEach((el) => {
    el.classList.remove(APPROX_CLASS, FOCUS_CLASS);
    el.removeAttribute(MARK_ATTR);
  });
}

interface HighlightLayerProps {
  containerRef: RefObject<HTMLDivElement | null>;
  threads: Thread[];
  focusedThreadId?: string;
  /** Only used to re-run highlighting after content re-renders; not read directly. */
  html?: string;
}

export function HighlightLayer({ containerRef, threads, focusedThreadId, html }: HighlightLayerProps) {
  // Scrolling belongs to the moment focus CHANGES, not to every repaint:
  // the effect also re-runs when the thread list refreshes (e.g. right
  // after submitting a new comment), and scrolling then would yank the
  // page back to whatever thread happened to be focused earlier.
  const lastScrolledFocus = useRef<string | undefined>(undefined);

  useEffect(() => {
    const root = containerRef.current;
    if (!root) return;

    const scrollOnFocus = focusedThreadId !== lastScrolledFocus.current;
    lastScrolledFocus.current = focusedThreadId;

    clearMarks(root);

    const markApprox = (block: HTMLElement | null, threadId: string, focused: boolean) => {
      if (!block) return;
      block.classList.add(APPROX_CLASS);
      // Approx/orphan blocks are click targets too (the reverse focus link
      // in MdCanvas matches on [data-art-thread]); first thread wins when
      // several share a block.
      if (!block.hasAttribute(MARK_ATTR)) block.setAttribute(MARK_ATTR, threadId);
      if (focused) {
        block.classList.add(FOCUS_CLASS);
        if (scrollOnFocus) block.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    };

    for (const thread of threads) {
      if (thread.anchor.kind !== 'text') continue;
      const { start, end, exact, orphan, approx } = thread.anchor;
      const focused = thread.thread === focusedThreadId;
      const block = findCoveringBlock(root, start, end);

      if (orphan || approx || !exact) {
        markApprox(block, thread.thread, focused);
        continue;
      }

      const range = findTextRange(block ?? root, exact);
      if (!range) {
        markApprox(block, thread.thread, focused);
        continue;
      }

      const marks = wrapRangeTextNodes(range, () => {
        const mark = document.createElement('mark');
        mark.className = focused ? `${EXACT_CLASS} ${FOCUS_CLASS}` : EXACT_CLASS;
        mark.setAttribute(MARK_ATTR, thread.thread);
        return mark;
      });
      if (marks.length === 0) {
        markApprox(block, thread.thread, focused);
        continue;
      }
      if (focused && scrollOnFocus) marks[0]!.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }

    return () => clearMarks(root);
  }, [containerRef, threads, focusedThreadId, html]);

  return null;
}
