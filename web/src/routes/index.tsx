import { useMemo, type ReactNode } from 'react';
import { useDocs } from '@/lib/queries';
import { useDocsSearch } from '@/components/layout/docs-search-context';
import { DocCard } from '@/components/docs/DocCard';

/** The index owns its centered column (RootLayout's main is full-bleed so the doc route can be). */
function Shell({ children }: { children: ReactNode }) {
  return <div className="mx-auto max-w-6xl px-4 py-6 sm:px-6">{children}</div>;
}

export function DocsIndex() {
  const { data, isPending, isError, error } = useDocs();
  const { query } = useDocsSearch();

  const allDocs = data?.docs ?? [];
  const docs = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return allDocs;
    return allDocs.filter(
      (doc) =>
        doc.title.toLowerCase().includes(q) ||
        doc.slug.toLowerCase().includes(q) ||
        doc.rel_path.toLowerCase().includes(q),
    );
  }, [allDocs, query]);

  if (isPending) {
    return (
      <Shell>
        <p className="text-sm text-muted-foreground">Loading…</p>
      </Shell>
    );
  }
  if (isError) {
    return (
      <Shell>
        <p className="text-sm text-destructive">Failed to load: {error.message}</p>
      </Shell>
    );
  }

  if (docs.length === 0) {
    if (query) {
      return (
        <Shell>
          <p className="text-sm text-muted-foreground">No documents match "{query}"</p>
        </Shell>
      );
    }
    // Empty vault: for this audience, a one-line command to copy-paste is the best onboarding.
    return (
      <Shell>
        <div className="rounded-lg border bg-card px-4 py-6 text-sm text-muted-foreground shadow-xs">
          <p>This vault has no documents yet.</p>
          <pre className="art-mono mt-3 overflow-x-auto rounded-md border bg-muted px-3 py-2 text-xs text-foreground">
            artx new &lt;slug&gt; --type md --json
          </pre>
        </div>
      </Shell>
    );
  }

  return (
    <Shell>
      <div className="overflow-hidden rounded-lg border bg-card shadow-xs">
        {docs.map((doc) => (
          <DocCard key={doc.id} doc={doc} />
        ))}
      </div>
    </Shell>
  );
}
