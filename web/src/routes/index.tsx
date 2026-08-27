import { useMemo } from 'react';
import { useDocs } from '@/lib/queries';
import { useDocsSearch } from '@/components/layout/docs-search-context';
import { DocCard } from '@/components/docs/DocCard';

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
    return <p className="text-sm text-ink-2">Loading…</p>;
  }
  if (isError) {
    return <p className="text-sm text-danger">Failed to load: {error.message}</p>;
  }

  if (docs.length === 0) {
    if (query) {
      return <p className="text-sm text-ink-2">No documents match "{query}"</p>;
    }
    // Empty vault: for this audience, a one-line command to copy-paste is the best onboarding.
    return (
      <div className="border border-line bg-sheet px-4 py-6 text-sm text-ink-2">
        <p>This vault has no documents yet.</p>
        <pre className="art-mono mt-3 overflow-x-auto rounded border border-line bg-muted px-3 py-2 text-xs text-ink">
          artx new &lt;slug&gt; --type md --json
        </pre>
      </div>
    );
  }

  return (
    <div className="border-t border-line">
      {docs.map((doc) => (
        <DocCard key={doc.id} doc={doc} />
      ))}
    </div>
  );
}
