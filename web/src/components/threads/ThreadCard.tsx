import { useEffect, useRef } from 'react';
import type { Thread } from '@/lib/types';
import { cn } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { RelTime } from '@/components/ui/rel-time';
import { AnchorPreview } from './AnchorPreview';
import { ReplyList } from './ReplyList';
import { ReplyComposer } from './ReplyComposer';
import { ThreadActions } from './StatusActions';

const STATUS_DOT = {
  open: 'bg-status-open',
  addressed: 'bg-status-addressed',
  resolved: 'bg-status-resolved',
} as const;

interface ThreadCardProps {
  docId: string;
  thread: Thread;
  focused: boolean;
  flash?: boolean;
  onFocus: (threadId: string) => void;
}

/**
 * A proper card: status is a dot in the header badge (never a stripe or a
 * filled pill), actions stay hidden until the card is hovered or focused,
 * and a resolved thread dims as a whole.
 */
export function ThreadCard({ docId, thread, focused, flash, onFocus }: ThreadCardProps) {
  const resolved = thread.status === 'resolved';
  const ref = useRef<HTMLDivElement>(null);

  // The sidebar half of the two-way focus link: focusing from the prose
  // (clicking a highlight) scrolls this card into view, mirroring how
  // HighlightLayer scrolls the prose when focusing from the sidebar.
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [focused]);

  return (
    <div
      ref={ref}
      id={`thread-${thread.thread}`}
      onClick={() => onFocus(thread.thread)}
      className={cn(
        'group cursor-pointer space-y-2.5 rounded-lg border bg-card p-3 shadow-xs transition-colors',
        resolved && 'opacity-60',
        focused ? 'border-ring ring-1 ring-ring' : 'hover:border-ring/50',
        flash && 'art-thread-flash',
      )}
    >
      {/* Fixed-height header row: the hover-revealed action cluster is
          taller than the text line, so the row reserves its height up front
          instead of jumping on hover. No author here — the event log still
          records one, but the default identity chain gives every writer
          (browser and CLI alike) the same name, so displaying it is noise. */}
      <div className="flex h-7 items-center gap-2">
        {/* Status is a bare dot — a text label would just repeat the active
            filter tab on every card. Under "All" the color differentiates
            (title carries the word for hover); orphan keeps its text badge
            since it's the one state worth spelling out. Display-layer
            decision only: filter/counts still go by thread.status, so
            orphan threads keep showing up under the open filter as usual. */}
        <span
          className={cn('size-2 shrink-0 rounded-full', STATUS_DOT[thread.status])}
          title={thread.status}
          aria-label={thread.status}
        />
        <RelTime date={thread.created_at} className="text-xs text-muted-foreground" />
        {thread.anchor.orphan && <Badge variant="orphan">orphan</Badge>}
        <div
          className="invisible ml-auto shrink-0 focus-within:visible group-hover:visible has-[[data-state=open]]:visible"
          onClick={(e) => e.stopPropagation()}
        >
          <ThreadActions docId={docId} thread={thread} />
        </div>
      </div>

      <AnchorPreview anchor={thread.anchor} hint={thread.hint} />

      <p className="whitespace-pre-wrap text-sm leading-relaxed text-foreground">{thread.body}</p>

      {/* The status dot already says "addressed" — this line exists only for
          the extra facts (commit, note), muted like any other metadata, and
          disappears entirely when there are none. */}
      {thread.addressed && (thread.addressed.commit || thread.addressed.note) && (
        <p className="text-xs text-muted-foreground">
          {thread.addressed.commit && (
            <span className="art-mono">{thread.addressed.commit.slice(0, 7)}</span>
          )}
          {thread.addressed.commit && thread.addressed.note && ' · '}
          {thread.addressed.note}
        </p>
      )}

      {/* The contract field replies is typed as required Reply[], but the backend occasionally sends null; the frozen type doesn't change, so we guard here. */}
      <ReplyList replies={thread.replies ?? []} />

      <div onClick={(e) => e.stopPropagation()}>
        <ReplyComposer docId={docId} threadId={thread.thread} />
      </div>
    </div>
  );
}
