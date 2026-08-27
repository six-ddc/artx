import { Link, useLocation } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useHealth } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { useSSEStatus } from './sse-status-context';
import { useDocsSearch } from './docs-search-context';

const STATUS_LABEL: Record<string, string> = {
  connecting: 'Connecting',
  open: 'Live',
  closed: 'Offline',
};

const STATUS_DOT: Record<string, string> = {
  connecting: 'bg-marker animate-pulse',
  open: 'bg-resolved',
  closed: 'bg-danger',
};

export function Header() {
  const { data: health } = useHealth();
  const status = useSSEStatus();
  const { query, setQuery } = useDocsSearch();
  const location = useLocation();
  const onIndex = location.pathname === '/';

  return (
    <header className="sticky top-0 z-10 h-12 border-b border-line bg-desk/90 backdrop-blur">
      <div className="mx-auto flex h-full max-w-6xl items-center gap-4 px-4 sm:px-6">
        <Link to="/" className="art-mono flex items-baseline gap-2.5 lowercase">
          {/* The one branding flourish: the last letter sits on a marker-amber highlight. */}
          <span className="text-base font-semibold text-ink">
            art<span className="bg-marker px-0.5 text-marker-ink">x</span>
          </span>
          <span className="truncate text-xs text-ink-2">~/{health?.vault ?? '…'}</span>
        </Link>

        {onIndex && (
          <div className="relative ml-2 max-w-sm flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-ink-3" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search documents…"
              className="h-7 w-full rounded border border-line bg-transparent pl-8 pr-2 text-sm text-ink outline-none placeholder:text-ink-3"
            />
          </div>
        )}

        <div className="art-mono ml-auto flex items-center gap-2 text-[11px] text-ink-2">
          <span
            className={cn('size-1.5 rounded-full', STATUS_DOT[status])}
            title={STATUS_LABEL[status]}
            aria-hidden
          />
          <span className="hidden sm:inline">{STATUS_LABEL[status]}</span>
        </div>
      </div>
    </header>
  );
}
