// Plain data the reporter collects from Playwright and the renderer turns into report.md. No Playwright types here,
// so the rendering is testable with literals.
import type { RunState } from "../contract.js";

export type RunMode = "SCRIPTED" | "LIVE";

/** How a test ended. "notRun" is a test the runner never started (setup failed, serial failure, filtered by a failure). */
export type TestOutcome = "passed" | "flaky" | "failed" | "timedOut" | "interrupted" | "skipped" | "notRun";

export interface TestRecord {
  title: string;
  /** The row of the report: annotation `feature`, else the title. */
  feature: string;
  mode: RunMode;
  /** Annotation `requires`: the feature that must be «готово» first. */
  requires?: string;
  /** Folder under specs/ ("smoke"); empty when unknown. */
  pack: string;
  outcome: TestOutcome;
  /** Reason given to test.skip / test.fixme. */
  skipReason?: string;
  /** Message of the error, as Playwright gave it (multi-line, maybe with colour codes). */
  error?: string;
  /** Name of the deepest test.step that carries the error. */
  brokenStep?: string;
  /** The error is an environment problem (message starts with EnvironmentMarker). */
  environment?: boolean;
  /** Links relative to the report folder: "screenshots/<file>.png". */
  screenshots: string[];
  durationMs: number;
}

export interface ReportInput {
  tests: TestRecord[];
  state?: RunState;
  mode: RunMode;
  /** Messages of reporter.onError (setup failures and the like). */
  runnerErrors: string[];
  elapsedMs: number;
  /** ISO time of the start of the run, when known. */
  startedAt?: string;
}

export type Verdict = "ready" | "broken" | "unchecked";

export interface TestVerdict {
  verdict: Verdict;
  /** "готово после повтора" and similar notes. */
  note?: string;
  /** For unchecked: what is needed. */
  need?: string;
  /** For broken: step and first line of the error. */
  step?: string;
  message?: string;
}

export interface Row {
  feature: string;
  modes: string;
  verdict: Verdict;
  /** The text of the «Итог» cell. */
  outcome: string;
  /** The text of the «Шаг, где сломалось» cell, "" when none. */
  step: string;
  snapshots: string[];
}

export interface EnvProblem {
  kind: string;
  message: string;
  /** The machine or the setup is not ready: nothing could be checked (state.json or onError), not one test. */
  global: boolean;
}
