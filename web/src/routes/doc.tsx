import { useEffect, useState } from 'react';
import { getRouteApi } from '@tanstack/react-router';
import type { SelectionInput } from '@/lib/types';
import { useComments, useDoc } from '@/lib/queries';
import { clearPendingHighlight, setPendingHighlight } from '@/lib/pending-highlight';
import { DocToolbar } from '@/components/doc/DocToolbar';
import { MdCanvas } from '@/components/doc/MdCanvas';
import { HtmlCanvas, type HtmlCanvasMode } from '@/components/doc/HtmlCanvas';
import { ThreadSidebar } from '@/components/threads/ThreadSidebar';
import { CommentComposer } from '@/components/threads/CommentComposer';

export interface DocViewSearch {
  /** Historical git sha; undefined = the working-copy version. */
  v?: string;
  /** The focused thread id. */
  t?: string;
}

const routeApi = getRouteApi('/a/$docId');

export function DocView() {
  const { docId } = routeApi.useParams();
  const search = routeApi.useSearch();
  const navigate = routeApi.useNavigate();
  const [mode, setMode] = useState<HtmlCanvasMode>('browse');
  // The in-flight new comment: anchor input captured from a selection,
  // composing in the sidebar while the prose shows the pending highlight.
  const [pending, setPending] = useState<SelectionInput | null>(null);

  const readOnly = Boolean(search.v);
  const docQuery = useDoc(docId, search.v);
  const commentsQuery = useComments(docId);

  // The pending highlight lives in the global CSS highlight registry, not
  // React state — clear it whenever this view unmounts or switches docs.
  useEffect(() => {
    return () => clearPendingHighlight();
  }, [docId]);

  function setRev(rev: string | undefined) {
    void navigate({ search: (prev) => ({ ...prev, v: rev }) });
  }

  function focusThread(threadId: string) {
    void navigate({ search: (prev) => ({ ...prev, t: prev.t === threadId ? undefined : threadId }) });
  }

  function startComment(selection: SelectionInput, range: Range) {
    setPendingHighlight(range);
    setPending(selection);
  }

  function endComment() {
    clearPendingHighlight();
    setPending(null);
  }

  if (docQuery.isPending) {
    return <p className="text-sm text-ink-2">Loading…</p>;
  }
  if (docQuery.isError) {
    return <p className="text-sm text-danger">Failed to load: {docQuery.error.message}</p>;
  }

  const doc = docQuery.data;
  const threads = commentsQuery.data?.threads ?? [];
  const effectiveMode: HtmlCanvasMode = readOnly && mode !== 'browse' ? 'browse' : mode;

  return (
    <div>
      <DocToolbar
        docId={docId}
        doc={doc}
        rev={search.v}
        onRevChange={setRev}
        mode={effectiveMode}
        onModeChange={setMode}
        readOnly={readOnly}
      />
      <div className="flex flex-col gap-6 lg:flex-row lg:items-start">
        <div className="min-w-0 flex-1">
          {doc.type === 'md' ? (
            <MdCanvas
              doc={doc}
              threads={threads}
              reviewMode={effectiveMode === 'review'}
              readOnly={readOnly}
              focusedThreadId={search.t}
              onFocusThread={focusThread}
              onStartComment={startComment}
            />
          ) : (
            <HtmlCanvas
              doc={doc}
              docId={docId}
              threads={threads}
              mode={effectiveMode}
              focusedThreadId={search.t}
            />
          )}
        </div>
        <ThreadSidebar
          docId={docId}
          threads={threads}
          focusedThreadId={search.t}
          onFocusThread={focusThread}
          composer={
            pending && <CommentComposer docId={docId} selection={pending} onDone={endComment} />
          }
        />
      </div>
    </div>
  );
}
