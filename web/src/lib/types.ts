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
  | 'reopen';

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
