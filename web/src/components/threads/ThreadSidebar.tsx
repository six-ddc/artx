import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { X } from 'lucide-react';
import type { Thread } from '@/lib/types';
import { ThreadFilter, type ThreadFilterValue } from './ThreadFilter';
import { ThreadCard } from './ThreadCard';

interface ThreadSidebarProps {
  docId: string;
  threads: Thread[];
  focusedThreadId?: string;
  onFocusThread: (threadId: string) => void;
  /** Closes the drawer (the topbar comments toggle reopens it). */
  onClose: () => void;
  /** The new-comment composer card, docked above the thread list while a selection is pending. */
  composer?: ReactNode;
}

const FLASH_MS = 650;

export function ThreadSidebar({ docId, threads, focusedThreadId, onFocusThread, onClose, composer }: ThreadSidebarProps) {
  const [filter, setFilter] = useState<ThreadFilterValue>('open');
  const [flashIds, setFlashIds] = useState<ReadonlySet<string>>(new Set());
  const knownIds = useRef<Set<string> | null>(null);

  // A soft amber flash, once, for threads that just arrived via SSE: diff
  // against the last-rendered set of thread ids. The first mount only
  // establishes the baseline and never flashes (otherwise every thread would
  // flash together the moment the page opens).
  useEffect(() => {
    const currentIds = new Set(threads.map((t) => t.thread));
    if (knownIds.current) {
      const arrived = [...currentIds].filter((id) => !knownIds.current!.has(id));
      if (arrived.length > 0) {
        setFlashIds((prev) => new Set([...prev, ...arrived]));
        const timer = window.setTimeout(() => {
          setFlashIds((prev) => {
            const next = new Set(prev);
            for (const id of arrived) next.delete(id);
            return next;
          });
        }, FLASH_MS);
        knownIds.current = currentIds;
        return () => window.clearTimeout(timer);
      }
    }
    knownIds.current = currentIds;
  }, [threads]);

  // Focus must never point at an invisible card: clicking a highlight whose
  // thread the current filter hides (e.g. an addressed thread under the
  // default "open" filter) widens the filter to "all" so the card can light
  // up and scroll into view.
  useEffect(() => {
    if (!focusedThreadId || filter === 'all') return;
    const focused = threads.find((t) => t.thread === focusedThreadId);
    if (focused && focused.status !== filter) setFilter('all');
  }, [focusedThreadId, threads, filter]);

  const counts = useMemo(() => {
    const c: Record<ThreadFilterValue, number> = { open: 0, addressed: 0, resolved: 0, all: threads.length };
    for (const t of threads) c[t.status]++;
    return c;
  }, [threads]);

  const filtered = useMemo(
    () => (filter === 'all' ? threads : threads.filter((t) => t.status === filter)),
    [threads, filter],
  );

  return (
    <>
      {/* Below lg the drawer floats over the page; the scrim closes it. */}
      <div className="fixed inset-0 z-20 bg-ink/25 lg:hidden" onClick={onClose} aria-hidden />
      <aside className="art-slide-in flex w-80 shrink-0 flex-col gap-3 border-l border-line bg-sheet px-4 pt-4 max-lg:fixed max-lg:bottom-0 max-lg:right-0 max-lg:top-12 max-lg:z-30 max-lg:max-w-[85vw] max-lg:shadow-xl lg:sticky lg:top-12 lg:h-[calc(100dvh-3rem)]">
        {/* Two-row layout: title and filter each get their own row instead of
            fighting for one and wrapping — the filter's up to 4 count segments
            sharing a row with the title would wrap almost every time in a
            narrow sidebar. */}
        <div className="flex flex-col gap-2">
          <div className="flex items-center">
            <h2 className="text-sm font-semibold text-ink">Threads</h2>
            <button
              type="button"
              onClick={onClose}
              title="Hide comments"
              className="ml-auto flex size-6 items-center justify-center rounded text-ink-3 transition-colors hover:bg-hover hover:text-ink"
            >
              <X className="size-3.5" />
            </button>
          </div>
          <ThreadFilter value={filter} counts={counts} onChange={setFilter} />
        </div>
        {composer}
        <div className="flex-1 overflow-y-auto border-t border-line">
          {filtered.length === 0 ? (
            <p className="px-1 py-3 text-xs text-ink-3">
              {filter === 'all' ? 'No threads' : `No ${filter} threads`}
            </p>
          ) : (
            filtered.map((thread) => (
              <ThreadCard
                key={thread.thread}
                docId={docId}
                thread={thread}
                focused={thread.thread === focusedThreadId}
                flash={flashIds.has(thread.thread)}
                onFocus={onFocusThread}
              />
            ))
          )}
        </div>
      </aside>
    </>
  );
}
