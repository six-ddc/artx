import { useEffect, useState, type RefObject } from 'react';
import { MessageSquarePlus, X } from 'lucide-react';
import { selectionInputFromRange } from '@/lib/selection';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

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
  docId: string;
}

interface AnchorPoint {
  top: number;
  left: number;
}

/** A selection surfaces a "Comment" button → expands into a composer → POST create (§7.2/§7.4). */
export function SelectionPopover({ containerRef, positionRef, docId }: SelectionPopoverProps) {
  const [range, setRange] = useState<Range | null>(null);
  const [anchor, setAnchor] = useState<AnchorPoint | null>(null);
  const [composing, setComposing] = useState(false);
  const [body, setBody] = useState('');
  const postEvent = usePostEvent(docId);

  useEffect(() => {
    function onSelectionChange() {
      if (composing) return; // Don't let the browser's selection change interrupt an in-progress comment
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
  }, [containerRef, positionRef, composing]);

  function closeAll() {
    setRange(null);
    setAnchor(null);
    setComposing(false);
    setBody('');
  }

  function submit() {
    if (!range || !body.trim()) return;
    const selection = selectionInputFromRange(range);
    if (!selection) return;
    postEvent.mutate({ type: 'create', body: body.trim(), selection }, { onSuccess: closeAll });
  }

  if (!anchor) return null;

  return (
    <div
      className="art-pop-in absolute z-20 -translate-x-1/2 -translate-y-full"
      style={{ top: anchor.top, left: anchor.left }}
    >
      {!composing ? (
        <Button size="sm" onMouseDown={(e) => e.preventDefault()} onClick={() => setComposing(true)}>
          <MessageSquarePlus className="size-3.5" />
          Comment
        </Button>
      ) : (
        <div
          className="w-72 rounded border border-line bg-sheet p-2 shadow-lg"
          onMouseDown={(e) => e.preventDefault()}
        >
          <Textarea
            autoFocus
            rows={3}
            placeholder="Add a comment…"
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
          <div className="mt-2 flex items-center justify-end gap-1.5">
            <Button variant="ghost" size="sm" onClick={closeAll}>
              <X className="size-3.5" />
            </Button>
            <Button size="sm" disabled={!body.trim() || postEvent.isPending} onClick={submit}>
              {postEvent.isPending ? 'Submitting…' : 'Submit'}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
