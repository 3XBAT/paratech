// Turns a Playwright TestCase and its last result into a TestRecord. Works on structural "Like" types, which the
// real TestCase / TestResult satisfy, so tests can feed plain objects and nothing here needs the Playwright runner.
import { AnnotationFeature, AnnotationMode, AnnotationRequires } from "../contract.js";
import type { RunMode, TestOutcome, TestRecord } from "./model.js";
import { isEnvironmentMessage } from "./rows.js";
import type { AttachmentLike } from "./screenshots.js";

export interface AnnotationLike {
  type: string;
  description?: string;
}

export interface ErrorLike {
  message?: string;
}

export interface StepLike {
  title: string;
  category: string;
  error?: ErrorLike;
  steps: readonly StepLike[];
}

export interface ResultLike {
  status: "passed" | "failed" | "timedOut" | "skipped" | "interrupted";
  duration: number;
  error?: ErrorLike;
  errors: readonly ErrorLike[];
  steps: readonly StepLike[];
  attachments: readonly AttachmentLike[];
  annotations?: readonly AnnotationLike[];
}

export interface TestLike {
  title: string;
  annotations: readonly AnnotationLike[];
  results: readonly ResultLike[];
  outcome(): "skipped" | "expected" | "unexpected" | "flaky";
  location: { file: string };
}

/** Value of the last annotation of a type: runtime ones (on the result) win over the static ones of the test. */
function annotation(test: TestLike, result: ResultLike | undefined, type: string): string | undefined {
  const all = [...test.annotations, ...(result?.annotations ?? [])];
  for (let i = all.length - 1; i >= 0; i--) {
    const a = all[i];
    if (a && a.type === type && a.description?.trim()) {
      return a.description.trim();
    }
  }
  return undefined;
}

/** "specs/<pack>/x.spec.ts" -> "<pack>". */
export function packOf(file: string): string {
  return /[\\/]specs[\\/]([^\\/]+)[\\/]/.exec(file)?.[1] ?? "";
}

/** Title of the deepest test.step that carries the error; steps of other categories (expect, hooks) are skipped. */
export function deepestBrokenStep(steps: readonly StepLike[]): string | undefined {
  for (const s of steps) {
    if (!s.error) {
      continue;
    }
    const inner = deepestBrokenStep(s.steps);
    if (inner) {
      return inner;
    }
    if (s.category === "test.step") {
      return s.title;
    }
  }
  return undefined;
}

function modeOf(value: string | undefined): RunMode {
  return value?.toUpperCase() === "LIVE" ? "LIVE" : "SCRIPTED";
}

function outcomeOf(test: TestLike, last: ResultLike | undefined): TestOutcome {
  if (!last) {
    return "notRun";
  }
  if (test.outcome() === "flaky") {
    return "flaky";
  }
  return last.status === "passed" ? "passed" : last.status;
}

/** The record of a test; `screenshots` are links already placed by the ScreenshotStore. */
export function toRecord(test: TestLike, screenshots: string[]): TestRecord {
  const last = test.results[test.results.length - 1];
  let outcome = outcomeOf(test, last);
  let skipReason: string | undefined;

  if (outcome === "skipped" || outcome === "notRun") {
    const why = annotation(test, last, "skip") ?? annotation(test, last, "fixme");
    const marked = [...test.annotations, ...(last?.annotations ?? [])].some((a) => a.type === "skip" || a.type === "fixme");
    if (marked) {
      skipReason = why ?? "пропущен без причины";
    }
  }
  // test.fail(): the failure is expected, so it is a known problem, not a verdict on the feature.
  if (test.outcome() === "expected" && last && (last.status === "failed" || last.status === "timedOut")) {
    outcome = "skipped";
    skipReason = annotation(test, last, "fail") ?? "известный сбой (test.fail)";
  }

  const failed = outcome === "failed" || outcome === "timedOut" || outcome === "interrupted";
  const messages = last ? [last.error?.message, ...last.errors.map((e) => e.message)].filter((m): m is string => !!m) : [];
  const error = failed ? messages[0] : undefined;
  const record: TestRecord = {
    title: test.title,
    feature: annotation(test, last, AnnotationFeature) ?? test.title,
    mode: modeOf(annotation(test, last, AnnotationMode)),
    pack: packOf(test.location.file),
    outcome,
    screenshots,
    durationMs: last?.duration ?? 0,
  };
  const requires = annotation(test, last, AnnotationRequires);
  if (requires) {
    record.requires = requires;
  }
  if (skipReason) {
    record.skipReason = skipReason;
  }
  if (error) {
    record.error = error;
    record.environment = messages.some((m) => isEnvironmentMessage(m));
    const step = last ? deepestBrokenStep(last.steps) : undefined;
    if (step) {
      record.brokenStep = step;
    }
  }
  return record;
}
