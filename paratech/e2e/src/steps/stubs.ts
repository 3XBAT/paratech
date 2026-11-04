// Page-side stubs (ADR-0035 §3): the page's own navigator.clipboard writes are recorded in the page and never reach the
// system clipboard, so a test cannot clobber the owner's clipboard. Types only from Playwright (pure otherwise).
// Page code runs through evalInPage (CDP Runtime.evaluate): Playwright's own page.evaluate of a string goes through eval,
// which the app CSP blocks and which the page then reports as a console error (TASK-376).
import type { BrowserContext, Page } from "@playwright/test";

import { evalInPage } from "../checks/inpage.js";

/** Where the stub keeps the texts, in the page. */
export const ClipboardStore = "__e2eClipboard";

/**
 * The stub as the source of a function with no arguments (evalInPage takes this form). Idempotent; does nothing it cannot
 * do (no throw when navigator.clipboard is missing: a stub is defined instead, so the copy button still has something to call).
 */
export const clipboardStubFunction = `() => {
  try {
    if (window.__e2eClipboardInstalled) return;
    window.__e2eClipboardInstalled = true;
    const store = (window.${ClipboardStore} = window.${ClipboardStore} || []);
    const writeText = (text) => { store.push(String(text)); return Promise.resolve(); };
    const write = (items) => {
      const jobs = [];
      for (const item of Array.from(items || [])) {
        if (item && item.types && item.types.includes('text/plain')) {
          jobs.push(item.getType('text/plain').then((b) => b.text()).then((t) => { store.push(t); }));
        }
      }
      return Promise.all(jobs).then(() => undefined);
    };
    const target = navigator.clipboard;
    if (target) {
      try { target.writeText = writeText; target.write = write; } catch (e) { /* fall through */ }
      if (target.writeText === writeText && target.write === write) return;
    }
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText, write, readText: () => Promise.resolve(''), read: () => Promise.resolve([]) },
    });
  } catch (e) { /* never break the page */ }
}`;

/** The same stub as a script for addInitScript: it runs in every new document. */
export const clipboardStubSource = `(${clipboardStubFunction})();`;

/** Stubs navigator.clipboard.writeText/write in every page of the context, now and for every later document. */
export async function installPageStubs(context: BrowserContext): Promise<void> {
  await context.addInitScript(clipboardStubSource);
  for (const page of context.pages()) {
    // The page may be closing or not scriptable yet; the init script still covers its next document.
    await evalInPage(page, clipboardStubFunction).catch(() => undefined);
  }
}

/** Texts the page tried to put on the clipboard through navigator.clipboard.writeText / write, in order. */
export async function clipboardWrites(page: Page): Promise<string[]> {
  const v = await evalInPage<unknown>(page, `() => window.${ClipboardStore} || []`);
  return Array.isArray(v) ? v.map((x) => String(x)) : [];
}
