import { useEffect, useState, type RefObject } from 'react';
import { MessageSquarePlus } from 'lucide-react';
import { selectionInputFromRange } from '@/lib/selection';
import type { SelectionInput } from '@/lib/types';
import { Button } from '@/components/ui/button';

interface SelectionPopoverProps {
  /** Used for the selection hit-test ("is the selection inside the document") — not for the positioning math. */
  containerRef: RefObject<HTMLDivElement | null>;
  /**
   * The coordinate origin for the positioning math: the popover's actual
   * CSS containing block (its nearest position:relative ancestor).
   * **Cannot just reuse containerRef** — containerRef points at
   * `.art-prose`, which has no padding/border of its own, so its first
   * child's margin (e.g. an h1's margin-top) collapses through it, pushing
   * its "measured top" tens of pixels below the popover's real positioning
   * origin (measured: when the document starts with an h1, the drift ≈
   * padding-top + h1.margin-top).
   */
  positionRef: RefObject<HTMLElement | null>;
  /** Fires with the collected anchor input; the composer itself lives in the thread sidebar (§7.2/§7.4). */
  onStartComment: (selection: SelectionInput, range: Range) => void;
}

interface AnchorPoint {
  top: number;
  left: number;
}

/** A selection surfaces a "Comment" trigger; clicking it hands the anchor off to the sidebar composer. */
export function SelectionPopover({ containerRef, positionRef, onStartComment }: SelectionPopoverProps) {
  const [range, setRange] = useState<Range | null>(null);
  const [anchor, setAnchor] = useState<AnchorPoint | null>(null);

  useEffect(() => {
    function onSelectionChange() {
      const container = containerRef.current;
      const positionEl = positionRef.current;
      const sel = window.getSelection();
      if (!container || !positionEl || !sel || sel.isCollapsed || sel.rangeCount === 0) {
        setRange(null);
        setAnchor(null);
        return;
      }
      const r = sel.getRangeAt(0);
      if (!container.contains(r.commonAncestorContainer)) {
        setRange(null);
        setAnchor(null);
        return;
      }
      const selRect = r.getBoundingClientRect();
      if (selRect.width === 0 && selRect.height === 0) return;
      const originRect = positionEl.getBoundingClientRect();
      setRange(r.cloneRange());
      setAnchor({
        top: selRect.top - originRect.top - 8,
        left: selRect.left - originRect.left + selRect.width / 2,
      });
    }
    document.addEventListener('selectionchange', onSelectionChange);
    return () => document.removeEventListener('selectionchange', onSelectionChange);
  }, [containerRef, positionRef]);

  function start() {
    if (!range) return;
    const selection = selectionInputFromRange(range);
    if (!selection) return;
    onStartComment(selection, range);
    // The native selection has served its purpose; clearing it avoids a
    // double echo next to the pending highlight the parent paints.
    window.getSelection()?.removeAllRanges();
    setRange(null);
    setAnchor(null);
  }

  if (!anchor) return null;

  return (
    <div
      className="art-pop-in absolute z-20 -translate-x-1/2 -translate-y-full"
      style={{ top: anchor.top, left: anchor.left }}
    >
      <Button size="sm" onMouseDown={(e) => e.preventDefault()} onClick={start}>
        <MessageSquarePlus className="size-3.5" />
        Comment
      </Button>
    </div>
  );
}
