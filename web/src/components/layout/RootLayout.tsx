import { useState } from 'react';
import { Outlet } from '@tanstack/react-router';
import { useEventStream } from '@/lib/sse';
import { SSEStatusContext } from './sse-status-context';
import { DocsSearchContext } from './docs-search-context';
import { HeaderSlotContext } from './header-slot-context';
import { Header } from './Header';

/** Mounts useEventStream() (SSE→invalidate, §7.3) and the global search state. */
export function RootLayout() {
  const status = useEventStream();
  const [query, setQuery] = useState('');
  const [headerSlot, setHeaderSlot] = useState<HTMLElement | null>(null);

  return (
    <SSEStatusContext.Provider value={status}>
      <DocsSearchContext.Provider value={{ query, setQuery }}>
        <HeaderSlotContext.Provider value={{ el: headerSlot, setEl: setHeaderSlot }}>
          <div className="min-h-dvh bg-desk text-ink">
            <Header />
            {/* Routes own their width: the index constrains itself to a
                centered column, the doc route runs full-bleed — below the
                topbar the page IS the sheet. */}
            <main>
              <Outlet />
            </main>
          </div>
        </HeaderSlotContext.Provider>
      </DocsSearchContext.Provider>
    </SSEStatusContext.Provider>
  );
}
