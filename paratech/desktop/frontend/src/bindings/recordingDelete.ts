// Recording deletion bindings (TASK-095): calls into the Go core (internal/input/wails/recordingdelete.go). The core
// decides what can be deleted and what goes with it; the page shows the preview and sends the user's choice. Titles
// are imported text or model output: plain text only.

import type { CallByName } from "./settings";

/** Why a recording cannot be deleted now; "" when it can. */
export type DeleteRefusal = "" | "in_progress" | "recovering";

/**
 * plain: offered checked; draft: has a draft, deleted only when checked; emails: emails were sent, it stays in
 * «Домашние задания» and is never deleted.
 */
export type DeleteAssignmentClass = "plain" | "draft" | "emails";

export interface DeleteAssignment {
  id: string;
  title: string;
  class: DeleteAssignmentClass;
}

export interface DeleteRecording {
  id: string;
  /** The lesson title; "" for an ad-hoc recording (titled «Запись <дата, время>» from startedAt). */
  title: string;
  /** RFC 3339 in UTC. */
  startedAt: string;
  deletable: boolean;
  refusal: DeleteRefusal;
  /** The core's text of the refusal; "" when deletable. */
  refusalMessage: string;
  sound: boolean;
  transcript: boolean;
  summary: boolean;
  lectureText: boolean;
  jobs: number;
  /** What goes, ready Russian lines. */
  deletes: string[];
  assignments: DeleteAssignment[];
}

export interface DeletePreview {
  recordings: DeleteRecording[];
  /** Ids that are gone already. */
  missing: string[];
}

export interface DeleteRequest {
  recordingId: string;
  assignmentIds: string[];
}

export interface DeleteFailure {
  recordingId: string;
  /** The core's text, shown as it is. */
  message: string;
}

export interface DeleteResult {
  /** deleted + pending: gone from the lists. */
  removed: number;
  deleted: number;
  /** Gone from the lists; the next start finishes them. */
  pending: number;
  /** «Удалено N», shown as it is. */
  message: string;
  failures: DeleteFailure[];
  keptAssignments: string[];
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  preview: "paratech/desktop/internal/input/wails.RecordingDeleteService.Preview",
  delete: "paratech/desktop/internal/input/wails.RecordingDeleteService.Delete",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  invalidId: "Неверный идентификатор записи.",
  notFound: "Запись не найдена: обновите список.",
  inProgress: "Запись ещё идёт: сначала остановите её.",
  recovering: "Запись восстанавливается после сбоя. Попробуйте через минуту.",
  choice: "Список найденных заданий изменился: откройте удаление заново.",
  tooMany: "Слишком много записей сразу: выберите не больше 100.",
  unavailable: "Не удалось удалить запись. Попробуйте ещё раз.",
} as const;

/** The most ids one Preview or Delete takes. */
export const maxBatch = 100;

const refusals: readonly DeleteRefusal[] = ["", "in_progress", "recovering"];
const classes: readonly DeleteAssignmentClass[] = ["plain", "draft", "emails"];

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

function strings(r: Record<string, unknown>, key: string, what: string): string[] {
  return list(r, key, what).map((s) => {
    if (typeof s !== "string") {
      throw new Error(`${what}: an item of ${key} is not a string`);
    }
    return s;
  });
}

function parseAssignment(raw: unknown): DeleteAssignment {
  const w = "delete assignment";
  const r = record(raw, w);
  return { id: str(r, "id", w), title: str(r, "title", w), class: oneOf(r, "class", classes, w) };
}

function parseRecording(raw: unknown): DeleteRecording {
  const w = "delete recording";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    title: str(r, "title", w),
    startedAt: str(r, "startedAt", w),
    deletable: bool(r, "deletable", w),
    refusal: oneOf(r, "refusal", refusals, w),
    refusalMessage: str(r, "refusalMessage", w),
    sound: bool(r, "sound", w),
    transcript: bool(r, "transcript", w),
    summary: bool(r, "summary", w),
    lectureText: bool(r, "lectureText", w),
    jobs: count(r, "jobs", w),
    deletes: strings(r, "deletes", w),
    assignments: list(r, "assignments", w).map(parseAssignment),
  };
}

/** Checks the shape of a reply; a malformed reply is an error, not a default. */
export function parsePreview(raw: unknown): DeletePreview {
  const w = "delete preview";
  const r = record(raw, w);
  return { recordings: list(r, "recordings", w).map(parseRecording), missing: strings(r, "missing", w) };
}

export function parseResult(raw: unknown): DeleteResult {
  const w = "delete result";
  const r = record(raw, w);
  return {
    removed: count(r, "removed", w),
    deleted: count(r, "deleted", w),
    pending: count(r, "pending", w),
    message: str(r, "message", w),
    failures: list(r, "failures", w).map((f) => {
      const fr = record(f, `${w}: failure`);
      return { recordingId: str(fr, "recordingId", `${w}: failure`), message: str(fr, "message", `${w}: failure`) };
    }),
    keptAssignments: strings(r, "keptAssignments", w),
  };
}

export interface RecordingDeleteApi {
  /** What deleting each recording removes and the homework found; works offline. */
  preview(recordingIds: readonly string[]): Promise<DeletePreview>;
  /** Deletes the recordings with the checked assignments; may block for a while per recording. */
  delete(requests: readonly DeleteRequest[]): Promise<DeleteResult>;
}

/** Builds the deletion API over a call function (the Wails runtime in the app, a fake in tests). */
export function recordingDeleteApi(call: CallByName): RecordingDeleteApi {
  return {
    preview: async (ids) => parsePreview(await call(methods.preview, [...ids])),
    delete: async (reqs) =>
      parseResult(
        await call(
          methods.delete,
          reqs.map((r) => ({ recordingId: r.recordingId, assignmentIds: [...r.assignmentIds] })),
        ),
      ),
  };
}
