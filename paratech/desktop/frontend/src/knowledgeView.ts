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
} from "./bindings/knowledgeBase.js";
import type { SummaryView } from "./bindings/summary.js";
import { lectureBlocks } from "./lectureTextView.js";
import { plural } from "./plural.js";
import { kindLabel } from "./scheduleView.js";
import { keyPointList } from "./summaryView.js";
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

/** «5 занятий · последнее 19.10». */
export function subjectMeta(s: Pick<KnowledgeSubject, "lessons" | "latest">): string {
  const n = `${s.lessons} ${plural(s.lessons, "занятие", "занятия", "занятий")}`;
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
 * segment, for "summary" -1 for the text, else the key point; -1 for none.
 */
export interface LessonFocus {
  tab: LessonTab;
  index: number;
}

export const noFocus: LessonFocus = { tab: "summary", index: -1 };

/** The place a search hit opens: the lecture block, the transcript segment, the summary text or a key point. */
export function hitFocus(h: Pick<KnowledgeHit, "section" | "index">): LessonFocus {
  switch (h.section) {
    case "lectureText":
      return { tab: "lecture", index: h.index };
    case "transcript":
      return { tab: "transcript", index: h.index };
    case "summary":
      return { tab: "summary", index: h.index - 1 };
    case "title":
      return noFocus;
  }
}

export const sectionLabels: Readonly<Record<KnowledgeSection, string>> = {
  title: "Название",
  summary: "Кратко",
  lectureText: "Текст лекции",
  transcript: "Расшифровка",
};

/** «Расшифровка · 12:05»: the section of a hit and where in it. */
export function hitPlace(h: Pick<KnowledgeHit, "section" | "time">): string {
  return h.time !== "" ? `${sectionLabels[h.section]} · ${h.time}` : sectionLabels[h.section];
}

export interface HitGroup {
  lesson: KnowledgeLesson;
  hits: KnowledgeHit[];
}

/** The hits by lesson, in the order the core sent them (newest lessons first). */
export function groupHits(hits: readonly KnowledgeHit[]): HitGroup[] {
  const out: HitGroup[] = [];
  const byId = new Map<string, HitGroup>();
  for (const h of hits) {
    let g = byId.get(h.recordingId);
    if (g === undefined) {
      g = { lesson: h.lesson, hits: [] };
      byId.set(h.recordingId, g);
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
