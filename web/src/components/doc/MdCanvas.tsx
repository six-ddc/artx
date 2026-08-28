import { useEffect, useRef, useState, type MouseEvent } from 'react';
import { BookOpen, MessageSquarePlus, Pencil } from 'lucide-react';
import type { DocDetail, SelectionInput, Thread } from '@/lib/types';
import { BlockEditLayer } from './BlockEditLayer';
import { HighlightLayer } from './HighlightLayer';
import { SelectionPopover } from './SelectionPopover';
import { MermaidMath } from './MermaidMath';
import { ToolPill, type ToolPillItem } from './ToolPill';

// The md canvas mirrors the html canvas's cursor-tool pill (the user-facing
// contract: both doc types offer the same three buttons). Read is the
// default; Comment arms the selection popover; Edit arms the gutter pencil.
// Comment is one-shot — handing a selection to the composer retires the
// tool — while Edit stays armed for consecutive block edits; Esc always
// returns to Read.
type MdTool = 'read' | 'comment' | 'edit';

const TOOLS: ToolPillItem<MdTool>[] = [
  { tool: 'read', label: 'Read', icon: BookOpen },
  { tool: 'comment', label: 'Comment on a selection', icon: MessageSquarePlus },
  { tool: 'edit', label: 'Edit a block', icon: Pencil },
];

interface MdCanvasProps {
  doc: DocDetail;
  docId: string;
  threads: Thread[];
  readOnly: boolean;
  focusedThreadId?: string;
  onFocusThread: (threadId: string) => void;
  onStartComment: (selection: SelectionInput, range: Range) => void;
}

export function MdCanvas({
  doc,
  docId,
  threads,
  readOnly,
  focusedThreadId,
  onFocusThread,
  onStartComment,
}: MdCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  // SelectionPopover's positioning origin: its nearest position:relative
  // ancestor. Must be passed separately — it can't reuse containerRef, see
  // the detailed note in SelectionPopover.tsx.
  const positionRef = useRef<HTMLDivElement>(null);
  const [tool, setTool] = useState<MdTool>('read');
  const activeTool: MdTool = readOnly ? 'read' : tool;

  useEffect(() => {
    if (activeTool === 'read') return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setTool('read');
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [activeTool]);

  // The reverse of HighlightLayer's echo: clicking a painted anchor focuses
  // its thread in the drawer. Delegated, since the marks are re-created on
  // every highlight pass and can't carry React handlers of their own. The
  // selector is attribute-only (not mark[...]): approx/orphan anchors carry
  // data-art-thread on the whole block, not on a <mark>.
  function onProseClick(e: MouseEvent<HTMLDivElement>) {
    if (activeTool === 'edit') return; // a click means "edit this block" there
    const mark = (e.target as Element).closest?.('[data-art-thread]');
    const threadId = mark?.getAttribute('data-art-thread');
    if (threadId) onFocusThread(threadId);
  }

  function startComment(selection: SelectionInput, range: Range) {
    onStartComment(selection, range);
    setTool('read'); // one-shot, matching the html comment picker
  }

  return (
    // The page below the topbar IS the sheet — no paper box around the
    // prose. padding must live on the same element as position:relative —
    // the containing block for the absolutely positioned children
    // (SelectionPopover, BlockEditLayer) is this element's padding box, so
    // the "offset relative to containerRef" they compute internally lines
    // up with this layer's content area. Don't add another wrapping div.
    <div ref={positionRef} className="relative mx-auto w-full max-w-[46rem] px-6 py-10 sm:px-8 sm:py-14">
      <div
        ref={containerRef}
        className="art-prose mx-auto"
        onClick={onProseClick}
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
      {activeTool === 'comment' && (
        <SelectionPopover
          containerRef={containerRef}
          positionRef={positionRef}
          onStartComment={startComment}
        />
      )}
      {activeTool === 'edit' && (
        <BlockEditLayer containerRef={containerRef} positionRef={positionRef} docId={docId} />
      )}
      <MermaidMath
        containerRef={containerRef}
        hasMermaid={Boolean(doc.has_mermaid)}
        hasMath={Boolean(doc.has_math)}
        html={doc.html}
      />
      {!readOnly && <ToolPill tools={TOOLS} active={activeTool} onSelect={setTool} />}
    </div>
  );
}
