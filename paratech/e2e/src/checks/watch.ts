// Console watch: errors the page printed or threw since the previous layout check, so a console error is attributed to
// the screen that produced it. Unhandled promise rejections arrive as `pageerror`.
import type { Page } from "@playwright/test";

import type { ConsoleRecord } from "./types.js";

interface Watch {
  records: ConsoleRecord[];
}

const watches = new WeakMap<Page, Watch>();

/** Starts recording console errors and page errors of the page. Idempotent: a second call changes nothing. */
export function watchPage(page: Page): void {
  if (watches.has(page)) {
    return;
  }
  const w: Watch = { records: [] };
  watches.set(page, w);
  page.on("console", (msg) => {
    if (msg.type() !== "error") {
      return;
    }
    const loc = msg.location();
    const rec: ConsoleRecord = { kind: "console", text: msg.text() };
    if (loc.url) {
      rec.location = `${loc.url}:${loc.lineNumber}`;
    }
    w.records.push(rec);
  });
  page.on("pageerror", (err) => {
    // An Error has a message; a thrown string or object arrives as is.
    const text = err instanceof Error ? err.message : String(err);
    w.records.push({ kind: "pageerror", text });
  });
}

/** The records since the last drain, which are then forgotten. A page that was never watched has none. */
export function drainConsole(page: Page): ConsoleRecord[] {
  const w = watches.get(page);
  return w ? w.records.splice(0) : [];
}

export function isWatched(page: Page): boolean {
  return watches.has(page);
}
