import { useEffect, type RefObject } from 'react';

// §6.6: mermaid / katex are lazy-loaded purely client-side, only import()'d
// when has_mermaid / has_math is true, and never from a CDN (the binary
// needs to run offline). The Go side does nothing special to support this.

async function renderMermaid(root: HTMLElement): Promise<void> {
  const blocks = Array.from(root.querySelectorAll<HTMLElement>('pre > code.language-mermaid'));
  if (blocks.length === 0) return;

  const { default: mermaid } = await import('mermaid');
  mermaid.initialize({
    startOnLoad: false,
    theme: window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'default',
  });

  const nodes = blocks.map((code, i) => {
    const pre = code.parentElement;
    const div = document.createElement('div');
    div.className = 'mermaid';
    div.id = `art-mermaid-${i}-${Math.random().toString(36).slice(2, 8)}`;
    div.textContent = code.textContent ?? '';
    pre?.replaceWith(div);
    return div;
  });

  await mermaid.run({ nodes });
}

type AutoRenderFn = (
  element: HTMLElement,
  options: {
    delimiters: { left: string; right: string; display: boolean }[];
    ignoredTags?: string[];
    throwOnError?: boolean;
  },
) => void;

async function renderMath(root: HTMLElement): Promise<void> {
  const [autoRenderModule] = await Promise.all([
    import('katex/contrib/auto-render'),
    import('katex/dist/katex.min.css'),
  ]);
  const renderMathInElement = autoRenderModule.default as unknown as AutoRenderFn;
  renderMathInElement(root, {
    delimiters: [
      { left: '$$', right: '$$', display: true },
      { left: '$', right: '$', display: false },
    ],
    // Avoids a `$` inside a code block being mistaken for a math delimiter and swallowed.
    ignoredTags: ['pre', 'code', 'script', 'style', 'textarea'],
    throwOnError: false,
  });
}

interface MermaidMathProps {
  containerRef: RefObject<HTMLDivElement | null>;
  hasMermaid: boolean;
  hasMath: boolean;
  html?: string;
}

export function MermaidMath({ containerRef, hasMermaid, hasMath, html }: MermaidMathProps) {
  useEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    if (hasMermaid) void renderMermaid(root);
    if (hasMath) void renderMath(root);
  }, [containerRef, hasMermaid, hasMath, html]);

  return null;
}
