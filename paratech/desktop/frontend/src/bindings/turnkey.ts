// Bindings of the turnkey work (TASK-132): calls into internal/input/wails/turnkey.go. The page never talks to the Hub
// and keeps nothing in browser storage. Topic, section titles, paragraphs, notes, sources and messages are model or
// student text: show them as text nodes only. Errors of the binding are fixed Russian texts, shown as they are.

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.TurnkeyService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  work: `${service}.Work`,
  startWriting: `${service}.StartWriting`,
  approveOutline: `${service}.ApproveOutline`,
  editOutline: `${service}.EditOutline`,
  regenerate: `${service}.Regenerate`,
} as const;

export type WorkStatus = "triage" | "questions" | "review" | "confirmed" | "writing" | "done" | "helper" | "failed";

export const workStatuses: readonly WorkStatus[] = ["triage", "questions", "review", "confirmed", "writing", "done", "helper", "failed"];

/** outline: the plan is written; approve: it waits for «Утвердить план»; blocked: a step used its attempts; none: done. */
export type WritingStep = "outline" | "approve" | "section" | "assemble" | "blocked" | "none";

export const writingSteps: readonly WritingStep[] = ["outline", "approve", "section", "assemble", "blocked", "none"];

export interface WorkSource {
  text: string;
  materialId: string;
  /** Named by the model, not taken from the student's materials. */
  verify: boolean;
}

export interface WorkSection {
  /** 1-based number of a section of the plan. */
  number: number;
  title: string;
  words: number;
  note: string;
  written: boolean;
  paragraphs: string[];
  sources: WorkSource[];
}

export interface WorkWriting {
  step: WritingStep;
  generation: number;
  topic: string;
  sections: WorkSection[];
  done: number;
  total: number;
  subjectId: string;
  materialId: string;
  result: string;
  job: string;
}

/** The part of TurnkeyWork the writing cards need; the questions, the card and the helper are TASK-130. */
export interface TurnkeyWork {
  id: string;
  status: WorkStatus;
  version: number;
  writing: WorkWriting | null;
  /** card.workType of the Go view ("" without a card); "presentation" is not written as a paper. */
  workType: string;
  problem: string;
  message: string;
}

/** A section of the plan the student sends; number 0 is a new section. */
export interface OutlineItem {
  number: number;
  title: string;
  words: number;
}

export const notFoundText = "Работа не найдена: возможно, её отменили.";

/** The fixed Russian texts of the Go binding (turnkey.go); anything else gets the generic one. */
export const errorTexts: readonly string[] = [
  notFoundText,
  "Это действие сейчас недоступно: обновите карточку.",
  "Вопросы по шагу пока недоступны.",
  "Не удалось выполнить. Попробуйте ещё раз.",
  "Работа ещё пишется: дождитесь окончания.",
  "Работа уже написана: измените план или сгенерируйте раздел заново.",
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
export function turnkeyErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) ? msg : genericError;
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
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
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

/** A list of the Go side may come as null (an empty slice of an older build); anything but a list is an error. */
function list<T>(r: Record<string, unknown>, key: string, what: string, parse: (raw: unknown) => T): T[] {
  const v = r[key];
  if (v === null || v === undefined) {
    return [];
  }
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

function parseSource(raw: unknown): WorkSource {
  const w = "work source";
  const r = record(raw, w);
  return { text: str(r, "text", w), materialId: str(r, "materialId", w), verify: bool(r, "verify", w) };
}

function parseSection(raw: unknown): WorkSection {
  const w = "work section";
  const r = record(raw, w);
  return {
    number: count(r, "number", w),
    title: str(r, "title", w),
    words: count(r, "words", w),
    note: str(r, "note", w),
    written: bool(r, "written", w),
    paragraphs: list(r, "paragraphs", w, (p) => {
      if (typeof p !== "string") {
        throw new Error(`${w}: paragraph is not a string`);
      }
      return p;
    }),
    sources: list(r, "sources", w, parseSource),
  };
}

function parseWriting(raw: unknown): WorkWriting {
  const w = "work writing";
  const r = record(raw, w);
  return {
    step: oneOf(r, "step", writingSteps, w),
    generation: count(r, "generation", w),
    topic: str(r, "topic", w),
    sections: list(r, "sections", w, parseSection),
    done: count(r, "done", w),
    total: count(r, "total", w),
    subjectId: str(r, "subjectId", w),
    materialId: str(r, "materialId", w),
    result: str(r, "result", w),
    job: str(r, "job", w),
  };
}

/** Checks the shape of a work; a malformed reply is an error, not a default. */
export function parseWork(raw: unknown): TurnkeyWork {
  const w = "turnkey work";
  const r = record(raw, w);
  const writing = r.writing === null || r.writing === undefined ? null : parseWriting(r.writing);
  const card = r.card === null || r.card === undefined ? null : record(r.card, w);
  return {
    id: str(r, "id", w),
    status: oneOf(r, "status", workStatuses, w),
    version: count(r, "version", w),
    writing,
    workType: card === null ? "" : str(card, "workType", w),
    problem: str(r, "problem", w),
    message: str(r, "message", w),
  };
}

export interface TurnkeyApi {
  /** The work of a work card. */
  work(workId: string): Promise<TurnkeyWork>;
  /** «Написать работу» on a confirmed work (a paid job). */
  startWriting(workId: string): Promise<TurnkeyWork>;
  /** «Утвердить план»: the sections are written. */
  approveOutline(workId: string): Promise<TurnkeyWork>;
  /** Saves the plan: the items in their order, number 0 is a new section. */
  editOutline(workId: string, items: OutlineItem[]): Promise<TurnkeyWork>;
  /** Section `number` again with the student's note; 0 is every section (or the plan of a blocked outline). */
  regenerate(workId: string, number: number, note: string): Promise<TurnkeyWork>;
}

/** Builds the turnkey API over a call function (the Wails runtime in the app, a fake in tests). */
export function turnkeyApi(call: CallByName): TurnkeyApi {
  return {
    work: async (id) => parseWork(await call(methods.work, id)),
    startWriting: async (id) => parseWork(await call(methods.startWriting, id)),
    approveOutline: async (id) => parseWork(await call(methods.approveOutline, id)),
    editOutline: async (id, items) =>
      parseWork(await call(methods.editOutline, id, items.map((i) => ({ number: i.number, title: i.title, words: i.words })))),
    regenerate: async (id, number, note) => parseWork(await call(methods.regenerate, id, number, note)),
  };
}
