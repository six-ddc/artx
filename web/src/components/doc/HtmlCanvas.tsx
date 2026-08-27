import { useEffect, useRef, useState } from 'react';
import { Check, X } from 'lucide-react';
import type { DocDetail, Thread } from '@/lib/types';
import type { HoverMsg, PickMsg } from '@/lib/protocol';
import { usePostElement } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { useFrameBridge } from './frame-bridge';
import { HoverOutline } from './HoverOutline';
import { ElementPopover } from './ElementPopover';
import { ReviewModeBar } from './ReviewModeBar';

export type HtmlCanvasMode = 'browse' | 'review' | 'edit';

interface HtmlCanvasProps {
  doc: DocDetail;
  docId: string;
  threads: Thread[];
  mode: HtmlCanvasMode;
  focusedThreadId?: string;
}

/** The html artifact sits in a sandboxed iframe, talking postMessage with the injected reviewer.ts (§7.5). */
export function HtmlCanvas({ doc, docId, threads, mode, focusedThreadId }: HtmlCanvasProps) {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const { lastMessage, postToFrame } = useFrameBridge(iframeRef);
  const [height, setHeight] = useState(480);
  const [hover, setHover] = useState<HoverMsg | null>(null);
  const [pick, setPick] = useState<PickMsg | null>(null);
  const [saveNotice, setSaveNotice] = useState<'saved' | 'error' | null>(null);
  const postElement = usePostElement(docId);

  useEffect(() => {
    if (!saveNotice) return;
    const t = window.setTimeout(() => setSaveNotice(null), 2000);
    return () => window.clearTimeout(t);
  }, [saveNotice]);

  const aidsWithThreads = threads
    .filter((t) => t.anchor.kind === 'element' && t.anchor.aid)
    .map((t) => t.anchor.aid as string);

  useEffect(() => {
    if (!lastMessage) return;
    switch (lastMessage.type) {
      case 'ready': {
        // Script just became ready: push the current mode/highlight/pending focused thread in one shot.
        postToFrame({ type: 'mode', mode });
        postToFrame({ type: 'highlight', aids: aidsWithThreads });
        const focused = threads.find((t) => t.thread === focusedThreadId);
        if (focused?.anchor.aid) postToFrame({ type: 'scrollTo', aid: focused.anchor.aid });
        break;
      }
      case 'hover':
        setHover(lastMessage.aid ? lastMessage : null);
        break;
      case 'pick':
        // In edit mode, reviewer.ts has already turned the element into
        // contenteditable within the same click; popping the comment
        // popover here too would let its autoFocus textarea immediately
        // steal focus, blurring the contenteditable element and breaking
        // editing outright. The comment popover only opens in review mode.
        if (mode === 'review') setPick(lastMessage);
        break;
      case 'size':
        setHeight(Math.max(200, lastMessage.height));
        break;
      case 'edit':
        postElement.mutate(
          { aid: lastMessage.aid, html: lastMessage.html },
          {
            onSuccess: () => setSaveNotice('saved'),
            onError: () => setSaveNotice('error'),
          },
        );
        break;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastMessage]);

  useEffect(() => {
    postToFrame({ type: 'mode', mode });
    if (mode !== 'review') {
      setHover(null);
      setPick(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

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

  return (
    <div className="art-paper overflow-hidden">
      {mode === 'review' && <ReviewModeBar />}
      <div className="relative">
        <iframe
          ref={iframeRef}
          src={doc.raw_url}
          sandbox="allow-scripts"
          title={doc.title}
          style={{ height, width: '100%', border: 0, display: 'block' }}
        />
        {hover?.rect && <HoverOutline rect={hover.rect} tag={hover.tag} />}
        {pick && <ElementPopover docId={docId} pick={pick} onClose={() => setPick(null)} />}
        {saveNotice && (
          <div
            className={cn(
              'art-mono pointer-events-none absolute bottom-2 right-2 z-20 flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium text-white shadow',
              saveNotice === 'saved' ? 'bg-resolved' : 'bg-danger',
            )}
          >
            {saveNotice === 'saved' ? <Check className="size-3.5" /> : <X className="size-3.5" />}
            {saveNotice === 'saved' ? 'Saved' : 'Save failed'}
          </div>
        )}
      </div>
    </div>
  );
}
