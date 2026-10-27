// Notes bindings («Мой конспект», TASK-151): calls into the Go core (internal/input/wails/notes.go). Titles, file names
// and lesson titles are typed or imported text: show them as text nodes only. Files come only through the native open
// dialog (addFiles, replaceFile) or a drop on an element marked as a lecture (dropAttributes); no path reaches the page.
// The Go errors are fixed Russian texts; only a text from the list below is shown, anything else gets a generic one.

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.NotesService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  addFiles: `${service}.AddFiles`,
  paste: `${service}.Paste`,
  replaceFile: `${service}.ReplaceFile`,
  undoReplace: `${service}.UndoReplace`,
  reorder: `${service}.Reorder`,
  reanchor: `${service}.Reanchor`,
  retitle: `${service}.Retitle`,
  remove: `${service}.Remove`,
  restore: `${service}.Restore`,
  forLesson: `${service}.ForLesson`,
  forSubject: `${service}.ForSubject`,
} as const;

/** Emitted after files are dropped on a lecture, with a NotesAdd (librarydrop.go). */
export const notesDropEvent = "notes:dropped";

/** The error texts of the Go binding (notes.go, library.go); a Go test pins the same strings. */
export const errorTexts: readonly string[] = [
  "Неверный идентификатор: обновите страницу.",
  "Неверная дата.",
  "Конспект не найден: обновите страницу.",
  "Эта запись ещё не готова или удаляется: прикрепить к ней конспект нельзя.",
  "Этот файл уже лежит в другом предмете.",
  "Конспект удалён: сначала восстановите его.",
  "Название конспекта не может быть пустым.",
  "Не выбрано ни одного файла.",
  "В конспекте может быть не больше 200 частей.",
  "Этот файл уже добавлен.",
  "Этой части нет в конспекте: обновите страницу.",
  "Порядок частей устарел: обновите страницу.",
  "Текст пустой.",
  "Текст больше 1 МБ.",
  "Это не похоже на обычный текст.",
  "Предмет или файл изменился: обновите страницу.",
  "Не удалось прочитать конспекты. Попробуйте ещё раз.",
  "За раз можно добавить не больше 200 файлов.",
  // The library's texts a file of notes can meet.
  "Занятие не найдено в расписании.",
  "Файл не найден: обновите список.",
  "Файл удалён: сначала восстановите его.",
  "Данные сейчас меняются. Попробуйте ещё раз.",
  "Предмет не найден: обновите список.",
  "Файл пустой.",
  "Файл больше 100 МБ.",
  "Подходят только DOCX, PPTX, PDF и текст в UTF-8.",
  "Не удалось прочитать файл.",
  "Этот файл добавить нельзя: перетащите файл с диска этого компьютера.",
  "За раз можно перетащить не больше 10 файлов.",
  "Фото больше 25 МБ или 40 мегапикселей.",
  "Фото подходят только в JPEG или PNG: HEIC, GIF, WebP, BMP, TIFF и SVG не подходят.",
  "Не удалось прочитать фото: файл повреждён или обрезан.",
];

export const genericError = "Не удалось выполнить действие.";

function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}

/** The core's text of a refused call (or of a file) when it is one of the known texts; a generic text otherwise. */
export function notesErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) ? msg : genericError;
}

export type AnchorKind = "recording" | "lesson" | "day" | "undated";

/** The lecture files go to. With noteId they go to that note and the rest is not read; date is "YYYY-MM-DD". */
export interface NoteTarget {
  noteId: string;
  subjectId: string;
  kind: AnchorKind | "";
  recordingId: string;
  lessonId: string;
  date: string;
}

/** The lecture whose notes forLesson lists: recordingId, lessonId, or subjectId with date. */
export interface NoteLecture {
  recordingId: string;
  lessonId: string;
  subjectId: string;
  date: string;
}

export interface NoteAnchor {
  kind: string;
  recordingId: string;
  lessonId: string;
  date: string;
  lessonKind: string;
  /** Imported text: a text node only. */
  lessonTitle: string;
  /** The recording or lesson is gone: the note reads as the day of its snapshot. */
  detached: boolean;
}

export interface NotePart {
  materialId: string;
  name: string;
  type: string;
  textStatus: string;
  removed: boolean;
  missing: boolean;
  /** "/media/notepage/<material id>" for a photo page, "" otherwise. The only source of a page image. */
  pageUrl: string;
  /** Pieces of the text: a text has one kind of piece, so at most one of sections and pages is non-zero. */
  sections: number;
  pages: number;
  /** Total pieces. */
  fragments: number;
}

export interface NoteView {
  id: string;
  subjectId: string;
  title: string;
  anchor: NoteAnchor;
  parts: NotePart[];
  removed: boolean;
  /** Photo pages without recognized text. Text typed by hand is a separate part and does not change this count. */
  noTextPages: number;
  createdAt: string;
  updatedAt: string;
}

export interface NoteFile {
  name: string;
  materialId: string;
  existing: boolean;
  /** «уже добавлено»: the file is a part of the note already. */
  alreadyAdded: boolean;
  /** "" or one of errorTexts. */
  error: string;
}

export interface NotesAdd {
  cancelled: boolean;
  created: boolean;
  note: NoteView | null;
  files: NoteFile[];
  /** The refusal of a whole drop; "" otherwise. */
  error: string;
  target: NoteTarget;
}

export interface NotesReplace {
  cancelled: boolean;
  note: NoteView | null;
  materialId: string;
  previousId: string;
  existing: boolean;
}

/** The attributes that mark an element as a lecture to drop files on (with Wails' data-file-drop-target). */
export function dropAttributes(t: NoteTarget): Record<string, string> {
  const out: Record<string, string> = { "data-file-drop-target": "", "data-note-kind": t.kind };
  const add = (name: string, v: string) => {
    if (v !== "") {
      out[name] = v;
    }
  };
  add("data-note-id", t.noteId);
  add("data-note-subject", t.subjectId);
  add("data-note-recording", t.recordingId);
  add("data-note-lesson", t.lessonId);
  add("data-note-date", t.date);
  return out;
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

function optStr(r: Record<string, unknown>, key: string): string {
  return typeof r[key] === "string" ? (r[key] as string) : "";
}

function list<T>(raw: unknown, what: string, parse: (item: unknown) => T): T[] {
  if (raw === null || raw === undefined) {
    return [];
  }
  if (!Array.isArray(raw)) {
    throw new Error(`${what}: not a list`);
  }
  return raw.map(parse);
}

/** A file's error as shown: a known text, or the generic one. */
function fileError(r: Record<string, unknown>): string {
  const e = optStr(r, "error");
  return e === "" ? "" : notesErrorText(e);
}

export function parseAnchor(raw: unknown): NoteAnchor {
  const r = record(raw, "anchor");
  return {
    kind: optStr(r, "kind"),
    recordingId: optStr(r, "recordingId"),
    lessonId: optStr(r, "lessonId"),
    date: optStr(r, "date"),
    lessonKind: optStr(r, "lessonKind"),
    lessonTitle: optStr(r, "lessonTitle"),
    detached: r.detached === true,
  };
}

export function parsePart(raw: unknown): NotePart {
  const w = "note part";
  const r = record(raw, w);
  return {
    materialId: str(r, "materialId", w),
    name: optStr(r, "name"),
    type: optStr(r, "type"),
    textStatus: optStr(r, "textStatus"),
    removed: r.removed === true,
    missing: r.missing === true,
    pageUrl: pageUrl(optStr(r, "pageUrl")),
    sections: pieceCount(r, "sections"),
    pages: pieceCount(r, "pages"),
    fragments: pieceCount(r, "fragments"),
  };
}

const maxPieces = 1_000_000;

/** A piece count: a non-negative integer up to a sane limit; anything else is 0. */
function pieceCount(r: Record<string, unknown>, key: string): number {
  const v = r[key];
  return typeof v === "number" && Number.isInteger(v) && v > 0 ? Math.min(v, maxPieces) : 0;
}

export const pageUrlPrefix = "/media/notepage/";

/** A page image address only when it is the core's same-origin one; anything else is no image. */
export function pageUrl(v: string): string {
  return v.startsWith(pageUrlPrefix) && v.length > pageUrlPrefix.length ? v : "";
}

function count(r: Record<string, unknown>, key: string): number {
  const v = r[key];
  return typeof v === "number" && Number.isInteger(v) && v > 0 ? v : 0;
}

/** Checks the shape of a note; a malformed reply is an error, not a default. */
export function parseNote(raw: unknown): NoteView {
  const w = "note";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    subjectId: str(r, "subjectId", w),
    title: str(r, "title", w),
    anchor: parseAnchor(r.anchor),
    parts: list(r.parts, w, parsePart),
    removed: r.removed === true,
    noTextPages: count(r, "noTextPages"),
    createdAt: optStr(r, "createdAt"),
    updatedAt: optStr(r, "updatedAt"),
  };
}

export function parseFile(raw: unknown): NoteFile {
  const r = record(raw, "note file");
  return {
    name: optStr(r, "name"),
    materialId: optStr(r, "materialId"),
    existing: r.existing === true,
    alreadyAdded: r.alreadyAdded === true,
    error: fileError(r),
  };
}

function parseTarget(raw: unknown): NoteTarget {
  const r = typeof raw === "object" && raw !== null && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
  const kind = optStr(r, "kind");
  return {
    noteId: optStr(r, "noteId"),
    subjectId: optStr(r, "subjectId"),
    kind: kind === "recording" || kind === "lesson" || kind === "day" || kind === "undated" ? kind : "",
    recordingId: optStr(r, "recordingId"),
    lessonId: optStr(r, "lessonId"),
    date: optStr(r, "date"),
  };
}

/** Reads a NotesAdd (the reply of addFiles and paste, and the payload of notesDropEvent). */
export function parseAdd(raw: unknown): NotesAdd {
  const w = "notes add";
  const r = record(raw, w);
  return {
    cancelled: r.cancelled === true,
    created: r.created === true,
    note: r.note === null || r.note === undefined ? null : parseNote(r.note),
    files: list(r.files, w, parseFile),
    error: fileError(r),
    target: parseTarget(r.target),
  };
}

export function parseReplace(raw: unknown): NotesReplace {
  const r = record(raw, "notes replace");
  return {
    cancelled: r.cancelled === true,
    note: r.note === null || r.note === undefined ? null : parseNote(r.note),
    materialId: optStr(r, "materialId"),
    previousId: optStr(r, "previousId"),
    existing: r.existing === true,
  };
}

export interface NotesApi {
  /** «Добавить файлы»: the native open dialog, several files at once. */
  addFiles(target: NoteTarget): Promise<NotesAdd>;
  /** Pasted text becomes a text file of the notes (at most 1 MB). */
  paste(target: NoteTarget, text: string): Promise<NotesAdd>;
  /** «Заменить»: the native open dialog, one file in the place of the part. */
  replaceFile(noteId: string, materialId: string): Promise<NotesReplace>;
  /** Undo of replaceFile: previousId back in the place of currentId. */
  undoReplace(noteId: string, currentId: string, previousId: string): Promise<NotesReplace>;
  reorder(noteId: string, order: string[]): Promise<NoteView>;
  /** Moves the note to another lecture (target's noteId and subjectId are not read). */
  reanchor(noteId: string, target: NoteTarget): Promise<NoteView>;
  retitle(noteId: string, title: string): Promise<NoteView>;
  remove(noteId: string): Promise<NoteView>;
  restore(noteId: string): Promise<NoteView>;
  forLesson(lecture: NoteLecture): Promise<NoteView[]>;
  forSubject(subjectId: string): Promise<NoteView[]>;
}

export function notesApi(call: CallByName): NotesApi {
  return {
    addFiles: async (target) => parseAdd(await call(methods.addFiles, target)),
    paste: async (target, text) => parseAdd(await call(methods.paste, target, text)),
    replaceFile: async (noteId, materialId) => parseReplace(await call(methods.replaceFile, noteId, materialId)),
    undoReplace: async (noteId, currentId, previousId) =>
      parseReplace(await call(methods.undoReplace, noteId, currentId, previousId)),
    reorder: async (noteId, order) => parseNote(await call(methods.reorder, noteId, order)),
    reanchor: async (noteId, target) => parseNote(await call(methods.reanchor, noteId, target)),
    retitle: async (noteId, title) => parseNote(await call(methods.retitle, noteId, title)),
    remove: async (noteId) => parseNote(await call(methods.remove, noteId)),
    restore: async (noteId) => parseNote(await call(methods.restore, noteId)),
    forLesson: async (lecture) => list(await call(methods.forLesson, lecture), "notes", parseNote),
    forSubject: async (subjectId) => list(await call(methods.forSubject, subjectId), "notes", parseNote),
  };
}
