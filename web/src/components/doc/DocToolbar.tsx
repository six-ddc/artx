import { ExternalLink, GitCommitHorizontal } from 'lucide-react';
import type { DocDetail } from '@/lib/types';
import { api } from '@/lib/api';
import { useHistory } from '@/lib/queries';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import type { HtmlCanvasMode } from './HtmlCanvas';

interface DocToolbarProps {
  docId: string;
  doc: DocDetail;
  rev?: string;
  onRevChange: (rev: string | undefined) => void;
  mode: HtmlCanvasMode;
  onModeChange: (mode: HtmlCanvasMode) => void;
  readOnly: boolean;
}

const SEGMENTS: { mode: HtmlCanvasMode; label: string; htmlOnly?: boolean }[] = [
  { mode: 'browse', label: 'Browse' },
  { mode: 'review', label: 'Review' },
  { mode: 'edit', label: 'Edit', htmlOnly: true },
];

export function DocToolbar({ docId, doc, rev, onRevChange, mode, onModeChange, readOnly }: DocToolbarProps) {
  const { data: history } = useHistory(docId);
  const segments = SEGMENTS.filter((s) => !s.htmlOnly || doc.type === 'html');

  return (
    <div className="mb-5 flex flex-wrap items-center gap-3 border-b border-line pb-3">
      <h1 className="mr-auto truncate text-lg font-semibold text-ink">{doc.title || doc.slug}</h1>

      {readOnly && (
        <span className="art-mono rounded-full bg-addressed/15 px-2 py-0.5 text-[11px] text-addressed">
          History · read-only
        </span>
      )}

      {/* mono sha chip: version selector */}
      <div className="flex items-center gap-1 rounded-full border border-line pl-2 pr-1">
        <GitCommitHorizontal className="size-3 text-ink-3" />
        <Select
          value={rev ?? ''}
          onChange={(e) => onRevChange(e.target.value || undefined)}
          aria-label="History"
          className="h-6 border-0 pl-1 pr-5 text-[11px]"
        >
          <option value="">Working copy</option>
          {/* commits can likewise come back as null from the backend, even though the response shape marks it required. */}
          {(history?.commits ?? []).map((c) => (
            <option key={c.sha} value={c.sha}>
              {c.sha.slice(0, 7)} · {c.subject}
            </option>
          ))}
        </Select>
      </div>

      <a href={api.rawUrl(docId)} target="_blank" rel="noreferrer">
        <Button variant="outline" size="sm">
          <ExternalLink className="size-3.5" />
          raw
        </Button>
      </a>

      {/* Two-way (or three-way for html, with Edit) segmented control; the active segment gets a marker underline, not a solid fill. */}
      <div className="art-mono flex items-center overflow-hidden rounded border border-line text-xs">
        {segments.map((s, i) => {
          const active = mode === s.mode;
          const disabled = s.mode !== 'browse' && readOnly;
          return (
            <div key={s.mode} className="flex items-center">
              {i > 0 && <div className="h-4 w-px bg-line" aria-hidden />}
              <button
                type="button"
                disabled={disabled}
                onClick={() => onModeChange(s.mode)}
                className={cn(
                  'h-7 border-b-2 px-3 transition-colors disabled:pointer-events-none disabled:opacity-40',
                  active
                    ? 'border-b-marker text-ink'
                    : 'border-b-transparent text-ink-2 hover:bg-hover hover:text-ink',
                )}
              >
                {s.label}
              </button>
            </div>
          );
        })}
      </div>
    </div>
  );
}
