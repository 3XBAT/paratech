// «Сегодня» bindings: calls into the Go core (internal/input/wails/today.go). Lesson titles, subjects, teachers,
// headlines and recipients are imported or model text: show them as text nodes only. The Go errors are fixed Russian
// texts; only a text from the list below is shown, anything else gets a generic one.

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.TodayService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  today: `${service}.Today`,
  subjectPosts: `${service}.SubjectPosts`,
} as const;

/** The error texts of the Go binding (today.go: ErrTodayInvalidID, ErrTodayUnavailable). */
export const errorTexts: readonly string[] = [
  "Неверный идентификатор предмета: обновите страницу.",
  "Не удалось прочитать данные «Сегодня». Попробуйте ещё раз.",
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

/** The core's text of a refused call when it is one of the known texts; a generic text otherwise. */
export function todayErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) ? msg : genericError;
}

export interface TodayPostLesson {
  lessonId: string;
  title: string;
  subject: string;
  subjectId: string;
  subjectName: string;
  /** A lesson kind code, "" if unknown. */
  kind: string;
  start: string;
}

export interface TodayCalendarAction {
  kind: string;
  assignmentId: string;
  due: string;
}

export interface TodayPost {
  recordingId: string;
  transcriptId: string;
  lesson: TodayPostLesson;
  headline: string;
  summaryReady: boolean;
  homework: number;
  /** "" when no homework has a date. */
  earliestDue: string;
  calendar: TodayCalendarAction[];
  shownAt: string;
}

export interface TodayLesson {
  id: string;
  title: string;
  subject: string;
  kind: string;
  teacher: string;
  start: string;
  end: string;
  post: TodayPost | null;
}

export interface TodayWaiting {
  proposalId: string;
  /** "create_deadline_event", "send_homework_email" or "export_homework". */
  kind: string;
  assignmentId: string;
  title: string;
  subject: string;
  due: string;
  recipient: string;
  createdAt: string;
}

export interface TodayDeadline {
  assignmentId: string;
  title: string;
  subject: string;
  due: string;
  daysLeft: number;
  urgent: boolean;
}

export interface TodayDone {
  recordingId: string;
  lesson: TodayPostLesson;
  summaryReady: boolean;
  homework: number;
  at: string;
}

export interface TodayChip {
  kind: string;
  text: string;
}

export interface TodayScreen {
  day: string;
  lessons: TodayLesson[];
  posts: TodayPost[];
  waiting: TodayWaiting[];
  burning: TodayDeadline[];
  done: TodayDone[];
  chips: TodayChip[];
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

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isFinite(v)) {
    throw new Error(`${what}: ${key} is not a number`);
  }
  return v;
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

export function parsePostLesson(raw: unknown): TodayPostLesson {
  const w = "post lesson";
  const r = record(raw, w);
  return {
    lessonId: optStr(r, "lessonId"),
    title: str(r, "title", w),
    subject: optStr(r, "subject"),
    subjectId: optStr(r, "subjectId"),
    subjectName: optStr(r, "subjectName"),
    kind: optStr(r, "kind"),
    start: optStr(r, "start"),
  };
}

function parseCalendar(raw: unknown): TodayCalendarAction {
  const w = "calendar action";
  const r = record(raw, w);
  return { kind: str(r, "kind", w), assignmentId: str(r, "assignmentId", w), due: optStr(r, "due") };
}

export function parsePost(raw: unknown): TodayPost {
  const w = "post";
  const r = record(raw, w);
  return {
    recordingId: str(r, "recordingId", w),
    transcriptId: optStr(r, "transcriptId"),
    lesson: parsePostLesson(r.lesson),
    headline: str(r, "headline", w),
    summaryReady: r.summaryReady === true,
    homework: count(r, "homework", w),
    earliestDue: optStr(r, "earliestDue"),
    calendar: list(r.calendar, w, parseCalendar),
    shownAt: optStr(r, "shownAt"),
  };
}

export function parseLesson(raw: unknown): TodayLesson {
  const w = "lesson";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    title: str(r, "title", w),
    subject: optStr(r, "subject"),
    kind: optStr(r, "kind"),
    teacher: optStr(r, "teacher"),
    start: str(r, "start", w),
    end: optStr(r, "end"),
    post: r.post === null || r.post === undefined ? null : parsePost(r.post),
  };
}

function parseWaiting(raw: unknown): TodayWaiting {
  const w = "waiting";
  const r = record(raw, w);
  return {
    proposalId: str(r, "proposalId", w),
    kind: str(r, "kind", w),
    assignmentId: optStr(r, "assignmentId"),
    title: str(r, "title", w),
    subject: optStr(r, "subject"),
    due: optStr(r, "due"),
    recipient: optStr(r, "recipient"),
    createdAt: optStr(r, "createdAt"),
  };
}

function parseDeadline(raw: unknown): TodayDeadline {
  const w = "deadline";
  const r = record(raw, w);
  return {
    assignmentId: str(r, "assignmentId", w),
    title: str(r, "title", w),
    subject: optStr(r, "subject"),
    due: str(r, "due", w),
    daysLeft: count(r, "daysLeft", w),
    urgent: r.urgent === true,
  };
}

function parseDone(raw: unknown): TodayDone {
  const w = "done";
  const r = record(raw, w);
  return {
    recordingId: str(r, "recordingId", w),
    lesson: parsePostLesson(r.lesson),
    summaryReady: r.summaryReady === true,
    homework: count(r, "homework", w),
    at: optStr(r, "at"),
  };
}

function parseChip(raw: unknown): TodayChip {
  const w = "chip";
  const r = record(raw, w);
  return { kind: optStr(r, "kind"), text: str(r, "text", w) };
}

export function parseScreen(raw: unknown): TodayScreen {
  const w = "today";
  const r = record(raw, w);
  return {
    day: str(r, "day", w),
    lessons: list(r.lessons, w, parseLesson),
    posts: list(r.posts, w, parsePost),
    waiting: list(r.waiting, w, parseWaiting),
    burning: list(r.burning, w, parseDeadline),
    done: list(r.done, w, parseDone),
    chips: list(r.chips, w, parseChip),
  };
}

export interface TodayApi {
  today(): Promise<TodayScreen>;
  /** The posts of the agent thread of a library subject; "" is «Без предмета». */
  subjectPosts(subjectId: string): Promise<TodayPost[]>;
}

export function todayApi(call: CallByName): TodayApi {
  return {
    today: async () => parseScreen(await call(methods.today)),
    subjectPosts: async (subjectId) => list(await call(methods.subjectPosts, subjectId), "posts", parsePost),
  };
}
