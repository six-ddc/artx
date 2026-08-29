// TanStack Query key table + hooks. Key shapes follow docs/blueprint.md §7.3 exactly.

import { useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { api } from './api';
import type { EventRequest } from './types';

export const queryKeys = {
  health: (): QueryKey => ['health'],
  docs: (): QueryKey => ['docs'],
  /** rev undefined means the working-copy version. */
  doc: (docId: string, rev?: string): QueryKey => ['doc', docId, rev],
  history: (docId: string): QueryKey => ['history', docId],
  comments: (docId: string): QueryKey => ['comments', docId],
  raw: (docId: string): QueryKey => ['raw', docId],
  /** to undefined means against the working copy. Invalidate via the ['diff', docId] prefix. */
  diff: (docId: string, from: string, to?: string): QueryKey => ['diff', docId, from, to],
};

export function useHealth() {
  return useQuery({
    queryKey: queryKeys.health(),
    queryFn: api.health,
    staleTime: Infinity,
  });
}

export function useDocs() {
  return useQuery({ queryKey: queryKeys.docs(), queryFn: api.docs });
}

export function useDoc(docId: string, rev?: string) {
  return useQuery({
    queryKey: queryKeys.doc(docId, rev),
    queryFn: () => api.doc(docId, rev),
    enabled: docId.length > 0,
  });
}

export function useHistory(docId: string) {
  return useQuery({
    queryKey: queryKeys.history(docId),
    queryFn: () => api.history(docId),
    enabled: docId.length > 0,
  });
}

/** Version compare (from → to ?? working copy); only fetched while a compare is active. */
export function useDocDiff(docId: string, from?: string, to?: string) {
  return useQuery({
    queryKey: queryKeys.diff(docId, from ?? '', to),
    queryFn: () => api.diff(docId, from ?? '', to),
    enabled: docId.length > 0 && Boolean(from),
  });
}

export function useComments(docId: string) {
  return useQuery({
    queryKey: queryKeys.comments(docId),
    queryFn: () => api.comments(docId),
    enabled: docId.length > 0,
  });
}

/**
 * All comment write operations (create/reply/edit/addressed/resolve/reopen)
 * go through POST /api/docs/{id}/events, dispatched by EventRequest.type, so
 * a single mutation hook covers all of them. No optimistic updates (§7.3):
 * the server fills in the thread id, the anchor's precise offsets, and the
 * approx flag, so we let onSuccess's invalidate fetch the authoritative result.
 */
export function usePostEvent(docId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: EventRequest) => api.postEvent(docId, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.comments(docId) });
    },
  });
}

/** Raw md source for the block editor; only fetched while edit mode is active. */
export function useRawSource(docId: string, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.raw(docId),
    queryFn: () => api.raw(docId),
    enabled: enabled && docId.length > 0,
  });
}

/**
 * md block edit write-back; the rendered doc and the raw source are stale on
 * success — and so are the commit history (every save lands a git commit)
 * and any diff against the working copy, hence the extra invalidations.
 */
export function usePostBlock(docId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { start: number; end: number; original: string; content: string }) =>
      api.postBlock(docId, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.doc(docId, undefined) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.raw(docId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.history(docId) });
      void queryClient.invalidateQueries({ queryKey: ['diff', docId] });
    },
  });
}

/** M2: element edit write-back; refresh the doc's content on success (html changed), plus history/diff like usePostBlock. */
export function usePostElement(docId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { aid: string; html: string }) => api.postElement(docId, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.doc(docId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.history(docId) });
      void queryClient.invalidateQueries({ queryKey: ['diff', docId] });
    },
  });
}
