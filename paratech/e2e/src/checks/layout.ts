// Layout invariants of ADR-0035 §5 (TASK-376), the public entry points:
//   checkLayout(page, screen)  - probe once, throw one Error that lists every violation;
//   layoutSession(page)        - collect the violations of several states of one scenario, fail once at the end;
//   watchPage(page)            - record console errors / page errors, attributed to the next check;
//   checkAgentThread(page)     - the «Стоп» row never overlaps message text;
//   evalInPage(page, fn, arg)  - page-side code through CDP (the page CSP blocks Playwright's own evaluate).
// The rules, thresholds and the decisions behind them are in types.ts, probe.ts and evaluate.ts.
import type { Page } from "@playwright/test";

import { checkAgentThread as checkThread, type AgentThreadResult } from "./agent.js";
import { evaluateLayout, formatViolations, lineOf, pluralViolations } from "./evaluate.js";
import { evalInPage } from "./inpage.js";
import { probeLayoutSource } from "./probe.js";
import { emptySnapshot, type LayoutCounts, type LayoutOptions, type LayoutSnapshot, type LayoutViolation, probeOptions, resolveOptions } from "./types.js";
import { drainConsole, watchPage } from "./watch.js";

export { checkAgentThread, type AgentThreadResult } from "./agent.js";
export { capPerRule, colorAlpha, evaluateAgentThread, evaluateLayout, formatCounts, formatViolations } from "./evaluate.js";
export { evalInPage, releaseEvalSession } from "./inpage.js";
export { probeLayout, probeLayoutSource } from "./probe.js";
export * from "./types.js";
export { drainConsole, isWatched, watchPage } from "./watch.js";

const animationWaitMs = 2_000;
const animationPollMs = 50;

// A string, not a function: it must not depend on how the spec's TypeScript was transformed.
const settledSource = `() => document.getAnimations().every((a) => {
  const t = a.effect ? a.effect.getComputedTiming() : undefined;
  return a.playState !== 'running' || t === undefined || t.iterations === Infinity;
})`;

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Waits until no finite animation or transition is running (the entry animation of a screen moves its boxes), then
 * measures. Infinite animations (the breathing dots) are ignored. Never throws: a page that keeps animating is measured
 * as it is. Polled by timer from here, not in the page: a hidden window (the popup) may not run requestAnimationFrame.
 */
async function settleAnimations(page: Page): Promise<void> {
  const until = Date.now() + animationWaitMs;
  for (;;) {
    try {
      if (await evalInPage<boolean>(page, settledSource)) {
        return;
      }
    } catch {
      return; // the probe that follows reports a page that cannot be evaluated
    }
    if (Date.now() >= until) {
      return;
    }
    await sleep(animationPollMs);
  }
}

export interface LayoutReport {
  violations: LayoutViolation[];
  /** What was examined (visible elements, icon buttons, statuses, titles, text nodes ...). */
  counts: LayoutCounts;
}

/** Probes the page now: the violations (including the console records since the last check) and what was examined. Never throws on violations. */
export async function layoutReport(page: Page, options: LayoutOptions = {}): Promise<LayoutReport> {
  const o = resolveOptions(options);
  watchPage(page); // idempotent; a page nobody watched yet has nothing to drain
  await settleAnimations(page);
  const snap = await evalInPage<LayoutSnapshot>(page, probeLayoutSource, probeOptions(o));
  snap.console = drainConsole(page);
  return { violations: evaluateLayout(snap, o), counts: snap.counts };
}

/** Probes the page now and returns the violations (including the console records since the last check). Never throws on violations. */
export async function layoutViolations(page: Page, options: LayoutOptions = {}): Promise<LayoutViolation[]> {
  return (await layoutReport(page, options)).violations;
}

/** Only the console records since the last check (no probe): for the end of a stage that is not a layout state, for example the seeding. */
export function consoleViolations(page: Page, options: LayoutOptions = {}): LayoutViolation[] {
  watchPage(page);
  const snap = emptySnapshot();
  snap.console = drainConsole(page);
  return evaluateLayout(snap, options);
}

/** Checks the layout invariants of the page and throws ONE error that starts with `Раскладка «<screenName>»` and lists every violation. */
export async function checkLayout(page: Page, screenName: string, options: LayoutOptions = {}): Promise<void> {
  const violations = await layoutViolations(page, options);
  if (violations.length > 0) {
    throw new Error(formatViolations(screenName, violations));
  }
}

export interface LayoutFinding {
  name: string;
  violations: LayoutViolation[];
  /** What the last probe of this state examined; undefined for states added from elsewhere and for the agent thread. */
  counts?: LayoutCounts;
}

export interface SessionHooks {
  /**
   * Called after every probe of a state (counts: undefined for the agent thread), also when it is clean. A failing hook
   * never fails the check: the hook is for reporting (counts line, screenshot of the offending state).
   */
  onChecked?(name: string, violations: LayoutViolation[], counts: LayoutCounts | undefined): Promise<void> | void;
}

export interface LayoutSession {
  /** Probes the page for the state `name` (for example «Предметы · панель открыта»); returns the violations, never throws on them. */
  check(name: string, options?: LayoutOptions): Promise<LayoutViolation[]>;
  /** The agent thread check of the state `name`; its violations count for assertAll. */
  checkAgent(name: string, options?: LayoutOptions): Promise<AgentThreadResult>;
  /** Adds violations found elsewhere (for example carried from an earlier test) under the state name. */
  add(name: string, violations: LayoutViolation[]): void;
  /** Everything found so far, by state name. */
  readonly findings: ReadonlyArray<LayoutFinding>;
  /** Throws one error listing every state that had violations; nothing when all were clean. */
  assertAll(): void;
}

/** One scenario visits a screen in several states; all findings are reported, not only the first. */
export function layoutSession(page: Page, defaults: LayoutOptions = {}, hooks: SessionHooks = {}): LayoutSession {
  const findings: LayoutFinding[] = [];
  const record = (name: string, violations: LayoutViolation[], counts?: LayoutCounts) => {
    const known = findings.find((f) => f.name === name);
    if (known) {
      known.violations.push(...violations);
      if (counts) {
        known.counts = counts;
      }
    } else {
      findings.push(counts ? { name, violations, counts } : { name, violations });
    }
  };
  const notify = async (name: string, violations: LayoutViolation[], counts: LayoutCounts | undefined) => {
    try {
      await hooks.onChecked?.(name, violations, counts);
    } catch {
      // reporting must not hide a finding
    }
  };
  return {
    findings,
    add: (name, violations) => record(name, violations),
    async check(name, options) {
      const r = await layoutReport(page, { ...defaults, ...options });
      record(name, r.violations, r.counts);
      await notify(name, r.violations, r.counts);
      return r.violations;
    },
    async checkAgent(name, options) {
      const r = await checkThread(page, { ...defaults, ...options });
      record(name, r.violations);
      await notify(name, r.violations, undefined);
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
