import { useState } from 'react';
import { Outlet } from '@tanstack/react-router';
import { useEventStream } from '@/lib/sse';
import { SSEStatusContext } from './sse-status-context';
import { DocsSearchContext } from './docs-search-context';
import { Header } from './Header';

/** Mounts useEventStream() (SSE→invalidate, §7.3) and the global search state. */
export function RootLayout() {
  const status = useEventStream();
  const [query, setQuery] = useState('');

  return (
    <SSEStatusContext.Provider value={status}>
      <DocsSearchContext.Provider value={{ query, setQuery }}>
        <div className="min-h-dvh bg-desk text-ink">
          <Header />
          <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6">
            <Outlet />
          </main>
        </div>
      </DocsSearchContext.Provider>
    </SSEStatusContext.Provider>
  );
}
