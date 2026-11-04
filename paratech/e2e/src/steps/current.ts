// The test that is running in this worker, set by the auto fixture in index.ts, so step() and snapshot() can be
// imported on their own. Only types come from Playwright.
import type { TestInfo } from "@playwright/test";

import type { AttachedApp } from "../app/cdp.js";
import { EnvFakeUrl } from "../contract.js";
import { createLlm, UnscriptedGuard } from "./llm.js";

export interface CurrentTest {
  testInfo: TestInfo;
  app: AttachedApp;
}

let current: CurrentTest | undefined;

export function setCurrent(next: CurrentTest | undefined): void {
  current = next;
}

export function getCurrent(): CurrentTest | undefined {
  return current;
}

let guard: { url: string; guard: UnscriptedGuard } | undefined;

/** The guard of the fake LLM of this run, or undefined when the run has no fake (nothing to check). */
export function unscriptedGuard(env: NodeJS.ProcessEnv = process.env): UnscriptedGuard | undefined {
  const url = env[EnvFakeUrl];
  if (!url) {
    return undefined;
  }
  if (guard?.url !== url) {
    guard = { url, guard: new UnscriptedGuard(createLlm(url)) };
  }
  return guard.guard;
}
