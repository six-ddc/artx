import { useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { ApiError } from '@/lib/api';
import { usePostBlock, useRawSource } from '@/lib/queries';
import { encodeSource, parseSourcepos, sliceSourceBytes } from '@/lib/source-bytes';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

// Block-level md editing (the Typora/live-preview model): with the Edit tool
// armed, hovering outlines the block under the cursor and a plain click
// opens that block's SOURCE slice, which is written back verbatim — the
// same direct pick-and-edit contract as the html canvas. (An earlier
// gutter-pencil design proved unreachable for fine-grained targets like
// table cells: moving toward the pencil crossed the neighboring cell and
// retargeted it.) The handle is the data-sourcepos byte range goldmark
// already stamps on every block — no ids are ever injected into the md
// file, and there is no HTML→markdown conversion anywhere, so the
// round-trip is exact by construction.

interface BlockEditLayerProps {
  containerRef: RefObject<HTMLDivElement | null>;
  /** The CSS containing block for absolute positioning (same contract as SelectionPopover's positionRef). */
  positionRef: RefObject<HTMLElement | null>;
  docId: string;
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
  const [editing, setEditing] = useState<EditingBlock | null>(null);
  const [text, setText] = useState('');
  const hoveredRef = useRef<HTMLElement | null>(null);

  // data-sourcepos is a BYTE range into the source file; JS strings are
  // UTF-16, so slicing must go through an encoded view.
  const bytes = useMemo(
    () => (rawQuery.data != null ? encodeSource(rawQuery.data) : null),
    [rawQuery.data],
  );

  useEffect(() => {
    const positionEl = positionRef.current;
    const container = containerRef.current;
    if (!positionEl || !container || editing || !bytes) return;

    const setHovered = (el: HTMLElement | null) => {
      if (hoveredRef.current === el) return;
      hoveredRef.current?.classList.remove(HOVER_CLASS);
      hoveredRef.current = el;
      el?.classList.add(HOVER_CLASS);
    };

    function resolveBlock(t: EventTarget | null): HTMLElement | null {
      const block = (t as Element | null)?.closest?.<HTMLElement>('[data-sourcepos]');
      return block && container!.contains(block) ? block : null;
    }

    function onOver(e: Event) {
      setHovered(resolveBlock(e.target));
    }
    function onLeave() {
      setHovered(null);
    }
    function onClick(e: Event) {
      // A drag-selection (copying text out of a block) also ends in a
      // click; only a plain click with a collapsed selection edits.
      const sel = window.getSelection();
      if (sel && !sel.isCollapsed) return;
      const block = resolveBlock(e.target);
      if (!block) return;
      e.preventDefault(); // links inside the block don't navigate while editing
      beginEdit(block);
    }

    positionEl.addEventListener('mouseover', onOver);
    positionEl.addEventListener('mouseleave', onLeave);
    positionEl.addEventListener('click', onClick);
    return () => {
      positionEl.removeEventListener('mouseover', onOver);
      positionEl.removeEventListener('mouseleave', onLeave);
      positionEl.removeEventListener('click', onClick);
      setHovered(null);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [containerRef, positionRef, editing, bytes]);

  function beginEdit(block: HTMLElement) {
    const positionEl = positionRef.current;
    if (!bytes || !positionEl) return;
    const pos = parseSourcepos(block.dataset.sourcepos);
    if (!pos) return;
    const original = sliceSourceBytes(bytes, pos.start, pos.end);
    if (original == null) return;
    block.classList.remove(HOVER_CLASS);
    const top = block.getBoundingClientRect().top - positionEl.getBoundingClientRect().top;
    setText(original);
    setEditing({ start: pos.start, end: pos.end, original, top });
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

  if (!editing) return null;

  const conflict = postBlock.error instanceof ApiError && postBlock.error.status === 409;
  const rows = Math.min(20, Math.max(3, text.split('\n').length + 1));

  return (
    <div
      className="absolute inset-x-0 z-20 rounded-lg border bg-popover p-2.5 shadow-md"
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
          if (e.key === 'Escape') {
            // Only close the editor; swallow the event so the canvas's own
            // Esc handler doesn't also drop the tool back to Read — the
            // next edit should be one click away.
            e.stopPropagation();
            close();
          }
        }}
      />
      <div className="mt-2 flex items-center gap-2">
        <p className="art-mono mr-auto text-xs text-muted-foreground">
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
