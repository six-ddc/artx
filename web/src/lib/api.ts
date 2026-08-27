// Thin fetch wrapper. Fields/endpoints follow docs/blueprint.md §5 exactly;
// field names match web/src/lib/types.ts (frozen).

import type {
  CommentsResponse,
  DocDetail,
  DocsResponse,
  ErrorResponse,
  EventRequest,
  EventResponse,
  HealthResponse,
} from './types';

export class ApiError extends Error {
  readonly status: number;
  readonly body: ErrorResponse;

  constructor(status: number, body: ErrorResponse) {
    super(body.message || body.error);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

async function request<T>(input: string, init?: RequestInit): Promise<T> {
  const res = await fetch(input, {
    ...init,
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  });

  if (!res.ok) {
    let body: ErrorResponse;
    try {
      body = (await res.json()) as ErrorResponse;
    } catch {
      body = { error: 'internal', message: res.statusText || 'request failed' };
    }
    throw new ApiError(res.status, body);
  }

  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

/** GET /api/docs/{id}/history response; not mirrored in types.ts (this endpoint doesn't return an api.* DTO). */
export interface HistoryCommit {
  sha: string;
  subject: string;
  author: string;
  date: string;
}

export interface HistoryResponse {
  commits: HistoryCommit[];
}

function docPath(id: string): string {
  return `/api/docs/${encodeURIComponent(id)}`;
}

export const api = {
  health: () => request<HealthResponse>('/api/health'),

  docs: () => request<DocsResponse>('/api/docs'),

  doc: (id: string, rev?: string) =>
    request<DocDetail>(`${docPath(id)}${rev ? `?v=${encodeURIComponent(rev)}` : ''}`),

  history: (id: string) => request<HistoryResponse>(`${docPath(id)}/history`),

  comments: (id: string) => request<CommentsResponse>(`${docPath(id)}/comments`),

  postEvent: (id: string, body: EventRequest) =>
    request<EventResponse>(`${docPath(id)}/events`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  /** M2: element edit write-back (protocol: protocol.ts EditMsg). Not listed in §5.1; added alongside M2. */
  postElement: (id: string, body: { aid: string; html: string }) =>
    request<void>(`${docPath(id)}/element`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  /** Raw source bytes (`text/plain`), used by DocToolbar's raw link. */
  rawUrl: (id: string) => `${docPath(id)}/raw`,
};
