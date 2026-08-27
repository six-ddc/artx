import path from 'node:path';
import { defineConfig } from 'vite';

/**
 * Separate build entry for the reviewer script injected into sandboxed html
 * artifacts (R2: vanilla, zero dependencies — React never enters the
 * iframe). Built as a standalone iife with a fixed, unhashed filename
 * because the backend injects it at the constant path `/_art/reviewer.js`
 * (see internal/htmlaid.ReviewerScriptPath in the blueprint).
 *
 * Run after the main `vite build` with `emptyOutDir: false` so it adds to
 * web/dist/ instead of wiping the app's output.
 */
export default defineConfig({
  base: '/_art/',
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    lib: {
      entry: path.resolve(import.meta.dirname, 'src/reviewer/reviewer.ts'),
      formats: ['iife'],
      name: 'ArtReviewer',
      fileName: () => 'reviewer.js',
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true,
      },
    },
  },
});
