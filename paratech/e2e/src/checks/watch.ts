// Console watch: errors the page printed or threw since the previous layout check, so a console error is attributed to
// the screen that produced it. Unhandled promise rejections arrive as `pageerror`. Failed requests are recorded too, as
// context: Chromium prints «Failed to load resource: 404» without the URL in the text (TASK-376 follow-up).
import type { Page } from "@playwright/test";

import type { ConsoleRecord } from "./types.js";

interface Watch {
  records: ConsoleRecord[];
}

const watches = new WeakMap<Page, Watch>();

/**
 * A URL for a record: scheme, host and path only. The query and the fragment may carry a token, user info a password, so
 * none of them is kept; a text that is not a URL is cut to 120 characters up to its first `?` or `#`.
 */
export function safeUrl(raw: string): string {
  try {
    const u = new URL(raw);
    if (u.protocol === "data:" || u.protocol === "blob:" || u.protocol === "javascript:") {
      return `${u.protocol}…`;
    }
    return `${u.protocol}//${u.host}${u.pathname}`;
  } catch {
    return raw.split(/[?#]/)[0]?.slice(0, 120) ?? "";
  }
}

/** «GET http://127.0.0.1:34115/favicon.ico → 404» (a status) or «… — net::ERR_FAILED» (a transport failure). */
export function requestRecord(method: string, rawUrl: string, outcome: { status: number } | { failure: string }): ConsoleRecord {
  const what = "status" in outcome ? `→ ${outcome.status}` : `— ${outcome.failure || "сбой запроса"}`;
  return { kind: "request", text: `${method} ${safeUrl(rawUrl)} ${what}` };
}

/** Starts recording console errors, page errors and failed requests of the page. Idempotent: a second call changes nothing. */
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
    // url:line of the message, so a false or real error is traceable; a message with no source (a CSP note) has none
    if (loc.url || loc.lineNumber > 0) {
      rec.location = `${loc.url || "?"}:${loc.lineNumber}`;
    }
    w.records.push(rec);
  });
  page.on("pageerror", (err) => {
    // An Error has a message; a thrown string or object arrives as is.
    const text = err instanceof Error ? err.message : String(err);
    w.records.push({ kind: "pageerror", text });
  });
  page.on("requestfailed", (req) => {
    w.records.push(requestRecord(req.method(), req.url(), { failure: req.failure()?.errorText ?? "" }));
  });
  page.on("response", (res) => {
    if (res.status() >= 400) {
      w.records.push(requestRecord(res.request().method(), res.url(), { status: res.status() }));
    }
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
