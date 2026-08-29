import type { DiffHunk } from '@/lib/types';
import { cn } from '@/lib/utils';

/**
 * The Source half of the compare view: unified hunks rendered line by line
 * in the mono metadata language. Both doc types share it — the hunks come
 * from the same dmp pass as the block/element ops, so the two views can
 * never disagree about what changed.
 */
export function UnifiedDiffView({ hunks }: { hunks: DiffHunk[] }) {
  if (hunks.length === 0) {
    return (
      <p className="mx-auto w-full max-w-[52rem] px-6 py-10 text-sm text-muted-foreground sm:px-8">
        No source changes.
      </p>
    );
  }
  return (
    <div className="mx-auto w-full max-w-[52rem] px-6 py-10 sm:px-8">
      <div className="overflow-x-auto rounded-md border">
        {hunks.map((hunk, i) => (
          <div key={i} className={cn(i > 0 && 'border-t')}>
            <div className="art-mono bg-muted px-3 py-1 text-xs text-muted-foreground">
              @@ -{hunk.from_start},{hunk.from_count} +{hunk.to_start},{hunk.to_count} @@
            </div>
            {hunk.lines.map((line, j) => (
              <div
                key={j}
                className={cn(
                  'art-mono flex px-3 text-[13px] leading-6',
                  line.op === 'add' && 'art-diff-line-add',
                  line.op === 'del' && 'art-diff-line-del',
                )}
              >
                <span className="w-4 shrink-0 select-none text-muted-foreground">
                  {line.op === 'add' ? '+' : line.op === 'del' ? '-' : ' '}
                </span>
                <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{line.text}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
