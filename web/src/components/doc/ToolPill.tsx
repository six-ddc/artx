import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';

export interface ToolPillItem<T extends string> {
  tool: T;
  label: string;
  icon: LucideIcon;
}

/**
 * The floating cursor-tool pill shared by both canvases (bottom center,
 * devtools-picker style): one active tool, Esc handling and one-shot
 * semantics stay with the owning canvas.
 */
export function ToolPill<T extends string>({
  tools,
  active,
  onSelect,
}: {
  tools: ToolPillItem<T>[];
  active: T;
  onSelect: (tool: T) => void;
}) {
  return (
    <div className="fixed bottom-5 left-1/2 z-10 flex -translate-x-1/2 items-center gap-0.5 rounded-full border bg-card p-1 shadow-lg">
      {tools.map(({ tool, label, icon: Icon }) => (
        <button
          key={tool}
          type="button"
          title={label}
          aria-pressed={active === tool}
          onClick={() => onSelect(tool)}
          className={cn(
            'flex size-7 items-center justify-center rounded-full transition-colors',
            active === tool
              ? 'bg-primary text-primary-foreground'
              : 'text-muted-foreground hover:bg-accent hover:text-foreground',
          )}
        >
          <Icon className="size-3.5" />
        </button>
      ))}
    </div>
  );
}
