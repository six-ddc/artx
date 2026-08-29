// EventSource → invalidate mapping. Event table follows docs/blueprint.md §5.4 / §7.3 exactly.

import { useEffect, useState } from 'react';
import { useQueryClient, type QueryKey } from '@tanstack/react-query';
import type { SSEComment, SSEDocChange } from './types';
import { queryKeys } from './queries';

export type ArtSSEEventType = 'comments' | 'doc' | 'docs' | 'ping';
export type SSEStatus = 'connecting' | 'open' | 'closed';

/**
 * Pure function, testable without a real EventSource: given an event type
 * and its parsed payload, returns the list of query keys to invalidate.
 *
 * - comments → invalidate(['comments', doc])
 * - doc      → invalidate(['doc', doc]), ['history', doc] and ['diff', doc]
 *              (a content change lands a git commit, so the version list and
 *              any diff against the working copy are stale too); also
 *              ['comments', doc] when kind === 'remap'
 * - docs     → invalidate(['docs'])
 * - ping     → none (heartbeat only)
 */
export function invalidationKeysForEvent(type: ArtSSEEventType, data: unknown): QueryKey[] {
  switch (type) {
    case 'comments': {
      const { doc } = data as SSEComment;
      return [queryKeys.comments(doc)];
    }
    case 'doc': {
      const { doc, kind } = data as SSEDocChange;
      // ['doc', doc] and ['diff', doc] are partial keys without rev/from/to:
      // they prefix-match every version and compare query for this doc.
      const keys: QueryKey[] = [['doc', doc], queryKeys.history(doc), ['diff', doc]];
      if (kind === 'remap') {
        keys.push(queryKeys.comments(doc));
      }
      return keys;
    }
    case 'docs':
      return [queryKeys.docs()];
    case 'ping':
      return [];
  }
}

const EVENT_TYPES: ArtSSEEventType[] = ['comments', 'doc', 'docs', 'ping'];

function parseData(raw: string): unknown {
  if (!raw) return {};
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return {};
  }
}

/**
 * Subscribes to GET /api/stream and invalidates the matching query key per
 * event type. The browser auto-reconnects on drop; after reconnecting (the
 * second and later 'open' events) we invalidate every key to catch up on
 * whatever changed during the gap.
 */
export function useEventStream(): SSEStatus {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<SSEStatus>('connecting');

  useEffect(() => {
    const source = new EventSource('/api/stream');
    let hasOpenedBefore = false;

    const handleOpen = () => {
      setStatus('open');
      if (hasOpenedBefore) {
        void queryClient.invalidateQueries();
      }
      hasOpenedBefore = true;
    };
    const handleError = () => setStatus('closed');

    const cleanups = EVENT_TYPES.map((type) => {
      const listener = (event: MessageEvent<string>) => {
        const data = parseData(event.data);
        for (const key of invalidationKeysForEvent(type, data)) {
          void queryClient.invalidateQueries({ queryKey: key });
        }
      };
      source.addEventListener(type, listener);
      return () => source.removeEventListener(type, listener);
    });

    source.addEventListener('open', handleOpen);
    source.addEventListener('error', handleError);

    return () => {
      source.removeEventListener('open', handleOpen);
      source.removeEventListener('error', handleError);
      for (const cleanup of cleanups) cleanup();
      source.close();
    };
  }, [queryClient]);

  return status;
}
