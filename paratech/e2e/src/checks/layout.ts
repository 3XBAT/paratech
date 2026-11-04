// Layout invariants of ADR-0035 §5 (TASK-376), the public entry points:
//   checkLayout(page, screen)  - probe once, throw one Error that lists every violation;
//   layoutSession(page)        - collect the violations of several states of one scenario, fail once at the end;
//   watchPage(page)            - record console errors / page errors, attributed to the next check;
//   checkAgentThread(page)     - the «Стоп» row never overlaps message text.
// The rules, thresholds and the decisions behind them are in types.ts, probe.ts and evaluate.ts.
import type { Page } from "@playwright/test";

import { checkAgentThread as checkThread, type AgentThreadResult } from "./agent.js";
import { evaluateLayout, formatViolations, lineOf, pluralViolations } from "./evaluate.js";
import { probeLayoutSource } from "./probe.js";
import { type LayoutOptions, type LayoutSnapshot, type LayoutViolation, probeOptions, resolveOptions } from "./types.js";
import { drainConsole, watchPage } from "./watch.js";

export { checkAgentThread, type AgentThreadResult } from "./agent.js";
export { capPerRule, colorAlpha, evaluateAgentThread, evaluateLayout, formatViolations } from "./evaluate.js";
export { probeLayout, probeLayoutSource } from "./probe.js";
export * from "./types.js";
export { drainConsole, isWatched, watchPage } from "./watch.js";

const animationWaitMs = 2_000;

// A string, not a function: it must not depend on how the spec's TypeScript was transformed.
const settledSource = `document.getAnimations().every((a) => {
  const t = a.effect ? a.effect.getComputedTiming() : undefined;
  return a.playState !== 'running' || t === undefined || t.iterations === Infinity;
})`;

/**
 * Waits until no finite animation or transition is running (the entry animation of a screen moves its boxes), then
 * measures. Infinite animations (the breathing dots) are ignored. Never throws: a page that keeps animating is measured
 * as it is.
 */
async function settleAnimations(page: Page): Promise<void> {
  await page
    // interval polling: a hidden window (the popup) may not run requestAnimationFrame, which the default polling uses
    .waitForFunction(settledSource, undefined, { timeout: animationWaitMs, polling: 50 })
    .catch(() => undefined);
}

/** Probes the page now and returns the violations (including the console records since the last check). Never throws on violations. */
export async function layoutViolations(page: Page, options: LayoutOptions = {}): Promise<LayoutViolation[]> {
  const o = resolveOptions(options);
  watchPage(page); // idempotent; a page nobody watched yet has nothing to drain
  await settleAnimations(page);
  const snap = (await page.evaluate(`(${probeLayoutSource})(${JSON.stringify(probeOptions(o))})`)) as LayoutSnapshot;
  snap.console = drainConsole(page);
  return evaluateLayout(snap, o);
}

/** Checks the layout invariants of the page and throws ONE error that starts with `Раскладка «<screenName>»` and lists every violation. */
export async function checkLayout(page: Page, screenName: string, options: LayoutOptions = {}): Promise<void> {
  const violations = await layoutViolations(page, options);
  if (violations.length > 0) {
    throw new Error(formatViolations(screenName, violations));
  }
}

export interface LayoutSession {
  /** Probes the page for the state `name` (for example «Предметы · панель открыта»); returns the violations, never throws. */
  check(name: string, options?: LayoutOptions): Promise<LayoutViolation[]>;
  /** The agent thread check of the state `name`; its violations count for assertAll. */
  checkAgent(name: string, options?: LayoutOptions): Promise<AgentThreadResult>;
  /** Adds violations found elsewhere (for example carried from an earlier test) under the state name. */
  add(name: string, violations: LayoutViolation[]): void;
  /** Everything found so far, by state name. */
  readonly findings: ReadonlyArray<{ name: string; violations: LayoutViolation[] }>;
  /** Throws one error listing every state that had violations; nothing when all were clean. */
  assertAll(): void;
}

/** One scenario visits a screen in several states; all findings are reported, not only the first. */
export function layoutSession(page: Page, defaults: LayoutOptions = {}): LayoutSession {
  const findings: Array<{ name: string; violations: LayoutViolation[] }> = [];
  const record = (name: string, violations: LayoutViolation[]) => {
    const known = findings.find((f) => f.name === name);
    if (known) {
      known.violations.push(...violations);
    } else {
      findings.push({ name, violations });
    }
  };
  return {
    findings,
    add: record,
    async check(name, options) {
      const v = await layoutViolations(page, { ...defaults, ...options });
      record(name, v);
      return v;
    },
    async checkAgent(name, options) {
      const r = await checkThread(page, { ...defaults, ...options });
      record(name, r.violations);
      return r;
    },
    assertAll() {
      const bad = findings.filter((f) => f.violations.length > 0);
      if (bad.length === 0) {
        return;
      }
      const total = bad.reduce((n, f) => n + f.violations.length, 0);
      const first = bad[0];
      const firstV = first?.violations[0];
      const names = bad.map((f) => `«${f.name}»`).join(", ");
      const head = `Раскладка ${names}: ${total} ${pluralViolations(total)} (состояний: ${bad.length})${firstV ? `, первое: [${firstV.rule}] ${firstV.element}` : ""}`;
      const body = bad.flatMap((f) => [`[${f.name}]`, ...f.violations.map(lineOf)]);
      throw new Error([head, ...body].join("\n"));
    },
  };
}
