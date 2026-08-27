import type { Rect } from '@/lib/protocol';

/** Draws a box on the shell page from HoverMsg.rect (rect is in iframe-viewport coordinates, 1:1 with the iframe's own box). */
export function HoverOutline({ rect, tag }: { rect: Rect; tag: string }) {
  return (
    <div
      className="pointer-events-none absolute z-10 rounded-sm border-[1.5px] border-marker"
      style={{ left: rect.x, top: rect.y, width: rect.w, height: rect.h }}
    >
      <span className="art-mono absolute -top-5 left-0 rounded bg-marker px-1 text-[10px] leading-4 text-marker-ink">
        {tag}
      </span>
    </div>
  );
}
