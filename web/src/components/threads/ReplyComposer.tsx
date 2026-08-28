import { useState } from 'react';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

/**
 * Collapsed by default: a quiet input-shaped "Reply…" row, so a card at
 * rest shows content only. Clicking it expands to a real textarea with
 * Cancel/Reply; submit or Esc collapses it again (the draft survives a
 * Cancel — collapsing is not discarding).
 */
export function ReplyComposer({ docId, threadId }: { docId: string; threadId: string }) {
  const [expanded, setExpanded] = useState(false);
  const [body, setBody] = useState('');
  const postEvent = usePostEvent(docId);

  function submit() {
    if (!body.trim()) return;
    postEvent.mutate(
      { type: 'reply', thread: threadId, body: body.trim() },
      {
        onSuccess: () => {
          setBody('');
          setExpanded(false);
        },
      },
    );
  }

  if (!expanded) {
    return (
      <button
        type="button"
        onClick={() => setExpanded(true)}
        className="w-full cursor-text rounded-md border border-input/60 px-2.5 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:border-input hover:bg-muted/50"
      >
        {body.trim() ? `Reply… (draft)` : 'Reply…'}
      </button>
    );
  }

  return (
    <div className="space-y-2">
      <Textarea
        autoFocus
        rows={2}
        placeholder="Reply…"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            submit();
          }
          if (e.key === 'Escape') setExpanded(false);
        }}
      />
      <div className="flex items-center justify-end gap-1.5">
        <Button variant="ghost" size="sm" onClick={() => setExpanded(false)}>
          Cancel
        </Button>
        <Button size="sm" disabled={!body.trim() || postEvent.isPending} onClick={submit}>
          Reply
        </Button>
      </div>
    </div>
  );
}
