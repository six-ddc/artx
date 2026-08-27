import { useEffect, type RefObject } from 'react';
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
  const idx = full.indexOf(needle);
  if (idx === -1) return null;
  const endIdx = idx + needle.length;

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
  useEffect(() => {
    const root = containerRef.current;
    if (!root) return;

    clearMarks(root);

    const markApprox = (block: HTMLElement | null, focused: boolean) => {
      if (!block) return;
      block.classList.add(APPROX_CLASS);
      if (focused) {
        block.classList.add(FOCUS_CLASS);
        block.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    };

    for (const thread of threads) {
      if (thread.anchor.kind !== 'text') continue;
      const { start, end, exact, orphan, approx } = thread.anchor;
      const focused = thread.thread === focusedThreadId;
      const block = findCoveringBlock(root, start, end);

      if (orphan || approx || !exact) {
        markApprox(block, focused);
        continue;
      }

      const range = findTextRange(block ?? root, exact);
      if (!range) {
        markApprox(block, focused);
        continue;
      }

      const mark = document.createElement('mark');
      mark.className = focused ? `${EXACT_CLASS} ${FOCUS_CLASS}` : EXACT_CLASS;
      mark.setAttribute(MARK_ATTR, thread.thread);
      try {
        range.surroundContents(mark);
        if (focused) mark.scrollIntoView({ behavior: 'smooth', block: 'center' });
      } catch {
        // The range crosses a non-text-node boundary, so surroundContents throws — fall back to a whole-block marker.
        markApprox(block, focused);
      }
    }

    return () => clearMarks(root);
  }, [containerRef, threads, focusedThreadId, html]);

  return null;
}
