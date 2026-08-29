// Route table (§7.1). Uses TanStack Router's code-based routing — a
// single-file route tree, no filesystem-generated routing plugin, keeping
// web/src's file list matching what the blueprint lists.

import { createRootRoute, createRoute, createRouter } from '@tanstack/react-router';
import { RootLayout } from './components/layout/RootLayout';
import { DocsIndex } from './routes/index';
import { DocView, type DocViewSearch } from './routes/doc';

const rootRoute = createRootRoute({ component: RootLayout });

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: DocsIndex,
});

const docRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/a/$docId',
  validateSearch: (search: Record<string, unknown>): DocViewSearch => ({
    v: typeof search.v === 'string' ? search.v : undefined,
    t: typeof search.t === 'string' ? search.t : undefined,
    cmp: typeof search.cmp === 'string' ? search.cmp : undefined,
  }),
  component: DocView,
});

const routeTree = rootRoute.addChildren([indexRoute, docRoute]);

export const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
