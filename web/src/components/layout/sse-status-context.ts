import { createContext, useContext } from 'react';
import type { SSEStatus } from '@/lib/sse';

export const SSEStatusContext = createContext<SSEStatus>('connecting');

export function useSSEStatus(): SSEStatus {
  return useContext(SSEStatusContext);
}
