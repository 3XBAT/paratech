// step(): test.step with the two things a scenario needs on failure: a screenshot of the windows and the
// «unscripted LLM call» rule (supervisor rule: a scenario fails on the first provider call no rule answered).
import { test } from "@playwright/test";

import { firstLine } from "../report/rows.js";
import { scrubSecrets } from "../report/scrub.js";
import { getCurrent, unscriptedGuard } from "./current.js";
import { pagesToCapture, savePageShot } from "./shots.js";
import type { UnscriptedLlmError } from "./llm.js";

/** Screenshots every window of the app and attaches the files. Best effort: it never throws. */
export async function captureWindows(name: string): Promise<void> {
  const cur = getCurrent();
  if (!cur) {
    return;
  }
  for (const [label, page] of await pagesToCapture(cur.app)) {
    try {
      await savePageShot(cur.testInfo, page, name, label);
    } catch {
      // a closed page or a hidden window must not hide the real error
    }
  }
}

/** The unscripted-call error not yet reported, if any. A fake that cannot be asked is ignored here. */
async function pendingUnscripted(strict: boolean): Promise<UnscriptedLlmError | undefined> {
  try {
    return await unscriptedGuard()?.check();
  } catch (err) {
    if (strict) {
      throw err;
    }
    return undefined;
  }
}

/**
 * Runs fn as a Playwright step. On failure the windows are screenshotted (attachment «снимок», file in the report
 * folder) and the original error is rethrown, unless the fake LLM saw an unscripted call: that is the root cause
 * and is thrown first. After a passing body the unscripted-call check runs too. Returns fn's result.
 */
export async function step<T>(name: string, fn: () => Promise<T> | T): Promise<T> {
  return test.step(name, async () => {
    let result: T;
    try {
      result = await fn();
    } catch (err) {
      const unscripted = await pendingUnscripted(false);
      await captureOnce(unscripted ?? err, name);
      if (!unscripted) {
        throw err;
      }
      unscripted.cause = err;
      unscripted.message = scrubSecrets(`${unscripted.message}; the step then failed: ${firstLine(errorText(err))}`);
      throw unscripted;
    }
    const unscripted = await pendingUnscripted(true);
    if (unscripted) {
      await captureOnce(unscripted, name);
      throw unscripted;
    }
    return result;
  });
}

// An error passes through every enclosing step; the innermost one has the window as it was, so only it captures.
const captured = new WeakSet<object>();

async function captureOnce(err: unknown, name: string): Promise<void> {
  if (typeof err === "object" && err !== null) {
    if (captured.has(err)) {
      return;
    }
    captured.add(err);
  }
  await captureWindows(name);
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
