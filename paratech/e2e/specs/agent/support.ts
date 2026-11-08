// Helpers of the agent pack (TASK-380, part A): annotations, the panel like a student drives it, one probe of the thread, what the
// provider received. Not a spec. Every wait is on UI state with an explicit timeout; page code goes through evalInPage (the app CSP
// blocks string eval).
//
// The pack is SCRIPTED: the real Hub with a fake provider that plays the MODEL (scripts/llm/agent/rules.ts). It asserts what the APP
// sends to the model (`llm.requests()`) and what the PANEL shows; the model's own wording is the LIVE set (TASK-382).
import type { Locator, Page } from "@playwright/test";

import { catalogProblem, toolNamesOf } from "../../scripts/llm/agent/rules.js";
import { evalInPage } from "../../src/checks/layout.js";
import { AnnotationFeature, AnnotationMode, AnnotationRequires } from "../../src/contract.js";
import { collapse } from "../../src/fixtures/text.js";
import { expect, type Llm, type RecordedRequest, test } from "../../src/steps/index.js";
import { act, actionMs } from "../settings/support.js";

export { act, actionMs, firstLine, openSection, textOf } from "../settings/support.js";
export { collapse };

export const Tag = "@agent";

// The rows of the pack (one report row each).
export const Feature = {
  reply: "Агент: ответ, копирование, повтор, таймер",
  widths: "Агент: строка «Стоп» не перекрывает текст на двух ширинах панели",
  cut: "Агент: оборванный ответ сохраняет текст",
  busy: "Агент: второй вопрос во время первого",
  restart: "Агент: начать заново отсюда и форк",
  compact: "Агент: уплотнение контекста",
} as const;

/** The rows of the settings pack the agent needs: the Hub key of the run (the panel asks the Hub) and the subjects of the schedule. */
export const Prerequisites = ["Hub: ключ прогона принят после проверки отзыва", "Расписание: предметы из расписания"].join(" + ");

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
const fieldOf = (main: Page): Locator => panelOf(main).locator("textarea.ag-input");

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
  /** The timers «5с». */
  elapsed: string[];
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
    elapsed: texts(".ag-thread .ag-elapsed"),
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
