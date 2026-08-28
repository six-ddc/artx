import { Link, useLocation } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useHealth } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { useSSEStatus } from './sse-status-context';
import { useDocsSearch } from './docs-search-context';
import { useHeaderSlot } from './header-slot-context';

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
  const { setEl } = useHeaderSlot();
  const location = useLocation();
  const onIndex = location.pathname === '/';

  return (
    <header className="sticky top-0 z-40 h-12 border-b border-line bg-desk/90 backdrop-blur">
      <div className="flex h-full items-center gap-3 px-4 sm:px-5">
        <Link to="/" className="art-mono flex shrink-0 items-baseline gap-2.5">
          {/* The one branding flourish: the whole wordmark struck by the
              highlighter — the logo is the product's core action itself,
              same fixed marker-pen colors as the anchor highlight (see
              .art-wordmark in styles.css). */}
          <span className="art-wordmark text-base font-semibold lowercase">artx</span>
          {/* The vault's registry name, verbatim — a ~/ prefix or forced
              lowercase would make it read as a filesystem path it isn't.
              Index only: on a doc page the slot's breadcrumbed title takes
              this spot. */}
          {onIndex && (
            <span className="truncate text-xs text-ink-2" title={health?.root}>
              {health?.vault ?? '…'}
            </span>
          )}
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

        {/* Portal target: the doc route mounts its title + controls here (see DocHeaderBar). */}
        <div ref={setEl} className="flex h-full min-w-0 flex-1 items-center gap-2.5" />

        <div className="art-mono flex shrink-0 items-center gap-2 text-[11px] text-ink-2">
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
