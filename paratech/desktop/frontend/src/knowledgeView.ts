// «База знаний» (TASK-091): texts and the arrangement of what the core sends. The core groups, searches and checks
// the correction; here the values are only labelled and placed. Every text stays a plain string.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import {
  errorMessages,
  type ExportResult,
  type KnowledgeHit,
  type KnowledgeLesson,
  type KnowledgeSection,
  type KnowledgeSubject,
  type LessonKind,
  lessonKinds,
  type NoteLocatorKind,
} from "./bindings/knowledgeBase.js";
import type { SummaryContent, SummaryView } from "./bindings/summary.js";
import { lectureBlocks } from "./lectureTextView.js";
import { ownNotesTexts, shortName } from "./ownNotesView.js";
import { plural } from "./plural.js";
import { kindLabel } from "./scheduleView.js";
import { keyPointList, summarySections } from "./summaryView.js";
import type { Paragraph } from "./transcriptView.js";

function localDate(rfc3339: string): Date | null {
  if (rfc3339 === "") {
    return null;
  }
  const d = new Date(rfc3339);
  return Number.isNaN(d.getTime()) ? null : d;
}

const two = (n: number) => String(n).padStart(2, "0");

/** "19.10" in local time; "" when unknown. */
export function dayText(rfc3339: string): string {
  const d = localDate(rfc3339);
  return d === null ? "" : `${two(d.getDate())}.${two(d.getMonth() + 1)}`;
}

/** "14:05" in local time; "" when unknown. */
export function clockText(rfc3339: string): string {
  const d = localDate(rfc3339);
  return d === null ? "" : `${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** The lesson title, else «Запись 14:05» (the date stands next to it in the label). */
export function lessonName(l: Pick<KnowledgeLesson, "title" | "startedAt">): string {
  const t = l.title.trim();
  if (t !== "") {
    return t;
  }
  const at = clockText(l.startedAt);
  return at === "" ? "Запись" : `Запись ${at}`;
}

/** «19.10 · лекция · Название»; empty parts are skipped. */
export function lessonLabel(l: Pick<KnowledgeLesson, "title" | "startedAt" | "kind">): string {
  return [dayText(l.startedAt), kindLabel(l.kind), lessonName(l)].filter((p) => p !== "").join(" · ");
}

/** The subject of a lesson for a search hit or the lesson page; «Без предмета» when there is none. */
export const noSubjectName = "Без предмета";

export function subjectName(l: Pick<KnowledgeLesson, "subject">): string {
  const s = l.subject.trim();
  return s === "" ? noSubjectName : s;
}

/** The heading of an opened lesson: its title, else «Запись 19.10 14:05». */
export function lessonHeading(l: Pick<KnowledgeLesson, "title" | "startedAt">): string {
  const t = l.title.trim();
  if (t !== "") {
    return t;
  }
  const at = [dayText(l.startedAt), clockText(l.startedAt)].filter((p) => p !== "").join(" ");
  return at === "" ? "Запись" : `Запись ${at}`;
}

/** «Матанализ · лекция · 19.10 в 14:05» under the heading of an opened lesson. */
export function lessonMeta(l: Pick<KnowledgeLesson, "subject" | "kind" | "startedAt">): string {
  const day = dayText(l.startedAt);
  const when = day === "" ? "" : `${day} в ${clockText(l.startedAt)}`;
  return [subjectName(l), kindLabel(l.kind), when].filter((p) => p !== "").join(" · ");
}

export interface ReadyMark {
  key: "summary" | "lectureText" | "transcript";
  label: string;
  ready: boolean;
}

/** The readiness marks of a lesson, in the order of the lesson tabs. */
export function readyMarks(l: Pick<KnowledgeLesson, "summaryReady" | "lectureTextReady" | "transcriptReady">): ReadyMark[] {
  return [
    { key: "summary", label: "Кратко", ready: l.summaryReady },
    { key: "lectureText", label: "Текст лекции", ready: l.lectureTextReady },
    { key: "transcript", label: "Расшифровка", ready: l.transcriptReady },
  ];
}

/** «5 занятий · последнее 19.10»; a subject known only from the student's notes has no recorded lessons. */
export function subjectMeta(s: Pick<KnowledgeSubject, "lessons" | "latest">): string {
  if (s.lessons === 0) {
    return ownNotesTexts.ownChip;
  }
  const n =`${s.lessons} ${plural(s.lessons, "занятие", "занятия", "занятий")}`;
  const day = dayText(s.latest);
  return day === "" ? n : `${n} · последнее ${day}`;
}

/** The tabs of an opened lesson. */
export type LessonTab = "summary" | "lecture" | "transcript" | "homework";

export const lessonTabs: readonly { value: LessonTab; label: string }[] = [
  { value: "summary", label: "Кратко" },
  { value: "lecture", label: "Текст лекции" },
  { value: "transcript", label: "Расшифровка" },
  { value: "homework", label: "Задания" },
];

/**
 * Where a lesson opens. index: for "lecture" the block of SummaryView.lectureText.blocks, for "transcript" the
 * segment, for "summary" the hit's index (summaryFocus maps it onto a section or a key point); -1 for none.
 */
export interface LessonFocus {
  tab: LessonTab;
  index: number;
}

export const noFocus: LessonFocus = { tab: "summary", index: -1 };

/**
 * The place a search hit opens in a recording's page: the lecture block, the transcript segment, a summary section or a
 * key point. A hit in the student's notes opens the notes block, which is not a tab: no focus.
 */
export function hitFocus(h: Pick<KnowledgeHit, "section" | "index">): LessonFocus {
  switch (h.section) {
    case "lectureText":
      return { tab: "lecture", index: h.index };
    case "transcript":
      return { tab: "transcript", index: h.index };
    case "summary":
      return { tab: "summary", index: h.index };
    case "title":
    case "ownNote":
      return noFocus;
  }
}

/** The section names of a recording; a hit in the student's notes is named by the core's label instead. */
export const sectionLabels: Readonly<Record<Exclude<KnowledgeSection, "ownNote">, string>> = {
  title: "Название",
  summary: "Кратко",
  lectureText: "Текст лекции",
  transcript: "Расшифровка",
};

/**
 * «Расшифровка · 12:05»: the section of a hit and where in it. A hit in the student's notes shows the core's label
 * («ваш конспект · стр. 3») as it is: plain text, never rebuilt here.
 */
export function hitPlace(h: Pick<KnowledgeHit, "section" | "time" | "label">): string {
  if (h.section === "ownNote") {
    return h.label !== "" ? h.label : ownNotesTexts.ownChip;
  }
  return h.time !== "" ? `${sectionLabels[h.section]} · ${h.time}` : sectionLabels[h.section];
}

/** A lecture known only from notes has no recording: the page must not ask a recording-only binding for it. */
export function isNoteOnly(l: Pick<KnowledgeLesson, "noteOnly" | "recordingId">): boolean {
  return l.noteOnly || l.recordingId === "";
}

/** What a click on a hit opens: the lesson, the place in a recording and, for the student's notes, the file. */
export function hitOpening(h: Pick<KnowledgeHit, "section" | "index" | "lesson" | "materialId">): {
  lesson: KnowledgeLesson;
  focus: LessonFocus;
  materialId: string;
} {
  return { lesson: h.lesson, focus: hitFocus(h), materialId: h.section === "ownNote" ? h.materialId : "" };
}

export interface HitGroup {
  lesson: KnowledgeLesson;
  hits: KnowledgeHit[];
}

/** The hits by lesson, in the order the core sent them; a note-only lecture (no recording id) is told apart by its key. */
export function groupHits(hits: readonly KnowledgeHit[]): HitGroup[] {
  const out: HitGroup[] = [];
  const byId = new Map<string, HitGroup>();
  for (const h of hits) {
    const id = h.recordingId !== "" ? h.recordingId : h.lesson.key;
    let g = byId.get(id);
    if (g === undefined) {
      g = { lesson: h.lesson, hits: [] };
      byId.set(id, g);
      out.push(g);
    }
    g.hits.push(h);
  }
  return out;
}

/** The core's bounds of a query (ADR-0019), so the page does not ask for what it would refuse. */
export const minQueryChars = 2;
export const maxQueryChars = 100;
/** The pause in typing after which the search starts by itself (Enter starts it at once). */
export const searchPauseMs = 700;

/**
 * At most one search call at a time (a full scan takes seconds and is not cancelled): while one runs, only the
 * latest wanted query is kept. next: null = nothing new, "" = the field was cleared (drop the answer).
 */
export type SearchFlight = { kind: "idle" } | { kind: "running"; query: string; next: string | null };

export const idleFlight: SearchFlight = { kind: "idle" };

/** The user wants q ("" = no search): send it now, or keep it until the running call returns. */
export function askSearch(f: SearchFlight, q: string): { flight: SearchFlight; send: string | null } {
  if (f.kind === "running") {
    return { flight: { ...f, next: q }, send: null };
  }
  return q === "" ? { flight: f, send: null } : { flight: { kind: "running", query: q, next: null }, send: q };
}

/** The running call returned: send the query wanted since, show this answer, or drop it. */
export function searchDone(f: SearchFlight): { flight: SearchFlight; send: string | null; show: boolean } {
  if (f.kind === "idle") {
    return { flight: f, send: null, show: false };
  }
  if (f.next === null || f.next === f.query) {
    return { flight: idleFlight, send: null, show: true };
  }
  if (f.next === "") {
    return { flight: idleFlight, send: null, show: false };
  }
  return { flight: { kind: "running", query: f.next, next: null }, send: f.next, show: false };
}

export type QueryCheck = { kind: "empty" } | { kind: "short" } | { kind: "long" } | { kind: "ok"; query: string };

/** The query as the core would read it: trimmed, spaces collapsed, counted in characters. */
export function checkQuery(raw: string): QueryCheck {
  const q = raw.trim().replace(/\s+/g, " ");
  const n = [...q].length;
  if (n === 0) {
    return { kind: "empty" };
  }
  if (n < minQueryChars) {
    return { kind: "short" };
  }
  return n > maxQueryChars ? { kind: "long" } : { kind: "ok", query: q };
}

export const searchTexts = {
  short: `Введите хотя бы ${minQueryChars} символа.`,
  running: "Поиск…",
  long: errorMessages.query,
  none: "Ничего не найдено.",
  local: "Поиск идёт по этому компьютеру, в сеть ничего не уходит.",
};

/** «Найдено 12 мест в 3 занятиях»; with truncated «Показаны первые 50 мест: уточните запрос». */
export function hitCountText(hits: number, lessons: number, truncated: boolean): string {
  if (truncated) {
    return `Показаны первые ${hits} ${plural(hits, "место", "места", "мест")}: уточните запрос.`;
  }
  return `Найдено ${hits} ${plural(hits, "место", "места", "мест")} в ${lessons} ${plural(lessons, "занятии", "занятиях", "занятиях")}.`;
}

/** The outcome of «Экспорт в .md»: what to show and how. */
export function exportResultLine(r: ExportResult): { kind: "ok" | "wait" | "fail"; text: string } {
  switch (r.status) {
    case "saved":
      return { kind: "ok", text: r.name !== "" ? `Сохранено: ${r.name}` : r.message };
    case "cancelled":
      return { kind: "wait", text: r.message };
    case "failed":
      return { kind: "fail", text: r.message };
  }
}

/** What the correction form starts with: only the user's own values; empty fields show the recording's ones. */
export function correctionDraft(l: Pick<KnowledgeLesson, "subject" | "kind" | "subjectEdited" | "kindEdited">): {
  subject: string;
  kind: LessonKind;
} {
  const kind = lessonKinds.includes(l.kind as LessonKind) ? (l.kind as LessonKind) : "";
  return { subject: l.subjectEdited ? l.subject : "", kind: l.kindEdited ? kind : "" };
}

/** The placeholder of the subject field: what shows without a correction. */
export function subjectPlaceholder(l: Pick<KnowledgeLesson, "subject" | "subjectEdited">): string {
  return l.subjectEdited || l.subject.trim() === "" ? noSubjectName : l.subject;
}

/** The kind options; "" names what shows without a correction when it is known. */
export function kindOptions(l: Pick<KnowledgeLesson, "kind" | "kindEdited">): { value: LessonKind; label: string }[] {
  const shown = l.kindEdited ? "" : kindLabel(l.kind);
  return lessonKinds.map((k) =>
    k === "" ? { value: k, label: shown !== "" ? `Как в записи (${shown})` : "Как в записи" } : { value: k, label: kindLabel(k) },
  );
}

/** The longest subject the core keeps (values.MaxLessonSubjectBytes is in bytes; the core says when it is too long). */
export const maxSubjectLength = 256;

/** The lecture block as shown by LectureTextCard: empty blocks are skipped there; -1 when this one is not shown. */
export function shownBlock(v: Pick<SummaryView, "lectureText">, index: number): number {
  const blocks = v.lectureText.blocks;
  if (!v.lectureText.ready || index < 0 || index >= blocks.length) {
    return -1;
  }
  const before = lectureBlocks({ lectureText: { ...v.lectureText, blocks: blocks.slice(0, index) } }).length;
  const upTo = lectureBlocks({ lectureText: { ...v.lectureText, blocks: blocks.slice(0, index + 1) } }).length;
  return upTo > before ? before : -1;
}

/** The key point as listed by SummaryCard (empty ones are skipped); -1 when it is not listed. */
export function shownPoint(points: readonly string[], index: number): number {
  if (index < 0 || index >= points.length || points[index].trim() === "") {
    return -1;
  }
  return keyPointList(points.slice(0, index)).length;
}

/**
 * A summary hit's index as SummaryCard marks it: i < n is section i of n sections, n+j is key point j (the core's
 * KnowledgeHit). section is an index of summary.sections, point one of the listed key points; -1 when not shown.
 */
export function summaryFocus(
  s: Pick<SummaryContent, "ready" | "sections" | "keyPoints">,
  index: number,
): { section: number; point: number } {
  const none = { section: -1, point: -1 };
  if (!s.ready || index < 0) {
    return none;
  }
  const n = s.sections.length;
  if (index >= n) {
    return { section: -1, point: shownPoint(s.keyPoints, index - n) };
  }
  return summarySections(s.sections.slice(index, index + 1)).length > 0 ? { section: index, point: -1 } : none;
}

/** The paragraph holding the transcript segment; -1 while it is not loaded. */
export function paragraphOf(paras: readonly Paragraph[], segment: number): number {
  if (segment < 0) {
    return -1;
  }
  return paras.findIndex((p) => {
    const s = p.segments;
    return s.length > 0 && s[0].index <= segment && segment <= s[s.length - 1].index;
  });
}

// ---- Lectures known only from the student's notes, and «Добавить конспект прошедшей пары» (TASK-155, ADR-0027) ----

/** Every text of the note-only lectures, the add flow and the citations in one place (the naming is the owner's call). */
export const noteLectureTexts = {
  ownChip: ownNotesTexts.ownChip,
  undated: "без даты",
  pair: "Пара",
  addPast: "Добавить конспект прошедшей пары",
  addPastTitle: "Конспект прошедшей пары",
  pickRecording: "Занятие с записью",
  pickRecordingNone: "Нет занятий с записью без конспекта.",
  pickDate: "Дата пары",
  pickUndated: "Без даты",
  choose: "Выбрать",
  close: "Закрыть",
  dateEmpty: "Укажите дату.",
  dateInvalid: "Неверная дата.",
  dateFuture: "Дата не может быть в будущем.",
  // Fallbacks only: the binding sends the label of a source and «источник удалён» for a removed one.
  sourceRemoved: "источник удалён",
} as const;

// Follow-up (ADR-0027 Open 2): the core has no list of a subject's past schedule slots yet, so the add flow offers
// recorded lessons, a date and «без даты» only.

const dayPattern = /^(\d{4})-(\d{2})-(\d{2})$/;

/** "12.09" from "2026-09-12" (the day is a plain local date, no time zone involved); "" when it is not one. */
export function dateText(date: string): string {
  const m = dayPattern.exec(date);
  return m === null ? "" : `${m[3]}.${m[2]}`;
}

/** "12.09.2026" from "2026-09-12". */
export function dateFullText(date: string): string {
  const m = dayPattern.exec(date);
  return m === null ? "" : `${m[3]}.${m[2]}.${m[1]}`;
}

/** The day of a lecture for its line: «19.10», «12.09» or «без даты». */
export function lectureDay(l: Pick<KnowledgeLesson, "noteOnly" | "undated" | "date" | "startedAt">): string {
  if (l.undated) {
    return noteLectureTexts.undated;
  }
  return l.noteOnly || l.date !== "" ? dateText(l.date) : dayText(l.startedAt);
}

/** The name of a lecture without a recording: its first note's title (typed text or a file name), plain text. */
export function noteLectureName(l: Pick<KnowledgeLesson, "notes">): string {
  const t = l.notes[0]?.title.trim() ?? "";
  const name = t === "" ? ownNotesTexts.untitled : shortName(t);
  return l.notes.length > 1 ? `${name} и ещё ${l.notes.length - 1}` : name;
}

/** The line of a lecture in the list: a recording's usual label, or «12.09 · Ряды.docx» / «без даты · Ряды.docx». */
export function lectureLabel(l: Pick<KnowledgeLesson, "noteOnly" | "recordingId" | "undated" | "date" | "notes" | "title" | "startedAt" | "kind">): string {
  if (!isNoteOnly(l)) {
    return lessonLabel(l);
  }
  return [lectureDay(l), noteLectureName(l)].filter((p) => p !== "").join(" · ");
}

/** The heading of a note-only lecture page: «Пара 12.09.2026» / «Пара без даты». */
export function noteLectureHeading(l: Pick<KnowledgeLesson, "undated" | "date">): string {
  const day = l.undated || l.date === "" ? noteLectureTexts.undated : dateFullText(l.date);
  return `${noteLectureTexts.pair} ${day}`;
}

/** «Матанализ · ваш конспект» under that heading. */
export function noteLectureMeta(l: Pick<KnowledgeLesson, "subject">): string {
  return `${subjectName(l)} · ${noteLectureTexts.ownChip}`;
}

/**
 * The lectures of a subject by date, newest first, those «без даты» (and any with an unreadable start) last. The core
 * already sends this order; it is kept here so a page never shows a different one. Stable for equal dates.
 */
export function orderLectures<T extends Pick<KnowledgeLesson, "undated" | "startedAt">>(lectures: readonly T[]): T[] {
  const at = (l: T) => (l.undated ? Number.NaN : (localDate(l.startedAt)?.getTime() ?? Number.NaN));
  return lectures
    .map((l, i) => ({ l, i, t: at(l) }))
    .sort((a, b) => {
      const an = Number.isNaN(a.t);
      const bn = Number.isNaN(b.t);
      if (an !== bn) {
        return an ? 1 : -1;
      }
      return an || a.t === b.t ? a.i - b.i : b.t - a.t;
    })
    .map((x) => x.l);
}

/** The count and the newest start of a subject's lectures for its heading; only recorded ones have a count of lessons. */
export function lecturesMeta(lectures: readonly KnowledgeLesson[]): Pick<KnowledgeSubject, "lessons" | "latest"> {
  const recorded = lectures.filter((l) => !isNoteOnly(l));
  return { lessons: recorded.length, latest: recorded[0]?.startedAt ?? "" };
}

/** The recorded lessons of the subject that have no notes yet: the ones «Добавить конспект прошедшей пары» offers. */
export function lessonsWithoutNotes(lectures: readonly KnowledgeLesson[]): KnowledgeLesson[] {
  return lectures.filter((l) => !isNoteOnly(l) && l.notes.length === 0);
}

/** What the student picks in «Добавить конспект прошедшей пары». */
export type PastChoice =
  | { kind: "recording"; lesson: KnowledgeLesson }
  | { kind: "day"; date: string }
  | { kind: "undated" };

/**
 * The lecture the block opens for a choice. A recorded lesson is itself; a day or «без даты» is a lecture that is not
 * in the list yet (no notes), opened by the subject's id and the date, never by a path.
 */
export function pastLesson(subject: Pick<KnowledgeSubject, "key" | "name" | "noSubject" | "subjectId">, c: PastChoice): KnowledgeLesson {
  if (c.kind === "recording") {
    return c.lesson;
  }
  const undated = c.kind === "undated";
  return {
    key: undated ? `new:undated:${subject.key}` : `new:day:${subject.key}:${c.date}`,
    noteOnly: true,
    undated,
    anchor: undated ? "undated" : "day",
    scheduleLessonId: "",
    date: undated ? "" : c.date,
    notes: [],
    recordingId: "",
    startedAt: "",
    durationSeconds: 0,
    title: "",
    subject: subject.noSubject ? "" : subject.name,
    subjectKey: subject.key,
    kind: "",
    subjectEdited: false,
    kindEdited: false,
    transcriptReady: false,
    summaryReady: false,
    lectureTextReady: false,
    librarySubjectId: subject.subjectId,
    librarySubjectState: subject.subjectId !== "" ? "matched" : "none",
  };
}

/** "YYYY-MM-DD" of a moment in local time. */
export function localDay(d: Date): string {
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;
}

export type DateCheck = { kind: "empty" | "invalid" | "future" } | { kind: "ok"; date: string };

/** The typed date of a past pair: a real calendar day, not after today (both "YYYY-MM-DD", local). */
export function checkPastDate(value: string, today: string): DateCheck {
  if (value === "") {
    return { kind: "empty" };
  }
  const m = dayPattern.exec(value);
  if (m === null) {
    return { kind: "invalid" };
  }
  const probe = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  if (localDay(probe) !== value) {
    return { kind: "invalid" };
  }
  return value > today ? { kind: "future" } : { kind: "ok", date: value };
}

export function dateCheckText(c: DateCheck): string {
  switch (c.kind) {
    case "empty":
      return noteLectureTexts.dateEmpty;
    case "invalid":
      return noteLectureTexts.dateInvalid;
    case "future":
      return noteLectureTexts.dateFuture;
    case "ok":
      return "";
  }
}

/** The part of an answer's source a citation needs (agent answer card, ADR-0027); every text is plain text. */
export interface CitationSource {
  section: KnowledgeSection;
  recordingId: string;
  index: number;
  time: string;
  noteId: string;
  materialId: string;
  locatorKind: NoteLocatorKind;
  locatorN: number;
  /** The binding's name of the place; «источник удалён» when removed. */
  label: string;
  removed: boolean;
}

/** The text of a citation's place: the binding's label for the student's notes and for a removed source. */
export function citationPlace(s: CitationSource): string {
  if (s.removed) {
    return s.label !== "" ? s.label : noteLectureTexts.sourceRemoved;
  }
  return hitPlace(s);
}

export type CitationOpening =
  | { kind: "note"; noteId: string; materialId: string; locatorKind: NoteLocatorKind; locatorN: number }
  | { kind: "recording"; recordingId: string; focus: LessonFocus };

/** What a click on a citation opens, by ids only; a removed source, or one without ids, does not open (null). */
export function citationOpening(s: CitationSource): CitationOpening | null {
  if (s.removed) {
    return null;
  }
  if (s.section === "ownNote") {
    return s.noteId === "" || s.materialId === ""
      ? null
      : { kind: "note", noteId: s.noteId, materialId: s.materialId, locatorKind: s.locatorKind, locatorN: s.locatorN };
  }
  return s.recordingId === "" ? null : { kind: "recording", recordingId: s.recordingId, focus: hitFocus(s) };
}

const knownErrors: ReadonlySet<string> = new Set(Object.values(errorMessages));

/** The core's fixed text of a failed call as it is; anything else (a malformed reply) gets the generic text. */
export function errorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}
