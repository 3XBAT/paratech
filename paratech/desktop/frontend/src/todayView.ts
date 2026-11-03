// View logic of «Сегодня»: dates, times, the wording of rows and chips. The core decides what belongs to the day, what
// burns and what waits (ARCH-44); here it is only put into words. The voice is impersonal. Titles, subjects, teachers,
// headlines and recipients are imported or model text, passed through untouched and rendered as text nodes only.

import type { RecordingItem } from "./bindings/activity.js";
import type { TodayDeadline, TodayDone, TodayLesson, TodayPost, TodayWaiting } from "./bindings/today.js";
import { dayAndTime, dayText, daysBetween, daysLeftText, dueText, monthsShort, pad, parseMoment, timeText, weekdaysShort } from "./dueText.js";
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
  askNoChips: "Подсказок пока нет.",
  lessons: "Пары дня",
  lessonsEmpty: "На сегодня пар нет.",
  waiting: "Ждёт вашего «Да»",
  waitingEmpty: "Ничего не ждёт подтверждения.",
  waitingFoot: "Письма, календарь и выгрузка выполняются только после подтверждения.",
  burning: "Горит на 7 дней",
  burningEmpty: "Ближайших сроков на неделю нет.",
  done: "Готово за день",
  doneEmpty: "Сегодня пока ничего не готово.",
  posts: "Сообщения paratech",
  postsEmpty: "Сообщений после пар пока нет.",
  loading: "Загрузка…",
  refresh: "Обновить",
  allRecords: "Записи",
  back: "К сегодняшнему дню",
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

/** The weekday and the date of a deadline row: «пт» over «23.10». */
export function deadlineDay(due: string): { weekday: string; date: string } {
  const d = parseMoment(due);
  return d === null ? { weekday: "", date: "" } : { weekday: weekdaysShort[d.getDay()], date: shortDate(due) };
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
  weekday: string;
  date: string;
  title: string;
  subject: string;
  chip: string;
  urgent: boolean;
}

/**
 * The chip of a «Горит» row. The days come from the local `now` (a stale snapshot never says «завтра» for today); the
 * core's daysLeft is only the fallback for an unreadable due. A date alone reads as «до конца дня», so the time is
 * always there: «сегодня до 20:00», «завтра до 18:30», «через 3 дня, до 18:30». A moment that has passed reads «просрочено».
 */
export function deadlineChip(b: Pick<TodayDeadline, "due" | "daysLeft">, now: Date): string {
  const d = parseMoment(b.due);
  if (d === null) {
    return daysLeftText(b.daysLeft);
  }
  if (d.getTime() <= now.getTime()) {
    return daysLeftText(-1);
  }
  const days = daysBetween(d, now);
  const time = timeText(b.due);
  if (days === 0) {
    return `сегодня до ${time}`;
  }
  if (days === 1) {
    return `завтра до ${time}`;
  }
  return `${daysLeftText(days)}, до ${time}`;
}

export function deadlineLine(b: TodayDeadline, now: Date): DeadlineLine {
  const { weekday, date } = deadlineDay(b.due);
  return { key: b.assignmentId, weekday, date, title: titleOf(b.title), subject: b.subject.trim(), chip: deadlineChip(b, now), urgent: b.urgent };
}

/**
 * «Ближайший срок — пт, 23 окт., 20:00» from the deadlines of the week (the earliest one); "" when there is none. With
 * `now` the deadlines that have passed are skipped.
 */
export function nearestDeadlineText(burning: readonly Pick<TodayDeadline, "due">[], now?: Date): string {
  const dues = burning
    .map((b) => ({ due: b.due, at: parseMoment(b.due) }))
    .filter((b) => b.at !== null && (now === undefined || b.at.getTime() > now.getTime()));
  if (dues.length === 0) {
    return "";
  }
  const first = dues.reduce((a, b) => ((b.at as Date) < (a.at as Date) ? b : a));
  return `Ближайший срок — ${dayAndTime(first.at as Date)}`;
}

export const statusTexts = { local: "Звук обрабатывается только на этом компьютере" } as const;

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
