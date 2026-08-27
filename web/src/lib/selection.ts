// Selection → SelectionInput collection. Follows the algorithm in
// docs/blueprint.md §7.4 step by step: only collect
// block_start/block_end/exact/before/after, **do not** convert to source
// file offsets — the server does the quote match within the block's source
// to compute the final anchor (see SelectionInput's field comments).

import type { SelectionInput } from './types';

const CONTEXT_CHARS = 64;

function findBlockElement(node: Node): HTMLElement | null {
  const el = node.nodeType === Node.ELEMENT_NODE ? (node as Element) : node.parentElement;
  return el ? el.closest<HTMLElement>('[data-sourcepos]') : null;
}

function parseSourcepos(value: string): { blockStart: number; blockEnd: number } | null {
  const [startStr, endStr] = value.split(':');
  if (startStr === undefined || endStr === undefined) return null;
  const blockStart = Number.parseInt(startStr, 10);
  const blockEnd = Number.parseInt(endStr, 10);
  if (Number.isNaN(blockStart) || Number.isNaN(blockEnd)) return null;
  return { blockStart, blockEnd };
}

/**
 * §7.4 steps 2-6. Given a non-collapsed Range, computes a SelectionInput;
 * returns null when no data-sourcepos block can be found, or the selection
 * is empty/collapsed.
 */
export function selectionInputFromRange(range: Range): SelectionInput | null {
  if (range.collapsed) return null;

  let blockEl = findBlockElement(range.commonAncestorContainer);
  let effectiveRange = range;

  if (!blockEl) {
    // Selection spans multiple blocks → fall back to the block on the
    // startContainer side and shrink the selection to fit inside it.
    blockEl = findBlockElement(range.startContainer);
    if (!blockEl) return null;
    if (!blockEl.contains(range.endContainer)) {
      effectiveRange = range.cloneRange();
      effectiveRange.setEnd(blockEl, blockEl.childNodes.length);
    }
  }

  const sourcepos = blockEl.dataset.sourcepos;
  if (!sourcepos) return null;
  const parsed = parseSourcepos(sourcepos);
  if (!parsed) return null;

  const exact = effectiveRange.toString();
  if (!exact) return null;

  // before: the last 64 characters of text preceding the selection start, within the block.
  const beforeRange = effectiveRange.cloneRange();
  beforeRange.selectNodeContents(blockEl);
  beforeRange.setEnd(effectiveRange.startContainer, effectiveRange.startOffset);
  const before = beforeRange.toString().slice(-CONTEXT_CHARS);

  // after: the first 64 characters of text following the selection end, within the block.
  const afterRange = effectiveRange.cloneRange();
  afterRange.selectNodeContents(blockEl);
  afterRange.setStart(effectiveRange.endContainer, effectiveRange.endOffset);
  const after = afterRange.toString().slice(0, CONTEXT_CHARS);

  return {
    block_start: parsed.blockStart,
    block_end: parsed.blockEnd,
    exact,
    before,
    after,
  };
}

/** Convenience wrapper: reads from window.getSelection(); returns null when empty/collapsed (§7.4 step 1). */
export function selectionInputFromSelection(selection: Selection | null): SelectionInput | null {
  if (!selection || selection.rangeCount === 0) return null;
  return selectionInputFromRange(selection.getRangeAt(0));
}
