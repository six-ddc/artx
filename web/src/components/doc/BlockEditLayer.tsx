import { useEffect, useMemo, useState, type RefObject } from 'react';
import { ApiError } from '@/lib/api';
import { usePostBlock, useRawSource } from '@/lib/queries';
import { encodeSource, parseSourcepos, sliceSourceBytes } from '@/lib/source-bytes';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

// Block-level md editing (the Typora/live-preview model): click a rendered
// block, edit its SOURCE slice, write the slice back verbatim. The handle is
// the data-sourcepos byte range goldmark already stamps on every block — no
// ids are ever injected into the md file, and there is no HTML→markdown
// conversion anywhere, so the round-trip is exact by construction.

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

  // data-sourcepos is a BYTE range into the source file; JS strings are
  // UTF-16, so slicing must go through an encoded view.
  const bytes = useMemo(
    () => (rawQuery.data != null ? encodeSource(rawQuery.data) : null),
    [rawQuery.data],
  );

  useEffect(() => {
    const container = containerRef.current;
    if (!container || editing) return;

    const blockOf = (e: Event): HTMLElement | null => {
      const el = (e.target as Element).closest?.<HTMLElement>('[data-sourcepos]');
      return el && container.contains(el) ? el : null;
    };

    function onOver(e: Event) {
      container!.querySelectorAll(`.${HOVER_CLASS}`).forEach((n) => n.classList.remove(HOVER_CLASS));
      blockOf(e)?.classList.add(HOVER_CLASS);
    }
    function onOut() {
      container!.querySelectorAll(`.${HOVER_CLASS}`).forEach((n) => n.classList.remove(HOVER_CLASS));
    }
    function onClick(e: Event) {
      const block = blockOf(e);
      const positionEl = positionRef.current;
      if (!block || !positionEl || !bytes) return;
      const pos = parseSourcepos(block.dataset.sourcepos);
      if (!pos) return;
      const original = sliceSourceBytes(bytes, pos.start, pos.end);
      if (original == null) return;
      e.preventDefault();
      e.stopPropagation();
      const top = block.getBoundingClientRect().top - positionEl.getBoundingClientRect().top;
      onOut();
      setText(original);
      setEditing({ start: pos.start, end: pos.end, original, top });
    }

    container.addEventListener('mouseover', onOver);
    container.addEventListener('mouseleave', onOut);
    container.addEventListener('click', onClick, true);
    return () => {
      container.removeEventListener('mouseover', onOver);
      container.removeEventListener('mouseleave', onOut);
      container.removeEventListener('click', onClick, true);
      onOut();
    };
  }, [containerRef, positionRef, bytes, editing]);

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
