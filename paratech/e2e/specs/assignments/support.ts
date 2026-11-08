// Helpers of the assignments pack (TASK-381): annotations, the deadlines the pack types and the words the app prints for them.
// Not a spec. Every wait is on UI state with an explicit timeout; page code goes through evalInPage.
import { AnnotationFeature, AnnotationMode, AnnotationRequires } from "../../src/contract.js";
import { dateInput, dueDayRu } from "../../src/fixtures/lecture.js";
import { test } from "../../src/steps/index.js";

export { act, actionMs, firstLine, openSection, textOf } from "../settings/support.js";

export const Tag = "@assignments";

/** The subject every assignment of the pack is given (the settings pack creates it from the schedule). */
export const Subject = "История";

// The rows of the pack (one report row each).
export const Feature = {
  add: "Задание: добавление со сроком (с временем и без)",
  overdue: "Задание: просроченный срок не читается как «сегодня»",
  edit: "Задание: смена срока переносит в другую группу",
  journal: "Журнал: своя правка задания не пишется (ADR-0029)",
  done: "Задание: «Выполнено» и возврат в работу",
  remove: "Задание: удаление через «⋯» и «Вернуть»",
  page: "Задание: страница открывается без ошибок консоли и сети",
  draft: "Задание: быстрый черновик, правка, новая версия, файлы",
  sendRefusal: "Задание: отправка без почты преподавателя говорит причину",
  sendDialog: "Задание: окно «Отправить задание?» и отмена",
} as const;

// Rows of the settings pack, under their real titles (a unit test checks the recording pack's list the same way).
export const NeedsSubjects = "Расписание: предметы из расписания";
export const NeedsHub = "Hub: ключ прогона принят после проверки отзыва";
/** A row that only fills the lists needs the subject «История». */
export const OnlySubjects = NeedsSubjects;
/** A row that asks the Hub for a draft needs the key of the run too. */
export const WithHub = [NeedsHub, NeedsSubjects].join(" + ");

export function details(feature: string, opts: { requires?: string } = {}) {
  return {
    tag: Tag,
    annotation: [
      { type: AnnotationFeature, description: feature },
      { type: AnnotationMode, description: "SCRIPTED" },
      ...(opts.requires ? [{ type: AnnotationRequires, description: opts.requires }] : []),
    ],
  };
}

/** A fact worth keeping in the report: an annotation of the test and a line in the run output. Never put a secret here. */
export function note(text: string): void {
  test.info().annotations.push({ type: "note", description: text });
  console.log(`[assignments] ${test.info().title}: ${text}`);
}

// ---- deadlines: the clock is the real one, the dates are relative to it ----

/** The local calendar day `n` days from `now` («ГГГГ-ММ-ДД»: the value of a native date input). */
export function dayFromNow(n: number, now = new Date()): string {
  return dateInput(new Date(now.getFullYear(), now.getMonth(), now.getDate() + n));
}

/** A deadline as the pack sees it: what is typed into the form and what the app prints for it. */
export interface Deadline {
  /** «ГГГГ-ММ-ДД». */
  date: string;
  /** «ЧЧ:ММ» as the app prints it (20:00 for a date without a time). */
  time: string;
}

/** «вт, 10 нояб.» of a deadline day, as dueText prints it (the year follows only when it is not the current one). */
export const dayRu = (d: Deadline): string => dueDayRu(`${d.date}T${d.time}:00+00:00`);

const escapeRe = (s: string): string => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/** «до вт, 10 нояб., 20:00»: a deadline that is still to come, at least two days away (no «завтра», no «сегодня»). */
export const comingText = (d: Deadline): RegExp => new RegExp(`до ${escapeRe(dayRu(d))}(?: \\d{4})?, ${escapeRe(d.time)}`);

/** «просрочено, пт, 23 окт., 20:00»: a deadline that has passed. */
export const overdueText = (d: Deadline): RegExp => new RegExp(`просрочено, ${escapeRe(dayRu(d))}(?: \\d{4})?, ${escapeRe(d.time)}`);

/** What the app prints for a deadline of today or tomorrow that is still to come: it must never appear for a passed one. */
export const todayWords = /(?:сегодня|завтра) до \d{2}:\d{2}/;
