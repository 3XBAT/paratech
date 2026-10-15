// Schedule import bindings: calls into the Go core (internal/input/wails/schedule.go). The page never sends a path or
// file bytes: the core shows the open dialog, keeps the chosen file and commits exactly the previewed bytes.

import type { CallByName } from "./settings";

/** A lesson as the preview shows it. Title is file text: render it as plain text only. */
export interface LessonText {
  title: string;
  /** "02.01.2006 15:04" in the lesson's zone. */
  start: string;
  end: string;
  zone: string;
}

/** An added lesson (line in the file) or a removed one (line 0). */
export interface ImportLesson extends LessonText {
  line: number;
}

export interface ImportChange {
  line: number;
  before: LessonText;
  after: LessonText;
}

/** A time overlap with another line (withLine > 0) or with a lesson the import keeps (withLine 0). */
export interface ImportConflict {
  line: number;
  lesson: LessonText;
  withLine: number;
  with: LessonText;
}

/** A problem of one line; line 0 is a problem of the whole file. */
export interface ImportRowError {
  line: number;
  field: string;
  reason: string;
}

/** The import dates of an ICS file, in UTC. */
export interface ImportWindow {
  from: string;
  to: string;
}

export interface ImportPreview {
  /** The user closed the dialog; every other field is empty. */
  cancelled: boolean;
  selectionId: string;
  fileName: string;
  source: string;
  errors: ImportRowError[];
  added: ImportLesson[];
  changed: ImportChange[];
  removed: ImportLesson[];
  unchangedCount: number;
  conflicts: ImportConflict[];
  conflictsTruncated: boolean;
  window: ImportWindow | null;
  /** The core accepts a commit of this preview. */
  canCommit: boolean;
}

export interface ImportResult {
  added: number;
  changed: number;
  removed: number;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  chooseFile: "paratech/desktop/internal/input/wails.ScheduleImportService.ChooseFile",
  commit: "paratech/desktop/internal/input/wails.ScheduleImportService.Commit",
  discard: "paratech/desktop/internal/input/wails.ScheduleImportService.Discard",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page maps them to its texts. */
export const errorMessages = {
  busy: "a file is already being chosen",
  unsupportedFile: "only .ics, .csv and .xlsx files can be imported",
  fileTooLarge: "the file is too large",
  fileUnreadable: "the file cannot be read",
  notSchedule: "the file is not a schedule in a supported format",
  noPreview: "there is no preview to apply",
  stale: "the schedule or the file changed, choose the file again",
  invalid: "the file has errors, nothing was written",
  failed: "the schedule import failed",
} as const;

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

function num(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
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

function list<T>(r: Record<string, unknown>, key: string, what: string, parse: (item: unknown) => T): T[] {
  const v = r[key];
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

function parseLessonText(raw: unknown): LessonText {
  const w = "lesson";
  const r = record(raw, w);
  return { title: str(r, "title", w), start: str(r, "start", w), end: str(r, "end", w), zone: str(r, "zone", w) };
}

function parseImportLesson(raw: unknown): ImportLesson {
  return { line: num(record(raw, "lesson"), "line", "lesson"), ...parseLessonText(raw) };
}

function parseChange(raw: unknown): ImportChange {
  const w = "change";
  const r = record(raw, w);
  return { line: num(r, "line", w), before: parseLessonText(r.before), after: parseLessonText(r.after) };
}

function parseConflict(raw: unknown): ImportConflict {
  const w = "conflict";
  const r = record(raw, w);
  return {
    line: num(r, "line", w),
    lesson: parseLessonText(r.lesson),
    withLine: num(r, "withLine", w),
    with: parseLessonText(r.with),
  };
}

function parseRowError(raw: unknown): ImportRowError {
  const w = "row error";
  const r = record(raw, w);
  return { line: num(r, "line", w), field: str(r, "field", w), reason: str(r, "reason", w) };
}

function parseWindow(raw: unknown): ImportWindow | null {
  if (raw === null || raw === undefined) {
    return null;
  }
  const w = "window";
  const r = record(raw, w);
  return { from: str(r, "from", w), to: str(r, "to", w) };
}

/** Checks the shape of a preview; a malformed reply is an error, not a default. */
export function parsePreview(raw: unknown): ImportPreview {
  const w = "preview";
  const r = record(raw, w);
  // Go encodes an empty list as [] here, but a null list is accepted as empty.
  const lists = { ...r };
  for (const key of ["errors", "added", "changed", "removed", "conflicts"]) {
    lists[key] ??= [];
  }
  return {
    cancelled: bool(r, "cancelled", w),
    selectionId: str(r, "selectionId", w),
    fileName: str(r, "fileName", w),
    source: str(r, "source", w),
    errors: list(lists, "errors", w, parseRowError),
    added: list(lists, "added", w, parseImportLesson),
    changed: list(lists, "changed", w, parseChange),
    removed: list(lists, "removed", w, parseImportLesson),
    unchangedCount: num(r, "unchangedCount", w),
    conflicts: list(lists, "conflicts", w, parseConflict),
    conflictsTruncated: bool(r, "conflictsTruncated", w),
    window: parseWindow(r.window),
    canCommit: bool(r, "canCommit", w),
  };
}

/** Checks the shape of a commit result. */
export function parseResult(raw: unknown): ImportResult {
  const w = "import result";
  const r = record(raw, w);
  return { added: num(r, "added", w), changed: num(r, "changed", w), removed: num(r, "removed", w) };
}

export interface ScheduleApi {
  /** Opens the dialog in the core and returns the preview of the chosen file. */
  chooseFile(): Promise<ImportPreview>;
  commit(selectionId: string): Promise<ImportResult>;
  discard(selectionId: string): Promise<void>;
}

/** Builds the schedule import API over a call function (the Wails runtime in the app, a fake in tests). */
export function scheduleApi(call: CallByName): ScheduleApi {
  return {
    chooseFile: async () => parsePreview(await call(methods.chooseFile)),
    commit: async (selectionId) => parseResult(await call(methods.commit, selectionId)),
    discard: async (selectionId) => {
      await call(methods.discard, selectionId);
    },
  };
}
