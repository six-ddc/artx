import { useState } from 'react';
import { X } from 'lucide-react';
import type { PickMsg } from '@/lib/protocol';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

interface ElementPopoverProps {
  docId: string;
  pick: PickMsg;
  onClose: () => void;
}

/** The comment box that pops up after receiving a PickMsg (§7.2). */
export function ElementPopover({ docId, pick, onClose }: ElementPopoverProps) {
  const [body, setBody] = useState('');
  const postEvent = usePostEvent(docId);

  function submit() {
    if (!body.trim()) return;
    postEvent.mutate(
      { type: 'create', body: body.trim(), element: { aid: pick.aid, quote: pick.quote } },
      { onSuccess: onClose },
    );
  }

  return (
    <div
      className="art-pop-in absolute z-20 w-72 rounded-lg border bg-popover p-2.5 shadow-md"
      style={{ left: pick.rect.x, top: pick.rect.y + pick.rect.h + 6 }}
    >
      <p className="art-mono mb-1.5 truncate text-xs text-muted-foreground">
        {pick.tag} · {pick.text || '(empty)'}
      </p>
      <Textarea
        autoFocus
        rows={3}
        placeholder="Add a comment…"
        value={body}
        onChange={(e) => setBody(e.target.value)}
      />
      <div className="mt-2 flex items-center justify-end gap-1.5">
        <Button variant="ghost" size="sm" onClick={onClose}>
          <X className="size-3.5" />
        </Button>
        <Button size="sm" disabled={!body.trim() || postEvent.isPending} onClick={submit}>
          {postEvent.isPending ? 'Submitting…' : 'Submit'}
        </Button>
      </div>
    </div>
  );
}
