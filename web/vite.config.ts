import path from 'node:path';
import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const BACKEND = 'http://127.0.0.1:7777';

/**
 * Dev-only convenience: serve the vanilla reviewer bundle at the same fixed
 * path the backend injects in production (`/_artx/reviewer.js`), without
 * needing a separate `vite build --watch` running alongside `pnpm dev`.
 * Built on demand via the Vite JS API using the real reviewer config, so the
 * dev output takes the same iife/no-deps shape as the production build.
 */
function reviewerDevMiddleware(): Plugin {
  return {
    name: 'art-reviewer-dev-middleware',
    apply: 'serve',
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        if (req.url !== '/_artx/reviewer.js') {
          next();
          return;
        }
        try {
          const { build } = await import('vite');
          const result = await build({
            configFile: path.resolve(import.meta.dirname, 'vite.reviewer.config.ts'),
            logLevel: 'silent',
            build: { write: false },
          });
          const output = Array.isArray(result) ? result[0] : result;
          const chunk =
            output && 'output' in output ? output.output[0] : undefined;
          const code = chunk && 'code' in chunk ? chunk.code : '';
          res.setHeader('Content-Type', 'application/javascript; charset=utf-8');
          res.end(code);
        } catch (err) {
          next(err instanceof Error ? err : new Error(String(err)));
        }
      });
    },
  };
}

export default defineConfig({
  base: '/_artx/',
  plugins: [
    // @vitejs/plugin-react 6.x is oxc-based (Vite 8/rolldown); React Compiler
    // is wired in via `compiler: true` + the oxc-transform-react peer dep,
    // not a babel plugin.
    react({ compiler: true }),
    tailwindcss(),
    reviewerDevMiddleware(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, 'src'),
    },
  },
  server: {
    proxy: {
      '/api': { target: BACKEND, changeOrigin: true },
      '/raw': { target: BACKEND, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    // src/reviewer/reviewer.ts is built separately by vite.reviewer.config.ts
    // (fixed-name iife entry, R2). Exclude it from the app's module graph so
    // it never gets pulled into a hashed chunk here.
    rollupOptions: {
      input: path.resolve(import.meta.dirname, 'index.html'),
    },
  },
});
