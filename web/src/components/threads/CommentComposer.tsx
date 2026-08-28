import { useState } from 'react';
import type { SelectionInput } from '@/lib/types';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

interface CommentComposerProps {
  docId: string;
  selection: SelectionInput;
  /** Display-only quote; a whole-block pick has an empty selection.exact, so the owner passes the block's rendered text instead. */
  quote?: string;
  /** Called on both submit success and cancel; the owner clears the pending highlight. */
  onDone: () => void;
}

/**
 * The new-comment composer, docked at the top of the thread sidebar. The
 * quoted excerpt uses the same neutral quote treatment as AnchorPreview;
 * the pending marker highlight in the prose is what ties this card to its
 * selection.
 */
export function CommentComposer({ docId, selection, quote, onDone }: CommentComposerProps) {
  const [body, setBody] = useState('');
  const postEvent = usePostEvent(docId);

  function submit() {
    if (!body.trim()) return;
    postEvent.mutate({ type: 'create', body: body.trim(), selection }, { onSuccess: onDone });
  }

  return (
    // Pops in and lights up while focused: starting a comment moves focus
    // across the page into the drawer, and without a visible cue over here
    // the jump reads as "nothing happened". The amber quote ties the card
    // to the pending highlight in the prose.
    <div className="art-pop-in space-y-2 rounded-lg border bg-card p-3 shadow-xs transition-[border-color,box-shadow] focus-within:border-ring focus-within:shadow-md">
      <p className="text-xs font-medium">New comment</p>
      <p className="art-mono art-pending-quote truncate rounded-md px-2.5 py-1.5 text-xs text-foreground">
        {quote ?? selection.exact}
      </p>
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
