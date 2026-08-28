import { useState } from 'react';
import type { SelectionInput } from '@/lib/types';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

interface CommentComposerProps {
  docId: string;
  selection: SelectionInput;
  /** Called on both submit success and cancel; the owner clears the pending highlight. */
  onDone: () => void;
}

/**
 * The new-comment composer, docked at the top of the thread sidebar so it
 * reads as marginalia-in-progress next to the existing cards. The quoted
 * excerpt mirrors AnchorPreview's open-status highlighter treatment, tying
 * the card to the pending highlight in the prose.
 */
export function CommentComposer({ docId, selection, onDone }: CommentComposerProps) {
  const [body, setBody] = useState('');
  const postEvent = usePostEvent(docId);

  function submit() {
    if (!body.trim()) return;
    postEvent.mutate({ type: 'create', body: body.trim(), selection }, { onSuccess: onDone });
  }

  return (
    <div className="space-y-2 border-b border-line px-3 py-3">
      <p className="art-mono text-[11px] font-medium text-ink-2">New comment</p>
      <p className="art-mono truncate bg-marker/12 px-1.5 py-1 text-xs text-ink-2">{selection.exact}</p>
      <Textarea
        autoFocus
        rows={3}
        placeholder="Add a comment…"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            submit();
          }
          if (e.key === 'Escape') onDone();
        }}
      />
      <div className="flex items-center justify-end gap-1.5">
        <Button variant="ghost" size="sm" onClick={onDone}>
          Cancel
        </Button>
        <Button size="sm" disabled={!body.trim() || postEvent.isPending} onClick={submit}>
          {postEvent.isPending ? 'Submitting…' : 'Comment'}
        </Button>
      </div>
    </div>
  );
}
