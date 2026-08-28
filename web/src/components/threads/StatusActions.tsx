import { useState } from 'react';
import { Check, MoreHorizontal } from 'lucide-react';
import type { Thread } from '@/lib/types';
import { usePostEvent } from '@/lib/queries';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

interface ThreadActionsProps {
  docId: string;
  thread: Thread;
}

/** Popover dialogs reachable from the ⋯ menu; anchored to the menu button. */
type Dialog = 'address' | 'reopen' | 'delete';

/**
 * The card's action cluster (§7.2), collapsed into two icon buttons so a
 * card at rest shows content, not chrome:
 * - ✓ advances the status one step directly (open → addressed → resolved);
 * - ⋯ holds the full menu: the with-note variants, Reopen, and Delete.
 * Delete is destructive-looking but is actually a tombstone event in the
 * append-only log (recoverable until compact) — still, it disappears from
 * every view immediately, so it keeps a two-step confirm.
 */
export function ThreadActions({ docId, thread }: ThreadActionsProps) {
  const postEvent = usePostEvent(docId);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [note, setNote] = useState('');
  const pending = postEvent.isPending;
  const status = thread.status;

  function mutate(event: Parameters<typeof postEvent.mutate>[0]) {
    postEvent.mutate(event);
  }

  function closeDialog() {
    setDialog(null);
    setNote('');
  }

  function submitNote() {
    const trimmed = note.trim() || undefined;
    if (dialog === 'address') mutate({ type: 'addressed', thread: thread.thread, note: trimmed });
    if (dialog === 'reopen') mutate({ type: 'reopen', thread: thread.thread, note: trimmed });
    closeDialog();
  }

  const quick =
    status === 'open'
      ? { label: 'Mark addressed', fn: () => mutate({ type: 'addressed', thread: thread.thread }) }
      : status === 'addressed'
        ? { label: 'Resolve', fn: () => mutate({ type: 'resolve', thread: thread.thread }) }
        : null;

  return (
    <div className="flex items-center gap-0.5">
      {quick && (
        <Button
          variant="ghost"
          size="icon-sm"
          title={quick.label}
          aria-label={quick.label}
          disabled={pending}
          onClick={quick.fn}
        >
          <Check className="size-3.5" />
        </Button>
      )}
      <Popover open={dialog !== null} onOpenChange={(open) => !open && closeDialog()}>
        <DropdownMenu>
          <PopoverAnchor asChild>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Thread actions"
                disabled={pending}
              >
                <MoreHorizontal className="size-3.5" />
              </Button>
            </DropdownMenuTrigger>
          </PopoverAnchor>
          <DropdownMenuContent align="end">
            {status === 'open' && (
              <>
                <DropdownMenuItem onSelect={() => setDialog('address')}>
                  Mark addressed…
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => mutate({ type: 'resolve', thread: thread.thread })}>
                  Resolve
                </DropdownMenuItem>
              </>
            )}
            {status === 'addressed' && (
              <>
                <DropdownMenuItem onSelect={() => mutate({ type: 'resolve', thread: thread.thread })}>
                  Resolve
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setDialog('reopen')}>Reopen…</DropdownMenuItem>
              </>
            )}
            {status === 'resolved' && (
              <DropdownMenuItem onSelect={() => setDialog('reopen')}>Reopen…</DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => setDialog('delete')}>
              Delete…
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <PopoverContent align="end" className="w-64">
          {dialog === 'delete' ? (
            <>
              <p className="text-xs text-muted-foreground">Delete this thread and its replies?</p>
              <div className="mt-2.5 flex justify-end gap-1.5">
                <Button size="sm" variant="ghost" onClick={closeDialog}>
                  Cancel
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => {
                    mutate({ type: 'delete', thread: thread.thread });
                    closeDialog();
                  }}
                >
                  Delete
                </Button>
              </div>
            </>
          ) : (
            <>
              <Textarea
                autoFocus
                rows={2}
                placeholder={dialog === 'address' ? 'Note (optional)' : 'Reason (optional)'}
                value={note}
                onChange={(e) => setNote(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault();
                    submitNote();
                  }
                }}
              />
              <div className="mt-2.5 flex justify-end gap-1.5">
                <Button size="sm" variant="ghost" onClick={closeDialog}>
                  Cancel
                </Button>
                <Button size="sm" onClick={submitNote}>
                  {dialog === 'address' ? 'Mark addressed' : 'Reopen'}
                </Button>
              </div>
            </>
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
