// View logic of «Сегодня»: dates, times, the wording of rows and chips. The core decides what belongs to the day, what
// burns and what waits (ARCH-44); here it is only put into words. The voice is impersonal. Titles, subjects, teachers,
// headlines and recipients are imported or model text, passed through untouched and rendered as text nodes only.

import type { RecordingItem } from "./bindings/activity.js";
import type { TodayDeadline, TodayDone, TodayLesson, TodayPost, TodayWaiting } from "./bindings/today.js";
import { plural } from "./plural.js";

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
  allRecords: "Записи и задания",
  back: "К сегодняшнему дню",
  open: "Открыть",
  summaryReady: "Конспект готов",
  summaryPending: "Конспект готовится",
  noTitle: "Без названия",
} as const;

const weekdaysShort = ["вс", "пн", "вт", "ср", "чт", "пт", "сб"] as const;
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
const monthsShort = ["янв.", "февр.", "мар.", "апр.", "мая", "июн.", "июл.", "авг.", "сент.", "окт.", "нояб.", "дек."] as const;

const pad = (n: number) => String(n).padStart(2, "0");

/** The moment of an RFC 3339 text, null for an empty or unreadable one. */
export function parseMoment(s: string): Date | null {
  if (s === "") {
    return null;
  }
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** «10:30» in the local time zone, "" when unknown. */
export function timeText(s: string): string {
  const d = parseMoment(s);
  return d === null ? "" : `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

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

/** «до сб, 24 окт.» for a due date, "" when unknown. */
export function dueText(due: string): string {
  const d = parseMoment(due);
  return d === null ? "" : `до ${weekdaysShort[d.getDay()]}, ${d.getDate()} ${monthsShort[d.getMonth()]}`;
}

/** The weekday and the date of a deadline row: «пт» over «23.10». */
export function deadlineDay(due: string): { weekday: string; date: string } {
  const d = parseMoment(due);
  return d === null ? { weekday: "", date: "" } : { weekday: weekdaysShort[d.getDay()], date: shortDate(due) };
}

/** «сегодня», «завтра», «через 2 дня», «через 5 дней»; a past date reads «просрочено». */
export function daysLeftText(n: number): string {
  if (n < 0) {
    return "просрочено";
  }
  if (n === 0) {
    return "сегодня";
  }
  if (n === 1) {
    return "завтра";
  }
  return `через ${n} ${plural(n, "день", "дня", "дней")}`;
}

const kinds: Readonly<Record<string, string>> = {
  lecture: "Лекция",
  seminar: "Семинар",
  practice: "Практика",
  lab: "Лабораторная",
  other: "Занятие",
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

/** «1 задание · до сб, 24 окт.»; "" when no homework was found. */
export function homeworkText(count: number, earliestDue: string): string {
  if (count <= 0) {
    return "";
  }
  const n = `${count} ${plural(count, "задание", "задания", "заданий")}`;
  const due = dueText(earliestDue);
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
    return { kind: "run", text: `Распознавание · ${pct} %` };
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
  /** Subject, due date or recipient: the parts that are known. */
  meta: string;
  icon: "cal" | "mail" | "tasks";
}

const waitingKinds: Readonly<Record<string, { label: string; icon: WaitingLine["icon"] }>> = {
  create_deadline_event: { label: "Срок в календарь", icon: "cal" },
  send_homework_email: { label: "Письмо преподавателю", icon: "mail" },
  export_homework: { label: "Выгрузка задания", icon: "tasks" },
};

export function waitingLine(w: TodayWaiting): WaitingLine {
  const k = waitingKinds[w.kind] ?? { label: "Действие", icon: "tasks" as const };
  const recipient = w.recipient === "" ? "" : `→ ${w.recipient}`;
  return {
    key: w.proposalId,
    label: k.label,
    title: titleOf(w.title),
    meta: [w.subject, dueText(w.due), recipient].filter((p) => p !== "").join(" · "),
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

export function deadlineLine(b: TodayDeadline): DeadlineLine {
  const { weekday, date } = deadlineDay(b.due);
  return { key: b.assignmentId, weekday, date, title: titleOf(b.title), subject: b.subject, chip: daysLeftText(b.daysLeft), urgent: b.urgent };
}

export interface DoneLine {
  key: string;
  title: string;
  meta: string;
  canOpen: boolean;
  recordingId: string;
}

/** «Конспект · Матанализ» over «Лекция · 21.10 · 11:52 · нашлось 1 задание». */
export function doneLine(d: TodayDone): DoneLine {
  const name = d.lesson.subjectName || d.lesson.subject || d.lesson.title;
  const found = d.homework > 0 ? `нашлось ${d.homework} ${plural(d.homework, "задание", "задания", "заданий")}` : "";
  const meta = [kindLabel(d.lesson.kind), shortDate(d.lesson.start), timeText(d.at), found].filter((p) => p !== "").join(" · ");
  return { key: d.recordingId, title: `Конспект · ${titleOf(name)}`, meta, canOpen: d.summaryReady, recordingId: d.recordingId };
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
export function postLine(p: TodayPost): PostLine {
  const meta = [titleOf(p.lesson.title), timeText(p.shownAt)].filter((x) => x !== "").join(" · ");
  return {
    key: p.recordingId,
    headline: p.headline,
    meta,
    homework: homeworkText(p.homework, p.earliestDue),
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
  /** The chip of the lesson's post; null while it has none. */
  post: { text: string; homework: string; canOpen: boolean; recordingId: string } | null;
}

export function lessonLine(l: TodayLesson, recordings: readonly RecordingItem[]): LessonLine {
  const { start, end } = lessonTimes(l);
  return {
    key: l.id,
    start,
    end,
    title: titleOf(l.title),
    meta: lessonMeta(l),
    live: liveStatus(l.id, recordings),
    post:
      l.post === null
        ? null
        : {
            text: l.post.summaryReady ? texts.summaryReady : texts.summaryPending,
            homework: homeworkText(l.post.homework, l.post.earliestDue),
            canOpen: l.post.summaryReady,
            recordingId: l.post.recordingId,
          },
  };
}
