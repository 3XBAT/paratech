// «Импортировать запись» (TASK-162): the form's checks and the texts, as pure functions. The core shows the file dialog,
// tells the format and copies the file; the page only names the lecture and shows the answer. File names are plain text.
import type { ImportLecture, ImportResult } from "./bindings/recordingImport.js";
import { errorMessages } from "./bindings/recordingImport.js";
import { formatDuration } from "./activityView.js";

export const importTexts = {
  open: "Импортировать запись",
  title: "Импорт записи лекции",
  explain: "Сначала нужно указать, какая это лекция, затем выбрать файл с записью.",
  local: "Звук не покидает компьютер, распознаётся локально.",
  formats: "Подходят аудиофайлы WAV, FLAC, MP3 и OGG (Vorbis) размером до 2 ГБ и длиной до 4 часов.",
  lectureStep: "1. Какая это лекция",
  fileStep: "2. Файл записи",
  modeLabel: "Лекция",
  modeLesson: "Пара из расписания",
  modeManual: "Дата и время",
  lessonLabel: "Прошедшая пара",
  lessonNone: "Выберите пару",
  lessonsEmpty: "Среди сегодняшних пар прошедших нет: укажите дату и время.",
  dateLabel: "Дата",
  timeLabel: "Время начала",
  subjectLabel: "Предмет",
  kindLabel: "Тип занятия",
  titleLabel: "Название (необязательно)",
  choose: "Выбрать файл и импортировать",
  importing: "Импорт…",
  close: "Закрыть",
  cancelled: "Файл не выбран, импорт отменён.",
  queued: "Расшифровка поставлена в очередь, ход виден в списке записей.",
  imported: "импортирована",
  needLesson: "Выберите пару.",
  needDate: "Укажите дату.",
  needTime: "Укажите время начала.",
  needSubject: "Укажите предмет.",
} as const;

/** The longest subject or title the core keeps (values.MaxNameRunes). */
export const maxNameLength = 200;

export type LectureMode = "lesson" | "manual";

export const lectureKinds: readonly { value: string; label: string }[] = [
  { value: "lecture", label: "Лекция" },
  { value: "seminar", label: "Семинар" },
  { value: "practice", label: "Практика" },
  { value: "lab", label: "Лабораторная" },
];

/** What the student typed. date is «ГГГГ-ММ-ДД» and time «ЧЧ:ММ» (the values of the native inputs). */
export interface ImportForm {
  mode: LectureMode;
  lessonId: string;
  date: string;
  time: string;
  subject: string;
  kind: string;
  title: string;
}

export function initialForm(subject: string, now: Date, haveLessons: boolean): ImportForm {
  return {
    mode: haveLessons ? "lesson" : "manual",
    lessonId: "",
    date: dateInput(now),
    time: "",
    subject,
    kind: "lecture",
    title: "",
  };
}

/** «ГГГГ-ММ-ДД» of the local date. */
export function dateInput(d: Date): string {
  const p = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${p(d.getFullYear(), 4)}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** RFC 3339 with the local offset of the entered date and time; null when they are not a real moment. */
export function localRfc3339(date: string, time: string): string | null {
  const d = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const t = /^(\d{2}):(\d{2})$/.exec(time);
  if (d === null || t === null) {
    return null;
  }
  const [y, mo, da, h, mi] = [d[1], d[2], d[3], t[1], t[2]].map(Number);
  const at = new Date(y, mo - 1, da, h, mi);
  // A date that rolled over (31 February) or a time out of range is not what was typed.
  if (at.getFullYear() !== y || at.getMonth() !== mo - 1 || at.getDate() !== da || at.getHours() !== h || at.getMinutes() !== mi) {
    return null;
  }
  const off = -at.getTimezoneOffset();
  const sign = off < 0 ? "-" : "+";
  const abs = Math.abs(off);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(y).padStart(4, "0")}-${p(mo)}-${p(da)}T${p(h)}:${p(mi)}:00${sign}${p(Math.floor(abs / 60))}:${p(abs % 60)}`;
}

export type LectureCheck = { ok: true; lecture: ImportLecture; at: number } | { ok: false; problem: string };

/** The lecture to send, or the first problem to show. The core checks all of it again. */
export function checkLecture(form: ImportForm, now: number): LectureCheck {
  const title = form.title.trim();
  const kind = lectureKinds.some((k) => k.value === form.kind) ? form.kind : "lecture";
  if (form.mode === "lesson") {
    return form.lessonId === ""
      ? { ok: false, problem: importTexts.needLesson }
      : { ok: true, lecture: { lessonId: form.lessonId, startedAt: "", subject: "", kind: "", title: "" }, at: now };
  }
  const subject = form.subject.trim();
  if (form.date === "") {
    return { ok: false, problem: importTexts.needDate };
  }
  if (form.time === "") {
    return { ok: false, problem: importTexts.needTime };
  }
  const startedAt = localRfc3339(form.date, form.time);
  if (startedAt === null) {
    return { ok: false, problem: errorMessages.invalidDate };
  }
  const ms = Date.parse(startedAt);
  if (ms > now) {
    return { ok: false, problem: errorMessages.future };
  }
  if (subject === "") {
    return { ok: false, problem: importTexts.needSubject };
  }
  return {
    ok: true,
    lecture: { lessonId: "", startedAt, subject: subject.slice(0, maxNameLength), kind, title: title.slice(0, maxNameLength) },
    at: ms,
  };
}

/** A lesson of today's schedule, as much as the choice needs. */
export interface LessonChoice {
  id: string;
  title: string;
  subject: string;
  start: string;
}

export interface LessonOption {
  id: string;
  label: string;
}

const timeOnly = new Intl.DateTimeFormat("ru-RU", { hour: "2-digit", minute: "2-digit" });

/**
 * The lessons that have already started, newest first; with a subject only that subject's lessons. Plain text labels
 * «09:00 · Матанализ».
 */
export function pastLessonOptions(lessons: readonly LessonChoice[], now: number, subject: string): LessonOption[] {
  const want = subject.trim().toLowerCase();
  return lessons
    .map((l) => ({ l, at: Date.parse(l.start) }))
    .filter((x) => !Number.isNaN(x.at) && x.at <= now && (want === "" || x.l.subject.trim().toLowerCase() === want))
    .sort((a, b) => b.at - a.at)
    .map(({ l, at }) => {
      const name = [l.title.trim(), l.subject.trim()].find((s) => s !== "") ?? "Занятие";
      return { id: l.id, label: `${timeOnly.format(new Date(at))} · ${name}` };
    });
}

const knownErrors: ReadonlySet<string> = new Set(Object.values(errorMessages));

/** The core's refusal in Russian; any other error gets the generic text (raw text is not shown). */
export function importErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}

export type ImportOutcome =
  | { kind: "cancelled"; text: string }
  | { kind: "existing"; text: string; recordingId: string }
  | { kind: "ok"; text: string; recordingId: string };

/** What the page says about the core's answer. The file name is plain text. */
export function importOutcome(r: ImportResult): ImportOutcome {
  if (r.cancelled) {
    return { kind: "cancelled", text: importTexts.cancelled };
  }
  const name = r.name.trim();
  if (r.existing) {
    return { kind: "existing", text: r.message || "Эта запись уже добавлена.", recordingId: r.recordingId };
  }
  const parts = [name === "" ? "Запись добавлена" : `Запись «${name}» добавлена`];
  if (r.durationSeconds > 0) {
    parts.push(`длительность ${formatDuration(r.durationSeconds)}`);
  }
  return { kind: "ok", text: `${parts.join(", ")}. ${importTexts.queued}`, recordingId: r.recordingId };
}

/** The ids of recordings imported in this session: their rows carry «импортирована». */
export function withImported(ids: ReadonlySet<string>, id: string): ReadonlySet<string> {
  return id === "" || ids.has(id) ? ids : new Set(ids).add(id);
}
