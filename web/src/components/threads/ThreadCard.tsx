import type { Thread } from '@/lib/types';
import { cn, formatDateTime } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { AnchorPreview } from './AnchorPreview';
import { ReplyList } from './ReplyList';
import { ReplyComposer } from './ReplyComposer';
import { StatusActions } from './StatusActions';

const STATUS_VARIANT = {
  open: 'open',
  addressed: 'addressed',
  resolved: 'resolved',
} as const;

interface ThreadCardProps {
  docId: string;
  thread: Thread;
  focused: boolean;
  flash?: boolean;
  onFocus: (threadId: string) => void;
}

/**
 * Marginalia: a stack of hairline-separated rows, not boxes or color bars —
 * status is carried by AnchorPreview's highlighter background; the only
 * thing this component does is dim the whole card when resolved.
 */
export function ThreadCard({ docId, thread, focused, flash, onFocus }: ThreadCardProps) {
  const resolved = thread.status === 'resolved';

  return (
    <div
      id={`thread-${thread.thread}`}
      onClick={() => onFocus(thread.thread)}
      className={cn(
        'cursor-pointer space-y-2 border-b border-line px-3 py-3 transition-colors',
        resolved && 'opacity-60',
        focused ? 'bg-hover' : 'hover:bg-hover',
        flash && 'art-thread-flash',
      )}
    >
      <div className="flex items-center justify-between gap-2">
        {/* Display-layer decision only, doesn't touch the state machine:
            the badge shows ORPHAN instead of OPEN when orphaned, so it isn't
            mistaken for just another open thread at a glance next to other
            cards. The filter/counts still go by thread.status, so orphan
            threads keep showing up under the open filter as usual. */}
        {thread.anchor.orphan ? (
          <Badge variant="orphan">orphan</Badge>
        ) : (
          <Badge variant={STATUS_VARIANT[thread.status]}>{thread.status}</Badge>
        )}
        <span className="art-mono text-[11px] text-ink-3">{formatDateTime(thread.created_at)}</span>
      </div>

      <AnchorPreview anchor={thread.anchor} status={thread.status} hint={thread.hint} />

      <div>
        <p className="art-mono text-[11px] font-medium text-ink-2">{thread.author}</p>
        <p className="whitespace-pre-wrap text-sm text-ink">{thread.body}</p>
      </div>

      {thread.addressed && (
        <p className="art-mono text-[11px] text-addressed">
          Addressed by {thread.addressed.by}
          {thread.addressed.commit && <> · {thread.addressed.commit.slice(0, 7)}</>}
          {thread.addressed.note && <> · {thread.addressed.note}</>}
        </p>
      )}

      {/* The contract field replies is typed as required Reply[], but the backend occasionally sends null; the frozen type doesn't change, so we guard here. */}
      <ReplyList replies={thread.replies ?? []} />

      <div className="space-y-2 pt-1" onClick={(e) => e.stopPropagation()}>
        <ReplyComposer docId={docId} threadId={thread.thread} />
        <StatusActions docId={docId} thread={thread} />
      </div>
    </div>
  );
}
