import { useEffect, useMemo, useState, type RefObject } from 'react';
import { Pencil } from 'lucide-react';
import { ApiError } from '@/lib/api';
import { usePostBlock, useRawSource } from '@/lib/queries';
import { encodeSource, parseSourcepos, sliceSourceBytes } from '@/lib/source-bytes';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

// Block-level md editing (the Typora/live-preview model): a pencil surfaces
// in the left gutter of the hovered block; clicking it edits that block's
// SOURCE slice, which is written back verbatim. The handle is the
// data-sourcepos byte range goldmark already stamps on every block — no ids
// are ever injected into the md file, and there is no HTML→markdown
// conversion anywhere, so the round-trip is exact by construction.
//
// There is no edit mode: the pencil is the entire entry point, so plain
// clicks in the prose keep their browse meaning (links navigate, anchor
// highlights focus their thread).

interface BlockEditLayerProps {
  containerRef: RefObject<HTMLDivElement | null>;
  /** The CSS containing block for absolute positioning (same contract as SelectionPopover's positionRef). */
  positionRef: RefObject<HTMLElement | null>;
  docId: string;
}

interface HoverTarget {
  el: HTMLElement;
  start: number;
  end: number;
  top: number;
  left: number;
}

interface EditingBlock {
  start: number;
  end: number;
  original: string;
  top: number;
}

const HOVER_CLASS = 'art-edit-hover';

export function BlockEditLayer({ containerRef, positionRef, docId }: BlockEditLayerProps) {
  const rawQuery = useRawSource(docId, true);
  const postBlock = usePostBlock(docId);
  const [target, setTarget] = useState<HoverTarget | null>(null);
  const [editing, setEditing] = useState<EditingBlock | null>(null);
  const [text, setText] = useState('');

  // data-sourcepos is a BYTE range into the source file; JS strings are
  // UTF-16, so slicing must go through an encoded view.
  const bytes = useMemo(
    () => (rawQuery.data != null ? encodeSource(rawQuery.data) : null),
    [rawQuery.data],
  );

  // The hover listeners live on positionEl, not the prose container: the
  // pencil sits in the gutter outside the prose, and moving the pointer
  // onto it must not read as "left the block".
  useEffect(() => {
    const positionEl = positionRef.current;
    const container = containerRef.current;
    if (!positionEl || !container || editing) return;

    function onOver(e: Event) {
      const t = e.target as Element;
      if (t.closest?.('[data-art-block-pencil]')) return; // on the pencil: keep the current target
      const block = t.closest?.<HTMLElement>('[data-sourcepos]');
      if (!block || !container!.contains(block)) {
        // The pointer is over the gutter / a gap between blocks — the path
        // it must cross to REACH the pencil. Keep the current target, or
        // the pencil unmounts right before the click lands (Notion-style
        // handles have exactly this behavior); mouseleave on the whole
        // canvas is what actually clears it.
        return;
      }
      const pos = parseSourcepos(block.dataset.sourcepos);
      if (!pos) {
        setTarget(null);
        return;
      }
      const posRect = positionEl!.getBoundingClientRect();
      const rect = block.getBoundingClientRect();
      const next: HoverTarget = {
        el: block,
        start: pos.start,
        end: pos.end,
        top: rect.top - posRect.top,
        left: rect.left - posRect.left - 36,
      };
      // mouseover fires on every element boundary inside the block; only
      // re-render when the resolved block actually changed.
      setTarget((prev) =>
        prev && prev.el === next.el && prev.top === next.top ? prev : next,
      );
    }
    function onLeave() {
      setTarget(null);
    }

    positionEl.addEventListener('mouseover', onOver);
    positionEl.addEventListener('mouseleave', onLeave);
    return () => {
      positionEl.removeEventListener('mouseover', onOver);
      positionEl.removeEventListener('mouseleave', onLeave);
      container.querySelectorAll(`.${HOVER_CLASS}`).forEach((n) => n.classList.remove(HOVER_CLASS));
    };
  }, [containerRef, positionRef, editing]);

  function beginEdit() {
    if (!target || !bytes) return;
    const original = sliceSourceBytes(bytes, target.start, target.end);
    if (original == null) return;
    target.el.classList.remove(HOVER_CLASS);
    setText(original);
    setEditing({ start: target.start, end: target.end, original, top: target.top });
    setTarget(null);
  }

  function close() {
    setEditing(null);
    setText('');
    postBlock.reset();
  }

  function save() {
    if (!editing) return;
    postBlock.mutate(
      { start: editing.start, end: editing.end, original: editing.original, content: text },
      { onSuccess: close },
    );
  }

  if (!editing) {
    if (!target || !bytes) return null;
    return (
      <button
        type="button"
        data-art-block-pencil
        title="Edit block"
        onClick={beginEdit}
        // The amber outline previews exactly which block the pencil will
        // edit — only while the pencil itself is hovered, so plain reading
        // stays quiet.
        onMouseEnter={() => target.el.classList.add(HOVER_CLASS)}
        onMouseLeave={() => target.el.classList.remove(HOVER_CLASS)}
        className="absolute z-10 flex size-7 items-center justify-center rounded text-ink-3 transition-colors hover:bg-hover hover:text-ink"
        style={{ top: target.top, left: target.left }}
      >
        <Pencil className="size-3.5" />
      </button>
    );
  }

  const conflict = postBlock.error instanceof ApiError && postBlock.error.status === 409;
  const rows = Math.min(20, Math.max(3, text.split('\n').length + 1));

  return (
    <div
      className="absolute inset-x-0 z-20 rounded border border-marker bg-sheet p-2 shadow-lg"
      style={{ top: editing.top }}
    >
      <Textarea
        autoFocus
        rows={rows}
        className="art-mono text-[13px] leading-relaxed"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            save();
          }
          if (e.key === 'Escape') close();
        }}
      />
      <div className="mt-2 flex items-center gap-2">
        <p className="art-mono mr-auto text-[11px] text-ink-3">
          {conflict
            ? 'Document changed since it was rendered — close and retry.'
            : postBlock.isError
              ? `Save failed: ${postBlock.error.message}`
              : `source bytes ${editing.start}–${editing.end}`}
        </p>
        <Button variant="ghost" size="sm" onClick={close}>
          Cancel
        </Button>
        <Button size="sm" disabled={postBlock.isPending || text === editing.original} onClick={save}>
          {postBlock.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  );
}
