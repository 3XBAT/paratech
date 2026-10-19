// Summary viewer bindings: calls into the Go core (internal/input/wails/summary.go). The core reads the local
// post-processing results and writes every text; nothing here goes to the network. The summary, its key points and the
// assignment titles are model output: show them as plain text, never as HTML. Copy and save run only on the user's
// click.

import { type LessonSource, lessonSource } from "../lessonLabel.js";
import type { CallByName } from "./settings";

/** Where the summary or the homework search stands. */
export type SummaryState = "none" | "waiting" | "running" | "done" | "failed";

/** Why a waiting step waits ("" unless waiting); the values of the recordings screen's queue reasons. */
export type SummaryWaitReason =
  | ""
  | "inLine"
  | "retry"
  | "power"
  | "network"
  | "quota"
  | "serviceNotConfigured"
  | "serviceUnavailable";

export interface SummaryStep {
  state: SummaryState;
  waitReason: SummaryWaitReason;
  /** The core's text of the state, shown as it is. */
  message: string;
  /** The core's last error of a failed or retrying job; "" otherwise. Never model text. */
  jobError: string;
}

export interface SummaryContent extends SummaryStep {
  /** text holds a summary; it may be ready while a new run waits, runs or failed. */
  ready: boolean;
  text: string;
  keyPoints: string[];
  /** RFC 3339 in UTC, "" unless ready. */
  updatedAt: string;
}

/** An assignment found in the lesson; id opens it in «Домашние задания». */
export interface SummaryAssignment {
  id: string;
  title: string;
  description: string;
  /** RFC 3339 in UTC, "" without a deadline. */
  due: string;
  userEdited: boolean;
  /** The lesson (TASK-092), as on the homework page. */
  subject: string;
  subjectSource: LessonSource;
  kind: string;
  teacher: string;
  teacherSource: LessonSource;
  /** RFC 3339 in UTC, "" when unknown. */
  lessonDate: string;
}

export interface SummaryHomework extends SummaryStep {
  assignments: SummaryAssignment[];
}

export interface SummaryView {
  recordingId: string;
  /** "" for an ad-hoc recording. */
  lessonId: string;
  title: string;
  /** RFC 3339 in UTC. */
  startedAt: string;
  /** Valid when hasDuration. */
  durationSeconds: number;
  hasDuration: boolean;
  /** The recording has a transcript, so the summary and the homework can be made. */
  transcribed: boolean;
  summary: SummaryContent;
  homework: SummaryHomework;
}

export type SummarySaveFormat = "txt" | "md";

export type SummarySaveStatus = "saved" | "cancelled" | "failed";

export interface SummarySaveResult {
  status: SummarySaveStatus;
  message: string;
  /** The base name of the saved file; "" unless saved. */
  name: string;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  view: "paratech/desktop/internal/input/wails.SummaryService.View",
  copyAll: "paratech/desktop/internal/input/wails.SummaryService.CopyAll",
  saveAs: "paratech/desktop/internal/input/wails.SummaryService.SaveAs",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  invalidId: "Неверный идентификатор записи.",
  notFound: "Запись не найдена: обновите список.",
  empty: "Конспекта пока нет.",
  format: "Неизвестный формат файла.",
  unavailable: "Не удалось прочитать конспект. Попробуйте ещё раз.",
  clipboard: "Не удалось скопировать в буфер обмена. Попробуйте ещё раз.",
} as const;

export const summaryStates: readonly SummaryState[] = ["none", "waiting", "running", "done", "failed"];
export const summaryWaitReasons: readonly SummaryWaitReason[] = [
  "",
  "inLine",
  "retry",
  "power",
  "network",
  "quota",
  "serviceNotConfigured",
  "serviceUnavailable",
];
const saveStatuses: readonly SummarySaveStatus[] = ["saved", "cancelled", "failed"];

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${what}: reply is not an object`);
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

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a count`);
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

function oneOf<T extends string>(r: Record<string, unknown>, key: string, allowed: readonly T[], what: string): T {
  const v = r[key];
  if (typeof v !== "string" || !allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not one of ${allowed.join(", ")}`);
  }
  return v as T;
}

function list(r: Record<string, unknown>, key: string, what: string): unknown[] {
  const v = r[key];
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v;
}

function parseStep(r: Record<string, unknown>, what: string): SummaryStep {
  return {
    state: oneOf(r, "state", summaryStates, what),
    waitReason: oneOf(r, "waitReason", summaryWaitReasons, what),
    message: str(r, "message", what),
    jobError: str(r, "jobError", what),
  };
}

export function parseAssignment(raw: unknown): SummaryAssignment {
  const w = "summary assignment";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    title: str(r, "title", w),
    description: str(r, "description", w),
    due: str(r, "due", w),
    userEdited: bool(r, "userEdited", w),
    subject: str(r, "subject", w),
    subjectSource: lessonSource(str(r, "subjectSource", w)),
    kind: str(r, "kind", w),
    teacher: str(r, "teacher", w),
    teacherSource: lessonSource(str(r, "teacherSource", w)),
    lessonDate: str(r, "lessonDate", w),
  };
}

/** Checks the shape of a reply; a malformed reply is an error, not a default. */
export function parseView(raw: unknown): SummaryView {
  const w = "summary view";
  const r = record(raw, w);
  const s = record(r.summary, `${w}: summary`);
  const h = record(r.homework, `${w}: homework`);
  return {
    recordingId: str(r, "recordingId", w),
    lessonId: str(r, "lessonId", w),
    title: str(r, "title", w),
    startedAt: str(r, "startedAt", w),
    durationSeconds: count(r, "durationSeconds", w),
    hasDuration: bool(r, "hasDuration", w),
    transcribed: bool(r, "transcribed", w),
    summary: {
      ...parseStep(s, `${w}: summary`),
      ready: bool(s, "ready", `${w}: summary`),
      text: str(s, "text", `${w}: summary`),
      keyPoints: list(s, "keyPoints", `${w}: summary`).map((p) => {
        if (typeof p !== "string") {
          throw new Error(`${w}: summary: a key point is not a string`);
        }
        return p;
      }),
      updatedAt: str(s, "updatedAt", `${w}: summary`),
    },
    homework: {
      ...parseStep(h, `${w}: homework`),
      assignments: list(h, "assignments", `${w}: homework`).map(parseAssignment),
    },
  };
}

export function parseSaveResult(raw: unknown): SummarySaveResult {
  const w = "summary save result";
  const r = record(raw, w);
  return { status: oneOf(r, "status", saveStatuses, w), message: str(r, "message", w), name: str(r, "name", w) };
}

export interface SummaryApi {
  view(recordingId: string): Promise<SummaryView>;
  /** Puts the summary, its key points and the homework found on the clipboard as plain text. */
  copyAll(recordingId: string): Promise<void>;
  /** Shows the Windows save dialog; the path never comes from the page. */
  saveAs(recordingId: string, format: SummarySaveFormat): Promise<SummarySaveResult>;
}

/** Builds the summary API over a call function (the Wails runtime in the app, a fake in tests). */
export function summaryApi(call: CallByName): SummaryApi {
  return {
    view: async (id) => parseView(await call(methods.view, id)),
    copyAll: async (id) => {
      await call(methods.copyAll, id);
    },
    saveAs: async (id, format) => parseSaveResult(await call(methods.saveAs, id, format)),
  };
}
