// Turns the collected tests into one verdict per feature (ADR-0035 §6): «готово» / «сломано» / «не проверено».
// An environment problem is never «сломано»; a test whose prerequisite feature is not «готово» is «не проверено».
import { EnvironmentMarker } from "../contract.js";
import type { EnvProblem, ReportInput, Row, TestRecord, TestVerdict, Verdict } from "./model.js";

const maxMessage = 200;
const colourCodes = /\u001b\[[0-9;]*[A-Za-z]/g;

/** The first non-empty line of an error message, without colour codes, cut to ~200 characters. */
export function firstLine(text: string | undefined, max = maxMessage): string {
  const line =
    (text ?? "")
      .replace(colourCodes, "")
      .split(/\r?\n/)
      .map((l) => l.trim())
      .find((l) => l !== "") ?? "";
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
}

/** The message of an environment error without the marker. */
export function stripMarker(message: string): string {
  const m = message.replace(colourCodes, "");
  const at = m.indexOf(EnvironmentMarker); // Playwright prefixes the message with "Error: "
  return (at >= 0 ? m.slice(at + EnvironmentMarker.length) : m).trim();
}

export function isEnvironmentMessage(message: string | undefined): boolean {
  return (message ?? "").replace(colourCodes, "").includes(EnvironmentMarker);
}

/** The test failed with an environment error (flagged by the collector, or its message carries the marker). */
export function isEnvironmentTest(t: TestRecord): boolean {
  return t.environment === true || isEnvironmentMessage(t.error);
}

/** Environment problems of the run: state.json, setup errors, and tests that failed with the marker. */
export function envProblems(input: ReportInput): EnvProblem[] {
  const out: EnvProblem[] = [];
  const env = input.state?.environmentError;
  if (env) {
    out.push({ kind: env.kind || "среда", message: firstLine(env.message), global: true });
  }
  for (const e of input.runnerErrors) {
    if (isEnvironmentMessage(e)) {
      out.push({ kind: "среда", message: firstLine(stripMarker(e)), global: true });
    }
  }
  for (const t of input.tests) {
    if (isEnvironmentTest(t)) {
      out.push({ kind: `тест «${t.title}»`, message: firstLine(stripMarker(t.error ?? "")), global: false });
    }
  }
  return out;
}

/** The `requires` annotation may name several features, joined by « + »: all of them must be «готово». */
export const RequiresSeparator = " + ";

export function splitRequires(text: string): string[] {
  return text
    .split(RequiresSeparator)
    .map((s) => s.trim())
    .filter((s) => s !== "");
}

export interface Evaluation {
  rows: Row[];
  verdicts: Map<TestRecord, TestVerdict>;
  counts: { ready: number; broken: number; unchecked: number };
}

export function evaluate(input: ReportInput): Evaluation {
  const problems = envProblems(input);
  const globalEnv = problems.find((p) => p.global);
  const start = input.state?.startFailure;
  const features = orderedFeatures(input.tests);
  const byFeature = new Map(features.map((f) => [f, input.tests.filter((t) => t.feature === f)]));

  const firstBroken = features.find((f) => (byFeature.get(f) ?? []).some((t) => rawBroken(t) && !isEnvironmentTest(t)));
  const runnerError = input.runnerErrors.map((e) => firstLine(e)).find((e) => e !== "");

  const verdicts = new Map<TestRecord, TestVerdict>();
  const featureVerdict = new Map<string, Verdict>();
  const inProgress = new Set<string>();

  const featureReady = (name: string): boolean => {
    if (!byFeature.has(name)) {
      return false;
    }
    return verdictOfFeature(name) === "ready";
  };

  const verdictOfFeature = (name: string): Verdict => {
    const known = featureVerdict.get(name);
    if (known) {
      return known;
    }
    if (inProgress.has(name)) {
      return "unchecked"; // a requires cycle never counts as ready
    }
    inProgress.add(name);
    const list = (byFeature.get(name) ?? []).map(verdictOfTest);
    inProgress.delete(name);
    const v: Verdict = list.some((x) => x.verdict === "broken")
      ? "broken"
      : list.some((x) => x.verdict === "unchecked")
        ? "unchecked"
        : "ready";
    featureVerdict.set(name, v);
    return v;
  };

  const verdictOfTest = (t: TestRecord): TestVerdict => {
    const known = verdicts.get(t);
    if (known) {
      return known;
    }
    const v = judge(t);
    verdicts.set(t, v);
    return v;
  };

  const judge = (t: TestRecord): TestVerdict => {
    if (t.outcome === "passed") {
      return { verdict: "ready" };
    }
    if (t.outcome === "flaky") {
      return { verdict: "ready", note: "готово после повтора" };
    }
    if (t.skipReason) {
      return { verdict: "unchecked", need: t.skipReason };
    }
    if (isEnvironmentTest(t)) {
      return { verdict: "unchecked", need: firstLine(stripMarker(t.error ?? "")) || "среда не готова" };
    }
    if (globalEnv) {
      return { verdict: "unchecked", need: globalEnv.message || "среда не готова" };
    }
    if (start) {
      return { verdict: "broken", step: "Запуск", message: firstLine(start.message) };
    }
    const missing = t.requires === undefined ? undefined : splitRequires(t.requires).find((r) => !featureReady(r));
    if (missing !== undefined) {
      return { verdict: "unchecked", need: byFeature.has(missing) ? missing : `${missing} (не запускалась)` };
    }
    if (rawBroken(t)) {
      return { verdict: "broken", step: t.brokenStep || t.title, message: firstLine(t.error) };
    }
    return { verdict: "unchecked", need: firstBroken ?? runnerError ?? "предыдущие функции без ошибок" };
  };

  const rows: Row[] = features.map((name) => {
    const tests = byFeature.get(name) ?? [];
    const results = tests.map(verdictOfTest);
    const verdict = verdictOfFeature(name);
    const broken = results.find((r) => r.verdict === "broken");
    const needs = [...new Set(results.flatMap((r) => (r.verdict === "unchecked" && r.need ? [r.need] : [])))];
    const retried = results.every((r) => r.verdict === "ready") && results.some((r) => r.note);
    const outcome =
      verdict === "ready"
        ? retried
          ? "готово после повтора"
          : "готово"
        : verdict === "broken"
          ? "сломано"
          : `не проверено (нужно: ${needs.join("; ") || "причина не указана"})`;
    const step = broken ? [broken.step, broken.message].filter((x) => x).join(" — ") : "";
    return {
      feature: name,
      modes: [...new Set(tests.map((t) => t.mode))].join(", "),
      verdict,
      outcome,
      step,
      snapshots: tests.flatMap((t) => t.screenshots),
    };
  });

  if (rows.length === 0) {
    const row = syntheticRow(input, globalEnv, runnerError);
    if (row) {
      rows.push(row);
    }
  }

  const counts = { ready: 0, broken: 0, unchecked: 0 };
  for (const t of input.tests) {
    counts[verdictOfTest(t).verdict]++;
  }
  return { rows, verdicts, counts };
}

/** No test was listed (the setup failed before the suite was known): one row says why nothing was checked. */
function syntheticRow(input: ReportInput, globalEnv: EnvProblem | undefined, runnerError: string | undefined): Row | undefined {
  const modes = input.mode;
  if (globalEnv) {
    return {
      feature: "Все функции",
      modes,
      verdict: "unchecked",
      outcome: `не проверено (нужно: ${globalEnv.message || "среда не готова"})`,
      step: "",
      snapshots: [],
    };
  }
  const start = input.state?.startFailure;
  if (start) {
    return {
      feature: "Запуск приложения",
      modes,
      verdict: "broken",
      outcome: "сломано",
      step: ["Запуск", firstLine(start.message)].filter((x) => x).join(" — "),
      snapshots: [],
    };
  }
  if (runnerError) {
    return {
      feature: "Запуск приложения",
      modes,
      verdict: "unchecked",
      outcome: `не проверено (нужно: ${runnerError})`,
      step: "",
      snapshots: [],
    };
  }
  return undefined;
}

function rawBroken(t: TestRecord): boolean {
  return t.outcome === "failed" || t.outcome === "timedOut" || t.outcome === "interrupted";
}

function orderedFeatures(tests: TestRecord[]): string[] {
  return [...new Set(tests.map((t) => t.feature))];
}
