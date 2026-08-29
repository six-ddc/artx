import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

export function formatDateTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/**
 * Day-group label for the version menu: Today / Yesterday, then a short
 * local date (with the year once it differs). Comparison is on local
 * calendar days, not 24h windows — a commit from 23:50 yesterday must not
 * read as Today. `now` is injectable for tests.
 */
export function formatDayLabel(iso: string, now: Date = new Date()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const dayDiff = Math.round((startOfDay(now) - startOfDay(date)) / 86_400_000);
  if (dayDiff === 0) return 'Today';
  if (dayDiff === 1) return 'Yesterday';
  return date.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    ...(date.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' }),
  });
}

/**
 * Maps a commit author to its identity badge in the version menu. The
 * backend commits under exactly three fixed authors (gitx): artx-human is
 * the person clicking Save in this UI ("you"), artx-agent is an AI agent
 * writing through the CLI, and plain artx covers the tool's own commits
 * (watcher snapshots etc.). kind stays semantic — the icon choice
 * (User/Bot/…) belongs to the component.
 */
export type CommitIdentityKind = 'human' | 'agent' | 'artx';

export function commitIdentity(author: string): { kind: CommitIdentityKind; label: string } {
  switch (author) {
    case 'artx-human':
      return { kind: 'human', label: 'you' };
    case 'artx-agent':
      return { kind: 'agent', label: 'agent' };
    default:
      return { kind: 'artx', label: 'artx' };
  }
}
