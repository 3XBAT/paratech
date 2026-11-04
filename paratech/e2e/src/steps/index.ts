// What a spec imports: `import { test, expect, step } from "../../src/steps/index.js"`. ADR-0035 §2, §3, §6.
// No browser is launched: the `page` fixture of Playwright is not used, the pages come from the running app over CDP.
import { expect, test as base } from "@playwright/test";
import type { Page, TestInfo } from "@playwright/test";

import { attachApp, type AttachedApp } from "../app/cdp.js";
import { type Dialog, EnvCdpUrl, EnvCtlDir, EnvFakeUrl, EnvironmentMarker, EnvRunRoot, type OpenedRecord } from "../contract.js";
import { readRunState } from "../report/state.js";
import { getCurrent, setCurrent, unscriptedGuard } from "./current.js";
import { envError, requireEnv } from "./env.js";
import { createLlm, type Llm } from "./llm.js";
import { readOpened } from "./opened.js";
import { appendPick, consumedPicks } from "./picks.js";
import { savePageShot } from "./shots.js";
import { captureWindows, step } from "./step.js";
import { startTrace, stopTrace } from "./trace.js";

export { expect, step };
export { clipboardWrites, installPageStubs } from "./stubs.js";
export { type ConsumedPick, consumedPicks, PickError } from "./picks.js";
export { createLlm, describeCall, type Llm, type RecordedRequest, type Rule, UnscriptedLlmError } from "./llm.js";
export type { AttachedApp, Dialog, OpenedRecord };

export type PickFn = (dialog: Dialog, paths: string[]) => Promise<void>;

interface TestFixtures {
  /** The main window of the app. */
  main: Page;
  /** The popup window, or undefined when it does not exist (waits up to 2 s for it). */
  popup: Page | undefined;
  /** Queues the answer of the next native open dialog: pick("recording", ["fixtures/clip.wav"]). */
  pick: PickFn;
  /** What the app would have opened or copied so far (opened.jsonl). */
  opened: () => OpenedRecord[];
  /** The fake Messages API: script the answers, read what the Hub sent. */
  llm: Llm;
  /** Auto: current test for step(), per-test trace, unscripted-call check at the end. */
  e2eGuard: void;
}

interface WorkerFixtures {
  app: AttachedApp;
}

function hasFailed(testInfo: TestInfo): boolean {
  return testInfo.status !== undefined && testInfo.status !== testInfo.expectedStatus;
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  app: [
    async ({}, use) => {
      const root = process.env[EnvRunRoot];
      const state = root ? readRunState(root) : undefined;
      if (state?.environmentError) {
        const msg = state.environmentError.message;
        throw msg.includes(EnvironmentMarker) ? new Error(msg) : envError(msg);
      }
      const app = await attachApp(requireEnv(EnvCdpUrl));
      try {
        await use(app);
      } finally {
        await app.close().catch(() => undefined);
      }
    },
    { scope: "worker", timeout: 60_000 },
  ],

  main: async ({ app }, use) => {
    await use(await app.main());
  },

  popup: async ({ app }, use) => {
    await use(await app.popup(2_000));
  },

  pick: async ({}, use) => {
    await use(async (dialog, paths) => {
      appendPick(requireEnv(EnvCtlDir), dialog, paths);
    });
  },

  opened: async ({}, use) => {
    await use(() => readOpened(requireEnv(EnvCtlDir)));
  },

  llm: async ({}, use) => {
    await use(createLlm(requireEnv(EnvFakeUrl), undefined, unscriptedGuard()));
  },

  e2eGuard: [
    async ({ app }, use, testInfo) => {
      setCurrent({ testInfo, app });
      await unscriptedGuard()
        ?.baseline()
        .catch(() => undefined);
      await startTrace(app.context);
      let unscripted: Error | undefined;
      try {
        await use();
      } finally {
        let bad = hasFailed(testInfo);
        if (!bad) {
          // A test that passed can still have made an unscripted provider call nobody looked at.
          unscripted = await unscriptedGuard()
            ?.check()
            .catch(() => undefined);
          if (unscripted) {
            bad = true;
            await captureWindows("llm");
          }
        }
        await stopTrace(app.context, bad, testInfo.title);
        setCurrent(undefined);
      }
      if (unscripted) {
        throw unscripted;
      }
    },
    { auto: true },
  ],
});

/**
 * Takes a screenshot of the page (default: the main window) and attaches it to the test under the name the report
 * links. Use it for the screenshots that belong to a passing scenario; failures are captured by step().
 */
export async function snapshot(name: string, page?: Page): Promise<void> {
  const cur = getCurrent();
  if (!cur) {
    throw new Error("snapshot(): outside a test of src/steps");
  }
  await savePageShot(cur.testInfo, page ?? (await cur.app.main()), name);
}

/**
 * Waits until the app has used `count` lines of picks.jsonl and checks that they were taken (not cancelled): a
 * «cancelled» line carries the app's reason, so a wrong fixture path fails here with the cause.
 */
export async function expectPickConsumed(count: number): Promise<void> {
  const ctl = requireEnv(EnvCtlDir);
  await expect
    .poll(() => consumedPicks(ctl).length, { message: `the app should have used ${count} pick line(s) of picks.jsonl` })
    .toBe(count);
  const refused = consumedPicks(ctl).filter((p) => p.result !== "picked");
  expect(refused, `the app cancelled a dialog: ${refused.map((p) => `${p.dialog}: ${p.reason ?? "?"}`).join("; ")}`).toEqual([]);
}
