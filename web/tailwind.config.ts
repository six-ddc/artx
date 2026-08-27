import type { Config } from 'tailwindcss';

// Tailwind v4 is CSS-first (see src/styles.css `@theme`); this file only
// pins the content scan root explicitly so the Vite plugin never has to guess.
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
} satisfies Config;
