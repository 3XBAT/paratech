// Playwright reporter of the E2E run (ADR-0035 §6): writes <run root>/report/report.md, one row per feature. Runs in the
// main process; only types come from Playwright, the rendering lives in render.ts / rows.ts.
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import type { FullConfig, FullResult, Reporter, Suite, TestCase, TestError, TestResult } from "@playwright/test/reporter";

import { EnvMode, EnvRunRoot, ReportDirName, ReportFileName, type RunState } from "../contract.js";
import { toRecord } from "./collect.js";
import type { ReportInput, RunMode, TestRecord } from "./model.js";
import { budgetWarning, renderReport } from "./render.js";
import { ScreenshotStore } from "./screenshots.js";
import { scrubSecrets } from "./scrub.js";
import { readRunState } from "./state.js";

export interface ReporterOptions {
  /** The run root; default: PARATECH_E2E_RUN_ROOT, else ../dist/e2e-run next to the e2e package. */
  runRoot?: string;
}

export function resolveRunRoot(options: ReporterOptions | undefined, env: NodeJS.ProcessEnv, cwd: string): string {
  return options?.runRoot ?? env[EnvRunRoot] ?? path.resolve(cwd, "..", "dist", "e2e-run");
}

export default class E2EReporter implements Reporter {
  private readonly options: ReporterOptions | undefined;
  private readonly records = new Map<string, TestRecord>();
  private readonly runnerErrors: string[] = [];
  // Playwright builds the reporter at config load, before the global setup writes this run's state.json: a state older
  // than that is the previous run's (a wipe that failed half-way leaves it) and must not be read.
  private readonly created = Date.now();
  // When the tests start (after the setup and the exe build); the 15 min budget counts from here.
  private begun: number | undefined;
  private store?: ScreenshotStore;

  constructor(options?: ReporterOptions) {
    this.options = options;
  }

  printsToStdio(): boolean {
    return false;
  }

  onBegin(_config: FullConfig, suite: Suite): void {
    this.begun = Date.now();
    for (const t of suite.allTests()) {
      this.records.set(t.id, toRecord(t, [])); // no result yet: «не проверено» unless it runs
    }
  }

  onTestEnd(test: TestCase, result: TestResult): void {
    const before = this.records.get(test.id)?.screenshots ?? [];
    let links: string[] = [];
    try {
      links = this.screenshots().importFrom(result.attachments, test.title);
    } catch (err) {
      this.runnerErrors.push(`скриншоты теста «${test.title}» не сохранены: ${String(err)}`);
    }
    this.records.set(test.id, toRecord(test, [...new Set([...before, ...links])]));
  }

  onError(error: TestError): void {
    this.runnerErrors.push(error.message ?? error.value ?? "ошибка без текста");
  }

  onEnd(result: FullResult): void {
    const runRoot = resolveRunRoot(this.options, process.env, process.cwd());
    const state = this.currentState(runRoot);
    const elapsedMs = this.begun === undefined ? 0 : Date.now() - this.begun; // no onBegin: no test ever started
    const mode = modeOf(state?.mode ?? process.env[EnvMode]);

    if (result.status === "timedout" || result.status === "interrupted") {
      this.runnerErrors.push(result.status === "timedout" ? "Прогон остановлен по общему таймауту." : "Прогон прерван.");
    }
    const input: ReportInput = {
      tests: [...this.records.values()],
      mode,
      runnerErrors: this.runnerErrors,
      elapsedMs,
    };
    if (state) {
      input.state = state;
    }
    if (state?.startedAt) {
      input.startedAt = state.startedAt;
    }

    const file = path.join(runRoot, ReportDirName, ReportFileName);
    try {
      mkdirSync(path.dirname(file), { recursive: true });
      writeFileSync(file, renderReport(input), "utf8");
    } catch (err) {
      console.error(scrubSecrets(`Отчёт не записан: ${String(err)}`));
      return;
    }
    const warn = budgetWarning(mode, elapsedMs);
    if (warn) {
      console.warn(scrubSecrets(warn));
    }
    console.log(`Отчёт: ${file}`);
  }

  /** state.json of THIS run: written after the reporter was created. Missing, unreadable or older: undefined. */
  private currentState(runRoot: string): RunState | undefined {
    const state = readRunState(runRoot);
    const startedMs = state?.startedAt ? Date.parse(state.startedAt) : Number.NaN;
    return Number.isNaN(startedMs) || startedMs < this.created ? undefined : state;
  }

  private screenshots(): ScreenshotStore {
    this.store ??= new ScreenshotStore(path.join(resolveRunRoot(this.options, process.env, process.cwd()), ReportDirName));
    return this.store;
  }
}

function modeOf(value: string | undefined): RunMode {
  return value?.toUpperCase() === "LIVE" ? "LIVE" : "SCRIPTED";
}
