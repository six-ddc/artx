import { createContext, useContext } from 'react';

// The doc route renders its share of the global topbar (title, version
// picker, comments toggle) INTO the Header via a portal; this context hands
// the portal target element from Header down to whichever route wants it.
export interface HeaderSlot {
  el: HTMLElement | null;
  setEl: (el: HTMLElement | null) => void;
}

export const HeaderSlotContext = createContext<HeaderSlot>({ el: null, setEl: () => {} });

export function useHeaderSlot(): HeaderSlot {
  return useContext(HeaderSlotContext);
}
