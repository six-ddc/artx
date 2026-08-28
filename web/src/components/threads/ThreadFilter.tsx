import { cn } from '@/lib/utils';
import type { ThreadStatus } from '@/lib/types';

export type ThreadFilterValue = ThreadStatus | 'all';

const OPTIONS: { value: ThreadFilterValue; label: string }[] = [
  { value: 'open', label: 'Open' },
  { value: 'addressed', label: 'Addressed' },
  { value: 'resolved', label: 'Resolved' },
  { value: 'all', label: 'All' },
];

/**
 * Underline tabs with counts (a segment with a count of 0 shows only the
 * word). If the row still doesn't fit the narrow sidebar, it scrolls
 * horizontally instead of wrapping, scrollbar hidden.
 */
export function ThreadFilter({
  value,
  counts,
  onChange,
}: {
  value: ThreadFilterValue;
  counts: Record<ThreadFilterValue, number>;
  onChange: (value: ThreadFilterValue) => void;
}) {
  return (
    <div className="no-scrollbar -mb-px flex items-center gap-4 overflow-x-auto text-xs">
      {OPTIONS.map((opt) => {
        const count = counts[opt.value];
        const active = value === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            onClick={() => onChange(opt.value)}
            className={cn(
              'flex shrink-0 items-center gap-1 whitespace-nowrap border-b-2 pb-1.5 font-medium transition-colors',
              active
                ? 'border-b-foreground text-foreground'
                : 'border-b-transparent text-muted-foreground hover:text-foreground',
            )}
          >
            {opt.label}
            {count > 0 && (
              <span className={cn('tabular-nums', active ? 'text-muted-foreground' : 'opacity-70')}>
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}
