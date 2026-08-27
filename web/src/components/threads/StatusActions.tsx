import { useState } from 'react';
import type { Thread } from '@/lib/types';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

interface StatusActionsProps {
  docId: string;
  thread: Thread;
}

/** addressed / resolve / reopen (§7.2). addressed/reopen can optionally carry a one-line note. */
export function StatusActions({ docId, thread }: StatusActionsProps) {
  const postEvent = usePostEvent(docId);

  function resolve() {
    postEvent.mutate({ type: 'resolve', thread: thread.thread });
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {thread.status === 'open' && (
        <>
          <NoteAction
            label="Mark addressed"
            placeholder="Note (optional)"
            pending={postEvent.isPending}
            onSubmit={(note) =>
              postEvent.mutate({ type: 'addressed', thread: thread.thread, note: note || undefined })
            }
          />
          <Button size="sm" variant="ghost" disabled={postEvent.isPending} onClick={resolve}>
            resolve
          </Button>
        </>
      )}
      {thread.status === 'addressed' && (
        <>
          <Button size="sm" variant="ghost" disabled={postEvent.isPending} onClick={resolve}>
            resolve
          </Button>
          <NoteAction
            label="Reopen"
            placeholder="Reason (optional)"
            pending={postEvent.isPending}
            onSubmit={(note) =>
              postEvent.mutate({ type: 'reopen', thread: thread.thread, note: note || undefined })
            }
          />
        </>
      )}
      {thread.status === 'resolved' && (
        <NoteAction
          label="Reopen"
          placeholder="Reason (optional)"
          pending={postEvent.isPending}
          onSubmit={(note) =>
            postEvent.mutate({ type: 'reopen', thread: thread.thread, note: note || undefined })
          }
        />
      )}
    </div>
  );
}

function NoteAction({
  label,
  placeholder,
  pending,
  onSubmit,
}: {
  label: string;
  placeholder: string;
  pending: boolean;
  onSubmit: (note: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState('');

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" variant="ghost" disabled={pending}>
          {label}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64">
        <Textarea
          autoFocus
          rows={2}
          placeholder={placeholder}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
        <div className="mt-2 flex justify-end">
          <Button
            size="sm"
            onClick={() => {
              onSubmit(note.trim());
              setNote('');
              setOpen(false);
            }}
          >
            Confirm
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
