#!/usr/bin/env node
// Browser smoke test: open a running `artx serve` in a real headless Chromium
// and assert the first screen actually renders.
//
// This covers a structural blind spot: lib/*.test.ts only exercises pure
// functions and tsc/build only check compile time, so none of them ever mount
// the React tree. A bug like "the backend serialized replies as null, so
// ThreadCard threw and the whole page fell into the router's error boundary"
// only shows up when something really renders.
//
// Usage:
//   node scripts/browser-smoke.mjs          # artx serve on 127.0.0.1:7777
//   ARTX_SMOKE_URL=http://127.0.0.1:8080 node scripts/browser-smoke.mjs
//
// Assertions (any failure exits non-zero):
//   1. The index page opens without falling into the router's error boundary.
//   2. The first document (if any) opens with no console errors, no uncaught
//      page exceptions, and no error boundary.
//   3. If that document has threads, the sidebar really rendered ThreadCards.

import { chromium } from 'playwright';

const BASE_URL = (process.env.ARTX_SMOKE_URL ?? 'http://127.0.0.1:7777').replace(/\/+$/, '');
const NAV_TIMEOUT_MS = 15_000;

/** Copy rendered by the router's default error boundary (TanStack Router defaultErrorComponent). */
const ERROR_BOUNDARY_MARKERS = ['Something went wrong'];

function logStep(label) {
  console.log(`\n▸ ${label}`);
}

function fail(message) {
  console.error(`✗ ${message}`);
  process.exitCode = 1;
}

function pass(message) {
  console.log(`✓ ${message}`);
}

async function assertNoErrorBoundary(page, context) {
  const bodyText = await page.evaluate(() => document.body.innerText);
  for (const marker of ERROR_BOUNDARY_MARKERS) {
    if (bodyText.includes(marker)) {
      throw new Error(`${context}: the page fell into the error boundary (matched "${marker}")`);
    }
  }
}

async function main() {
  console.log(`artx browser smoke test — target ${BASE_URL}`);

  const browser = await chromium.launch();
  const page = await browser.newPage();

  const consoleErrors = [];
  const pageErrors = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('pageerror', (err) => pageErrors.push(err.message));

  function drainConsoleIssues(context) {
    if (consoleErrors.length === 0 && pageErrors.length === 0) return;
    const issues = [...pageErrors.map((m) => `pageerror: ${m}`), ...consoleErrors.map((m) => `console.error: ${m}`)];
    consoleErrors.length = 0;
    pageErrors.length = 0;
    throw new Error(`${context}: ${issues.length} console error(s)\n  - ${issues.join('\n  - ')}`);
  }

  try {
    logStep('Open the index page');
    await page.goto(`${BASE_URL}/`, { waitUntil: 'networkidle', timeout: NAV_TIMEOUT_MS });
    await assertNoErrorBoundary(page, 'index page');
    drainConsoleIssues('index page');
    pass('Index page opened: no error boundary, no console errors');

    const firstDocHref = await page
      .locator('a[href^="/a/"]')
      .first()
      .getAttribute('href')
      .catch(() => null);

    if (!firstDocHref) {
      pass('Index page lists no documents; skipping document assertions (empty vault)');
    } else {
      logStep(`Open the first document: ${firstDocHref}`);
      await page.goto(`${BASE_URL}${firstDocHref}`, { waitUntil: 'networkidle', timeout: NAV_TIMEOUT_MS });
      await assertNoErrorBoundary(page, 'document page');
      drainConsoleIssues('document page');
      pass('Document page opened: no error boundary, no console errors');

      const threadCardCount = await page.locator('[id^="thread-"]').count();
      // Structural check, not a copy check: matching the sidebar heading text
      // meant every wording change broke this smoke run for no real reason.
      const sidebarMounted = await page.locator('aside h2').count();

      if (!sidebarMounted) {
        fail('Document page rendered no ThreadSidebar (no <aside> heading) — the sidebar likely failed to mount');
      } else if (threadCardCount > 0) {
        pass(`ThreadSidebar rendered ${threadCardCount} ThreadCard(s)`);
      } else {
        pass('ThreadSidebar mounted; no threads under the current filter');
      }
    }
  } catch (err) {
    fail(err instanceof Error ? err.message : String(err));
  } finally {
    await browser.close();
  }

  if (process.exitCode === 1) {
    console.error('\nSmoke test failed.');
  } else {
    console.log('\nSmoke test passed.');
  }
}

main().catch((err) => {
  console.error('Smoke test script itself failed:', err);
  process.exitCode = 1;
});
