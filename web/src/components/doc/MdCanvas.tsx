import { useRef } from 'react';
import type { DocDetail, Thread } from '@/lib/types';
import { HighlightLayer } from './HighlightLayer';
import { SelectionPopover } from './SelectionPopover';
import { MermaidMath } from './MermaidMath';
import { ReviewModeBar } from './ReviewModeBar';

interface MdCanvasProps {
  doc: DocDetail;
  docId: string;
  threads: Thread[];
  reviewMode: boolean;
  readOnly: boolean;
  focusedThreadId?: string;
}

export function MdCanvas({ doc, docId, threads, reviewMode, readOnly, focusedThreadId }: MdCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  // SelectionPopover's positioning origin: its nearest position:relative
  // ancestor. Must be passed separately — it can't reuse containerRef, see
  // the detailed note in SelectionPopover.tsx.
  const positionRef = useRef<HTMLDivElement>(null);
  const active = reviewMode && !readOnly;

  return (
    // "Paper": the document body floats above the desk color; a marker
    // amber bar appears at the top when review mode is on.
    <div className="art-paper">
      {/* No overflow-hidden: SelectionPopover sometimes floats above the
          selection, and clipping overflow would cut off the comment popover
          near the top of the document. */}
      {active && <ReviewModeBar />}
      {/* padding must live on the same element as position:relative — the
          containing block for the absolutely positioned child
          (SelectionPopover) is this element's padding box, so the "offset
          relative to containerRef" it computes internally lines up with this
          layer's content area. Don't add another wrapping div. */}
      <div ref={positionRef} className="relative px-8 py-10 sm:px-14 sm:py-14">
        <div
          ref={containerRef}
          className="art-prose mx-auto"
          // R1 (hard rule): the single source of truth for md rendering is
          // goldmark on the Go side; DocDetail.html already carries
          // data-sourcepos. The frontend only does dangerouslySetInnerHTML —
          // never re-renders markdown.
          dangerouslySetInnerHTML={{ __html: doc.html ?? '' }}
        />
        <HighlightLayer
          containerRef={containerRef}
          threads={threads}
          focusedThreadId={focusedThreadId}
          html={doc.html}
        />
        {active && (
          <SelectionPopover containerRef={containerRef} positionRef={positionRef} docId={docId} />
        )}
        <MermaidMath
          containerRef={containerRef}
          hasMermaid={Boolean(doc.has_mermaid)}
          hasMath={Boolean(doc.has_math)}
          html={doc.html}
        />
      </div>
    </div>
  );
}
