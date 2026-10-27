// Knowledge base bindings (TASK-091): calls into the Go core (internal/input/wails/knowledgebase.go). The core groups
// the lessons by subject, searches and exports; the page shows what it gets. Every text is imported, typed or model
// text: plain text only.

import type { CallByName } from "./settings";

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  subjects: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Subjects",
  lessons: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Lessons",
  lectures: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Lectures",
  lesson: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Lesson",
  search: "paratech/desktop/internal/input/wails.KnowledgeBaseService.Search",
  exportSubject: "paratech/desktop/internal/input/wails.KnowledgeBaseService.ExportSubject",
  setLessonInfo: "paratech/desktop/internal/input/wails.KnowledgeBaseService.SetLessonInfo",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page shows them as they are. */
export const errorMessages = {
  invalidId: "Неверный идентификатор записи.",
  notFound: "Запись не найдена: обновите список.",
  subjectNotFound: "В этом предмете больше нет занятий: обновите список.",
  query: "Введите для поиска от 2 до 100 символов.",
  lessonInfo: "Слишком длинное название предмета или неизвестный тип занятия.",
  notFinished: "Запись ещё идёт: поправить её можно после окончания.",
  busy: "Запись сейчас меняется. Попробуйте ещё раз.",
  unavailable: "Не удалось прочитать базу знаний. Попробуйте ещё раз.",
} as const;

/** A group of lessons of one subject; key "" with noSubject is «Без предмета» (name holds that text). */
export interface KnowledgeSubject {
  key: string;
  name: string;
  /** Recorded lessons; 0 for a subject known only from the student's notes. */
  lessons: number;
  /** The start of the newest recorded lesson, RFC 3339 in UTC; "" without one. */
  latest: string;
  noSubject: boolean;
  /** The library subject of the group; "" for a group of a free string. */
  subjectId: string;
}

/** What a lesson is, as NoteTarget.kind names it. */
export type LessonAnchor = "recording" | "lesson" | "day" | "undated";

export const lessonAnchors: readonly LessonAnchor[] = ["recording", "lesson", "day", "undated"];

/** A note of the student a lesson lists, as made. title is typed text or a file name: plain text only. */
export interface KnowledgeNote {
  id: string;
  title: string;
  subjectId: string;
  /** RFC 3339 in UTC. */
  createdAt: string;
}

/** The lesson kind codes setLessonInfo accepts; "" for none. */
export type LessonKind = "" | "lecture" | "seminar" | "practice" | "lab";

export const lessonKinds: readonly LessonKind[] = ["", "lecture", "seminar", "practice", "lab"];

/**
 * One lesson of the base: a completed recording, or a lecture known only from the student's notes (noteOnly). It is
 * opened by ids only: recordingId for "recording"; the notes binding's forLesson with scheduleLessonId for "lesson" or
 * with the subject and date for "day"; an "undated" lecture is its one note (notes[0].id).
 */
export interface KnowledgeLesson {
  /** Identifies the lesson among the lessons of the base. */
  key: string;
  /** No recording: recordingId is "". */
  noteOnly: boolean;
  /** A lecture «без даты»: startedAt and date are "". */
  undated: boolean;
  anchor: LessonAnchor;
  /** The schedule lesson of a recording or of a "lesson" lecture; "" otherwise. */
  scheduleLessonId: string;
  /** "YYYY-MM-DD", the day of a "lesson" or "day" lecture; "" otherwise. */
  date: string;
  /** The student's notes of the lesson, as made; [] from lessons, lesson and setLessonInfo. */
  notes: KnowledgeNote[];
  recordingId: string;
  /** RFC 3339 in UTC; "" when undated. */
  startedAt: string;
  durationSeconds: number;
  /** "" for an ad-hoc recording. */
  title: string;
  /** "" without one. */
  subject: string;
  subjectKey: string;
  /** A kind code; an unknown one is shown as no kind. */
  kind: string;
  /** The user's correction holds the subject or the kind. */
  subjectEdited: boolean;
  kindEdited: boolean;
  transcriptReady: boolean;
  summaryReady: boolean;
  lectureTextReady: boolean;
  /** The library subject the core resolved the subject to; "" unless librarySubjectState is "matched". */
  librarySubjectId: string;
  librarySubjectState: LibrarySubjectState;
}

/**
 * How the core resolved the lesson's subject to the library, never a guess: one subject, none, several (the student
 * chooses) or no subject string at all.
 */
export type LibrarySubjectState = "matched" | "none" | "ambiguous" | "no-subject";

export const librarySubjectStates: readonly LibrarySubjectState[] = ["matched", "none", "ambiguous", "no-subject"];

export type KnowledgeSection = "title" | "summary" | "lectureText" | "transcript" | "ownNote";

export const knowledgeSections: readonly KnowledgeSection[] = ["title", "summary", "lectureText", "transcript", "ownNote"];

/** Where a hit is in a file of the student's notes; "" for the other sections. */
export type NoteLocatorKind = "" | "page" | "slide" | "section" | "fragment";

export const noteLocatorKinds: readonly NoteLocatorKind[] = ["", "page", "slide", "section", "fragment"];

/**
 * One place the query was found. index: the block of SummaryView.lectureText.blocks, the transcript segment, for the
 * summary 0 for its text and i+1 for key point i, 0 for the title, for "ownNote" the fragment of the lesson's notes.
 * The snippet is one plain line. recordingId is "" for a note-only lecture.
 */
export interface KnowledgeHit {
  recordingId: string;
  section: KnowledgeSection;
  index: number;
  startMs: number;
  /** "12:05" for a lecture block or a transcript segment, "" otherwise. */
  time: string;
  before: string;
  match: string;
  after: string;
  /** "ownNote" only: the note and its file to open at locatorKind locatorN (from 1); "" and 0 otherwise. */
  noteId: string;
  materialId: string;
  locatorKind: NoteLocatorKind;
  locatorN: number;
  /** "ownNote" only: the core's name of the place («ваш конспект · стр. 3»), plain text; "" otherwise. */
  label: string;
  lesson: KnowledgeLesson;
}

export interface KnowledgeSearchResult {
  /** The core's order: newest lessons first, the student's notes after the recording within a lesson. */
  hits: KnowledgeHit[];
  /** More places may exist than are listed. */
  truncated: boolean;
}

export type ExportStatus = "saved" | "cancelled" | "failed";

export interface ExportResult {
  status: ExportStatus;
  /** The core's text, shown as it is. */
  message: string;
  /** The base name of the saved file; "" unless saved. */
  name: string;
}

const exportStatuses: readonly ExportStatus[] = ["saved", "cancelled", "failed"];

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

function list(raw: unknown, what: string): unknown[] {
  if (!Array.isArray(raw)) {
    throw new Error(`${what}: reply is not a list`);
  }
  return raw;
}

/** Checks the shape of a reply; a malformed reply is an error, not a default. */
export function parseSubject(raw: unknown): KnowledgeSubject {
  const w = "knowledge subject";
  const r = record(raw, w);
  return {
    key: str(r, "key", w),
    name: str(r, "name", w),
    lessons: count(r, "lessons", w),
    latest: str(r, "latest", w),
    noSubject: bool(r, "noSubject", w),
    subjectId: str(r, "subjectId", w),
  };
}

const dayPattern = /^\d{4}-\d{2}-\d{2}$/;

export function parseNote(raw: unknown): KnowledgeNote {
  const w = "knowledge note";
  const r = record(raw, w);
  const id = str(r, "id", w);
  if (id === "") {
    throw new Error(`${w}: id is empty`);
  }
  return { id, title: str(r, "title", w), subjectId: str(r, "subjectId", w), createdAt: str(r, "createdAt", w) };
}

export function parseLesson(raw: unknown): KnowledgeLesson {
  const w = "knowledge lesson";
  const r = record(raw, w);
  const libraryId = str(r, "librarySubjectId", w);
  const libraryState = oneOf(r, "librarySubjectState", librarySubjectStates, w);
  if ((libraryState === "matched") !== (libraryId !== "")) {
    throw new Error(`${w}: librarySubjectId does not fit librarySubjectState`);
  }
  const recordingId = str(r, "recordingId", w);
  const startedAt = str(r, "startedAt", w);
  const noteOnly = bool(r, "noteOnly", w);
  const undated = bool(r, "undated", w);
  const anchor = oneOf(r, "anchor", lessonAnchors, w);
  const scheduleLessonId = str(r, "scheduleLessonId", w);
  const date = str(r, "date", w);
  const notes = list(r.notes, `${w}: notes`).map(parseNote);
  // The ids a lesson opens by must be those of its anchor, so the page never opens a note-only lecture as a recording.
  const fits =
    noteOnly === (recordingId === "") &&
    (anchor === "recording") === !noteOnly &&
    (anchor === "undated") === undated &&
    (!undated || startedAt === "") &&
    (anchor === "lesson" || anchor === "day") === (date !== "") &&
    (date === "" || dayPattern.test(date)) &&
    (anchor !== "lesson" || scheduleLessonId !== "") &&
    ((anchor !== "day" && anchor !== "undated") || scheduleLessonId === "") &&
    (!noteOnly || notes.length > 0);
  if (!fits) {
    throw new Error(`${w}: the anchor does not fit its ids`);
  }
  return {
    key: str(r, "key", w),
    noteOnly,
    undated,
    anchor,
    scheduleLessonId,
    date,
    notes,
    recordingId,
    startedAt,
    durationSeconds: count(r, "durationSeconds", w),
    title: str(r, "title", w),
    subject: str(r, "subject", w),
    subjectKey: str(r, "subjectKey", w),
    kind: str(r, "kind", w),
    subjectEdited: bool(r, "subjectEdited", w),
    kindEdited: bool(r, "kindEdited", w),
    transcriptReady: bool(r, "transcriptReady", w),
    summaryReady: bool(r, "summaryReady", w),
    lectureTextReady: bool(r, "lectureTextReady", w),
    librarySubjectId: libraryId,
    librarySubjectState: libraryState,
  };
}

export function parseHit(raw: unknown): KnowledgeHit {
  const w = "knowledge hit";
  const r = record(raw, w);
  const section = oneOf(r, "section", knowledgeSections, w);
  const noteId = str(r, "noteId", w);
  const materialId = str(r, "materialId", w);
  const locatorKind = oneOf(r, "locatorKind", noteLocatorKinds, w);
  const locatorN = count(r, "locatorN", w);
  const label = str(r, "label", w);
  const own = section === "ownNote";
  const fits = own
    ? noteId !== "" && materialId !== "" && locatorKind !== "" && locatorN > 0 && label !== ""
    : noteId === "" && materialId === "" && locatorKind === "" && locatorN === 0 && label === "";
  if (!fits) {
    throw new Error(`${w}: the note place does not fit the section`);
  }
  return {
    recordingId: str(r, "recordingId", w),
    section,
    index: count(r, "index", w),
    startMs: count(r, "startMs", w),
    time: str(r, "time", w),
    before: str(r, "before", w),
    match: str(r, "match", w),
    after: str(r, "after", w),
    noteId,
    materialId,
    locatorKind,
    locatorN,
    label,
    lesson: parseLesson(r.lesson),
  };
}

export function parseSearchResult(raw: unknown): KnowledgeSearchResult {
  const w = "knowledge search result";
  const r = record(raw, w);
  return { hits: list(r.hits, `${w}: hits`).map(parseHit), truncated: bool(r, "truncated", w) };
}

export function parseExportResult(raw: unknown): ExportResult {
  const w = "subject export result";
  const r = record(raw, w);
  return { status: oneOf(r, "status", exportStatuses, w), message: str(r, "message", w), name: str(r, "name", w) };
}

export interface KnowledgeBaseApi {
  /** The subjects with their lesson counts; «Без предмета» comes last. */
  subjects(): Promise<KnowledgeSubject[]>;
  /** The recorded lessons of a subject by its key, newest first, without the student's notes. */
  lessons(subjectKey: string): Promise<KnowledgeLesson[]>;
  /**
   * Every lesson of a subject by its key: the recorded ones with the student's notes and the lectures known only from
   * notes, newest first, those «без даты» last (the core's order).
   */
  lectures(subjectKey: string): Promise<KnowledgeLesson[]>;
  lesson(recordingId: string): Promise<KnowledgeLesson>;
  /** Searches the whole base on this computer; the core checks the query (2–100 characters). */
  search(query: string): Promise<KnowledgeSearchResult>;
  /** Shows the Windows save dialog and writes the subject as one .md; the path never comes from the page. */
  exportSubject(subjectKey: string): Promise<ExportResult>;
  /** Sets the subject and the kind of a recording; "" clears that correction. */
  setLessonInfo(recordingId: string, subject: string, kind: LessonKind): Promise<KnowledgeLesson>;
}

/** Builds the knowledge base API over a call function (the Wails runtime in the app, a fake in tests). */
export function knowledgeBaseApi(call: CallByName): KnowledgeBaseApi {
  return {
    subjects: async () => list(await call(methods.subjects), "knowledge subjects").map(parseSubject),
    lessons: async (key) => list(await call(methods.lessons, key), "knowledge lessons").map(parseLesson),
    lectures: async (key) => list(await call(methods.lectures, key), "knowledge lectures").map(parseLesson),
    lesson: async (id) => parseLesson(await call(methods.lesson, id)),
    search: async (query) => parseSearchResult(await call(methods.search, query)),
    exportSubject: async (key) => parseExportResult(await call(methods.exportSubject, key)),
    setLessonInfo: async (id, subject, kind) => parseLesson(await call(methods.setLessonInfo, id, subject, kind)),
  };
}
