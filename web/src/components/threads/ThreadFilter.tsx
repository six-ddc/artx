import { cn } from '@/lib/utils';
import type { ThreadStatus } from '@/lib/types';

export type ThreadFilterValue = ThreadStatus | 'all';

const OPTIONS: { value: ThreadFilterValue; label: string }[] = [
  { value: 'open', label: 'open' },
  { value: 'addressed', label: 'addressed' },
  { value: 'resolved', label: 'resolved' },
  { value: 'all', label: 'all' },
];

/**
 * A mono segmented filter with counts: open 2 · addressed · resolved · all 6
 * (a segment with a count of 0 shows only the word, skipping the number's
 * width — the common case then fits on one line). If it still doesn't fit:
 * the whole row scrolls horizontally instead of wrapping, scrollbar hidden.
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
    <div className="art-mono no-scrollbar flex items-center gap-2 overflow-x-auto text-[11px]">
      {OPTIONS.map((opt, i) => {
        const count = counts[opt.value];
        return (
          <div key={opt.value} className="flex shrink-0 items-center gap-2">
            {i > 0 && (
              <span className="text-ink-3" aria-hidden>
                ·
              </span>
            )}
            <button
              type="button"
              onClick={() => onChange(opt.value)}
              className={cn(
                'shrink-0 whitespace-nowrap border-b-2 pb-0.5 transition-colors',
                value === opt.value
                  ? 'border-b-marker text-ink'
                  : 'border-b-transparent text-ink-3 hover:text-ink-2',
              )}
            >
              {opt.label}
              {count > 0 && ` ${count}`}
            </button>
          </div>
        );
      })}
    </div>
  );
}
