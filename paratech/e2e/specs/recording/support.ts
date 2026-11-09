// Helpers of the recording pack (TASK-378): annotations, the lecture the pack imports, the row of «Записи», a watcher of the
// sandbox's whisper-cli, the file that carries what the Hub was asked across tests (and across a restart of the worker).
// Not a spec. Every wait is on UI or process state with an explicit timeout; page code goes through evalInPage.
import { appendFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

import type { Locator, Page } from "@playwright/test";

import { sandboxProcessesAsync } from "../../src/app/procs.js";
import { AnnotationFeature, AnnotationMode, AnnotationRequires, EnvCtlDir, EnvDataDir, EnvRunRoot } from "../../src/contract.js";
import type { RecordedRequest } from "../../src/fakellm/index.js";
import type { Lecture } from "../../src/fixtures/lecture.js";
import { collapse } from "../../src/fixtures/text.js";
import { readRunState } from "../../src/report/state.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, type Llm, test } from "../../src/steps/index.js";
import { act } from "../settings/support.js";

export { act, actionMs, firstLine, openSection, textOf, withPick } from "../settings/support.js";

export const Tag = "@recording";

// The rows of the pack (one report row each). The ones of the settings pack are the prerequisites.
export const Feature = {
  import: "Запись: импорт и локальная расшифровка",
  words: "Запись: расшифровка содержит слова из речи",
  summary: "Запись: конспект из ответа Hub",
  task: "Запись: задание со сроком в «Задания» и «Сегодня»",
  cancel: "Запись: отмена расшифровки",
  retranscribe: "Запись: отмена значком и «Расшифровать снова»",
  restart: "Запись: перезапуск приложения во время расшифровки",
  privacy: "Запись: звук не уходит в Hub",
} as const;

/** What the pack needs from the settings pack: the Hub key, the default speech model and the subjects of the schedule. */
export const Prerequisites = [
  "Hub: ключ прогона принят после проверки отзыва",
  "Модель: по умолчанию",
  "Модуль расшифровки готов",
  "Расписание: предметы из расписания",
].join(" + ");

export function details(feature: string, opts: { requires?: string } = {}) {
  return {
    tag: Tag,
    annotation: [
      { type: AnnotationFeature, description: feature },
      { type: AnnotationMode, description: "SCRIPTED" },
      ...(opts.requires ? [{ type: AnnotationRequires, description: opts.requires }] : []),
    ],
  };
}

/** A fact worth keeping in the report: an annotation of the test and a line in the run output. Never put a secret here. */
export function note(text: string): void {
  test.info().annotations.push({ type: "note", description: text });
  console.log(`[recording] ${test.info().title}: ${text}`);
}

// ---- the lecture ----

export { type Lecture, lectureOf, Subject } from "../../src/fixtures/lecture.js";

// ---- state shared by the tests of the pack (a failed test restarts the worker, so it lives in a file) ----

const stateFile = (ctl: string) => path.join(ctl, "recording-pack.json");

export interface PackState {
  main?: Lecture;
  /** The words of the README that the transcript of the first import contained. */
  words?: string[];
}

export function readPack(ctl = requireEnv(EnvCtlDir)): PackState {
  const file = stateFile(ctl);
  return existsSync(file) ? (JSON.parse(readFileSync(file, "utf8")) as PackState) : {};
}

export function updatePack(patch: Partial<PackState>, ctl = requireEnv(EnvCtlDir)): void {
  writeFileSync(stateFile(ctl), JSON.stringify({ ...readPack(ctl), ...patch }, null, 2), "utf8");
}

// ---- what the Hub was asked: the fake forgets it at every script() ----

const requestsFile = (ctl: string) => path.join(ctl, "recording-llm-requests.jsonl");

/**
 * Appends what the fake has recorded since its last script() to the pack's file (each request once, by seq). A Hub job can
 * still be asking the provider when a test ends, so it first waits until the number of records is the same at two polls in a
 * row; what was recorded is saved even if it never settles (and then the call fails, the AC-06 scan must not run on a part).
 */
export async function keepRequests(llm: Llm): Promise<number> {
  const ctl = requireEnv(EnvCtlDir);
  let latest: RecordedRequest[] = [];
  let before = -1;
  let settleError: unknown;
  try {
    await expect
      .poll(
        async () => {
          latest = await llm.requests();
          const same = latest.length === before;
          before = latest.length;
          return same;
        },
        { timeout: 20_000, intervals: [300, 500, 1_000], message: "запросы Hub к провайдеру всё ещё приходят: число записей фейка не устоялось" },
      )
      .toBe(true);
  } catch (err) {
    settleError = err;
  }
  const known = new Set(loadRequests(ctl).map((r) => r.seq));
  const fresh = latest.filter((r) => !known.has(r.seq));
  if (fresh.length > 0) {
    mkdirSync(ctl, { recursive: true });
    appendFileSync(requestsFile(ctl), fresh.map((r) => JSON.stringify(r)).join("\n") + "\n", "utf8");
  }
  if (settleError !== undefined) {
    throw settleError;
  }
  return fresh.length;
}

export function loadRequests(ctl = requireEnv(EnvCtlDir)): RecordedRequest[] {
  const file = requestsFile(ctl);
  if (!existsSync(file)) {
    return [];
  }
  return readFileSync(file, "utf8")
    .split("\n")
    .filter((l) => l.trim() !== "")
    .map((l) => JSON.parse(l) as RecordedRequest);
}

// ---- the row of «Записи» ----

/** The row of a recording by its title (the list is a `role="list"` of `.row` items). */
export function rowOf(main: Page, title: string): Locator {
  return main.locator(".page.recordings .row").filter({ has: main.locator("p.t", { hasText: title }) });
}

/** The chip of a row, whitespace collapsed: «Расшифровка · 42 %», «Конспект готов»... */
export async function chipOf(row: Locator): Promise<string> {
  if ((await row.count()) === 0) {
    return "";
  }
  return collapse(await row.locator(".st .chip").first().innerText({ timeout: 5_000 }));
}

/** The status word of a chip without its percentage or queue reason: «Расшифровка · 42 %» -> «Расшифровка». */
export const statusWord = (chip: string): string => chip.split(" · ")[0]?.trim() ?? chip;

/**
 * Opens «Записи» and unfolds «Завершённые» when the row is hidden there. The finished rows are in a folded list; the poll of
 * the page puts a row there when its chain is done.
 */
export async function showRow(main: Page, title: string): Promise<Locator> {
  const row = rowOf(main, title);
  if ((await row.count()) === 0) {
    const toggle = main.locator(".page.recordings .clist-toggle").filter({ hasText: /Завершённые/ });
    if ((await toggle.count()) > 0 && (await toggle.first().getAttribute("aria-expanded", act)) === "false") {
      await toggle.first().click(act);
    }
  }
  return row;
}

// ---- the sandbox's whisper-cli ----

export const WhisperImage = "whisper-cli.exe";

export interface WhisperSighting {
  pid: number;
  path: string;
  commandLine: string;
  firstSeen: number;
  lastSeen: number;
}

export interface WhisperWatch {
  /** Every whisper-cli.exe of the sandbox's exe folder seen so far, by pid. */
  seen: Map<number, WhisperSighting>;
  /** Stops watching; returns the problem of the last failed look, if any. */
  stop(): Promise<string | undefined>;
}

/** The folder of the exe the run started (whisper-cli.exe lives next to it). */
export function exeFolder(): string {
  const state = readRunState(requireEnv(EnvRunRoot));
  if (!state) {
    throw new Error("state.json прогона не найден");
  }
  return path.dirname(state.exePath);
}

/**
 * Looks for the sandbox's whisper-cli.exe in the background (about three times a second) until stop(). A process of another
 * folder with the same name is not the sandbox's and is never listed.
 */
export function watchWhisper(folder = exeFolder()): WhisperWatch {
  const seen = new Map<number, WhisperSighting>();
  let running = true;
  let problem: string | undefined;
  const loop = (async () => {
    while (running) {
      try {
        for (const p of await sandboxProcessesAsync(WhisperImage, folder)) {
          const now = Date.now();
          const old = seen.get(p.pid);
          seen.set(p.pid, { pid: p.pid, path: p.path, commandLine: p.commandLine, firstSeen: old?.firstSeen ?? now, lastSeen: now });
        }
        problem = undefined;
      } catch (err) {
        problem = err instanceof Error ? err.message : String(err);
      }
      await new Promise((r) => setTimeout(r, 250));
    }
  })();
  return {
    seen,
    async stop() {
      running = false;
      await loop;
      return problem;
    },
  };
}

/** The whisper-cli.exe processes of the sandbox that run right now. */
export async function whisperNow(folder = exeFolder()): Promise<WhisperSighting[]> {
  const now = Date.now();
  return (await sandboxProcessesAsync(WhisperImage, folder)).map((p) => ({ pid: p.pid, path: p.path, commandLine: p.commandLine, firstSeen: now, lastSeen: now }));
}

/** Waits until a sandbox whisper-cli.exe is running; returns it. */
export async function waitWhisper(timeoutMs: number, message: string, folder = exeFolder()): Promise<WhisperSighting> {
  let found: WhisperSighting | undefined;
  await expect
    .poll(
      async () => {
        found = (await whisperNow(folder))[0];
        return found !== undefined;
      },
      { timeout: timeoutMs, intervals: [250, 500], message },
    )
    .toBe(true);
  return found as WhisperSighting;
}

// ---- the log of the app ----

/** Lines of <data>/logs/desktop.log that match. The file is written by the app: read it shared. */
export function logLines(pattern: RegExp): string[] {
  const dataDir = requireEnv(EnvDataDir);
  const file = path.join(dataDir, "logs", "desktop.log");
  if (!existsSync(file)) {
    return [];
  }
  return readFileSync(file, "utf8")
    .split("\n")
    .filter((l) => pattern.test(l));
}
