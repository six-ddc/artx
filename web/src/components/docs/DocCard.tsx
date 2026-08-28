import { Link } from '@tanstack/react-router';
import type { Doc } from '@/lib/types';
import { RelTime } from '@/components/ui/rel-time';

/** One index row inside the list card: dense, hairline-separated, Vercel-style. */
export function DocCard({ doc }: { doc: Doc }) {
  return (
    <Link
      to="/a/$docId"
      params={{ docId: doc.id }}
      className="flex items-center gap-3 border-b px-4 py-3 transition-colors last:border-b-0 hover:bg-accent/50"
    >
      <span className="truncate text-sm font-medium">{doc.title || doc.slug}</span>
      <span className="art-mono truncate text-xs text-muted-foreground">{doc.rel_path}</span>
      <span className="shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-medium uppercase leading-none text-muted-foreground">
        {doc.type === 'md' ? 'MD' : 'HTML'}
      </span>
      {doc.open_count > 0 && (
        <span className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span className="size-1.5 rounded-full bg-status-open" aria-hidden />
          {doc.open_count} open
        </span>
      )}
      <RelTime date={doc.mod_time} className="ml-auto shrink-0 text-xs text-muted-foreground" />
    </Link>
  );
}
