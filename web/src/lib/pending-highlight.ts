// The "pending comment" selection echo, painted via the CSS Custom
// Highlight API rather than DOM <mark> surgery: the native selection
// collapses the moment focus moves into the sidebar composer's textarea,
// and mutating the prose DOM (HighlightLayer-style) would fight both
// React's dangerouslySetInnerHTML re-renders and the still-live Range the
// eventual submit needs. A registry entry has neither problem.
//
// On browsers without the API this no-ops: commenting still works, only
// the visual echo is missing.

const NAME = 'art-pending';

type HighlightRegistry = Map<string, unknown>;

function registry(): HighlightRegistry | null {
  const css = CSS as unknown as { highlights?: HighlightRegistry };
  return css.highlights ?? null;
}

export function setPendingHighlight(range: Range): void {
  const HighlightCtor = (globalThis as { Highlight?: new (range: Range) => unknown }).Highlight;
  const reg = registry();
  if (!reg || !HighlightCtor) return;
  reg.set(NAME, new HighlightCtor(range));
}

export function clearPendingHighlight(): void {
  registry()?.delete(NAME);
}
