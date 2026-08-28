import { useEffect, useState } from 'react';
import { getRouteApi } from '@tanstack/react-router';
import type { SelectionInput } from '@/lib/types';
import { useComments, useDoc } from '@/lib/queries';
import { clearPendingHighlight, setPendingHighlight } from '@/lib/pending-highlight';
import { DocHeaderBar } from '@/components/doc/DocHeaderBar';
import { MdCanvas } from '@/components/doc/MdCanvas';
import { HtmlCanvas } from '@/components/doc/HtmlCanvas';
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
  // The in-flight new comment: anchor input captured from a selection or a
  // block pick, composing in the drawer while the prose shows the pending
  // highlight. preview is display-only — a whole-block pick sends an empty
  // exact (the server's anchor-the-block path), so the composer shows the
  // block's rendered text instead.
  const [pending, setPending] = useState<{ selection: SelectionInput; preview: string } | null>(
    null,
  );
  // The comments drawer: null = the user hasn't chosen yet, so it defaults
  // to open exactly when the doc has open threads — a clean doc reads as a
  // plain page, a doc under review leads with its marginalia.
  const [commentsChoice, setCommentsChoice] = useState<boolean | null>(null);

  const readOnly = Boolean(search.v);
  const docQuery = useDoc(docId, search.v);
  const commentsQuery = useComments(docId);
  const threads = commentsQuery.data?.threads ?? [];
  const openCount = threads.filter((t) => t.status === 'open').length;
  const commentsOpen = commentsChoice ?? openCount > 0;

  // The pending highlight lives in the global CSS highlight registry, not
  // React state — clear it whenever this view unmounts or switches docs.
  useEffect(() => {
    return () => clearPendingHighlight();
  }, [docId]);

  // Focusing a thread (clicking a highlight in the prose, or arriving with
  // ?t=) must surface its card even when the drawer is closed.
  useEffect(() => {
    if (search.t) setCommentsChoice(true);
  }, [search.t]);

  function setRev(rev: string | undefined) {
    void navigate({ search: (prev) => ({ ...prev, v: rev }) });
  }

  function focusThread(threadId: string) {
    // Clicking always means "take me there" — never a toggle-off (that read
    // as "clicking does nothing", since unfocusing is visually subtle).
    // HighlightLayer only scrolls when the focused id CHANGES, so a
    // re-click on the already-focused thread re-scrolls imperatively.
    if (search.t === threadId) {
      document
        .querySelector(`[data-art-thread="${CSS.escape(threadId)}"]`)
        ?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      return;
    }
    void navigate({ search: (prev) => ({ ...prev, t: threadId }) });
  }

  function startComment(selection: SelectionInput, range: Range) {
    setPendingHighlight(range);
    const preview = selection.exact || range.toString().replace(/\s+/g, ' ').trim().slice(0, 200);
    setPending({ selection, preview });
    setCommentsChoice(true); // the composer lives in the drawer
  }

  function endComment() {
    clearPendingHighlight();
    setPending(null);
  }

  if (docQuery.isPending) {
    return <p className="p-6 text-sm text-muted-foreground">Loading…</p>;
  }
  if (docQuery.isError) {
    return <p className="p-6 text-sm text-destructive">Failed to load: {docQuery.error.message}</p>;
  }

  const doc = docQuery.data;

  return (
    <>
      <DocHeaderBar
        docId={docId}
        doc={doc}
        rev={search.v}
        onRevChange={setRev}
        readOnly={readOnly}
        openCount={openCount}
        commentsOpen={commentsOpen}
        onToggleComments={() => setCommentsChoice(!commentsOpen)}
      />
      <div className="flex min-h-[calc(100dvh-3rem)] items-start">
        <div className="min-w-0 flex-1">
          {doc.type === 'md' ? (
            <MdCanvas
              doc={doc}
              docId={docId}
              threads={threads}
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
              readOnly={readOnly}
              focusedThreadId={search.t}
            />
          )}
        </div>
        {commentsOpen && (
          <ThreadSidebar
            docId={docId}
            threads={threads}
            focusedThreadId={search.t}
            onFocusThread={focusThread}
            onClearFocus={() => void navigate({ search: (prev) => ({ ...prev, t: undefined }) })}
            onClose={() => setCommentsChoice(false)}
            composer={
              pending && (
                <CommentComposer
                  docId={docId}
                  selection={pending.selection}
                  quote={pending.preview}
                  onDone={endComment}
                />
              )
            }
          />
        )}
      </div>
    </>
  );
}
