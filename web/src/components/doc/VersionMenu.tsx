import { useState } from 'react';
import { Bot, Check, ChevronDown, GitCommitHorizontal, GitCompare, User } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { HistoryCommit } from '@/lib/api';
import { useHistory } from '@/lib/queries';
import { cn, commitIdentity, formatDayLabel, type CommitIdentityKind } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import { RelTime } from '@/components/ui/rel-time';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

// The identity badge stays neutral (outline, no fill): color in this UI is
// reserved for thread status and the in-document diff washes — provenance
// is carried by icon + word, not hue.
const IDENTITY_ICON: Record<CommitIdentityKind, LucideIcon> = {
  human: User,
  agent: Bot,
  artx: GitCommitHorizontal,
};

interface VersionMenuProps {
  docId: string;
  /** The viewed version; undefined = the working copy. */
  rev?: string;
  onRevChange: (rev: string | undefined) => void;
  /** Start comparing the given sha against the working copy. */
  onCompare: (sha: string) => void;
}

/**
 * The topbar's version selector: a rich dropdown replacing the bare
 * sha·subject <select>. Commits group by local day, each entry reads
 * subject + provenance on line one and sha + relative time on line two.
 * Clicking an entry views that version; the GitCompare button that
 * surfaces on hover/highlight starts a compare instead — it must both
 * stop the click from bubbling into Radix's item-select (which would
 * navigate to the version) and close the menu itself, hence the
 * controlled open state.
 */
export function VersionMenu({ docId, rev, onRevChange, onCompare }: VersionMenuProps) {
  const { data: history } = useHistory(docId);
  const [open, setOpen] = useState(false);
  // commits can come back as null from the backend, even though the response shape marks it required.
  const commits = history?.commits ?? [];

  function compare(sha: string) {
    setOpen(false);
    onCompare(sha);
  }

  // Group consecutive commits (already newest-first) by their local day.
  const groups: { label: string; commits: HistoryCommit[] }[] = [];
  for (const c of commits) {
    const label = formatDayLabel(c.date);
    const last = groups[groups.length - 1];
    if (last && last.label === label) {
      last.commits.push(c);
    } else {
      groups.push({ label, commits: [c] });
    }
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      {/* mono sha chip, same look the native select had */}
      <DropdownMenuTrigger className="hidden h-7 items-center gap-1 rounded-md border pl-2 pr-1.5 text-[11px] transition-colors hover:bg-accent sm:flex">
        <GitCommitHorizontal className="size-3 text-muted-foreground" />
        <span className={cn(rev && 'art-mono')}>{rev ? rev.slice(0, 7) : 'Working copy'}</span>
        <ChevronDown className="size-3 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-h-[60vh] w-80 overflow-y-auto">
        <DropdownMenuItem
          className={cn('gap-2', !rev && 'bg-accent')}
          onSelect={() => onRevChange(undefined)}
        >
          {/* live pulse: the working copy is the one entry that keeps moving */}
          <span className="relative flex size-2 shrink-0">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-status-resolved opacity-50 motion-reduce:hidden" />
            <span className="relative inline-flex size-2 rounded-full bg-status-resolved" />
          </span>
          <span className="flex-1 font-medium">Working copy</span>
          {!rev && <Check className="size-3.5 text-muted-foreground" />}
        </DropdownMenuItem>

        {groups.map((group) => (
          <DropdownMenuGroup key={group.label}>
            <DropdownMenuLabel>{group.label}</DropdownMenuLabel>
            {group.commits.map((c) => {
              const current = c.sha === rev;
              const identity = commitIdentity(c.author);
              const Icon = IDENTITY_ICON[identity.kind];
              return (
                <DropdownMenuItem
                  key={c.sha}
                  className={cn('group items-center gap-2 py-1.5', current && 'bg-accent')}
                  onSelect={() => onRevChange(c.sha)}
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="min-w-0 flex-1 truncate text-[13px]">{c.subject}</span>
                      <Badge variant="outline" className="shrink-0 gap-1 px-1.5 py-0.5 text-[10px]">
                        <Icon className="size-3" />
                        {identity.label}
                      </Badge>
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 text-xs text-muted-foreground">
                      <span className="art-mono">{c.sha.slice(0, 7)}</span>
                      <RelTime date={c.date} />
                    </div>
                  </div>
                  {/* One reserved slot at the row's end: the current-version
                      check normally, swapped for the compare affordance
                      while the entry is highlighted. */}
                  <span className="flex w-6 shrink-0 justify-center">
                    <Check
                      className={cn(
                        'size-3.5 text-muted-foreground group-data-[highlighted]:hidden',
                        !current && 'invisible',
                      )}
                    />
                    <button
                      type="button"
                      title="Compare with current"
                      // Mouse-only affordance: a focusable button nested in
                      // a role="menuitem" is invalid ARIA and unreachable by
                      // the menu's roving focus anyway. The keyboard path to
                      // compare is DocHeaderBar's "Compare with current"
                      // (open the version first, then compare it).
                      aria-hidden="true"
                      tabIndex={-1}
                      className="hidden size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-background/80 hover:text-foreground group-data-[highlighted]:flex"
                      onClick={(e) => {
                        // Without these, the click reaches Radix's item
                        // select and navigates to the version instead.
                        e.stopPropagation();
                        e.preventDefault();
                        compare(c.sha);
                      }}
                    >
                      <GitCompare className="size-3.5" />
                    </button>
                  </span>
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuGroup>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
