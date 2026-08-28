import { createPortal } from 'react-dom';
import { ExternalLink, GitCommitHorizontal, MessageSquareText } from 'lucide-react';
import type { DocDetail } from '@/lib/types';
import { api } from '@/lib/api';
import { useHistory } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { useHeaderSlot } from '@/components/layout/header-slot-context';
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
      <span className="text-ink-3" aria-hidden>
        /
      </span>
      <h1 className="min-w-0 flex-1 truncate text-sm font-medium text-ink">
        {doc.title || doc.slug}
      </h1>

      <div className="flex shrink-0 items-center gap-2">
        {readOnly && (
          <span className="art-mono rounded-full bg-addressed/15 px-2 py-0.5 text-[11px] text-addressed">
            History · read-only
          </span>
        )}

        {/* mono sha chip: version selector */}
        <div className="hidden items-center gap-1 rounded-full border border-line pl-2 pr-1 sm:flex">
          <GitCommitHorizontal className="size-3 text-ink-3" />
          <Select
            value={rev ?? ''}
            onChange={(e) => onRevChange(e.target.value || undefined)}
            aria-label="History"
            className="h-6 border-0 pl-1 pr-5 text-[11px]"
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
          className="hidden size-7 items-center justify-center rounded text-ink-2 transition-colors hover:bg-hover hover:text-ink sm:flex"
        >
          <ExternalLink className="size-3.5" />
        </a>

        <button
          type="button"
          aria-pressed={commentsOpen}
          onClick={onToggleComments}
          title={commentsOpen ? 'Hide comments' : 'Show comments'}
          className={cn(
            'art-mono flex h-7 items-center gap-1.5 rounded border px-2 text-[11px] transition-colors',
            commentsOpen
              ? 'border-marker/60 bg-marker/15 text-ink'
              : 'border-line text-ink-2 hover:bg-hover hover:text-ink',
          )}
        >
          <MessageSquareText className="size-3.5" />
          {openCount > 0 && <span>{openCount}</span>}
        </button>
      </div>
    </>,
    el,
  );
}
