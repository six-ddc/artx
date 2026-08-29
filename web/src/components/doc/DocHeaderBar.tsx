import { createPortal } from 'react-dom';
import { ExternalLink, GitCompare, MessageSquareText } from 'lucide-react';
import type { DocDetail } from '@/lib/types';
import { api } from '@/lib/api';
import { cn } from '@/lib/utils';
import { useHeaderSlot } from '@/components/layout/header-slot-context';
import { Badge } from '@/components/ui/badge';
import { VersionMenu } from './VersionMenu';

interface DocHeaderBarProps {
  docId: string;
  doc: DocDetail;
  rev?: string;
  onRevChange: (rev: string | undefined) => void;
  /** Viewing a historical version (?v=): the read-only badge + compare entry. */
  readOnly: boolean;
  /** Start comparing the given sha against the working copy. */
  onCompare: (sha: string) => void;
  /** A compare is active (?cmp=): comments are disabled, so hide the toggle. */
  comparing: boolean;
  /** Count of open threads, shown on the comments toggle. */
  openCount: number;
  commentsOpen: boolean;
  onToggleComments: () => void;
}

/**
 * The doc page's share of the single global topbar, portaled into the
 * Header's slot: breadcrumbed title on the left, version/raw/comments
 * controls on the right. There is no mode switch here by design — md
 * comments ride on text selection, md edits on the gutter pencil, and html
 * picking lives in the canvas's own floating tool pill.
 */
export function DocHeaderBar({
  docId,
  doc,
  rev,
  onRevChange,
  readOnly,
  onCompare,
  comparing,
  openCount,
  commentsOpen,
  onToggleComments,
}: DocHeaderBarProps) {
  const { el } = useHeaderSlot();
  if (!el) return null;

  return createPortal(
    <>
      <span className="text-muted-foreground" aria-hidden>
        /
      </span>
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{doc.title || doc.slug}</h1>

      <div className="flex shrink-0 items-center gap-2">
        {readOnly && (
          <>
            <Badge variant="outline">History · read-only</Badge>
            {/* the second door into compare: already looking at an old
                version, one click diffs it against today */}
            <button
              type="button"
              onClick={() => rev && onCompare(rev)}
              className="hidden h-7 items-center gap-1.5 rounded-md border px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground sm:flex"
            >
              <GitCompare className="size-3.5" />
              Compare with current
            </button>
          </>
        )}

        <VersionMenu docId={docId} rev={rev} onRevChange={onRevChange} onCompare={onCompare} />

        <a
          href={api.rawUrl(docId)}
          target="_blank"
          rel="noreferrer"
          title="Raw source"
          className="hidden size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground sm:flex"
        >
          <ExternalLink className="size-3.5" />
        </a>

        {!comparing && (
          <button
            type="button"
            aria-pressed={commentsOpen}
            onClick={onToggleComments}
            title={commentsOpen ? 'Hide comments' : 'Show comments'}
            className={cn(
              'flex h-7 items-center gap-1.5 rounded-md border px-2 text-xs font-medium transition-colors',
              commentsOpen
                ? 'border-input bg-accent text-foreground'
                : 'text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
          >
            <MessageSquareText className="size-3.5" />
            {openCount > 0 && <span className="tabular-nums">{openCount}</span>}
          </button>
        )}
      </div>
    </>,
    el,
  );
}
