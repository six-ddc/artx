import { useState } from 'react';
import { Monitor, Moon, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';

type Theme = 'system' | 'light' | 'dark';

const KEY = 'artx-theme';
const ORDER: Theme[] = ['system', 'light', 'dark'];
const ICON = { system: Monitor, light: Sun, dark: Moon } as const;
const LABEL: Record<Theme, string> = {
  system: 'Theme: system',
  light: 'Theme: light',
  dark: 'Theme: dark',
};

function readTheme(): Theme {
  try {
    const t = localStorage.getItem(KEY);
    return t === 'light' || t === 'dark' ? t : 'system';
  } catch {
    return 'system';
  }
}

/* The CSS side is already three-path (see styles.css): bare :root is light,
   the media query covers system-dark, and [data-theme] wins in both
   directions. This toggle only moves the data-theme stamp; index.html's
   inline script restores it before first paint so there's no flash.
   colorScheme is forced alongside so native widgets (scrollbars, form
   controls) follow the override, not the OS. */
function applyTheme(t: Theme) {
  const root = document.documentElement;
  if (t === 'system') {
    delete root.dataset.theme;
    root.style.colorScheme = '';
  } else {
    root.dataset.theme = t;
    root.style.colorScheme = t;
  }
  try {
    if (t === 'system') localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, t);
  } catch {
    /* private mode etc. — the choice just won't persist */
  }
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(readTheme);
  const Icon = ICON[theme];

  function cycle() {
    const next = ORDER[(ORDER.indexOf(theme) + 1) % ORDER.length] ?? 'system';
    applyTheme(next);
    setTheme(next);
  }

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      title={LABEL[theme]}
      aria-label={LABEL[theme]}
      onClick={cycle}
    >
      <Icon className="size-3.5" />
    </Button>
  );
}
