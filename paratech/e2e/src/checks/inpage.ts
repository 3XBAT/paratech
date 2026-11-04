// evalInPage: runs page-side code through a CDP session (Runtime.evaluate) instead of page.evaluate. The app has the
// CSP `default-src 'self'` (no 'unsafe-eval'); Playwright's own evaluate of the main world goes through eval and is
// blocked by it, and the page then logs «Note that 'script-src' was not explicitly set…» as a console error, which the
// console watch would blame on the app. Evaluation started by the inspector is exempt from the page CSP and does not
// print to the page console. The CSP is NOT bypassed (Page.setBypassCSP would change what is tested).
import type { CDPSession, Page } from "@playwright/test";

const sessions = new WeakMap<Page, Promise<CDPSession>>();

const defaultTimeoutMs = 30_000;

function sessionOf(page: Page): Promise<CDPSession> {
  let session = sessions.get(page);
  if (session === undefined) {
    const created = page.context().newCDPSession(page);
    session = created;
    sessions.set(page, created);
    // a failed attach is not cached; a closed page drops its session
    created.catch(() => {
      if (sessions.get(page) === created) {
        sessions.delete(page);
      }
    });
    page.once("close", () => {
      sessions.delete(page);
    });
  }
  return session;
}

/** Forgets the cached session of the page and detaches it. Never throws. */
export async function releaseEvalSession(page: Page): Promise<void> {
  const session = sessions.get(page);
  sessions.delete(page);
  if (session !== undefined) {
    await session
      .then((s) => s.detach())
      .catch(() => undefined);
  }
}

interface RemoteException {
  text?: string;
  exception?: { description?: string; value?: unknown };
}

function describeException(d: RemoteException): string {
  const text = d.exception?.description ?? (d.exception?.value !== undefined ? String(d.exception.value) : undefined) ?? d.text ?? "unknown error";
  return text.split("\n").slice(0, 3).join(" | ");
}

async function race<T>(work: Promise<T>, ms: number): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  const limit = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`no answer in ${ms} ms`)), ms);
  });
  try {
    return await Promise.race([work, limit]);
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Evaluates `fn(arg)` in the main world of the page and returns its value (awaiting a promise; the value must be
 * JSON-serialisable). `fn` is a function or the source text of one (`"() => 1"`, `"function (a) {...}"`); the argument is
 * passed as JSON. A function is sent by its toString(), so it must be self-contained (no closure), like the probes.
 * Throws a readable error when the code throws.
 */
export async function evalInPage<R = unknown>(page: Page, fn: string | ((arg: never) => unknown), arg?: unknown, timeoutMs = defaultTimeoutMs): Promise<R> {
  const source = typeof fn === "string" ? fn : fn.toString();
  const expression = `(${source})(${arg === undefined ? "" : JSON.stringify(arg)})`;
  for (let attempt = 0; ; attempt++) {
    let result;
    try {
      const cdp = await sessionOf(page);
      result = await race(cdp.send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true, userGesture: false }), timeoutMs);
    } catch (err) {
      sessions.delete(page); // a stale session (the target was replaced) is re-created once
      const message = err instanceof Error ? err.message : String(err);
      if (attempt === 0 && /detached|session|closed|not found/i.test(message) && !page.isClosed()) {
        continue;
      }
      throw new Error(`evalInPage: ${message.split("\n")[0]}`);
    }
    if (result.exceptionDetails !== undefined) {
      throw new Error(`evalInPage: код страницы упал: ${describeException(result.exceptionDetails)}`);
    }
    return result.result.value as R;
  }
}
