// Display mapping for the schedule import page: texts and formats only. Whether a preview can be applied is decided by
// the Go core (canCommit); the page only refuses to offer Apply when the reply contradicts it (ARCH-44).
// Text from the file is untrusted: the page renders it as plain text, never as HTML, each piece isolated (Part).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import {
  errorMessages,
  type ImportChange,
  type ImportConflict,
  type ImportPreview,
  type ImportResult,
  type ImportRowError,
  type ImportWindow,
  type LessonText,
} from "./bindings/schedule.js";
import { plural } from "./plural.js";

/** Whether the page offers "Применить": only for a received preview the core accepts, without row errors. */
export function canApply(p: ImportPreview | null): boolean {
  return p !== null && !p.cancelled && p.canCommit && p.selectionId !== "" && p.errors.length === 0;
}

/** Whether the page shows the empty state: nothing chosen yet, no error and no result to report. */
export function showEmptyState(p: ImportPreview | null, error: string, done: string): boolean {
  return p === null && error === "" && done === "";
}

/** Whether the preview changes anything. */
export function hasChanges(p: ImportPreview): boolean {
  return p.added.length + p.changed.length + p.removed.length > 0;
}

/**
 * A piece of display text. `file` marks text from the file (titles, zones, row error fields and reasons): the page
 * renders it in its own <bdi>, so a bidi control character in it (U+202E) cannot reorder the rest of the line.
 */
export interface Part {
  text: string;
  file: boolean;
}

const fixed = (text: string): Part => ({ text, file: false });
const fromFile = (text: string): Part => ({ text, file: true });

/** The text of parts as one string. */
export function plain(parts: readonly Part[]): string {
  return parts.map((p) => p.text).join("");
}

/** "строка 3, колонка start: причина"; a problem of the whole file has no line. */
export function rowErrorParts(e: ImportRowError): Part[] {
  const parts = [fixed(e.line > 0 ? `строка ${e.line}` : "файл")];
  if (e.field !== "") {
    parts.push(fixed(", колонка "), fromFile(e.field));
  }
  parts.push(fixed(": "), fromFile(e.reason));
  return parts;
}

/** "14.10.2026 09:00–10:30"; the end keeps its date when it is another day. */
export function timeRange(start: string, end: string): string {
  const [startDate] = start.split(" ");
  const [endDate, endTime] = end.split(" ");
  if (endTime !== undefined && endDate === startDate) {
    return `${start}–${endTime}`;
  }
  return `${start} – ${end}`;
}

/** "Матанализ, 14.10.2026 09:00–10:30 (Europe/Moscow)". */
export function lessonParts(l: LessonText): Part[] {
  return [fromFile(l.title), fixed(`, ${timeRange(l.start, l.end)} (`), fromFile(l.zone), fixed(")")];
}

/** "строка 2: Матанализ, ..."; a removed lesson (line 0) has no line. */
export function lineParts(line: number, parts: readonly Part[]): Part[] {
  return line > 0 ? [fixed(`строка ${line}: `), ...parts] : [...parts];
}

/** "строка 3: <before> → <after>". */
export function changeParts(c: ImportChange): Part[] {
  return lineParts(c.line, [...lessonParts(c.before), fixed(" → "), ...lessonParts(c.after)]);
}

export function conflictParts(c: ImportConflict): Part[] {
  const other = c.withLine > 0 ? `со строкой ${c.withLine}: ` : "с занятием в расписании: ";
  return [
    fixed(`строка ${c.line}: `),
    ...lessonParts(c.lesson),
    fixed(` — пересекается ${other}`),
    ...lessonParts(c.with),
  ];
}

/** "Добавится: 2, изменится: 1, удалится: 0, без изменений: 5." */
export function summaryText(p: ImportPreview): string {
  return (
    `Добавится: ${p.added.length}, изменится: ${p.changed.length}, удалится: ${p.removed.length}, ` +
    `без изменений: ${p.unchangedCount}.`
  );
}

export interface SummaryChip {
  kind: "added" | "changed" | "removed" | "unchanged" | "errors";
  /** "Добавится: 2"; label and count are the same split in two. */
  text: string;
  label: string;
  count: number;
}

/** The counters above the preview. */
export function summaryChips(p: ImportPreview): SummaryChip[] {
  const chip = (kind: SummaryChip["kind"], label: string, count: number) => ({
    kind,
    text: `${label}: ${count}`,
    label,
    count,
  });
  return [
    chip("added", "Добавится", p.added.length),
    chip("changed", "Изменится", p.changed.length),
    chip("removed", "Удалится", p.removed.length),
    chip("unchanged", "Без изменений", p.unchangedCount),
    chip("errors", "Ошибки", p.errors.length),
  ];
}

/** A collapsible group of the preview; the page keeps the collapse state by this key, not by preview. */
export type PreviewGroup = "errors" | "added" | "changed" | "removed" | "conflicts";

export const groupTitles: Readonly<Record<PreviewGroup, string>> = {
  errors: "Ошибки в строках",
  added: "Добавятся",
  changed: "Изменятся",
  removed: "Удалятся",
  conflicts: "Пересечения",
};

/** The groups the preview shows: only the row errors when there are any, otherwise the non-empty changes. */
export function previewGroups(p: ImportPreview): PreviewGroup[] {
  if (p.errors.length > 0) {
    return ["errors"];
  }
  const sizes: [PreviewGroup, number][] = [
    ["added", p.added.length],
    ["changed", p.changed.length],
    ["removed", p.removed.length],
    ["conflicts", p.conflicts.length],
  ];
  return sizes.filter(([, n]) => n > 0).map(([g]) => g);
}

/** "Применить 153 изменения": the button names what it does; plain "Применить" when nothing changes. */
export function applyLabel(p: ImportPreview): string {
  const n = p.added.length + p.changed.length + p.removed.length;
  return n === 0 ? "Применить" : `Применить ${n} ${plural(n, "изменение", "изменения", "изменений")}`;
}

/** A divider above the first lesson of a Monday–Sunday week: "12.10–18.10" and the lessons in a row in it. */
export interface WeekHeader {
  label: string;
  count: number;
  countText: string;
}

const pad2 = (n: number) => String(n).padStart(2, "0");

// The week of a "ДД.ММ.ГГГГ ЧЧ:ММ" start, keyed by its Monday; null when the date does not parse.
function weekOf(start: string): { key: string; label: string } | null {
  const m = /^(\d{2})\.(\d{2})\.(\d{4})/.exec(start);
  if (m === null) {
    return null;
  }
  const [day, month, year] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const d = new Date(Date.UTC(year, month - 1, day));
  if (d.getUTCDate() !== day || d.getUTCMonth() !== month - 1) {
    return null;
  }
  const monday = new Date(d.getTime() - ((d.getUTCDay() + 6) % 7) * 86_400_000);
  const sunday = new Date(monday.getTime() + 6 * 86_400_000);
  const dm = (x: Date) => `${pad2(x.getUTCDate())}.${pad2(x.getUTCMonth() + 1)}`;
  return { key: monday.toISOString().slice(0, 10), label: `${dm(monday)}–${dm(sunday)}` };
}

/**
 * For each lesson, the divider to show above it: at the start of every run of lessons in one week, null elsewhere.
 * The list keeps the core's order; an unparsable date gets no divider.
 */
export function weekHeaders(lessons: readonly { start: string }[]): (WeekHeader | null)[] {
  const weeks = lessons.map((l) => weekOf(l.start));
  const out: (WeekHeader | null)[] = weeks.map(() => null);
  for (let i = 0; i < weeks.length; i++) {
    const w = weeks[i];
    if (w === null || (i > 0 && weeks[i - 1]?.key === w.key)) {
      continue;
    }
    let n = 1;
    while (i + n < weeks.length && weeks[i + n]?.key === w.key) {
      n++;
    }
    out[i] = { label: w.label, count: n, countText: `${n} ${plural(n, "занятие", "занятия", "занятий")}` };
  }
  return out;
}

// A wall-clock time: a lesson start ("ДД.ММ.ГГГГ ЧЧ:ММ" in its zone) or "now" in that zone.
interface WallTime {
  y: number;
  mo: number;
  d: number;
  h: number;
  mi: number;
}

function parseWall(s: string): WallTime | null {
  const m = /^(\d{2})\.(\d{2})\.(\d{4}) (\d{2}):(\d{2})$/.exec(s);
  if (m === null) {
    return null;
  }
  const [d, mo, y, h, mi] = [Number(m[1]), Number(m[2]), Number(m[3]), Number(m[4]), Number(m[5])];
  const date = new Date(Date.UTC(y, mo - 1, d));
  if (date.getUTCDate() !== d || date.getUTCMonth() !== mo - 1 || h > 23 || mi > 59) {
    return null;
  }
  return { y, mo, d, h, mi };
}

const dayKey = (t: WallTime) => t.y * 10_000 + t.mo * 100 + t.d;
const minuteKey = (t: WallTime) => dayKey(t) * 10_000 + t.h * 100 + t.mi;
// Minutes since the epoch as if the wall time were UTC; with a zone offset it gives the instant.
const wallMinutes = (t: WallTime) => Date.UTC(t.y, t.mo - 1, t.d, t.h, t.mi) / 60_000;

// "now" as a wall time in each zone, one formatter per zone. The zone is file text: like the core (zoneOf in
// input/wails/prompt.go), an empty or "Local" zone means UTC; another invalid one makes Intl throw a RangeError and gives
// null, so such a lesson is neither muted nor highlighted.
function zoneClock(now: Date): (zone: string) => WallTime | null {
  const cache = new Map<string, WallTime | null>();
  return (zone) => {
    if (!cache.has(zone)) {
      cache.set(zone, wallIn(now, zone));
    }
    return cache.get(zone) ?? null;
  };
}

function wallIn(now: Date, zone: string): WallTime | null {
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: zone === "" || zone === "Local" ? "UTC" : zone,
      year: "numeric",
      month: "numeric",
      day: "numeric",
      hour: "numeric",
      minute: "numeric",
      hourCycle: "h23",
    }).formatToParts(now);
    const get = (type: string) => Number(parts.find((p) => p.type === type)?.value);
    const t = { y: get("year"), mo: get("month"), d: get("day"), h: get("hour") % 24, mi: get("minute") };
    return Object.values(t).every(Number.isFinite) ? t : null;
  } catch {
    return null;
  }
}

const weekdays = ["вс", "пн", "вт", "ср", "чт", "пт", "сб"];
const months = [
  "января", "февраля", "марта", "апреля", "мая", "июня",
  "июля", "августа", "сентября", "октября", "ноября", "декабря",
];

function labelOf(start: string, today: WallTime | null, now: Date): string | null {
  const s = parseWall(start);
  if (s === null) {
    return null;
  }
  if (today !== null) {
    const tomorrow = new Date(Date.UTC(today.y, today.mo - 1, today.d + 1));
    if (dayKey(s) === dayKey(today)) {
      return "Сегодня";
    }
    if (s.y === tomorrow.getUTCFullYear() && s.mo === tomorrow.getUTCMonth() + 1 && s.d === tomorrow.getUTCDate()) {
      return "Завтра";
    }
  }
  const weekday = weekdays[new Date(Date.UTC(s.y, s.mo - 1, s.d)).getUTCDay()];
  const year = today?.y ?? now.getFullYear();
  return `${weekday}, ${s.d} ${months[s.mo - 1]}${s.y !== year ? ` ${s.y}` : ""}`;
}

/**
 * The day of a lesson: «Сегодня», «Завтра» or "пт, 16 октября" (the year only when it is not the current one); null for
 * an unparsable start. Today is taken in the lesson's zone ("" and "Local" mean UTC); with another invalid zone the label
 * is always the plain date.
 */
export function dayLabel(start: string, zone: string, now: Date): string | null {
  return labelOf(start, wallIn(now, zone), now);
}

/**
 * For each lesson, the day header to show above it: at the start of every run of lessons on one day, null elsewhere.
 * Like weekHeaders, an unparsable start gets no header and breaks the run; in a list kept in file order a day header can
 * repeat, as week dividers do.
 */
export function dayHeaders(lessons: readonly { start: string; zone: string }[], now: Date): (string | null)[] {
  const clock = zoneClock(now);
  const labels = lessons.map((l) => labelOf(l.start, clock(l.zone), now));
  const keys = lessons.map((l, i) => (labels[i] === null ? null : `${l.start.slice(0, 10)}|${labels[i]}`));
  return labels.map((label, i) => (label !== null && (i === 0 || keys[i - 1] !== keys[i]) ? label : null));
}

/** How a lesson row looks: past ones muted, today's and the next one highlighted. */
export interface LessonMark {
  past: boolean;
  today: boolean;
  /** The lesson with the earliest start among those not ended yet (it may be going on now). */
  next: boolean;
}

const noMark: LessonMark = { past: false, today: false, next: false };

/**
 * Marks for lessons in list order. The list is not sorted (added lessons keep the file order), so `next` goes to the
 * earliest absolute start, the first in the list on a tie. Each lesson is compared with now in its own zone; an
 * unparsable start or an invalid zone gives no mark. Without `highlight` (lessons about to be removed) only `past` is set.
 */
export function lessonMarks(
  lessons: readonly { start: string; end: string; zone: string }[],
  now: Date,
  highlight = true,
): LessonMark[] {
  const clock = zoneClock(now);
  const nowMinutes = Math.floor(now.getTime() / 60_000);
  let next = -1;
  let nextAt = Infinity;
  const marks = lessons.map((l, i) => {
    const s = parseWall(l.start);
    const n = clock(l.zone);
    if (s === null || n === null) {
      return noMark;
    }
    const e = parseWall(l.end) ?? s;
    const past = minuteKey(e) <= minuteKey(n);
    if (!highlight) {
      return { ...noMark, past };
    }
    // The zone offset at now: wall minutes of now in the zone minus the real minutes.
    const at = wallMinutes(s) - (wallMinutes(n) - nowMinutes);
    if (!past && at < nextAt) {
      next = i;
      nextAt = at;
    }
    return { past, today: !past && (dayKey(s) === dayKey(n) || minuteKey(s) <= minuteKey(n)), next: false };
  });
  if (next >= 0) {
    marks[next] = { ...marks[next], next: true };
  }
  return marks;
}

/** The time of a lesson under its day header: "09:00–10:30"; a full end on another day; the full range when unparsable. */
export function lessonTime(start: string, end: string): string {
  if (parseWall(start) === null) {
    return timeRange(start, end);
  }
  const [startDate, startTime] = start.split(" ");
  const [endDate, endTime] = end.split(" ");
  if (endTime !== undefined && endDate === startDate) {
    return `${startTime}–${endTime}`;
  }
  return `${startTime} – ${end}`;
}

export function windowText(w: ImportWindow): string {
  return `Импортируются занятия с ${w.from} до ${w.to}; удаляются только занятия внутри этого периода.`;
}

export function resultText(r: ImportResult): string {
  return `Расписание обновлено: добавлено ${r.added}, изменено ${r.changed}, удалено ${r.removed}.`;
}

export const notes = {
  choose: "Нажмите «Выбрать файл…» вверху страницы.",
  formats: "Поддерживаются файлы ICS, CSV и XLSX до 2 МиБ. Сначала показываются изменения; расписание меняется только после «Применить».",
  allDay: "События на весь день в ICS пропускаются: они не импортируются.",
  uid:
    "CSV и XLSX: первая строка — заголовок с колонками title, date (ГГГГ-ММ-ДД), start и end (ЧЧ:ММ), timezone " +
    "(например Europe/Moscow) и uid. Без uid изменение названия, даты или начала занятия выглядит как «удалено + добавлено».",
  noNotifications: "Уведомления об импорте не показываются.",
  rowErrors: "Ничего не будет записано: исправьте ошибки в файле и выберите его снова.",
  noChanges: "Изменений нет: расписание уже совпадает с файлом.",
  conflicts: "Пересечения по времени — это предупреждения, они не мешают применить импорт.",
  conflictsTruncated: "Показаны не все пересечения.",
} as const;

/** The CSV template (docs/refs/schedule-csv.md); XLSX uses the same columns. */
export const csvTemplate = [
  "title,date,start,end,timezone,uid",
  "Математика,2026-10-14,09:00,10:30,Europe/Moscow,math-1",
].join("\n");

const errorTexts: Readonly<Record<string, string>> = {
  [errorMessages.busy]: "Окно выбора файла уже открыто.",
  [errorMessages.unsupportedFile]: "Можно импортировать только файлы .ics, .csv и .xlsx.",
  [errorMessages.fileTooLarge]: "Файл слишком большой: не более 2 МиБ.",
  [errorMessages.fileUnreadable]: "Не удалось прочитать файл.",
  [errorMessages.notSchedule]:
    "Файл не удалось прочитать как расписание: проверьте формат, кодировку UTF-8 и строку заголовка.",
  [errorMessages.noPreview]: "Нечего применять. Выберите файл снова.",
  [errorMessages.stale]: "Расписание или файл изменились. Выберите файл снова.",
  [errorMessages.invalid]: "В файле есть ошибки, ничего не записано. Выберите файл снова.",
  [errorMessages.failed]: "Не удалось импортировать расписание.",
};

const genericError = errorTexts[errorMessages.failed];

/** Russian text for an error of the core; an unknown error gets a generic text (raw text is not shown). */
export function errorText(err: unknown): string {
  const msg = errorMessage(err);
  return Object.hasOwn(errorTexts, msg) ? errorTexts[msg] : genericError;
}

/** Text for a failed commit: the core has dropped the preview, so the user always chooses the file again. */
export function commitErrorText(err: unknown): string {
  const text = errorText(err);
  return text.includes("Выберите файл снова") ? text : `${text} Выберите файл снова.`;
}

// Wails rejects a call with an Error-like object carrying the Go error text.
function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}
