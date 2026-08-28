import { useEffect, useRef, useState } from 'react';
import { Check, MessageSquarePlus, MousePointer2, Pencil, X } from 'lucide-react';
import type { DocDetail, Thread } from '@/lib/types';
import type { HoverMsg, PickMsg } from '@/lib/protocol';
import { usePostElement } from '@/lib/queries';
import { useFrameBridge } from './frame-bridge';
import { HoverOutline } from './HoverOutline';
import { ElementPopover } from './ElementPopover';
import { ToolPill, type ToolPillItem } from './ToolPill';

export type HtmlCanvasMode = 'browse' | 'review' | 'edit';

// An html artifact is an arbitrary interactive page, so a plain click is
// genuinely ambiguous (trigger the page / comment / edit). The cursor tool
// resolves it, scoped to this canvas only — like devtools' element picker,
// not a page-wide mode: Interact is the default, Comment is one-shot (it
// snaps back after use), Edit stays armed for consecutive edits, and Esc
// always returns to Interact. Alt+click is the no-tool-switch shortcut for
// commenting.
type CanvasTool = 'interact' | 'comment' | 'edit';

/** tool → reviewer-protocol mode (the frozen protocol keeps its historical mode names). */
const TOOL_MODE: Record<CanvasTool, HtmlCanvasMode> = {
  interact: 'browse',
  comment: 'review',
  edit: 'edit',
};

const TOOLS: ToolPillItem<CanvasTool>[] = [
  { tool: 'interact', label: 'Interact with the page', icon: MousePointer2 },
  { tool: 'comment', label: 'Comment on an element (or Alt+click anytime)', icon: MessageSquarePlus },
  { tool: 'edit', label: 'Edit an element', icon: Pencil },
];

interface HtmlCanvasProps {
  doc: DocDetail;
  docId: string;
  threads: Thread[];
  readOnly: boolean;
  focusedThreadId?: string;
}

/** The html artifact sits in a sandboxed iframe, talking postMessage with the injected reviewer.ts (§7.5). */
export function HtmlCanvas({ doc, docId, threads, readOnly, focusedThreadId }: HtmlCanvasProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(480);
  const [tool, setTool] = useState<CanvasTool>('interact');
  const [hover, setHover] = useState<HoverMsg | null>(null);
  const [pick, setPick] = useState<PickMsg | null>(null);
  const [saveNotice, setSaveNotice] = useState<'saved' | 'error' | null>(null);
  const postElement = usePostElement(docId);

  const mode: HtmlCanvasMode = readOnly ? 'browse' : TOOL_MODE[tool];

  useEffect(() => {
    if (!saveNotice) return;
    const t = window.setTimeout(() => setSaveNotice(null), 2000);
    return () => window.clearTimeout(t);
  }, [saveNotice]);

  // Same highlight lifecycle as the md canvas: resolved threads carry no
  // standing highlight, but focusing one re-lights its element.
  const aidsWithThreads = threads
    .filter(
      (t) =>
        t.anchor.kind === 'element' &&
        t.anchor.aid &&
        (t.status !== 'resolved' || t.thread === focusedThreadId),
    )
    .map((t) => t.anchor.aid as string);

  // Direct per-message delivery (see frame-bridge.ts: a lastMessage state
  // drops messages when Firefox delivers a burst in one task).
  const { postToFrame } = useFrameBridge(iframeRef, (msg) => {
    switch (msg.type) {
      case 'ready': {
        // Script just became ready: push the current mode/highlight/pending focused thread in one shot.
        postToFrame({ type: 'mode', mode });
        postToFrame({ type: 'highlight', aids: aidsWithThreads });
        const focused = threads.find((t) => t.thread === focusedThreadId);
        if (focused?.anchor.aid) postToFrame({ type: 'scrollTo', aid: focused.anchor.aid });
        break;
      }
      case 'hover':
        setHover(msg.aid ? msg : null);
        break;
      case 'pick':
        // The reviewer only sends picks for the comment tool or an
        // Alt+click, never while editing (see the note in reviewer.ts about
        // the popover's autoFocus stealing the contenteditable's focus).
        setPick(msg);
        break;
      case 'size':
        setHeight(Math.max(200, msg.height));
        break;
      case 'edit':
        postElement.mutate(
          { aid: msg.aid, html: msg.html },
          {
            onSuccess: () => setSaveNotice('saved'),
            onError: () => setSaveNotice('error'),
          },
        );
        // Edit stays armed after a commit (matching the md canvas), so a
        // pass over many elements doesn't require re-picking the tool every
        // time; Esc or the pill hands the cursor back to Interact.
        break;
    }
  });

  useEffect(() => {
    postToFrame({ type: 'mode', mode });
    if (mode !== 'review') setHover(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  // Esc drops the picker back to Interact (when focus is on the shell page).
  useEffect(() => {
    if (tool === 'interact') return;
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setTool('interact');
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [tool]);

  useEffect(() => {
    postToFrame({ type: 'highlight', aids: aidsWithThreads });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [aidsWithThreads.join(',')]);

  useEffect(() => {
    if (!focusedThreadId) return;
    const aid = threads.find((t) => t.thread === focusedThreadId)?.anchor.aid;
    if (aid) postToFrame({ type: 'scrollTo', aid });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusedThreadId]);

  function closePick() {
    setPick(null);
    // One-shot: after the comment popover closes, the picker retires.
    if (tool === 'comment') setTool('interact');
  }

  return (
    <div className="relative">
      <iframe
        ref={iframeRef}
        src={doc.raw_url}
        sandbox="allow-scripts"
        title={doc.title}
        style={{ height, width: '100%', border: 0, display: 'block' }}
      />
      {hover?.rect && <HoverOutline rect={hover.rect} tag={hover.tag} />}
      {pick && <ElementPopover docId={docId} pick={pick} onClose={closePick} />}
      {saveNotice && (
        <div
          className="pointer-events-none absolute bottom-2 right-2 z-20 flex items-center gap-1.5 rounded-full border bg-popover px-2.5 py-1 text-xs font-medium text-popover-foreground shadow-md"
        >
          {saveNotice === 'saved' ? (
            <Check className="size-3.5 text-status-resolved" />
          ) : (
            <X className="size-3.5 text-destructive" />
          )}
          {saveNotice === 'saved' ? 'Saved' : 'Save failed'}
        </div>
      )}
      {!readOnly && <ToolPill tools={TOOLS} active={tool} onSelect={setTool} />}
    </div>
  );
}
