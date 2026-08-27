import type { Reply } from '@/lib/types';
import { formatDateTime } from '@/lib/utils';

export function ReplyList({ replies }: { replies: Reply[] }) {
  if (replies.length === 0) return null;
  return (
    <ul className="space-y-2">
      {replies.map((reply) => (
        <li key={reply.id} className="border-l-2 border-line pl-2.5">
          <div className="art-mono flex items-baseline gap-1.5 text-[11px] text-ink-3">
            <span className="font-medium text-ink-2">{reply.author}</span>
            <span>{formatDateTime(reply.created_at)}</span>
            {reply.edited_at && <span>(edited)</span>}
          </div>
          <p className="whitespace-pre-wrap text-sm text-ink">{reply.body}</p>
        </li>
      ))}
    </ul>
  );
}
