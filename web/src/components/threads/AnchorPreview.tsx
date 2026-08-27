import { ORPHAN_HINT, type ThreadAnchor, type ThreadStatus } from '@/lib/types';
import { cn } from '@/lib/utils';

interface AnchorPreviewProps {
  anchor: ThreadAnchor;
  status: ThreadStatus;
  /** Thread.hint; the server fills it in with the fixed api.OrphanHint text when orphaned. */
  hint?: string;
}

/**
 * The anchor quote — status is carried by the "highlighter", not a color
 * bar: open = marker amber background; addressed = indigo background;
 * resolved = no background (the whole card dims instead, reading as "this
 * annotation is settled"); orphan = no background + dashed underline +
 * ink-3, which takes priority over the status color.
 */
const HIGHLIGHT_BG: Record<ThreadStatus, string> = {
  open: 'bg-marker/12',
  addressed: 'bg-addressed/10',
  resolved: '',
};

export function AnchorPreview({ anchor, status, hint }: AnchorPreviewProps) {
  if (anchor.orphan) {
    return (
      <div className="text-xs text-ink-3">
        <p>{hint ?? ORPHAN_HINT}</p>
        {anchor.last_exact && (
          <p className="art-mono mt-1 truncate underline decoration-dashed underline-offset-2">
            “{anchor.last_exact}”
          </p>
        )}
      </div>
    );
  }

  const quote = anchor.exact ?? (anchor.kind === 'element' ? `#${anchor.aid ?? '?'}` : '');

  return (
    <p className={cn('art-mono truncate px-1.5 py-1 text-xs text-ink-2', HIGHLIGHT_BG[status])}>
      {anchor.approx && <span className="mr-1 text-ink-3">block·</span>}
      {quote}
    </p>
  );
}
