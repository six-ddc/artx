import { ArrowRight, X } from 'lucide-react';
import type { DiffResponse } from '@/lib/types';
import { cn } from '@/lib/utils';

export type DiffViewMode = 'rendered' | 'source';

interface DiffToolbarProps {
  /** Undefined while the diff is still loading; the identity row renders from from/to. */
  diff?: DiffResponse;
  from: string;
  /** Undefined = comparing against the working copy. */
  to?: string;
  view: DiffViewMode;
  onViewChange: (view: DiffViewMode) => void;
  onClose: () => void;
}

/**
 * The compare header, sticky under the topbar (h-12): what's being
 * compared and how much changed on the left, the Rendered|Source switch
 * and the exit on the right. The stat dots reuse the small-dot-plus-text
 * status language, with the diff tokens as their one chrome-side use.
 */
export function DiffToolbar({ diff, from, to, view, onViewChange, onClose }: DiffToolbarProps) {
  const stats = diff?.stats;
  const total = stats ? stats.added + stats.removed + stats.modified : 0;
  return (
    <div className="sticky top-12 z-20 flex min-h-10 flex-wrap items-center gap-x-3 gap-y-1 border-b bg-background/95 px-4 py-1.5 backdrop-blur">
      <span className="flex items-center gap-1.5 text-xs">
        <span className="art-mono">{from.slice(0, 7)}</span>
        <ArrowRight className="size-3 text-muted-foreground" />
        <span className={to ? 'art-mono' : 'font-medium'}>{to ? to.slice(0, 7) : 'Current'}</span>
      </span>

      {stats && (
        <span className="flex items-center gap-3 text-xs text-muted-foreground">
          {stats.added > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="size-2 rounded-full bg-diff-added" />
              <span className="tabular-nums">{stats.added}</span> added
            </span>
          )}
          {stats.removed > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="size-2 rounded-full bg-diff-removed" />
              <span className="tabular-nums">{stats.removed}</span> removed
            </span>
          )}
          {stats.modified > 0 && (
            <span className="flex items-center gap-1.5">
              <span className="size-2 rounded-full bg-diff-modified" />
              <span className="tabular-nums">{stats.modified}</span> changed
            </span>
          )}
          {total === 0 && <span>No content changes</span>}
        </span>
      )}
      {diff?.frontmatter_changed && (
        <span className="text-xs text-muted-foreground">Frontmatter changed</span>
      )}
      {diff?.chrome_changed && (
        <span className="text-xs text-muted-foreground">
          Styles or scripts changed — not covered by element highlights
        </span>
      )}

      <span className="flex-1" />

      <div className="flex rounded-md border p-0.5">
        {(['rendered', 'source'] as const).map((v) => (
          <button
            key={v}
            type="button"
            aria-pressed={view === v}
            onClick={() => onViewChange(v)}
            className={cn(
              'rounded-[5px] px-2 py-0.5 text-xs font-medium capitalize transition-colors',
              view === v
                ? 'bg-accent text-foreground'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {v}
          </button>
        ))}
      </div>
      <button
        type="button"
        title="Exit compare"
        onClick={onClose}
        className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      >
        <X className="size-3.5" />
      </button>
    </div>
  );
}
