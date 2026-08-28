import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { X } from 'lucide-react';
import type { Thread } from '@/lib/types';
import { Button } from '@/components/ui/button';
import { ThreadFilter, type ThreadFilterValue } from './ThreadFilter';
import { ThreadCard } from './ThreadCard';

interface ThreadSidebarProps {
  docId: string;
  threads: Thread[];
  focusedThreadId?: string;
  onFocusThread: (threadId: string) => void;
  /** Clears the focused thread (?t=); narrowing the filter away from the focused thread's status counts as being done with it. */
  onClearFocus: () => void;
  /** Closes the drawer (the topbar comments toggle reopens it). */
  onClose: () => void;
  /** The new-comment composer card, docked above the thread list while a selection is pending. */
  composer?: ReactNode;
}

const FLASH_MS = 650;

export function ThreadSidebar({ docId, threads, focusedThreadId, onFocusThread, onClearFocus, onClose, composer }: ThreadSidebarProps) {
  const [filter, setFilter] = useState<ThreadFilterValue>('open');
  const [flashIds, setFlashIds] = useState<ReadonlySet<string>>(new Set());
  const knownIds = useRef<Set<string> | null>(null);

  // A soft flash, once, for threads that just arrived via SSE: diff
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
  // up and scroll into view. This must fire only when the focus CHANGES —
  // as a standing assertion it fought the user's own tab clicks: with a
  // mismatched thread focused (and focus is sticky now), every attempt to
  // narrow the filter snapped straight back to "all", so the tabs read as
  // dead.
  const lastWidenedFocus = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (focusedThreadId === lastWidenedFocus.current) return;
    lastWidenedFocus.current = focusedThreadId;
    if (!focusedThreadId || filter === 'all') return;
    const focused = threads.find((t) => t.thread === focusedThreadId);
    if (focused && focused.status !== filter) setFilter('all');
  }, [focusedThreadId, threads, filter]);

  // The inverse direction: deliberately narrowing the filter away from the
  // focused thread's status means the user is done with that focus — clear
  // it instead of hiding a still-focused card (or fighting the widen rule).
  function changeFilter(next: ThreadFilterValue) {
    setFilter(next);
    if (next !== 'all' && focusedThreadId) {
      const focused = threads.find((t) => t.thread === focusedThreadId);
      if (focused && focused.status !== next) onClearFocus();
    }
  }

  const counts = useMemo(() => {
    const c: Record<ThreadFilterValue, number> = { open: 0, addressed: 0, resolved: 0, all: threads.length };
    for (const t of threads) c[t.status]++;
    return c;
  }, [threads]);

  // Cards follow the document, not the clock: sort by the anchor's byte
  // offset into the source, so the sidebar reads top-to-bottom alongside
  // the prose (and an orphan stays where its text last lived). The server
  // returns creation order (event-id sort); created_at is only the
  // tie-break — and the de-facto order for html docs, whose element
  // anchors carry no position (start is 0 across the board there).
  const filtered = useMemo(() => {
    const shown = filter === 'all' ? threads : threads.filter((t) => t.status === filter);
    return [...shown].sort(
      (a, b) => a.anchor.start - b.anchor.start || a.created_at.localeCompare(b.created_at),
    );
  }, [threads, filter]);

  return (
    <>
      {/* Below lg the drawer floats over the page; the scrim closes it. */}
      <div className="fixed inset-0 z-20 bg-black/40 lg:hidden" onClick={onClose} aria-hidden />
      <aside className="art-slide-in flex w-[21rem] shrink-0 flex-col border-l bg-background max-lg:fixed max-lg:bottom-0 max-lg:right-0 max-lg:top-12 max-lg:z-30 max-lg:max-w-[85vw] max-lg:shadow-xl lg:sticky lg:top-12 lg:h-[calc(100dvh-3rem)]">
        {/* Two-row header: title and filter each get their own row instead of
            fighting for one and wrapping — the filter's up to 4 count segments
            sharing a row with the title would wrap almost every time in a
            narrow sidebar. The filter's underline sits on the header's own
            border-b, reading as one tab bar. */}
        <div className="border-b px-4 pt-3.5">
          <div className="mb-2.5 flex items-center">
            <h2 className="text-sm font-semibold">Threads</h2>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onClose}
              title="Hide comments"
              aria-label="Hide comments"
              className="ml-auto"
            >
              <X className="size-3.5" />
            </Button>
          </div>
          <ThreadFilter value={filter} counts={counts} onChange={changeFilter} />
        </div>
        {composer && <div className="border-b p-3">{composer}</div>}
        <div className="flex-1 space-y-2 overflow-y-auto p-3">
          {filtered.length === 0 ? (
            <p className="px-1 py-2 text-xs text-muted-foreground">
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
