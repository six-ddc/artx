import { createContext, useContext } from 'react';

export interface DocsSearchState {
  query: string;
  setQuery: (query: string) => void;
}

export const DocsSearchContext = createContext<DocsSearchState>({
  query: '',
  setQuery: () => {},
});

export function useDocsSearch(): DocsSearchState {
  return useContext(DocsSearchContext);
}
