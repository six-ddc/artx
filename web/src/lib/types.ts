// TypeScript mirror of internal/api.
//
// Frozen contract: this file must match internal/api/api.go field for field.
// W-web must not add or remove fields on its own; if the Go side changes,
// this file changes too, and vice versa.
// Hand-written rather than generated because there's only one DTO — the
// payoff from a codegen tool doesn't outweigh the build complexity it adds.

export type DocType = 'md' | 'html';
export type ThreadStatus = 'open' | 'addressed' | 'resolved';
export type AnchorKind = 'text' | 'element';

/** Fixed hint shown to agents for orphan threads; must match api.OrphanHint. */
export const ORPHAN_HINT =
  'The anchored text no longer exists — the feedback was likely addressed. Confirm with resolve, or re-anchor.';

export interface Doc {
  id: string;
  slug: string;
  title: string;
  type: DocType;
  path: string;
  rel_path: string;
  url: string;
  rev: string;
  mod_time: string;
  size: number;
  open_count: number;
  total_count: number;
}

export interface DocDetail extends Doc {
  /** md: HTML already rendered by goldmark on the Go side; block-level
   *  elements carry data-sourcepos="start:end".
   *  The frontend only does dangerouslySetInnerHTML — **never re-renders markdown**. */
  html?: string;
  body_offset?: number;
  frontmatter?: Record<string, unknown>;
  has_mermaid?: boolean;
  has_math?: boolean;
  /** html: the iframe src, shaped like /raw/{id}/ */
  raw_url?: string;
  rev0?: string;
}

export interface DocsResponse {
  vault: string;
  root: string;
  docs: Doc[];
}

export interface ThreadAnchor {
  kind: AnchorKind;
  exact?: string;
  prefix?: string;
  suffix?: string;
  start: number;
  end: number;
  line: number;
  context?: string;
  aid?: string;
  rev?: string;
  approx?: boolean;
  orphan?: boolean;
  last_exact?: string;
}

export interface Reply {
  id: string;
  author: string;
  body: string;
  created_at: string;
  edited_at?: string;
}

export interface Addressed {
  by: string;
  commit?: string;
  note?: string;
  at: string;
}

export interface Thread {
  thread: string;
  doc: string;
  slug: string;
  path: string;
  status: ThreadStatus;
  author: string;
  body: string;
  created_at: string;
  updated_at: string;
  edited_at?: string;
  created_rev?: string;
  anchor: ThreadAnchor;
  replies: Reply[];
  addressed?: Addressed;
  hint?: string;
}

export interface CommentsResponse {
  doc: string;
  threads: Thread[];
  warnings?: string[];
}

/**
 * md selection report shape.
 *
 * Key contract: the frontend **does not** convert to source-file offsets.
 * It only reports the start/end offsets of the "nearest block carrying
 * data-sourcepos", the selected rendered-form text, and the surrounding
 * text within that block. The Go side does the quote match within the
 * block's source to compute the final anchor.
 */
export interface SelectionInput {
  block_start: number;
  block_end: number;
  exact: string;
  before: string;
  after: string;
}

export interface ElementInput {
  aid: string;
  quote?: string;
}

export type EventType =
  | 'create'
  | 'reply'
  | 'edit'
  | 'addressed'
  | 'resolve'
  | 'reopen'
  | 'delete';

export interface EventRequest {
  type: EventType;
  thread?: string;
  target?: string;
  body?: string;
  author?: string;
  commit?: string;
  note?: string;
  selection?: SelectionInput;
  element?: ElementInput;
}

export interface EventResponse {
  ok: string;
  thread: string;
  event_id: string;
  status?: ThreadStatus;
}

export interface HealthResponse {
  ok: string;
  version: string;
  vault: string;
  root: string;
  watch: boolean;
}

export interface ErrorResponse {
  error: string;
  message: string;
  detail?: string;
}

// --- Diff ------------------------------------------------------------------

export type DiffBlockOp = 'unchanged' | 'modified' | 'removed' | 'added';
export type DiffElementOp = 'changed' | 'added' | 'removed';
export type DiffLineOp = 'ctx' | 'del' | 'add';

/**
 * One top-level block in the md diff. `from`/`to` are [start, end) byte
 * ranges into the old/new source — `to` is the join key against the
 * rendered HTML's data-sourcepos. blocks arrive in new-document order with
 * removed blocks already inserted before their old successor, so the
 * frontend never sorts.
 */
export interface DiffBlock {
  op: DiffBlockOp;
  from?: [number, number];
  to?: [number, number];
  /** modified only: word-level source-text segments for CSS Highlight painting. */
  added_texts?: string[];
  removed_texts?: string[];
  /** removed only: the block rendered against the OLD document, ready to re-insert. */
  html?: string;
}

/** One element in the html diff, keyed by its stable data-aid. */
export interface DiffElement {
  op: DiffElementOp;
  aid: string;
  /** removed only: the element's outerHTML for the removed-elements sidebar. */
  html?: string;
}

export interface DiffLine {
  op: DiffLineOp;
  text: string;
}

export interface DiffHunk {
  from_start: number;
  from_count: number;
  to_start: number;
  to_count: number;
  lines: DiffLine[];
}

export interface DiffStats {
  added: number;
  removed: number;
  modified: number;
}

/** GET /api/docs/{id}/diff?from=&to= — `to` empty means the working copy. */
export interface DiffResponse {
  doc: string;
  type: DocType;
  from: string;
  to: string;
  /** md documents: block-level ops. */
  blocks?: DiffBlock[];
  /** html documents: element-level ops keyed by data-aid. */
  elements?: DiffElement[];
  /** Always present ([] when nothing changed), unlike the type-specific ops above. */
  hunks: DiffHunk[];
  stats: DiffStats;
  /** md: the frontmatter bytes differ (the block table only covers the body). */
  frontmatter_changed?: boolean;
  /** html: <head>/<style>/<script> changed — element highlights don't cover it. */
  chrome_changed?: boolean;
}

// --- SSE -------------------------------------------------------------------

export interface SSEComment {
  doc: string;
  threads?: string[];
  rev?: string;
}

export interface SSEDocChange {
  doc: string;
  kind: 'content' | 'remap' | 'aid' | 'remove';
  rev?: string;
  remaps?: number;
  orphans?: number;
}
