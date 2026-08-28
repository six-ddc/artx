import { Link, useLocation } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useHealth } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { useSSEStatus } from './sse-status-context';
import { useDocsSearch } from './docs-search-context';
import { useHeaderSlot } from './header-slot-context';
import { ThemeToggle } from './ThemeToggle';

const STATUS_LABEL: Record<string, string> = {
  connecting: 'Connecting',
  open: 'Live',
  closed: 'Offline',
};

const STATUS_DOT: Record<string, string> = {
  connecting: 'bg-status-open animate-pulse',
  open: 'bg-status-resolved',
  closed: 'bg-destructive',
};

export function Header() {
  const { data: health } = useHealth();
  const status = useSSEStatus();
  const { query, setQuery } = useDocsSearch();
  const { setEl } = useHeaderSlot();
  const location = useLocation();
  const onIndex = location.pathname === '/';

  return (
    <header className="sticky top-0 z-40 h-12 border-b bg-background/80 backdrop-blur">
      <div className="flex h-full items-center gap-3 px-4 sm:px-5">
        <Link to="/" className="flex shrink-0 items-baseline gap-2.5">
          <span className="text-[15px] font-semibold tracking-tight">artx</span>
          {/* The vault's registry name, verbatim — a ~/ prefix or forced
              lowercase would make it read as a filesystem path it isn't.
              Index only: on a doc page the slot's breadcrumbed title takes
              this spot. */}
          {onIndex && (
            <span className="truncate text-xs text-muted-foreground" title={health?.root}>
              {health?.vault ?? '…'}
            </span>
          )}
        </Link>

        {onIndex && (
          <div className="relative ml-2 max-w-sm flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search documents…"
              className="h-7 w-full rounded-md border border-input bg-transparent pl-8 pr-2 text-sm shadow-xs transition-[color,border-color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20"
            />
          </div>
        )}

        {/* Portal target: the doc route mounts its title + controls here (see DocHeaderBar). */}
        <div ref={setEl} className="flex h-full min-w-0 flex-1 items-center gap-2.5" />

        <div className="flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
          <span
            className={cn('size-1.5 rounded-full', STATUS_DOT[status])}
            title={STATUS_LABEL[status]}
            aria-hidden
          />
          <span className="hidden sm:inline">{STATUS_LABEL[status]}</span>
        </div>

        <ThemeToggle />
      </div>
    </header>
  );
}
