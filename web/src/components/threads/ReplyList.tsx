import type { Reply } from '@/lib/types';
import { RelTime } from '@/components/ui/rel-time';

export function ReplyList({ replies }: { replies: Reply[] }) {
  if (replies.length === 0) return null;
  return (
    <ul className="space-y-2.5">
      {replies.map((reply) => (
        <li key={reply.id} className="border-l-2 pl-2.5">
          <div className="flex items-baseline gap-1.5 text-xs text-muted-foreground">
            <RelTime date={reply.created_at} />
            {reply.edited_at && <span>(edited)</span>}
          </div>
          <p className="mt-0.5 whitespace-pre-wrap text-sm leading-relaxed text-foreground">
            {reply.body}
          </p>
        </li>
      ))}
    </ul>
  );
}
