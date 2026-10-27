// Import of an old recording of a lecture (TASK-161): calls into the Go core (internal/input/wails/recordingimport.go).
// The core shows the open dialog, tells the format from the content and copies the file; the sound is recognized
// locally and never leaves the computer. Paths never reach the page; names are plain text.

import type { CallByName } from "./settings";

/** The lecture of the file: a past lesson, or a date and time (RFC 3339 with the offset) with the names given. */
export interface ImportLecture {
  lessonId: string;
  startedAt: string;
  subject: string;
  kind: string;
  title: string;
}

/** What the content says the file is. */
export type ImportFormat = "wav" | "flac" | "mp3" | "ogg";

export interface ImportResult {
  /** The user closed the dialog; nothing else is set. */
  cancelled: boolean;
  recordingId: string;
  /** The same file was imported before: recordingId is that recording and message says so. */
  existing: boolean;
  /** «Эта запись уже добавлена.» when existing, else "". */
  message: string;
  /** The base name of the file. */
  name: string;
  /** "" when cancelled. */
  format: ImportFormat | "";
  /** 0 while unknown (only a WAV header tells it before the transcript). */
  durationSeconds: number;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  import: "paratech/desktop/internal/input/wails.RecordingImportService.Import",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  invalid: "Неверные данные лекции: обновите страницу.",
  invalidDate: "Неверные дата и время лекции.",
  future: "Эта лекция ещё не началась: выберите прошедшую.",
  noLesson: "Этой пары больше нет в расписании: обновите страницу.",
  unreadable: "Не удалось прочитать файл.",
  empty: "Файл пустой.",
  tooLarge: "Файл больше 2 ГБ.",
  tooLong: "Запись длиннее 4 часов.",
  format: "Подходят только аудиофайлы WAV, FLAC, MP3 и OGG (Vorbis). Видео, M4A, AAC, WMA и Opus пока не подходят.",
  noSpace: "На диске не хватает места для копии записи.",
  changed: "Файл изменился во время копирования. Попробуйте ещё раз.",
  deleting: "Запись этого файла сейчас удаляется. Попробуйте через минуту.",
  unavailable: "Не удалось импортировать запись. Попробуйте ещё раз.",
} as const;

/** The message of an existing import. */
export const alreadyAdded = "Эта запись уже добавлена.";

const formats: readonly (ImportFormat | "")[] = ["", "wav", "flac", "mp3", "ogg"];

/** Checks the shape of a reply; a malformed reply is an error, not a default. */
export function parseImportResult(raw: unknown): ImportResult {
  const w = "recording import";
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${w}: reply is not an object`);
  }
  const r = raw as Record<string, unknown>;
  const str = (key: string): string => {
    const v = r[key];
    if (typeof v !== "string") {
      throw new Error(`${w}: ${key} is not a string`);
    }
    return v;
  };
  const bool = (key: string): boolean => {
    const v = r[key];
    if (typeof v !== "boolean") {
      throw new Error(`${w}: ${key} is not a boolean`);
    }
    return v;
  };
  const format = str("format");
  if (!formats.includes(format as ImportFormat | "")) {
    throw new Error(`${w}: format is not one of ${formats.join(", ")}`);
  }
  const d = r.durationSeconds;
  if (typeof d !== "number" || !Number.isSafeInteger(d) || d < 0) {
    throw new Error(`${w}: durationSeconds is not a count`);
  }
  return {
    cancelled: bool("cancelled"),
    recordingId: str("recordingId"),
    existing: bool("existing"),
    message: str("message"),
    name: str("name"),
    format: format as ImportFormat | "",
    durationSeconds: d,
  };
}

export interface RecordingImportApi {
  /** Asks for one audio file in the native dialog and imports it as a recording of the lecture. */
  importRecording(lecture: ImportLecture): Promise<ImportResult>;
}

/** Builds the import API over a call function (the Wails runtime in the app, a fake in tests). */
export function recordingImportApi(call: CallByName): RecordingImportApi {
  return {
    importRecording: async (l) =>
      parseImportResult(
        await call(methods.import, {
          lessonId: l.lessonId,
          startedAt: l.startedAt,
          subject: l.subject,
          kind: l.kind,
          title: l.title,
        }),
      ),
  };
}
