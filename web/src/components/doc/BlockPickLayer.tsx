import { useEffect, useRef, type RefObject } from 'react';
import { parseSourcepos } from '@/lib/source-bytes';
import type { SelectionInput } from '@/lib/types';

// Block picking for the md Comment tool — the counterpart of the html
// canvas's element picker: hovering shows a marker outline on the block
// (table cell, paragraph, list item…) under the cursor, and a plain click
// comments on the whole block. Drag-selection keeps working alongside it
// (SelectionPopover); a click that merely dismisses a selection never picks.
// The wire format is the ordinary SelectionInput with an empty `exact` —
// the server's documented "anchor the whole block" path (anchor.FromSelection
// returns the block-range anchor with Approx=true for an empty quote).

const HOVER_CLASS = 'art-pick-hover';

interface BlockPickLayerProps {
  containerRef: RefObject<HTMLDivElement | null>;
  positionRef: RefObject<HTMLElement | null>;
  onPickBlock: (selection: SelectionInput, range: Range) => void;
}

export function BlockPickLayer({ containerRef, positionRef, onPickBlock }: BlockPickLayerProps) {
  const hoveredRef = useRef<HTMLElement | null>(null);
  // Whether a non-collapsed selection existed at mousedown: that click is
  // the one that dismisses the selection (or ends the drag), not a pick.
  const wasSelectingRef = useRef(false);

  useEffect(() => {
    const positionEl = positionRef.current;
    const container = containerRef.current;
    if (!positionEl || !container) return;

    const setHovered = (el: HTMLElement | null) => {
      if (hoveredRef.current === el) return;
      hoveredRef.current?.classList.remove(HOVER_CLASS);
      hoveredRef.current = el;
      el?.classList.add(HOVER_CLASS);
    };

    function resolveBlock(t: EventTarget | null): HTMLElement | null {
      const el = t as Element | null;
      // Clicks on painted anchors keep their focus-the-thread meaning, and
      // links keep navigating; neither reads as a block pick.
      if (el?.closest?.('[data-art-thread], a')) return null;
      const block = el?.closest?.<HTMLElement>('[data-sourcepos]');
      return block && container!.contains(block) ? block : null;
    }

    function onOver(e: Event) {
      setHovered(resolveBlock(e.target));
    }
    function onLeave() {
      setHovered(null);
    }
    function onMouseDown() {
      const sel = window.getSelection();
      wasSelectingRef.current = Boolean(sel && !sel.isCollapsed);
    }
    function onClick(e: Event) {
      const sel = window.getSelection();
      if (wasSelectingRef.current || (sel && !sel.isCollapsed)) return;
      const block = resolveBlock(e.target);
      if (!block) return;
      const pos = parseSourcepos(block.dataset.sourcepos);
      if (!pos) return;
      const range = document.createRange();
      range.selectNodeContents(block);
      setHovered(null);
      onPickBlock(
        { block_start: pos.start, block_end: pos.end, exact: '', before: '', after: '' },
        range,
      );
    }

    positionEl.addEventListener('mouseover', onOver);
    positionEl.addEventListener('mouseleave', onLeave);
    positionEl.addEventListener('mousedown', onMouseDown);
    positionEl.addEventListener('click', onClick);
    return () => {
      positionEl.removeEventListener('mouseover', onOver);
      positionEl.removeEventListener('mouseleave', onLeave);
      positionEl.removeEventListener('mousedown', onMouseDown);
      positionEl.removeEventListener('click', onClick);
      setHovered(null);
    };
  }, [containerRef, positionRef, onPickBlock]);

  return null;
}
