import { Link } from '@tanstack/react-router';
import type { Doc } from '@/lib/types';
import { formatDateTime } from '@/lib/utils';

/** One index-page row: a dense list, not a floating card (§Annotation Desk index page redesign). */
export function DocCard({ doc }: { doc: Doc }) {
  const typeTag = doc.type === 'md' ? '[MD]' : '[HTML]';

  return (
    <Link
      to="/a/$docId"
      params={{ docId: doc.id }}
      className="flex items-center gap-3 border-b border-line px-3 py-2.5 transition-colors hover:bg-hover"
    >
      <span className="truncate font-medium text-ink">{doc.title || doc.slug}</span>
      <span className="art-mono truncate text-xs text-ink-3">{doc.rel_path}</span>
      <span className="art-mono shrink-0 text-[11px] text-ink-2">{typeTag}</span>
      {doc.open_count > 0 && (
        <span className="art-mono shrink-0 rounded-full bg-marker/15 px-1.5 py-0.5 text-[10px] font-medium text-marker-ink">
          {doc.open_count} open
        </span>
      )}
      <span className="art-mono ml-auto shrink-0 text-[11px] text-ink-3">
        {formatDateTime(doc.mod_time)}
      </span>
    </Link>
  );
}
