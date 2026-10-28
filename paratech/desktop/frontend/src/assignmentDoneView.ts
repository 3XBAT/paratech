// «Выполнено» of an assignment (ADR-0033 §9, TASK-316): the wording, the toast, the refusals with their way out and what
// the lists do with their rows after markDone or restoreDone. The core decides what happened (codes of DoneResult);
// here it is only put into words. The voice is impersonal. Assignment titles are student or model text: text nodes only.

import {
  type AssignmentItem,
  type AssignmentState,
  type DonePrior,
  type DoneResult,
  doneMessages,
  doneSummary,
} from "./bindings/homework.js";
import type { TodayDeadline } from "./bindings/today.js";
import { dayMonthText } from "./todayView.js";

/** mark: «Выполнено»; unmark: «Вернуть в работу»; undo: «Отменить» of the toast. */
export type DoneAction = "mark" | "unmark" | "undo";

export const doneTexts = {
  markTitle: "Отметить выполненным",
  unmarkTitle: "Вернуть в работу",
  /** The button of the assignment page: an action, so it does not read as the status next to «Выполнено 28 окт.». */
  pageMark: "Отметить выполненным",
  pageUnmark: "Вернуть в работу",
  saving: "Сохранение…",
  moreActions: "Ещё действия",
  /** The toast of a call without a known title, and the toast of several. */
  marked: "Выполнено",
  unmarked: "Возвращено в работу",
  markedMany: "Отмечены выполненными",
  unmarkedMany: "Возвращены в работу",
  undo: "Отменить",
  /** The tooltip of «Отменить» in the toast: the key works while the toast is shown. */
  undoTitle: "Отменить (Ctrl+Z)",
  close: "Закрыть",
  retry: "Повторить",
  openAssignment: "Открыть задание",
  back: "Все задания",
  gone: "Задание уже удалено",
  conflict: "Задание изменилось, попробуйте ещё раз",
  /** The conflict of «Отменить» on the assignment page, which already shows the assignment. */
  pageUndoConflict: "Задание изменилось, поэтому отменить нельзя. Отметку можно поставить или снять кнопкой выше.",
  busyHint: "Идёт сохранение",
} as const;

/** The longest title the toast names; a longer one is cut with «…». */
export const toastTitleMax = 40;

/** A title for the toast: one line, at most toastTitleMax characters (by code points). Plain text only. */
export function toastTitle(title: string): string {
  const flat = title.replace(/\s+/g, " ").trim();
  const chars = Array.from(flat);
  return chars.length <= toastTitleMax ? flat : `${chars.slice(0, toastTitleMax).join("").trimEnd()}…`;
}

/** The accessible name of the check in a row: the action first, then which assignment («Отметить выполненным: Задача»). */
export function checkLabel(title: string, done: boolean): string {
  return `${checkTitle(done)}: ${title}`;
}

/** The tooltip of the check: what a click does. */
export function checkTitle(done: boolean): string {
  return done ? doneTexts.unmarkTitle : doneTexts.markTitle;
}

/** The label of the button of the assignment page. */
export function pageDoneLabel(done: boolean): string {
  return done ? doneTexts.pageUnmark : doneTexts.pageMark;
}

/** «Выполнено 28 окт.» for a done assignment ("Выполнено" when the date is unknown), "" for an open one. */
export function doneStamp(a: Pick<AssignmentItem, "done" | "doneAt">): string {
  if (!a.done) {
    return "";
  }
  const day = dayMonthText(a.doneAt);
  return day === "" ? "Выполнено" : `Выполнено ${day}`;
}

/** The mark that filters the list: «Открытые / Выполненные / Все» (default «Открытые»). */
export const stateOptions: readonly { value: AssignmentState; label: string }[] = [
  { value: "open", label: "Открытые" },
  { value: "done", label: "Выполненные" },
  { value: "all", label: "Все" },
];

export const defaultState: AssignmentState = "open";

export const stateGroupLabel = "Какие задания показывать: по отметке";

/** What the list says when it has no rows. */
export type ListEmpty =
  | { kind: "first-run" }
  | { kind: "note"; text: string; hint: string; action: { label: string; state: AssignmentState } | null };

export const emptyTexts = {
  done: "Пока ничего не отмечено.",
  doneHint: "Отметить задание можно кружком слева от названия.",
  showOpen: "Показать открытые",
  open: "Открытых заданий нет",
  openAllDone: "Открытых заданий нет — всё выполнено.",
  showDone: "Показать выполненные",
} as const;

/**
 * The empty list of a state. The first-run text («Заданий пока нет») is only for a student who has no assignment at
 * all: anyExists is that fact (null: not known), and an empty «Открытые» does not tell it by itself. Open and empty
 * while assignments exist means every one of them is done.
 */
export function listEmpty(state: AssignmentState, anyExists: boolean | null): ListEmpty {
  if (state === "done") {
    return {
      kind: "note",
      text: emptyTexts.done,
      hint: emptyTexts.doneHint,
      action: { label: emptyTexts.showOpen, state: "open" },
    };
  }
  if (state === "all" || anyExists === false) {
    return { kind: "first-run" };
  }
  const text = anyExists === true ? emptyTexts.openAllDone : emptyTexts.open;
  return { kind: "note", text, hint: "", action: { label: emptyTexts.showDone, state: "done" } };
}

/** The toast of a call that changed something and can be put back; the undo is what the call returned, as it is. */
export interface ToastModel {
  text: string;
  undo: { priorDone: boolean; prior: DonePrior[] };
}

/** How the page reacts to a refusal: repeat the call, open the assignment, or nothing more than the text. */
export type IssueWay = "retry" | "open" | "none";

export interface DoneIssue {
  text: string;
  /** The same on the assignment page, which already shows the assignment (no «open it» in the text). */
  pageText: string;
  way: IssueWay;
  /** The assignment «Открыть задание» opens (the first one of the refusal); "" otherwise. */
  openId: string;
  /** An assignment is already deleted: the screen is read again and the row is gone. */
  gone: boolean;
}

/** A refusal that is on the screen: the ids it is about and the repeat of the same call. */
export interface ActiveIssue extends DoneIssue {
  ids: string[];
  retry: () => void;
}

export interface DoneOutcomeView {
  toast: ToastModel | null;
  issue: DoneIssue | null;
  /** The screen is read again: the core answered per assignment, so the stored state may differ from the shown one. */
  refresh: boolean;
}

/**
 * The toast text of a call; "" when nothing changed or the call does not get one (undo). One assignment is named
 * («Выполнено: «Реферат»»), several are counted. titles are the titles of the rows the student clicked, by id.
 */
function toastText(action: DoneAction, res: DoneResult, titles: Readonly<Record<string, string>>): string {
  const changed = res.outcomes.filter((o) => o.code === "changed");
  if (action === "undo" || changed.length === 0) {
    return "";
  }
  const mark = action === "mark";
  if (changed.length > 1) {
    return `${mark ? doneTexts.markedMany : doneTexts.unmarkedMany}: ${changed.length}`;
  }
  const base = mark ? doneTexts.marked : doneTexts.unmarked;
  const title = Object.hasOwn(titles, changed[0].id) ? toastTitle(titles[changed[0].id]) : "";
  return title === "" ? base : `${base}: «${title}»`;
}

/**
 * What the page shows after a call. A toast only when something changed and the reply says how to put it back; it is
 * never without «Отменить». A refusal says why in plain Russian and names its way out:
 * not_found / deleted - gone, the list is read again; conflict of a plain mark - repeat (a new mark has no version
 * check); conflict of an undo - open the assignment (a repeat conflicts again); failed, unavailable, invalid - repeat.
 * "unchanged" is no error: the row simply takes the stored state.
 */
export function doneOutcome(
  action: DoneAction,
  res: DoneResult,
  titles: Readonly<Record<string, string>> = {},
): DoneOutcomeView {
  const sum = doneSummary(res);
  const text = toastText(action, res, titles);
  const toast: ToastModel | null =
    text !== "" && sum.undo ? { text, undo: { priorDone: res.priorDone, prior: res.prior } } : null;

  if (res.problem !== "") {
    const problem = sum.messages[0];
    return { toast: null, issue: { text: problem, pageText: problem, way: "retry", openId: "", gone: false }, refresh: false };
  }

  const texts: string[] = [];
  const pageTexts: string[] = [];
  const add = (t: string, page = t) => {
    if (!texts.includes(t)) {
      texts.push(t);
      pageTexts.push(page);
    }
  };
  let retry = false;
  let openId = "";
  let gone = false;
  for (const o of res.outcomes) {
    if (o.code === "not_found" || o.code === "deleted") {
      gone = true;
      add(doneTexts.gone);
    } else if (o.code === "conflict") {
      if (action === "undo") {
        add(doneMessages.conflict, doneTexts.pageUndoConflict);
        openId = openId === "" ? o.id : openId;
      } else {
        add(doneTexts.conflict);
        retry = true;
      }
    } else if (o.code === "failed") {
      add(doneMessages.failed);
      retry = true;
    }
  }
  const way: IssueWay = retry ? "retry" : openId !== "" ? "open" : "none";
  const issue: DoneIssue | null =
    texts.length === 0 ? null : { text: texts.join(" "), pageText: pageTexts.join(" "), way, openId, gone };
  return { toast, issue, refresh: res.outcomes.length > 0 };
}

/** The button under a refusal; page: the assignment page itself (it cannot «open» what it shows). */
export type IssueButton = { kind: "retry" | "open" | "back"; label: string };

export function issueButton(issue: DoneIssue, where: "list" | "page"): IssueButton | null {
  if (issue.way === "retry") {
    return { kind: "retry", label: doneTexts.retry };
  }
  if (issue.way === "open" && where === "list") {
    return { kind: "open", label: doneTexts.openAssignment };
  }
  if (issue.gone && where === "page") {
    return { kind: "back", label: doneTexts.back };
  }
  return null;
}

/** The assignment after a call: the stored state of its outcome ("changed" and "unchanged"), else as it was. */
export function assignmentAfterDone(a: AssignmentItem, res: DoneResult, now: string): AssignmentItem {
  const o = res.outcomes.find((x) => x.id === a.id);
  if (o === undefined || (o.code !== "changed" && o.code !== "unchanged")) {
    return a;
  }
  // An already done one keeps its date; the exact stored time comes with the next read of the list.
  const doneAt = o.done ? (a.done && a.doneAt !== "" ? a.doneAt : now) : "";
  return { ...a, done: o.done, doneAt, version: o.version };
}

/** An assignment the core no longer has: its row goes. */
function isGone(res: DoneResult, id: string): boolean {
  return res.outcomes.some((o) => o.id === id && (o.code === "not_found" || o.code === "deleted"));
}

/**
 * The rows of the list after a call, by the state the list shows: «Открытые» lose a row that became done, «Выполненные»
 * one that was returned, «Все» keeps both (a done one muted); a deleted one goes from every list. The list is read
 * again after it, so the order (the open ones first) comes from the core.
 */
export function listAfterDone(
  items: readonly AssignmentItem[],
  state: AssignmentState,
  res: DoneResult,
  now: string,
): AssignmentItem[] {
  if (res.problem !== "") {
    return [...items];
  }
  return items
    .filter((a) => !isGone(res, a.id))
    .map((a) => assignmentAfterDone(a, res, now))
    .filter((a) => state === "all" || (state === "done") === a.done);
}

/**
 * Where the focus goes when rows leave a list (the focus was on the check of one of them): the check of the next row
 * that stays, else of the previous one, else "" (nothing left: the page's main region). null: no row of order leaves.
 * order is the ids of the checks on the screen, top to bottom, before the rows are removed.
 */
export function focusTargetAfter(order: readonly string[], leaving: readonly string[]): string | null {
  const first = order.findIndex((id) => leaving.includes(id));
  if (first < 0) {
    return null;
  }
  const stays = (id: string) => !leaving.includes(id);
  const next = order.slice(first + 1).find(stays);
  if (next !== undefined) {
    return next;
  }
  const before = order.slice(0, first).filter(stays);
  return before.length > 0 ? before[before.length - 1] : "";
}

/** The burning deadlines after a call: a done assignment and a deleted one leave at once (they are done, not late). */
export function burningAfterDone(burning: readonly TodayDeadline[], res: DoneResult): TodayDeadline[] {
  if (res.problem !== "") {
    return [...burning];
  }
  return burning.filter((b) => {
    const o = res.outcomes.find((x) => x.id === b.assignmentId);
    if (o === undefined) {
      return true;
    }
    if (o.code === "not_found" || o.code === "deleted") {
      return false;
    }
    return !((o.code === "changed" || o.code === "unchanged") && o.done);
  });
}
