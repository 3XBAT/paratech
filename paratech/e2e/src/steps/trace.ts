// Best-effort Playwright trace of one test, kept only when the test failed (<run root>/report/traces/<slug>.zip).
// Over connectOverCDP to the real WebView2 tracing may not work: the first failure turns it off for the worker, and
// it never fails a test.
import { mkdirSync } from "node:fs";
import path from "node:path";

import type { BrowserContext } from "@playwright/test";

import { reportDir, uniqueFile } from "./shots.js";
import { stepSlug } from "./slug.js";
import { withTimeout } from "./util.js";

const startTimeoutMs = 10_000;
const stopTimeoutMs = 30_000;

let disabled = false;
let running = false;
let counter = 0;

export async function startTrace(context: BrowserContext): Promise<void> {
  if (disabled || running) {
    return;
  }
  try {
    await withTimeout(context.tracing.start({ screenshots: true, snapshots: true }), startTimeoutMs, "tracing.start");
    running = true;
  } catch {
    disabled = true;
  }
}

/** Stops the trace; saves it when keep is true, else discards it. Returns the saved file, if any. */
export async function stopTrace(context: BrowserContext, keep: boolean, title: string): Promise<string | undefined> {
  if (!running) {
    return undefined;
  }
  running = false;
  try {
    const dir = reportDir();
    if (!keep || !dir) {
      await withTimeout(context.tracing.stop(), stopTimeoutMs, "tracing.stop");
      return undefined;
    }
    mkdirSync(path.join(dir, "traces"), { recursive: true });
    const file = uniqueFile(path.join(dir, "traces"), stepSlug(title, ++counter), ".zip");
    await withTimeout(context.tracing.stop({ path: file }), stopTimeoutMs, "tracing.stop");
    return file;
  } catch {
    disabled = true;
    return undefined;
  }
}
