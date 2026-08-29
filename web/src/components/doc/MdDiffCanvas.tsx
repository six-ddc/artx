import { useEffect, useMemo, useRef } from 'react';
import type { DiffBlock, DiffResponse, DocDetail } from '@/lib/types';
import { findTextRange } from './HighlightLayer';
import { MermaidMath } from './MermaidMath';

// The time-machine rendered diff: the NEW document's server-rendered HTML,
// with diff washes painted onto its top-level blocks and each removed
// block's old-version HTML re-inserted where it used to live. The whole
// thing is composed as a string BEFORE mount (DOMParser in a useMemo), not
// patched into the live DOM afterwards — that ordering is what lets
// MermaidMath see the re-inserted blocks and render mermaid/katex inside
// them exactly like the surviving ones.

const INS_HIGHLIGHT = 'art-diff-ins';
/** Marks a modified block with its index into diff.blocks, so the word-level effect can find its added_texts. */
const IDX_ATTR = 'data-diff-block';
/** A modified block where at least one added_text was located: the green
 *  word highlight carries the change, so the CSS clears the yellow wash. */
const LOCATED_CLASS = 'art-diff-located';

/**
 * Strips the leading block markers markdown syntax puts on a source line
 * (list bullets, ordered-list numbers, heading hashes, blockquote arrows,
 * nested combinations) — none of which exist in the rendered DOM text.
 * added_texts are source segments, so a new list item arrives as
 * "- item text\n" and a verbatim search can never find the "- ".
 */
export function stripBlockMarkers(line: string): string {
  return line.replace(/^\s*(?:[-*+]\s+|\d+[.)]\s+|#{1,6}\s+|>\s*)+/, '').trim();
}

/** Splits a line on inline markdown syntax chars into plain-text sub-segments, dropping fragments shorter than 3 chars. */
export function inlineSegments(line: string): string[] {
  return line
    .split(/[`*_[\]()~]+/)
    .map((s) => s.trim())
    .filter((s) => s.length >= 3);
}

/**
 * Locates the given source-text segments inside el's rendered subtree,
 * most-precise attempt first: the whole segment (findTextRange already
 * normalizes inline code/strong/blockquote syntax), then each line with
 * its block markers stripped, then that line's inline sub-segments. Any
 * hit counts — the caller treats a non-empty result as "located" and
 * clears the block wash; an empty one keeps the yellow degraded state.
 * Exported for tests.
 */
export function locateAddedTexts(el: HTMLElement, texts: string[]): Range[] {
  const ranges: Range[] = [];
  for (const text of texts) {
    const whole = findTextRange(el, text);
    if (whole) {
      ranges.push(whole);
      continue;
    }
    for (const rawLine of text.split('\n')) {
      const line = stripBlockMarkers(rawLine);
      if (!line) continue;
      const lineRange = findTextRange(el, line);
      if (lineRange) {
        ranges.push(lineRange);
        continue;
      }
      for (const seg of inlineSegments(line)) {
        const segRange = findTextRange(el, seg);
        if (segRange) ranges.push(segRange);
      }
    }
  }
  return ranges;
}

/**
 * Walks the diff ops and the rendered HTML's top-level elements with two
 * pointers, joined on data-sourcepos (each op's `to` range is the same
 * file-absolute byte range goldmark stamped on the element). Nodes without
 * a sourcepos (e.g. <hr>) are skipped in order on both sides; an op whose
 * range never matches degrades to uncolored rather than blocking the render.
 */
function composeDiffHtml(html: string, blocks: DiffBlock[]): string {
  const parsed = new DOMParser().parseFromString(html, 'text/html');
  const body = parsed.body;
  const children = Array.from(body.children);
  let j = 0;
  // Removed wrappers buffer here until the next op that successfully joins
  // a rendered node, then insert directly before THAT node. Inserting at
  // the raw pointer instead would drop every buffered block before
  // whatever keyless node (e.g. <hr>) the pointer happens to rest on,
  // collapsing removed blocks separated by an <hr> onto its one side.
  const pendingRemoved: HTMLElement[] = [];

  for (let i = 0; i < blocks.length; i++) {
    const op = blocks[i]!;

    if (op.op === 'removed') {
      const wrapper = parsed.createElement('div');
      wrapper.className = 'art-diff-removed';
      wrapper.innerHTML = op.html ?? '';
      // The old block carries OLD-version sourcepos ranges; renamed so no
      // anchor/edit machinery can ever mistake them for live blocks.
      for (const el of wrapper.querySelectorAll('[data-sourcepos]')) {
        el.setAttribute('data-diff-sourcepos', el.getAttribute('data-sourcepos') ?? '');
        el.removeAttribute('data-sourcepos');
      }
      pendingRemoved.push(wrapper);
      continue;
    }

    if (!op.to) continue;
    const key = `${op.to[0]}:${op.to[1]}`;
    let k = j;
    while (k < children.length && children[k]!.getAttribute('data-sourcepos') !== key) {
      k++;
    }
    if (k >= children.length) continue; // no match: degrade, keep the pointer
    const el = children[k]!;
    j = k + 1;

    // blocks arrive in new-document order with removed ops already placed
    // before their old successor, and el is that successor's rendered node.
    for (const wrapper of pendingRemoved) {
      body.insertBefore(wrapper, el);
    }
    pendingRemoved.length = 0;

    if (op.op === 'added') {
      el.classList.add('art-diff-added');
    } else if (op.op === 'modified') {
      el.classList.add('art-diff-modified');
      el.setAttribute(IDX_ATTR, String(i));
      if (op.removed_texts?.length) {
        el.setAttribute('title', `Removed: ${op.removed_texts.join(' · ')}`);
      }
    }
  }

  // Trailing removed blocks (deleted from the end of the document) have no
  // successor to anchor on; they belong at the end.
  for (const wrapper of pendingRemoved) {
    body.appendChild(wrapper);
  }

  return body.innerHTML;
}

interface MdDiffCanvasProps {
  /** The NEW side of the compare (v ?? working copy), fetched by the caller. */
  doc: DocDetail;
  diff: DiffResponse;
}

export function MdDiffCanvas({ doc, diff }: MdDiffCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const blocks = diff.blocks ?? [];

  const html = useMemo(() => composeDiffHtml(doc.html ?? '', diff.blocks ?? []), [
    doc.html,
    diff.blocks,
  ]);
  // A removed block may hold the document's only mermaid diagram or math
  // block, so the doc-level flags alone would leave it as a dead code fence
  // (or literal $$…$$). Recompute both against the composed HTML. The math
  // signal is a plain `$` scan — coarse, but the failure mode is just a
  // needlessly lazy-loaded katex whose auto-render finds nothing, while a
  // miss would show removed formulas as source text.
  const hasMermaid = Boolean(doc.has_mermaid) || html.includes('language-mermaid');
  const hasMath = Boolean(doc.has_math) || html.includes('$');

  // Word-level inserted text, via the CSS Custom Highlight registry (same
  // register/clean-up pattern as lib/pending-highlight.ts: no DOM surgery,
  // no fight with dangerouslySetInnerHTML; browsers without the API just
  // miss the word-level tier, the block wash still shows). Green leads:
  // a block whose added_texts were located gets LOCATED_CLASS, which
  // clears its yellow wash — the wash survives only as the degraded state
  // (words not locatable, or a purely-deletion modification). The class is
  // applied and removed alongside the Highlight registration so the two
  // can never drift apart.
  useEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const reg = (CSS as unknown as { highlights?: Map<string, unknown> }).highlights;
    const HighlightCtor = (globalThis as { Highlight?: new (...ranges: Range[]) => unknown })
      .Highlight;
    if (!reg || !HighlightCtor) return;

    const ranges: Range[] = [];
    const located: HTMLElement[] = [];
    for (const el of root.querySelectorAll<HTMLElement>(`[${IDX_ATTR}]`)) {
      const block = blocks[Number(el.getAttribute(IDX_ATTR))];
      const found = locateAddedTexts(el, block?.added_texts ?? []);
      if (found.length > 0) {
        ranges.push(...found);
        el.classList.add(LOCATED_CLASS);
        located.push(el);
      }
    }
    if (ranges.length > 0) reg.set(INS_HIGHLIGHT, new HighlightCtor(...ranges));
    return () => {
      reg.delete(INS_HIGHLIGHT);
      for (const el of located) el.classList.remove(LOCATED_CLASS);
    };
  }, [html, blocks]);

  return (
    <div className="mx-auto w-full max-w-[46rem] px-6 py-10 sm:px-8 sm:py-14">
      <div
        ref={containerRef}
        className="art-prose mx-auto"
        dangerouslySetInnerHTML={{ __html: html }}
      />
      <MermaidMath
        containerRef={containerRef}
        hasMermaid={hasMermaid}
        hasMath={hasMath}
        html={html}
      />
    </div>
  );
}
