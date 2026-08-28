import { createPortal } from 'react-dom';
import { ExternalLink, GitCommitHorizontal, MessageSquareText } from 'lucide-react';
import type { DocDetail } from '@/lib/types';
import { api } from '@/lib/api';
import { useHistory } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { useHeaderSlot } from '@/components/layout/header-slot-context';
import { Badge } from '@/components/ui/badge';
import { Select } from '@/components/ui/select';

interface DocHeaderBarProps {
  docId: string;
  doc: DocDetail;
  rev?: string;
  onRevChange: (rev: string | undefined) => void;
  readOnly: boolean;
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
  openCount,
  commentsOpen,
  onToggleComments,
}: DocHeaderBarProps) {
  const { el } = useHeaderSlot();
  const { data: history } = useHistory(docId);
  if (!el) return null;

  return createPortal(
    <>
      <span className="text-muted-foreground" aria-hidden>
        /
      </span>
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{doc.title || doc.slug}</h1>

      <div className="flex shrink-0 items-center gap-2">
        {readOnly && <Badge variant="outline">History · read-only</Badge>}

        {/* mono sha chip: version selector */}
        <div className="hidden items-center gap-1 rounded-md border pl-2 pr-1 sm:flex">
          <GitCommitHorizontal className="size-3 text-muted-foreground" />
          <Select
            value={rev ?? ''}
            onChange={(e) => onRevChange(e.target.value || undefined)}
            aria-label="History"
            className="h-6 border-0 pl-1 pr-5 text-[11px] hover:bg-transparent"
          >
            <option value="">Working copy</option>
            {/* commits can likewise come back as null from the backend, even though the response shape marks it required. */}
            {(history?.commits ?? []).map((c) => (
              <option key={c.sha} value={c.sha}>
                {c.sha.slice(0, 7)} · {c.subject}
              </option>
            ))}
          </Select>
        </div>

        <a
          href={api.rawUrl(docId)}
          target="_blank"
          rel="noreferrer"
          title="Raw source"
          className="hidden size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground sm:flex"
        >
          <ExternalLink className="size-3.5" />
        </a>

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
      </div>
    </>,
    el,
  );
}
