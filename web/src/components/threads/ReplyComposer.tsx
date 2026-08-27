import { useState } from 'react';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

export function ReplyComposer({ docId, threadId }: { docId: string; threadId: string }) {
  const [body, setBody] = useState('');
  const postEvent = usePostEvent(docId);

  function submit() {
    if (!body.trim()) return;
    postEvent.mutate(
      { type: 'reply', thread: threadId, body: body.trim() },
      { onSuccess: () => setBody('') },
    );
  }

  return (
    <div className="flex items-start gap-1.5">
      <Textarea
        rows={1}
        placeholder="Reply..."
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            submit();
          }
        }}
      />
      <Button size="sm" disabled={!body.trim() || postEvent.isPending} onClick={submit}>
        Reply
      </Button>
    </div>
  );
}
