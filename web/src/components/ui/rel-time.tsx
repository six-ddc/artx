import { useEffect, useReducer } from 'react';
import { cn } from '@/lib/utils';

// Friendly relative timestamps without the jitter (the GitHub/Linear
// pattern):
// - never counts seconds — everything under a minute is "just now", so the
//   text can only ever tick forward, once a minute at most;
// - m → h → d, then past 7 days it switches to an absolute date (old
//   content shouldn't be relative anyway, and absolute text never needs a
//   timer again);
// - each instance schedules exactly one timeout for the moment its text
//   would actually change (a "3h ago" sleeps for up to an hour), instead of
//   a global every-second interval;
// - background tabs throttle timers, so a visibilitychange re-render
//   catches the text up when the tab comes back;
// - hover always reveals the full absolute datetime via title.

const MINUTE = 60_000;
const HOUR = 3_600_000;
const DAY = 86_400_000;
/** Re-check at least this often even when "stable" — cheap insurance against clock changes. */
const MAX_DELAY = 6 * HOUR;

/** Exported for tests. */
export function relText(ts: number, now: number): { text: string; nextIn: number | null } {
  const diff = now - ts;
  if (diff < MINUTE) return { text: 'just now', nextIn: MINUTE - Math.max(0, diff) };
  if (diff < HOUR) return { text: `${Math.floor(diff / MINUTE)}m ago`, nextIn: MINUTE - (diff % MINUTE) };
  if (diff < DAY) return { text: `${Math.floor(diff / HOUR)}h ago`, nextIn: HOUR - (diff % HOUR) };
  if (diff < 7 * DAY) return { text: `${Math.floor(diff / DAY)}d ago`, nextIn: DAY - (diff % DAY) };
  const d = new Date(ts);
  const sameYear = d.getFullYear() === new Date(now).getFullYear();
  return {
    text: d.toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      ...(sameYear ? {} : { year: 'numeric' }),
    }),
    nextIn: null,
  };
}

function fullDateTime(ts: number): string {
  return new Date(ts).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function RelTime({ date, className }: { date: string; className?: string }) {
  const ts = Date.parse(date);
  const valid = !Number.isNaN(ts);
  const [tick, bump] = useReducer((n: number) => n + 1, 0);
  const { text, nextIn } = valid ? relText(ts, Date.now()) : { text: date, nextIn: null };

  useEffect(() => {
    if (nextIn == null) return;
    // +50ms past the boundary so the recompute lands on the new value —
    // firing a hair early would render the same text and reschedule.
    const t = window.setTimeout(bump, Math.min(Math.max(nextIn + 50, 1000), MAX_DELAY));
    return () => window.clearTimeout(t);
  }, [nextIn, tick]);

  useEffect(() => {
    function onVisible() {
      if (!document.hidden) bump();
    }
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  }, []);

  if (!valid) return <span className={className}>{date}</span>;

  return (
    <time
      dateTime={new Date(ts).toISOString()}
      title={fullDateTime(ts)}
      className={cn('whitespace-nowrap tabular-nums', className)}
    >
      {text}
    </time>
  );
}
