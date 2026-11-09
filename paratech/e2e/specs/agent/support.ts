// Helpers of the agent pack (TASK-380, part A): annotations, the panel like a student drives it, one probe of the thread, what the
// provider received. Not a spec. Every wait is on UI state with an explicit timeout; page code goes through evalInPage (the app CSP
// blocks string eval).
//
// The pack is SCRIPTED: the real Hub with a fake provider that plays the MODEL (scripts/llm/agent/rules.ts). It asserts what the APP
// sends to the model (`llm.requests()`) and what the PANEL shows; the model's own wording is the LIVE set (TASK-382).
import type { Locator, Page } from "@playwright/test";

import { catalogProblem, toolNamesOf } from "../../scripts/llm/agent/rules.js";
import { evalInPage } from "../../src/checks/layout.js";
import { AnnotationFeature, AnnotationMode, AnnotationRequires, EnvHubDatabaseUrl } from "../../src/contract.js";
import { collapse } from "../../src/fixtures/text.js";
import { pgQueryUrl } from "../../src/hub/pg.js";
import { expect, type Llm, type RecordedRequest, test } from "../../src/steps/index.js";
import { Feature as RecordingFeature } from "../recording/support.js";
import { act, actionMs } from "../settings/support.js";

export { act, actionMs, firstLine, openSection, textOf } from "../settings/support.js";
export { lectureOf } from "../recording/support.js";
export { collapse };

export const Tag = "@agent";

// The rows of the pack (one report row each).
export const Feature = {
  reply: "Агент: ответ, копирование, повтор, таймер",
  widths: "Агент: строка «Стоп» не перекрывает текст на двух ширинах панели",
  cut: "Агент: оборванный ответ сохраняет текст",
  cutNext: "Агент: после оборванного ответа можно писать дальше",
  busy: "Агент: второй вопрос во время первого",
  restart: "Агент: начать заново отсюда и форк",
  compact: "Агент: уплотнение контекста",
  // Part B: scenarios that drive the tools.
  titles: "Агент: названия записей доходят до модели",
  link: "Агент: ссылка на пару",
  linkNone: "Агент: ссылка на пару, когда её нет",
  source: "Агент: что было на паре — карточка с источником",
  answerText: "Агент: что было на паре — текст ответа в карточке",
  failure: "Агент: сбой инструмента — честно, без выдуманной причины",
  addSubject: "Агент: добавь предмет по последней записи",
  undoTree: "Агент: «Отменить» обновляет открытое дерево предметов",
  confirm: "Агент: действие класса C только через подтверждение",
  cardsStay: "Агент: карточки инструментов остаются после ответа модели",
  busyTool: "Агент: второй вопрос, пока идёт инструмент",
  // TASK-401.
  withdraw: "Агент: отмена ждущего вопроса — текст возвращается в поле",
  cutRetry: "Агент: один обрыв — одна автоповторная попытка под тем же ключом",
} as const;

/** The rows of the settings pack the agent needs: the Hub key of the run (the panel asks the Hub) and the subjects of the schedule. */
export const Prerequisites = ["Hub: ключ прогона принят после проверки отзыва", "Расписание: предметы из расписания"].join(" + ");

/** Part B reads the recordings of the recording pack (the main one and the one imported before a restart) and the deleted one. */
export const RecordingPrerequisites = [RecordingFeature.import, RecordingFeature.restart, RecordingFeature.cancel].join(" + ");
export const PrerequisitesB = [Prerequisites, RecordingPrerequisites].join(" + ");
/** The call link of «Матанализ» is saved by the settings pack on «Сегодня». */
export const LinkPrerequisite = "Расписание: ссылка на пару";

export function details(feature: string, opts: { requires?: string } = {}) {
  return {
    tag: Tag,
    annotation: [
      { type: AnnotationFeature, description: feature },
      { type: AnnotationMode, description: "SCRIPTED" },
      { type: AnnotationRequires, description: opts.requires ?? Prerequisites },
    ],
  };
}

/** A fact worth keeping in the report: an annotation of the test and a line in the run output. Never put a secret here. */
export function note(text: string): void {
  test.info().annotations.push({ type: "note", description: text });
  console.log(`[agent] ${test.info().title}: ${text}`);
}

// ---- the panel ----

export const panelOf = (main: Page): Locator => main.locator("aside.ag");
export const fieldOf = (main: Page): Locator => panelOf(main).locator("textarea.ag-input");

/** Ctrl+K opens a closed panel and closes an open one; the shell carries `agent-closed` while it is closed. */
export async function setAgentPanel(main: Page, open: boolean): Promise<void> {
  const closed = main.locator(".shell.agent-closed");
  if ((await closed.count()) === (open ? 0 : 1)) {
    return;
  }
  await main.keyboard.press("Control+K");
  await expect(closed).toHaveCount(open ? 0 : 1, act);
}

// frontend/src/agent/panelWidth.ts: default 340 px, step 16 px, Home = the narrowest (260), End = the widest the window allows.
const defaultPanelWidth = 340;

/** Opens the panel and sets its width with the keyboard on the resize handle (role=separator). Returns the width in CSS px. */
export async function setPanelWidth(main: Page, which: "widest" | "default"): Promise<number> {
  await setAgentPanel(main, true);
  const sep = main.getByRole("separator", { name: "Ширина панели помощника" });
  const read = async (attr: string) => Number(await sep.getAttribute(attr, act));
  const now = () => read("aria-valuenow");
  const max = () => read("aria-valuemax");
  if (which === "widest") {
    if ((await now()) !== (await max())) {
      await sep.focus(act);
      await main.keyboard.press("End");
    }
    await expect.poll(async () => (await now()) === (await max()), { timeout: actionMs, message: "the panel should reach its widest width" }).toBe(true);
  } else if ((await now()) !== defaultPanelWidth) {
    await sep.focus(act);
    await main.keyboard.press("Home");
    for (let i = 0; i < 5; i++) {
      await main.keyboard.press("ArrowLeft");
    }
    await expect.poll(now, { timeout: actionMs, message: `the panel should be back at ${defaultPanelWidth} px` }).toBe(defaultPanelWidth);
  }
  await main.mouse.move(1, 1);
  return (await panelOf(main).boundingBox(act))?.width ?? 0;
}

/** «Новый чат»: an empty chat, so the history sent to the model holds only what the scenario asked. */
export async function newChat(main: Page): Promise<void> {
  await setAgentPanel(main, true);
  await main.locator(".ag-chat-actions").getByRole("button", { name: "Новый чат", exact: true }).click(act);
  await expect(panelOf(main).locator(".ag-thread .ag-empty"), "в новом чате лента должна быть пустой").toBeVisible(act);
}

/** Types the text into the field of the panel and presses «Отправить». */
export async function ask(main: Page, text: string): Promise<void> {
  const field = fieldOf(main);
  await expect(field, "поле запроса помощника должно быть доступно").toBeEnabled(act);
  await field.fill(text, act);
  const send = panelOf(main).locator("button.ag-send");
  await expect(send, "кнопка «Отправить» должна включиться, когда в поле есть текст").toBeEnabled(act);
  await send.click(act);
  await expect(field, "после отправки поле должно очиститься").toHaveValue("", act);
}

/** The panel at the start of a scenario: «Сегодня», the panel open, a new empty chat. */
export async function prepare(main: Page): Promise<void> {
  const button = main.getByRole("navigation", { name: "Разделы", exact: true }).getByRole("button", { name: "Сегодня", exact: true });
  await button.click(act);
  await expect(button).toHaveAttribute("aria-current", "page", act);
  await setAgentPanel(main, true);
  await newChat(main);
  await main.mouse.move(1, 1);
}

// ---- one look at the thread ----

export interface Sample {
  /** Requests drawn live (article.ag-req). */
  requests: number;
  /** The student's texts, live and earlier, in order. */
  users: string[];
  /** The rows «Агент отвечает…/Стоп» (.ag-think). */
  think: number;
  /** «Остановить» buttons in those rows. */
  stopButtons: number;
  /** «Остановить предыдущий запрос» buttons. */
  stopPrevious: number;
  /** «Отменить этот запрос» buttons (the withdraw icon of a waiting question). */
  withdrawButtons: number;
  /** The timers «5с». */
  elapsed: string[];
  /** The quiet «Повторная попытка N из 3» inside the waiting rows (.ag-think), one entry per match. */
  attempts: string[];
  /** The same text anywhere in the thread: it must be gone once the request is final. */
  attemptsAnywhere: string[];
  /** The reply being typed (the draft card). */
  draft: string[];
  /** The text kept above a stop line (a stopped or broken reply). */
  kept: string[];
  /** The final reply cards. */
  replies: string[];
  /** The lines of stopped and problem cards (.ag-stop). */
  stopLines: string[];
  /** «ждёт предыдущий запрос» labels. */
  queued: string[];
  /** Quiet lines of a call (.ag-callline). */
  callLines: string[];
  /** The retry icons «Повторить» and «Повторить ответ» inside the thread. */
  retry: number;
  rerun: number;
  /** The text of the whole thread. */
  text: string;
  /** The value of the request field, and whether it is off. */
  field: string;
  fieldDisabled: boolean;
  /** The banner «Нет связи с paratech» ("" when none). */
  offline: string;
  /** The quiet line of the chat bar and the compaction line under the field. */
  chatNote: string;
  compaction: string;
  /** The title of the open chat (the aria-label of the chat button). */
  chat: string;
}

/** Runs in the page. Self-contained (no closure): evalInPage sends it by its source. */
function probeThread(): Sample {
  const norm = (t: string | null): string => (t ?? "").replace(/[\s   ]+/g, " ").trim();
  const all = (sel: string): HTMLElement[] => Array.from(document.querySelectorAll<HTMLElement>(sel));
  const texts = (sel: string): string[] => all(sel).map((e) => norm(e.textContent));
  const thread = document.querySelector<HTMLElement>("aside.ag .ag-thread");
  const field = document.querySelector<HTMLTextAreaElement>("aside.ag textarea.ag-input");
  const named = (label: string): number => all(".ag-thread button").filter((b) => b.getAttribute("aria-label") === label).length;
  return {
    requests: all(".ag-thread article.ag-req").length,
    users: texts(".ag-thread p.ag-user"),
    think: all(".ag-thread .ag-think").length,
    stopButtons: all(".ag-thread .ag-think button").filter((b) => b.getAttribute("aria-label") === "Остановить").length,
    stopPrevious: named("Остановить предыдущий запрос"),
    withdrawButtons: named("Отменить этот запрос"),
    elapsed: texts(".ag-thread .ag-elapsed"),
    attempts: texts(".ag-thread .ag-think").flatMap((t) => t.match(/Повторная попытка \d+ из \d+/g) ?? []),
    attemptsAnywhere: norm(thread?.textContent ?? "").match(/Повторная попытка \d+ из \d+/g) ?? [],
    draft: texts(".ag-thread .ag-reply-draft p.ag-text"),
    kept: texts(".ag-thread .ag-reply-kept p.ag-text"),
    replies: texts(".ag-thread section.ag-card:not(.ag-reply-draft):not(.ag-reply-kept) > p.ag-text"),
    stopLines: texts(".ag-thread .ag-stop > .ag-act-tx"),
    queued: texts(".ag-thread .ag-queued"),
    callLines: texts(".ag-thread .ag-callline"),
    retry: named("Повторить"),
    rerun: named("Повторить ответ"),
    text: norm(thread?.textContent ?? ""),
    field: field?.value ?? "",
    fieldDisabled: field?.disabled ?? false,
    offline: norm(document.querySelector("aside.ag .ag-offline")?.textContent ?? ""),
    chatNote: norm(document.querySelector("aside.ag .ag-chat-note")?.textContent ?? ""),
    compaction: norm(document.querySelector("aside.ag .ag-compact")?.textContent ?? ""),
    chat: document.querySelector("aside.ag .ag-chat-toggle")?.getAttribute("aria-label") ?? "",
  };
}

const probeSource = probeThread.toString();

export const sample = (main: Page): Promise<Sample> => evalInPage<Sample>(main, probeSource);

/** Polls the thread until `done` holds; returns the sample it held for. `each` sees every sample on the way. */
export async function waitThread(
  main: Page,
  message: string,
  done: (s: Sample) => boolean,
  opts: { timeout?: number; each?: (s: Sample) => Promise<void> | void } = {},
): Promise<Sample> {
  let last: Sample | undefined;
  await expect
    .poll(
      async () => {
        const s = await sample(main);
        last = s;
        await opts.each?.(s);
        return done(s);
      },
      { timeout: opts.timeout ?? 30_000, intervals: [100, 150, 200], message },
    )
    .toBe(true);
  return last as Sample;
}

/** Looks at the thread for `ms` more (every sample goes to `each`): "nothing happens after this" needs a window, not one sample. */
export async function settle(main: Page, ms: number, each?: (s: Sample) => Promise<void> | void): Promise<Sample> {
  const from = Date.now();
  return waitThread(main, `окно наблюдения ${ms} мс`, () => Date.now() - from > ms, { timeout: ms + 10_000, each });
}

// ---- the Hub's idempotency keys (TASK-401) ----

// The Hub keeps every call's key in quota_idempotency_keys (hub/pkg/migrations/0003_quota.sql); neither the fake provider's request
// nor the Hub log carries it. The step key of the agent is `ag:<thread>:<turn>:<step>` (domain/agent/thread stepKey), plus `:r<N>`
// after the student's N-th «Повторить» of a failed step. The automatic retry of a lost answer repeats the key without a suffix.
const stepKeyShape = /^ag:[^:]+:\d+:\d+(?::r\d+)?$/;
// the marker goes into SQL text: only the characters a thread id and a key are made of
const markerShape = /^[A-Za-z0-9:_.-]+$/;

export interface StepKeyRow {
  key: string;
  /** How many times the Hub saw this key (the column `attempts`). */
  attempts: number;
}

/**
 * The agent step keys the Hub of this run knows, oldest first (created_at; a re-attempt of a key moves it). `threadMarker` keeps
 * only the keys that contain it. The database URL stays inside (EnvHubDatabaseUrl, process memory); nothing here prints it.
 */
export async function agentStepRows(threadMarker?: string): Promise<StepKeyRow[]> {
  const url = process.env[EnvHubDatabaseUrl];
  if (!url) {
    throw new Error(`Нет ${EnvHubDatabaseUrl}: ключи Hub читаются только в прогоне, который поднял тестовый Hub`);
  }
  if (threadMarker !== undefined && !markerShape.test(threadMarker)) {
    throw new Error("threadMarker: допустимы буквы, цифры и «: _ . -»");
  }
  const marker = threadMarker === undefined ? "" : ` AND position('${threadMarker}' in idempotency_key) > 0`;
  const rows = await pgQueryUrl(
    url,
    EnvHubDatabaseUrl,
    `SELECT idempotency_key, attempts FROM quota_idempotency_keys WHERE idempotency_key LIKE 'ag:%'${marker} ORDER BY created_at, idempotency_key`,
  );
  return rows
    .map((r) => ({ key: r[0] ?? "", attempts: Number(r[1]) }))
    .filter((r) => stepKeyShape.test(r.key));
}

/** The agent step keys (`ag:…`, answer_question's `ans:v2:…` and the like are left out), oldest first. */
export async function agentStepKeys(threadMarker?: string): Promise<string[]> {
  return (await agentStepRows(threadMarker)).map((r) => r.key);
}

/** A key of a student's «Повторить»: the step key plus `:r<N>`. */
export const isRetryKey = (key: string): boolean => /:r\d+$/.test(key);

/** The keys in `after` that `before` did not have: what a scenario made, in a database that lives through the whole run. */
export const newKeys = (before: readonly string[], after: readonly string[]): string[] => after.filter((k) => !before.includes(k));

/** Leftovers of the code in a text for a student: «undefined», «NaN», a raw error. */
export function rawLeftovers(text: string): string[] {
  const found = text.match(/\bundefined\b|\bnull\b|\bNaN\b|\[object |\bError:|TypeError|panic|goroutine|hub_text_|toolu_/g);
  return found === null ? [] : [...new Set(found)];
}

// ---- what the provider received ----

/** The requests of the planner (the ones with a tool catalog) in the order the Hub sent them. */
export async function plannerRequests(llm: Llm): Promise<RecordedRequest[]> {
  return (await llm.requests()).filter((r) => toolNamesOf(r.body).length > 0);
}

/** The planner requests whose last user content contains `text`. */
export async function plannerRequestsAbout(llm: Llm, text: string): Promise<RecordedRequest[]> {
  return (await plannerRequests(llm)).filter((r) => r.lastUser.includes(text));
}

/** The v9 pin: every planner request carries the tool names of catalog 9, else the pack says so by name. */
export function expectCatalogV9(requests: readonly RecordedRequest[]): void {
  expect(requests.length, "ни одного запроса к модели с каталогом инструментов").toBeGreaterThan(0);
  for (const r of requests) {
    expect(catalogProblem(r.body), `запрос №${r.seq} к модели`).toBeUndefined();
  }
}

interface Message {
  role?: string;
  content?: unknown;
}

/** The messages of a planner request. */
export function messagesOf(r: RecordedRequest): Message[] {
  const messages = (r.body as { messages?: unknown } | null)?.messages;
  return Array.isArray(messages) ? (messages as Message[]) : [];
}

/** All of a request's messages as text (JSON), for «the request holds / does not hold this text». */
export const messagesText = (r: RecordedRequest): string => JSON.stringify(messagesOf(r));

/** The student's request as the app sent it: the last text block of the last user message. */
export function lastStudentText(r: RecordedRequest): string {
  const last = [...messagesOf(r)].reverse().find((m) => m.role === "user");
  if (last === undefined || !Array.isArray(last.content)) {
    return typeof last?.content === "string" ? last.content : "";
  }
  const blocks = (last.content as Array<{ type?: string; text?: string }>).filter((b) => b.type === "text");
  return blocks.at(-1)?.text ?? "";
}

// ---- part B: what the tools gave the model ----

export interface ToolResultBlock {
  /** The id of the tool call the result answers. */
  toolUseId: string;
  /** The tool the call named (read from the assistant message before the result), "" when not found. */
  tool: string;
  /** `is_error` of the block, as the app sent it. */
  isError: boolean;
  /** The text of the block: the compact JSON of the app's mapper (ADR-0022). */
  text: string;
  /** The text parsed; undefined when it is not JSON. */
  json: unknown;
}

/** The tool_result blocks of the last user message of a planner request (what the app told the model about its last calls). */
export function toolResultsOf(r: RecordedRequest): ToolResultBlock[] {
  const messages = messagesOf(r);
  const last = [...messages].reverse().find((m) => m.role === "user");
  if (last === undefined || !Array.isArray(last.content)) {
    return [];
  }
  const names = new Map<string, string>();
  for (const m of messages) {
    if (m.role !== "assistant" || !Array.isArray(m.content)) {
      continue;
    }
    for (const b of m.content as Array<{ type?: string; id?: string; name?: string }>) {
      if (b.type === "tool_use" && typeof b.id === "string") {
        names.set(b.id, String(b.name ?? ""));
      }
    }
  }
  const out: ToolResultBlock[] = [];
  for (const b of last.content as Array<{ type?: string; tool_use_id?: string; content?: unknown; is_error?: boolean }>) {
    if (b.type !== "tool_result") {
      continue;
    }
    const text =
      typeof b.content === "string"
        ? b.content
        : Array.isArray(b.content)
          ? (b.content as Array<{ text?: string }>).map((c) => c.text ?? "").join("")
          : "";
    let json: unknown;
    try {
      json = JSON.parse(text);
    } catch {
      json = undefined;
    }
    const id = String(b.tool_use_id ?? "");
    out.push({ toolUseId: id, tool: names.get(id) ?? "", isError: b.is_error === true, text, json });
  }
  return out;
}

/** The server prompt of a planner request (system block 0, the planner instructions) and the whole system as text. */
export function systemOf(r: RecordedRequest): { prompt: string; all: string } {
  const system = (r.body as { system?: unknown } | null)?.system;
  const blocks = Array.isArray(system) ? (system as Array<{ text?: string }>) : [];
  return { prompt: blocks[0]?.text ?? (typeof system === "string" ? system : ""), all: blocks.map((b) => b.text ?? "").join("\n") || (typeof system === "string" ? system : "") };
}

/** The Hub request of the ANSWER operation about `question`: no tools, the user message is {"question","fragments"}. */
export async function answerRequestsAbout(llm: Llm, question: string): Promise<RecordedRequest[]> {
  return (await llm.requests()).filter((r) => toolNamesOf(r.body).length === 0 && r.lastUser.includes(question));
}

// ---- part B: the recordings the app itself knows ----

export interface AppRecording {
  id: string;
  title: string;
  subject: string;
}

// The list of «Записи» asked the way the page asks it (bindings/activity.ts listRecordings): the ids the tools must show.
const recordingsSource = `async () => {
  const runtime = await import("/wails/runtime.js");
  const rows = await runtime.Call.ByName("paratech/desktop/internal/input/wails.ActivityService.ListRecordings");
  return (Array.isArray(rows) ? rows : []).map((r) => ({ id: String(r.id), title: String(r.title || ""), subject: String(r.subject || "") }));
}`;

export const recordingsOfApp = (main: Page): Promise<AppRecording[]> => evalInPage<AppRecording[]>(main, recordingsSource);

/** The recording with this title; fails with the titles there are. */
export async function recordingByTitle(main: Page, title: string): Promise<AppRecording> {
  const all = await recordingsOfApp(main);
  const found = all.find((r) => r.title === title);
  if (found === undefined) {
    throw new Error(`в «Записи» нет записи «${title}»; есть: ${all.map((r) => `«${r.title}»`).join(", ") || "ни одной"}`);
  }
  return found;
}

// ---- part B: the cards of the thread ----

export interface Cards {
  /** The question cards (ask_questions): there must be none when the model should look first. */
  askCards: number;
  /** The confirm cards (class C) and their text. */
  confirmCards: number;
  confirmText: string;
  /** The names of the buttons of the confirm card. */
  confirmButtons: string[];
  /** The headings of the cards (h3.ag-card-h), the answer cards among them. */
  captions: string[];
  /** The lines of the action cards (class U), with the names of their buttons. */
  actionLines: string[];
  actionButtons: string[];
  /** The sources under an answer card (aria-labels of ul.ag-src buttons). */
  sources: string[];
  /** Everything in the thread that can be pressed or followed: the names of buttons and links, and the number of a[href]. */
  controls: string[];
  hrefs: number;
  /** The cannot_do card: the sub-headings and the lines under them ("" when there is no such card). */
  cannotDo: { subs: string[]; lines: string[] } | undefined;
  /** The text under the answer card heading. */
  answerText: string;
  /** The stop/problem lines of the thread. */
  problems: string[];
}

function probeCards(): Cards {
  const norm = (t: string | null): string => (t ?? "").replace(/[\s   ]+/g, " ").trim();
  const all = (root: ParentNode, sel: string): HTMLElement[] => Array.from(root.querySelectorAll<HTMLElement>(sel));
  const thread = document.querySelector<HTMLElement>("aside.ag .ag-thread") ?? document.body;
  const nameOf = (e: HTMLElement): string => e.getAttribute("aria-label") ?? (norm(e.textContent) || e.getAttribute("title") || "");
  const sections = all(thread, "section.ag-card");
  const heading = (s: HTMLElement): string => norm(s.querySelector("h3.ag-card-h")?.textContent ?? "");
  const confirm = thread.querySelector<HTMLElement>("section.ag-confirm");
  const cannot = sections.find((s) => heading(s) === "Целиком не получится");
  const answer = sections.find((s) => heading(s) === "Ответ по материалам");
  return {
    askCards: all(thread, "section.ag-ask").length,
    confirmCards: all(thread, "section.ag-confirm").length,
    confirmText: norm(confirm?.textContent ?? ""),
    confirmButtons: confirm === null || confirm === undefined ? [] : all(confirm, "button").map(nameOf),
    captions: sections.map(heading).filter((h) => h !== ""),
    actionLines: all(thread, ".ag-act .ag-act-tx").map((e) => norm(e.textContent)),
    actionButtons: all(thread, ".ag-act button").map(nameOf),
    sources: all(thread, "ul.ag-src button").map(nameOf),
    controls: all(thread, "button, a, [role='button'], [role='link']").map(nameOf),
    hrefs: all(thread, "a[href]").length,
    cannotDo:
      cannot === undefined
        ? undefined
        : { subs: all(cannot, "p.ag-sub").map((e) => norm(e.textContent)), lines: all(cannot, "ul.ag-list li").map((e) => norm(e.textContent)) },
    answerText: norm(answer?.querySelector("p")?.textContent ?? ""),
    problems: all(thread, ".ag-stop > .ag-act-tx").map((e) => norm(e.textContent)),
  };
}

const probeCardsSource = probeCards.toString();

export const cardsOf = (main: Page): Promise<Cards> => evalInPage<Cards>(main, probeCardsSource);

/** The buttons the thread of a plain reply has: «Копировать» under the question and the answer, «Повторить ответ». */
export const ReplyControls: readonly string[] = ["Копировать", "Повторить ответ"];

/**
 * The controls of the thread that lead somewhere (a screen, a page, a link): by the words of the app's own screens. A reply that
 * offers one must lead to the thing it was asked for; the catalog v9 reply has none.
 */
export function navigationControls(c: Cards): string[] {
  return c.controls.filter((n) => /Открыть|Перейти|Расписани|Предмет|Сегодня|Журнал|Настройк|Задани|Записи|Подключиться|Добавить ссылку/i.test(n));
}

/** Polls the cards of the thread until `done` holds. */
export async function waitCards(main: Page, message: string, done: (c: Cards) => boolean, timeout = 30_000): Promise<Cards> {
  let last: Cards | undefined;
  await expect
    .poll(
      async () => {
        last = await cardsOf(main);
        return done(last);
      },
      { timeout, intervals: [100, 150, 200], message },
    )
    .toBe(true);
  return last as Cards;
}

// ---- part B: the subjects tree ----

/** The names in the tree of «Предметы» (nav.tree .nm), after opening the section. */
export async function subjectNames(main: Page): Promise<string[]> {
  const rail = main.getByRole("navigation", { name: "Разделы", exact: true }).getByRole("button", { name: "Предметы", exact: true });
  await rail.click(act);
  await expect(rail).toHaveAttribute("aria-current", "page", act);
  await main.mouse.move(1, 1);
  const tree = main.locator("nav.tree");
  await expect(tree, "в «Предметы» должно быть дерево предметов").toBeVisible(act);
  const names = await tree.locator(".nm").allInnerTexts();
  return names.map((n) => collapse(n));
}
