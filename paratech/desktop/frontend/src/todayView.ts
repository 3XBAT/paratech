// View logic of «Сегодня»: dates, times, the wording of rows and chips. The core decides what belongs to the day, what
// burns and what waits (ARCH-44); here it is only put into words. The voice is impersonal. Titles, subjects, teachers,
// headlines and recipients are imported or model text, passed through untouched and rendered as text nodes only.

import { actionErrorText } from "./activityView.js";
import type { RecordingItem } from "./bindings/activity.js";
import type { TodayDeadline, TodayDone, TodayLesson, TodayPost, TodayWaiting } from "./bindings/today.js";
import { dayAndTime, dayText, daysBetween, daysLeftText, dueText, monthsShort, pad, parseMoment, timeText } from "./dueText.js";
import { plural } from "./plural.js";
import { chainStatusView } from "./recordingRowView.js";
import type { ChipKind } from "./ui/StatusChip.js";
import type { IconName } from "./ui/Icon.js";

// The deadline wording lives in dueText.ts; the names stay importable from here.
export { dayAndTime, dayText, daysBetween, daysLeftText, dueText, parseMoment, timeText };

export const texts = {
  title: "Сегодня",
  askTitle: "Спросить paratech",
  askHint: "Вопрос вводится в панели справа (Ctrl+K). Подсказка только подставляет текст, отправка остаётся за вами.",
  lessons: "Пары дня",
  lessonsEmpty: "На сегодня пар нет.",
  waiting: "Нужно ваше решение",
  waitingEmpty: "Ничего не ждёт решения.",
  waitingFoot: "Письма, календарь и выгрузка выполняются только после подтверждения.",
  burning: "Сроки на неделе",
  burningEmpty: "Сроков на неделе нет.",
  summary: "Итоги дня",
  loading: "Загрузка…",
  loadFailed: "Не удалось загрузить день.",
  refreshFailed: "Не удалось обновить день.",
  retry: "Повторить",
  queueOpen: "Открыть раздел «Записи»",
  open: "Открыть",
  summaryReady: "Конспект готов",
  summaryPending: "Конспект готовится",
  recordingProblem: "Проблема с записью",
  noTitle: "Без названия",
} as const;

const weekdaysLong = ["Воскресенье", "Понедельник", "Вторник", "Среда", "Четверг", "Пятница", "Суббота"] as const;
const monthsGen = [
  "января",
  "февраля",
  "марта",
  "апреля",
  "мая",
  "июня",
  "июля",
  "августа",
  "сентября",
  "октября",
  "ноября",
  "декабря",
] as const;

/** «21.10» in the local time zone, "" when unknown. */
export function shortDate(s: string): string {
  const d = parseMoment(s);
  return d === null ? "" : `${pad(d.getDate())}.${pad(d.getMonth() + 1)}`;
}

/** «Среда, 21 октября» for the start of the day, "" when unknown. */
export function dayTitle(day: string): string {
  const d = parseMoment(day);
  return d === null ? "" : `${weekdaysLong[d.getDay()]}, ${d.getDate()} ${monthsGen[d.getMonth()]}`;
}

const kinds: Readonly<Record<string, string>> = {
  lecture: "Лекция",
  seminar: "Семинар",
  practice: "Практика",
  lab: "Лабораторная",
  other: "Пара",
};

/** The Russian word of a lesson kind code, "" when unknown. */
export function kindLabel(kind: string): string {
  return kinds[kind] ?? "";
}

/** «Семинар · Иванова Е. С.»: the parts that are known. */
export function lessonMeta(l: { kind: string; teacher: string }): string {
  return [kindLabel(l.kind), l.teacher].filter((p) => p !== "").join(" · ");
}

/** The time column of a lesson: the start and, when known, the end. */
export function lessonTimes(l: { start: string; end: string }): { start: string; end: string } {
  return { start: timeText(l.start), end: timeText(l.end) };
}

/** The title of a row: the lesson title, or a fixed word when it is empty. */
export function titleOf(title: string): string {
  return title.trim() === "" ? texts.noTitle : title;
}

/** «28 окт.» in the local time zone, "" when unknown. */
export function dayMonthText(s: string): string {
  const d = parseMoment(s);
  return d === null ? "" : `${d.getDate()} ${monthsShort[d.getMonth()]}`;
}

/**
 * «1 задание · до сб, 24 окт., 20:00»; "" when no homework was found. When all of it is marked done (done is the count of
 * the done ones) the deadline is of no use: «1 задание · выполнено». `now` makes today and tomorrow read shorter.
 */
export function homeworkText(count: number, earliestDue: string, done = 0, now?: Date): string {
  if (count <= 0) {
    return "";
  }
  const n = `${count} ${plural(count, "задание", "задания", "заданий")}`;
  if (done >= count) {
    return `${n} · ${plural(count, "выполнено", "выполнены", "выполнено")}`;
  }
  const due = dueText(earliestDue, now);
  return due === "" ? n : `${n} · ${due}`;
}

export interface Live {
  kind: "rec" | "run";
  text: string;
}

/**
 * The live status of a lesson from the recordings list of the activity binding: the lesson rows of «Сегодня» carry
 * none. A running recording wins over a recognition; null when the lesson has neither.
 */
export function liveStatus(lessonId: string, recordings: readonly RecordingItem[]): Live | null {
  if (lessonId === "") {
    return null;
  }
  const mine = recordings.filter((r) => r.lessonId === lessonId);
  if (mine.some((r) => r.stage === "recording")) {
    return { kind: "rec", text: "Идёт запись" };
  }
  const recognition = mine.find((r) => r.stage === "transcribing");
  if (recognition !== undefined) {
    const pct = Math.max(0, Math.min(100, Math.round(recognition.progressPercent)));
    return { kind: "run", text: `Расшифровка · ${pct} %` };
  }
  return null;
}

/** The main button of the head (TASK-348): «Записать пару», and while a recording runs its stop. */
export const recordTexts = {
  start: "Записать пару",
  running: "● Идёт запись",
  stop: "Остановить",
  stopping: "Остановка…",
  /** Under a failed stop: the way out besides pressing the button again. */
  stopWay: "Остановить запись можно и в разделе «Записи».",
  openRecordings: "Открыть «Записи»",
} as const;

/** «00:41» under an hour, «1:02:05» from an hour; "—" for an unusable value. */
export function clockText(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "—";
  }
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ms = `${pad(m)}:${pad(s)}`;
  return h > 0 ? `${h}:${ms}` : ms;
}

/** The elapsed time of a running recording by the clock `now` (ms); "" when the start is unknown or not capturing yet. */
export function elapsedText(r: Pick<RecordingItem, "status" | "startedAt">, now: number): string {
  if (r.status !== "RECORDING") {
    return "";
  }
  const started = parseMoment(r.startedAt);
  return started === null ? "" : clockText(Math.max(0, (now - started.getTime()) / 1000));
}

export type RecordButton =
  | { kind: "start"; label: string; id: "" }
  | { kind: "stop"; label: string; id: string }
  /** A recording runs but the core does not offer to stop it yet (it is starting): the button only says so. */
  | { kind: "wait"; label: string; id: string };

/**
 * «Записать пару» or, while a recording runs, «● Идёт запись 00:41 · Остановить». The running one is the notion of the
 * recordings page (stage «recording»); Stop is offered when the core says it is stoppable, as in the row. A start refused
 * for a known reason is explained on «Записи» (manualStartView), not here.
 */
export function recordButton(
  recordings: readonly Pick<RecordingItem, "id" | "stage" | "status" | "startedAt" | "stoppable">[] | null,
  now: number,
): RecordButton {
  const running = (recordings ?? []).filter((r) => r.stage === "recording");
  const stoppable = running.find((r) => r.stoppable);
  if (stoppable !== undefined) {
    const t = elapsedText(stoppable, now);
    return { kind: "stop", label: `${recordTexts.running}${t === "" ? "" : ` ${t}`} · ${recordTexts.stop}`, id: stoppable.id };
  }
  if (running.length > 0) {
    return { kind: "wait", label: recordTexts.running, id: running[0].id };
  }
  return { kind: "start", label: recordTexts.start, id: "" };
}

/** The line under a failed stop: what happened and the way out. */
export function stopProblemText(err: unknown): string {
  return `${actionErrorText(err)} ${recordTexts.stopWay}`;
}

/** A signature of the recordings stages: it changes when a recording starts, stops or finishes. */
export function stagesKey(recordings: readonly RecordingItem[]): string {
  return recordings.map((r) => `${r.id}:${r.stage}`).join("|");
}

export interface WaitingLine {
  key: string;
  label: string;
  title: string;
  /** The library subject name, "" when the assignment has none. */
  subject: string;
  /** Due date or recipient: the parts that are known. */
  meta: string;
  icon: "cal" | "mail" | "tasks";
}

const waitingKinds: Readonly<Record<string, { label: string; icon: WaitingLine["icon"] }>> = {
  create_deadline_event: { label: "Срок в календарь", icon: "cal" },
  send_homework_email: { label: "Письмо преподавателю", icon: "mail" },
  send_work_email: { label: "Работа преподавателю", icon: "mail" },
  export_homework: { label: "Выгрузка задания", icon: "tasks" },
};

export function waitingLine(w: TodayWaiting, now?: Date): WaitingLine {
  const k = waitingKinds[w.kind] ?? { label: "Действие", icon: "tasks" as const };
  const recipient = w.recipient === "" ? "" : `→ ${w.recipient}`;
  return {
    key: w.proposalId,
    label: k.label,
    title: titleOf(w.title),
    subject: w.subject.trim(),
    meta: [dueText(w.due, now), recipient].filter((p) => p !== "").join(" · "),
    icon: k.icon,
  };
}

export interface DeadlineLine {
  key: string;
  title: string;
  subject: string;
  chip: string;
  urgent: boolean;
  /** The moment has passed: the chip reads as a failure, not as «сегодня». */
  overdue: boolean;
}

/**
 * The chip of a «Сроки на неделе» row: the same phrase as everywhere (dueText). The days come from the local `now` (a
 * stale snapshot never says «завтра» for today) and the weekday and date are in the phrase, so the row has no date column:
 * «сегодня до 20:00», «завтра до 18:30», «до сб, 24 окт., 18:30», «просрочено, пт, 23 окт., 20:00». The core's daysLeft
 * is only the fallback for an unreadable due.
 */
export function deadlineChip(b: Pick<TodayDeadline, "due" | "daysLeft">, now: Date): string {
  const text = dueText(b.due, now);
  return text === "" ? daysLeftText(b.daysLeft) : text;
}

/** The deadline has passed: by the clock when the moment is readable, else by the day count of the core. */
export function isOverdue(b: Pick<TodayDeadline, "due" | "daysLeft">, now: Date): boolean {
  const at = parseMoment(b.due);
  return at === null ? b.daysLeft < 0 : at.getTime() <= now.getTime();
}

export function deadlineLine(b: TodayDeadline, now: Date): DeadlineLine {
  return {
    key: b.assignmentId,
    title: titleOf(b.title),
    subject: b.subject.trim(),
    chip: deadlineChip(b, now),
    urgent: b.urgent,
    overdue: isOverdue(b, now),
  };
}

/**
 * «Ближайший срок — сегодня до 20:00» from the deadlines of the week (the earliest one), worded as the list (dueText); ""
 * when there is none. With `now` the deadlines that have passed are skipped.
 */
export function nearestDeadlineText(burning: readonly Pick<TodayDeadline, "due">[], now?: Date): string {
  const dues = burning
    .map((b) => ({ due: b.due, at: parseMoment(b.due) }))
    .filter((b) => b.at !== null && (now === undefined || b.at.getTime() > now.getTime()));
  if (dues.length === 0) {
    return "";
  }
  const first = dues.reduce((a, b) => ((b.at as Date) < (a.at as Date) ? b : a));
  return `Ближайший срок — ${dueText(first.due, now)}`;
}

/** What the page shows about the read of the day: the first read, a retry in flight, or a failure (with or without a day). */
export type LoadView = "loading" | "error" | "stale" | "none";

export function loadView(hasDay: boolean, failed: boolean, retrying: boolean): LoadView {
  if (failed) {
    return hasDay ? "stale" : "error";
  }
  return !hasDay || retrying ? "loading" : "none";
}

/** The parts of the status bar: the queue and the nearest deadline, the empty ones left out; none, no bar. */
export function statusParts(queue: string, nearest: string): string[] {
  return [queue, nearest].filter((p) => p !== "");
}

/**
 * «Итоги дня»: the posts first, then the done lessons. A done lesson whose recording a post already tells about is not
 * repeated (the post says it). The count in the title is the number of shown rows.
 */
export function summaryRows(posts: readonly TodayPost[], done: readonly TodayDone[]): { posts: readonly TodayPost[]; done: TodayDone[]; count: number } {
  const told = new Set(posts.map((p) => p.recordingId));
  const rest = done.filter((d) => !told.has(d.recordingId));
  return { posts, done: rest, count: posts.length + rest.length };
}

export interface DoneLine {
  key: string;
  title: string;
  meta: string;
  icon: "ok" | "clock";
  canOpen: boolean;
  recordingId: string;
}

/** «Конспект · Матанализ» over «Лекция · 21.10 · 11:52 · нашлось 1 задание». */
export function doneLine(d: TodayDone): DoneLine {
  const name = d.lesson.subjectName || d.lesson.subject || d.lesson.title;
  const found = d.homework > 0 ? `нашлось ${d.homework} ${plural(d.homework, "задание", "задания", "заданий")}` : "";
  const meta = [kindLabel(d.lesson.kind), shortDate(d.lesson.start), timeText(d.at), found].filter((p) => p !== "").join(" · ");
  const head = d.summaryReady ? "Конспект" : texts.summaryPending;
  return {
    key: d.recordingId,
    title: `${head} · ${titleOf(name)}`,
    meta,
    icon: d.summaryReady ? "ok" : "clock",
    canOpen: d.summaryReady,
    recordingId: d.recordingId,
  };
}

export interface PostLine {
  key: string;
  headline: string;
  meta: string;
  homework: string;
  canOpen: boolean;
  recordingId: string;
}

/** A post of the agent after a lesson: the headline is the core's text; «Открыть» only for a ready summary. */
export function postLine(p: TodayPost, now?: Date): PostLine {
  const meta = [titleOf(p.lesson.title), timeText(p.shownAt)].filter((x) => x !== "").join(" · ");
  return {
    key: p.recordingId,
    headline: p.headline,
    meta,
    homework: homeworkText(p.homework, p.earliestDue, p.homeworkDone, now),
    canOpen: p.summaryReady,
    recordingId: p.recordingId,
  };
}

export interface LessonLine {
  key: string;
  start: string;
  end: string;
  title: string;
  meta: string;
  live: Live | null;
  /** A failed recording of the lesson of this day; null when there is none. */
  problem: Problem | null;
  /** The chip of the lesson's post; null while it has none. */
  post: { text: string; homework: string; canOpen: boolean; recordingId: string } | null;
}

export function lessonLine(l: TodayLesson, recordings: readonly RecordingItem[], day = "", now?: Date): LessonLine {
  const { start, end } = lessonTimes(l);
  return {
    key: l.id,
    start,
    end,
    title: titleOf(l.title),
    meta: lessonMeta(l),
    live: liveStatus(l.id, recordings),
    problem: problemFor(l.id, recordings, day),
    post:
      l.post === null
        ? null
        : {
            text: l.post.summaryReady ? texts.summaryReady : texts.summaryPending,
            homework: homeworkText(l.post.homework, l.post.earliestDue, l.post.homeworkDone, now),
            canOpen: l.post.summaryReady,
            recordingId: l.post.recordingId,
          },
  };
}

export interface Problem {
  label: string;
  kind: ChipKind;
  icon: IconName | null;
}

/** Failed recordings that started in the day; an old failure stays on «Записи» only. */
export function dayProblems(recordings: readonly RecordingItem[], day: string): RecordingItem[] {
  const from = parseMoment(day);
  return recordings.filter((r) => {
    if (r.stage !== "failed") {
      return false;
    }
    const at = parseMoment(r.startedAt);
    return from === null || (at !== null && at.getTime() >= from.getTime());
  });
}

/** The chip of a failed recording: its own status text when the core has one (no sound, no speech), else a fixed one. */
export function problemOf(r: RecordingItem): Problem {
  const v = chainStatusView(r, 0);
  const own = r.errorCode === "silentRecording" || r.errorCode === "noSpeech";
  return own ? { label: v.label, kind: v.kind, icon: v.icon } : { label: texts.recordingProblem, kind: "fail", icon: "warn" };
}

export function problemFor(lessonId: string, recordings: readonly RecordingItem[], day: string): Problem | null {
  if (lessonId === "") {
    return null;
  }
  const r = dayProblems(recordings, day).find((x) => x.lessonId === lessonId);
  return r === undefined ? null : problemOf(r);
}

/** The failed recordings of the day that no lesson row of the screen carries. */
export function looseProblems(lessonIds: readonly string[], recordings: readonly RecordingItem[], day: string): Problem[] {
  return dayProblems(recordings, day)
    .filter((r) => r.lessonId === "" || !lessonIds.includes(r.lessonId))
    .map(problemOf);
}

/** Answers are applied only if no newer request was started: a slow old reply never replaces a newer one. */
export function newestOnly(): { next(): number; isNewest(token: number): boolean } {
  let last = 0;
  return {
    next: () => ++last,
    isNewest: (token) => token === last,
  };
}

/** The stages key changed between two replies; null is "no reply yet", so the first reply is not a change. */
export function stagesChanged(prev: string | null, next: string | null): boolean {
  return prev !== null && next !== null && prev !== next;
}
