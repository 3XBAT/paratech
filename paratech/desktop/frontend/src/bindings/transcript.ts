// Transcript viewer bindings: calls into the Go core (internal/input/wails/transcript.go). The core reads the local
// transcript and writes every text; nothing here goes to the network. Segment text is speech recognised on this
// computer: show it as plain text, never as HTML. Copy and save run only on the user's click.

import type { CallByName } from "./settings";

export type TranscriptState = "none" | "waiting" | "recognising" | "done" | "failed";

/** The head of the viewer. */
export interface TranscriptStatus {
  recordingId: string;
  title: string;
  /** RFC 3339 in UTC, "" when unknown. */
  startedAt: string;
  state: TranscriptState;
  percent: number;
  /** The core's text of the state, shown as it is. */
  message: string;
  /** How many segments segments() pages through. */
  segments: number;
  /** The stored transcript; false: what was recognised so far (it may change when the recognition ends). */
  final: boolean;
  /** Plays the recording; "" when it has no track. */
  audioUrl: string;
}

export interface TranscriptSegment {
  index: number;
  startMs: number;
  endMs: number;
  /** mm:ss or hh:mm:ss. */
  start: string;
  text: string;
}

export interface TranscriptPage {
  from: number;
  total: number;
  final: boolean;
  segments: TranscriptSegment[];
}

export type SaveFormat = "txt" | "md";

export type SaveStatus = "saved" | "cancelled" | "failed";

export interface SaveResult {
  status: SaveStatus;
  message: string;
  /** The base name of the saved file; "" unless saved. */
  name: string;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  status: "paratech/desktop/internal/input/wails.TranscriptService.Status",
  segments: "paratech/desktop/internal/input/wails.TranscriptService.Segments",
  text: "paratech/desktop/internal/input/wails.TranscriptService.Text",
  copyAll: "paratech/desktop/internal/input/wails.TranscriptService.CopyAll",
  copyText: "paratech/desktop/internal/input/wails.TranscriptService.CopyText",
  saveAs: "paratech/desktop/internal/input/wails.TranscriptService.SaveAs",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  invalidId: "Неверный идентификатор записи.",
  notFound: "Запись не найдена: обновите список.",
  empty: "Распознанного текста пока нет.",
  format: "Неизвестный формат файла.",
  page: "Неверный номер фрагмента: обновите страницу.",
  unavailable: "Не удалось прочитать расшифровку. Попробуйте ещё раз.",
  clipboard: "Не удалось скопировать в буфер обмена. Попробуйте ещё раз.",
  copyTooBig: "Выделен слишком большой фрагмент: скопируйте его частями или сохраните файл.",
} as const;

/** The largest page the core returns. */
export const maxPage = 1000;

export const transcriptStates: readonly TranscriptState[] = ["none", "waiting", "recognising", "done", "failed"];
const saveStatuses: readonly SaveStatus[] = ["saved", "cancelled", "failed"];

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

/** Checks the shape of a reply; a malformed reply is an error, not a default. */
export function parseStatus(raw: unknown): TranscriptStatus {
  const w = "transcript status";
  const r = record(raw, w);
  const audioUrl = str(r, "audioUrl", w);
  // The player loads only the app's own media route.
  if (audioUrl !== "" && !audioUrl.startsWith("/media/recording/")) {
    throw new Error(`${w}: audioUrl is not a recording`);
  }
  return {
    recordingId: str(r, "recordingId", w),
    title: str(r, "title", w),
    startedAt: str(r, "startedAt", w),
    state: oneOf(r, "state", transcriptStates, w),
    percent: count(r, "percent", w),
    message: str(r, "message", w),
    segments: count(r, "segments", w),
    final: bool(r, "final", w),
    audioUrl,
  };
}

export function parseSegment(raw: unknown): TranscriptSegment {
  const w = "transcript segment";
  const r = record(raw, w);
  return {
    index: count(r, "index", w),
    startMs: count(r, "startMs", w),
    endMs: count(r, "endMs", w),
    start: str(r, "start", w),
    text: str(r, "text", w),
  };
}

export function parsePage(raw: unknown): TranscriptPage {
  const w = "transcript page";
  const r = record(raw, w);
  if (!Array.isArray(r.segments)) {
    throw new Error(`${w}: segments is not a list`);
  }
  return {
    from: count(r, "from", w),
    total: count(r, "total", w),
    final: bool(r, "final", w),
    segments: r.segments.map(parseSegment),
  };
}

export function parseSaveResult(raw: unknown): SaveResult {
  const w = "save result";
  const r = record(raw, w);
  return { status: oneOf(r, "status", saveStatuses, w), message: str(r, "message", w), name: str(r, "name", w) };
}

export interface TranscriptApi {
  status(recordingId: string): Promise<TranscriptStatus>;
  /** At most limit (≤ maxPage) segments from index from. */
  segments(recordingId: string, from: number, limit: number): Promise<TranscriptPage>;
  /** The whole transcript as plain text, "[mm:ss] " in front of each line when withTimestamps. */
  text(recordingId: string, withTimestamps: boolean): Promise<string>;
  copyAll(recordingId: string, withTimestamps: boolean): Promise<void>;
  /** Puts the page's selection on the clipboard. */
  copyText(text: string): Promise<void>;
  /** Shows the Windows save dialog; the path never comes from the page. */
  saveAs(recordingId: string, format: SaveFormat, withTimestamps: boolean): Promise<SaveResult>;
}

/** Builds the transcript API over a call function (the Wails runtime in the app, a fake in tests). */
export function transcriptApi(call: CallByName): TranscriptApi {
  return {
    status: async (id) => parseStatus(await call(methods.status, id)),
    segments: async (id, from, limit) => parsePage(await call(methods.segments, id, from, limit)),
    text: async (id, withTimestamps) => {
      const v = await call(methods.text, id, withTimestamps);
      if (typeof v !== "string") {
        throw new Error("transcript text: reply is not a string");
      }
      return v;
    },
    copyAll: async (id, withTimestamps) => {
      await call(methods.copyAll, id, withTimestamps);
    },
    copyText: async (text) => {
      await call(methods.copyText, text);
    },
    saveAs: async (id, format, withTimestamps) => parseSaveResult(await call(methods.saveAs, id, format, withTimestamps)),
  };
}
