import { ORPHAN_HINT, type ThreadAnchor } from '@/lib/types';

interface AnchorPreviewProps {
  anchor: ThreadAnchor;
  /** Thread.hint; the server fills it in with the fixed api.OrphanHint text when orphaned. */
  hint?: string;
}

/**
 * The anchor quote — a neutral muted block (status lives in the header
 * badge's dot, not here). Orphan swaps to a dashed border so a detached
 * anchor reads differently from a live one at a glance.
 */
export function AnchorPreview({ anchor, hint }: AnchorPreviewProps) {
  if (anchor.orphan) {
    return (
      <div className="rounded-md border border-dashed px-2.5 py-1.5 text-xs text-muted-foreground">
        <p>{hint ?? ORPHAN_HINT}</p>
        {anchor.last_exact && <p className="art-mono mt-1 truncate">“{anchor.last_exact}”</p>}
      </div>
    );
  }

  const quote = anchor.exact ?? (anchor.kind === 'element' ? `#${anchor.aid ?? '?'}` : '');

  return (
    <p className="art-mono truncate rounded-md bg-muted px-2.5 py-1.5 text-xs text-muted-foreground">
      {anchor.approx && <span className="mr-1 opacity-60">block·</span>}
      {quote}
    </p>
  );
}
