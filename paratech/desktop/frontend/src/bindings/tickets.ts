// Bindings of the exam tickets screen (TASK-134): calls into internal/input/wails/tickets.go. The page never talks to
// the Hub and keeps nothing in browser storage. Ticket titles come from the student's list: show them as text nodes
// only. Errors of the binding are fixed Russian texts, shown as they are. The .docx is opened by libraryApi.openMaterial(materialId).

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.TicketService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  propose: `${service}.Propose`,
  proposeFromMaterial: `${service}.ProposeFromMaterial`,
  get: `${service}.Get`,
  list: `${service}.List`,
  confirm: `${service}.Confirm`,
  cancel: `${service}.Cancel`,
  undo: `${service}.Undo`,
} as const;

export type SetStatus = "proposed" | "confirmed" | "answering" | "done" | "failed" | "cancelled";

export const setStatuses: readonly SetStatus[] = ["proposed", "confirmed", "answering", "done", "failed", "cancelled"];

/** "" is a ticket without an answer yet. */
export type Coverage = "" | "covered" | "partial" | "not_covered" | "failed";

export const coverages: readonly Coverage[] = ["", "covered", "partial", "not_covered", "failed"];

export interface TicketItem {
  number: number;
  title: string;
  answered: boolean;
  coverage: Coverage;
  answerId: string;
}

export interface TicketBudget {
  tickets: number;
  estimateMicroUsd: number;
  capMicroUsd: number;
  overCap: boolean;
}

export interface TicketReport {
  covered: number;
  partial: number;
  notCovered: number;
  failed: number;
  pending: number;
}

export interface TicketSet {
  id: string;
  subjectId: string;
  sourceId: string;
  status: SetStatus;
  tickets: TicketItem[];
  done: number;
  total: number;
  budget: TicketBudget;
  costAccepted: boolean;
  report: TicketReport;
  materialId: string;
  result: string;
  job: string;
  /** QUEUED, RUNNING, RETRY_WAIT, WAITING_*, COMPLETED, FAILED, CANCELLED; "" none. */
  jobStatus: string;
  stalled: boolean;
  version: number;
}

/** The fixed Russian texts of the Go binding (tickets.go). The two with numbers are matched by shape. */
export const errorTexts: readonly string[] = [
  "Билеты не найдены: возможно, их отменили.",
  "Билеты не найдены: нужен нумерованный список («1.», «1)» или «Билет 1»).",
  "Список слишком длинный.",
  "Сначала положите файл со списком в предмет.",
  "Предмет не найден.",
  "Файл со списком удалён.",
  "В файле нет текста, который можно прочитать.",
  "Прогон дороже обычного: подтвердите стоимость.",
  "Ответы уже готовы: файл в папке «Билеты».",
  "Файл уже перемещён или удалён: оставлен как есть.",
  "Это действие недоступно: билеты уже отменены или завершены, либо ответы ещё не готовы.",
  "Подбор ответов перезапускали слишком много раз. Добавьте список билетов заново.",
  "Не удалось выполнить. Попробуйте ещё раз.",
];

const numberedErrors: readonly RegExp[] = [
  /^Слишком много билетов: не больше \d+ за один раз\.$/,
  /^Один из билетов длиннее \d+ символов: сократите его\.$/,
];

export const genericError = "Не удалось выполнить. Попробуйте ещё раз.";

function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}

/** The core's text of a refused call when it is one of the known texts; a generic text otherwise. */
export function ticketsErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) || numberedErrors.some((re) => re.test(msg)) ? msg : genericError;
}

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new Error(`${what}: not an object`);
  }
  return raw as Record<string, unknown>;
}

function str(r: Record<string, unknown>, key: string, what: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`${what}: ${key} is not a string`);
  }
  return v;
}

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a count`);
  }
  return v;
}

function oneOf<T extends string>(r: Record<string, unknown>, key: string, allowed: readonly T[], what: string): T {
  const v = r[key];
  if (typeof v !== "string" || !allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not one of ${allowed.join(", ")}`);
  }
  return v as T;
}

/** A list of the Go side may come as null; anything but a list is an error. */
function items<T>(r: Record<string, unknown>, key: string, what: string, parse: (raw: unknown) => T): T[] {
  const v = r[key];
  if (v === null || v === undefined) {
    return [];
  }
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

function parseItem(raw: unknown): TicketItem {
  const w = "ticket";
  const r = record(raw, w);
  return {
    number: count(r, "number", w),
    title: str(r, "title", w),
    answered: bool(r, "answered", w),
    coverage: oneOf(r, "coverage", coverages, w),
    answerId: str(r, "answerId", w),
  };
}

function parseBudget(raw: unknown): TicketBudget {
  const w = "ticket budget";
  const r = record(raw, w);
  return {
    tickets: count(r, "tickets", w),
    estimateMicroUsd: count(r, "estimateMicroUsd", w),
    capMicroUsd: count(r, "capMicroUsd", w),
    overCap: bool(r, "overCap", w),
  };
}

function parseReport(raw: unknown): TicketReport {
  const w = "ticket report";
  const r = record(raw, w);
  return {
    covered: count(r, "covered", w),
    partial: count(r, "partial", w),
    notCovered: count(r, "notCovered", w),
    failed: count(r, "failed", w),
    pending: count(r, "pending", w),
  };
}

/** Checks the shape of a set; a malformed reply is an error, not a default. */
export function parseSet(raw: unknown): TicketSet {
  const w = "ticket set";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    subjectId: str(r, "subjectId", w),
    sourceId: str(r, "sourceId", w),
    status: oneOf(r, "status", setStatuses, w),
    tickets: items(r, "tickets", w, parseItem),
    done: count(r, "done", w),
    total: count(r, "total", w),
    budget: parseBudget(r.budget),
    costAccepted: bool(r, "costAccepted", w),
    report: parseReport(r.report),
    materialId: str(r, "materialId", w),
    result: str(r, "result", w),
    job: str(r, "job", w),
    jobStatus: str(r, "jobStatus", w),
    stalled: bool(r, "stalled", w),
    version: count(r, "version", w),
  };
}

export interface TicketsApi {
  /** Reads a pasted list for a subject; pays nothing. */
  propose(subjectId: string, text: string): Promise<TicketSet>;
  /** Reads the list of a library material; pays nothing. */
  proposeFromMaterial(materialId: string): Promise<TicketSet>;
  get(id: string): Promise<TicketSet>;
  /** The newest sets of a subject, newest first. */
  list(subjectId: string): Promise<TicketSet[]>;
  /** The paid run; acceptCost is the student's explicit consent to a run over the cap. */
  confirm(id: string, acceptCost: boolean): Promise<TicketSet>;
  cancel(id: string): Promise<TicketSet>;
  /** Moves the filed .docx back from «Билеты» to «Входящие». */
  undo(id: string): Promise<TicketSet>;
}

/** Builds the tickets API over a call function (the Wails runtime in the app, a fake in tests). */
export function ticketsApi(call: CallByName): TicketsApi {
  return {
    propose: async (subjectId, text) => parseSet(await call(methods.propose, subjectId, text)),
    proposeFromMaterial: async (id) => parseSet(await call(methods.proposeFromMaterial, id)),
    get: async (id) => parseSet(await call(methods.get, id)),
    list: async (subjectId) => {
      const raw = await call(methods.list, subjectId);
      if (raw === null || raw === undefined) {
        return [];
      }
      if (!Array.isArray(raw)) {
        throw new Error("ticket sets: reply is not a list");
      }
      return raw.map(parseSet);
    },
    confirm: async (id, acceptCost) => parseSet(await call(methods.confirm, id, acceptCost)),
    cancel: async (id) => parseSet(await call(methods.cancel, id)),
    undo: async (id) => parseSet(await call(methods.undo, id)),
  };
}
