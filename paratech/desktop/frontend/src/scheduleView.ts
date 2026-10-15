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

/** Whether the page offers "Применить": only for a received preview the core accepts, without row errors. */
export function canApply(p: ImportPreview | null): boolean {
  return p !== null && !p.cancelled && p.canCommit && p.selectionId !== "" && p.errors.length === 0;
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

export function windowText(w: ImportWindow): string {
  return `Импортируются занятия с ${w.from} до ${w.to}; удаляются только занятия внутри этого периода.`;
}

export function resultText(r: ImportResult): string {
  return `Расписание обновлено: добавлено ${r.added}, изменено ${r.changed}, удалено ${r.removed}.`;
}

export const notes = {
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
