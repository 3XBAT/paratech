// «Удалить предмет» and «Вернуть» (TASK-289, ADR-0027). Pure, no React: the warning with its counts, the dialog and the
// restore line as models, the texts of every refusal. Names of subjects are typed or imported text: they are passed
// through untouched and the page renders them as text nodes.

import type { DiscardProblem, LibraryApi, LibraryMaterial, LibrarySubject, RestoreProblem, SubjectDiscard, SubjectRestore } from "./bindings/library.js";
import type { NoteView, NotesApi } from "./bindings/notes.js";
import { plural } from "./plural.js";

export const removalTexts = {
  /** The quiet button on the page of a subject. */
  action: "Удалить предмет",
  confirm: "Удалить",
  cancel: "Отмена",
  close: "Закрыть",
  retry: "Повторить",
  refresh: "Обновить",
  restore: "Вернуть",
  deleting: "Удаление…",
  restoring: "Возврат…",
  counting: "Подсчёт моих конспектов и файлов…",
  window: "Вернуть можно в течение 3 дней.",
  /** What stays: the recordings of pairs and the assignments are not part of the subject's own things. */
  stays: "Записи пар и задания останутся.",
  unknownParts: "Вместе с ним удалятся мои конспекты и файлы.",
  changed: "Предмет изменился, пока был открыт этот вопрос.",
  gone: "Предмет уже удалён",
  failed: "Не удалось удалить предмет",
  /** The name a removed subject holds when the student creates a subject with it. */
  nameRemoved: "Предмет с таким названием удалён",
  restored: "Предмет возвращён",
  expired: "Предмет удалён окончательно, вернуть уже нельзя",
  restoreChanged: "Предмет изменился, вернуть его не удалось.",
  /** Where there is nothing to repeat (the line after «Удалить»): typing the name again leads back to «Вернуть». */
  restoreChangedRetype: "Предмет изменился. Название можно ввести ещё раз.",
  restoreGone: "Предмет не найден: возможно, он уже удалён окончательно.",
  retypeHint: "Название можно ввести ещё раз.",
  restoreFailed: "Не удалось вернуть предмет",
} as const;

/** The attribute that tells the row of a subject in the tree (the focus goes there after «Вернуть»). */
export const subjectRowAttr = "data-subject-id";

/** «Вернуть предмет «Физика»»: the accessible name of «Вернуть» when the subject's name is known; undefined otherwise. */
export function restoreAriaLabel(name: string): string | undefined {
  return name === "" ? undefined : `Вернуть предмет «${name}»`;
}

// ---- The warning ----

/** The student's own notes are «Мой конспект» in the glossary («Конспект» is what paratech makes from a recording). */
export function notesText(n: number): string {
  return `${n} ${plural(n, "мой конспект", "моих конспекта", "моих конспектов")}`;
}

export function filesText(n: number): string {
  return `${n} ${plural(n, "файл", "файла", "файлов")}`;
}

/** What goes with the subject. */
export interface Counts {
  notes: number;
  files: number;
}

/** «2 конспекта и 1 файл»; a part that is 0 is left out, "" for nothing at all. */
export function countsPhrase(c: Counts): string {
  const parts: string[] = [];
  if (c.notes > 0) {
    parts.push(notesText(c.notes));
  }
  if (c.files > 0) {
    parts.push(filesText(c.files));
  }
  return parts.join(" и ");
}

/** «удалится 1 конспект» (one part in the singular), «удалятся 2 конспекта и 1 файл» otherwise. */
function removeVerb(c: Counts): string {
  const single = (c.notes > 0) !== (c.files > 0);
  const n = c.notes > 0 ? c.notes : c.files;
  return single && n % 10 === 1 && n % 100 !== 11 ? "удалится" : "удалятся";
}

export function removeQuestion(name: string): string {
  return `Удалить предмет «${name}»?`;
}

/** What stays and for how long it can be brought back: said in every variant of the warning. */
const staysAndWindow = `${removalTexts.stays} ${removalTexts.window}`;

/**
 * The sentences under the question. `counts` null: the page does not know them (the notes or the files could not be
 * read), so the warning names no numbers but still says that both go.
 */
export function removeBody(counts: Counts | null): string {
  if (counts === null) {
    return `${removalTexts.unknownParts} ${staysAndWindow}`;
  }
  const phrase = countsPhrase(counts);
  return phrase === "" ? staysAndWindow : `Вместе с ним ${removeVerb(counts)} ${phrase}. ${staysAndWindow}`;
}

/** The whole warning as one text: «Удалить предмет «Физика»? Вместе с ним удалятся 2 моих конспекта и 1 файл. Записи …». */
export function removeWarning(name: string, counts: Counts | null): string {
  return `${removeQuestion(name)} ${removeBody(counts)}`;
}

/**
 * The counts the warning shows, from what the page already has: the active notes of the subject and its files that are no
 * part of such a note (the core counts the same way and answers with the real numbers after the call). null when either
 * list is unknown.
 */
export function knownCounts(notes: readonly NoteView[] | null, materials: readonly LibraryMaterial[] | null): Counts | null {
  if (notes === null || materials === null) {
    return null;
  }
  const active = notes.filter((n) => !n.removed);
  const inNotes = new Set(active.flatMap((n) => n.parts.map((p) => p.materialId)));
  return { notes: active.length, files: materials.filter((m) => !m.removed && !inNotes.has(m.id)).length };
}

/** The counts of the dialog: "loading" while the notes or the page's files are still being read. */
export function askingCounts(
  notes: readonly NoteView[] | null | "loading",
  materials: readonly LibraryMaterial[] | null | "loading",
): Counts | null | "loading" {
  return notes === "loading" || materials === "loading" ? "loading" : knownCounts(notes, materials);
}

// ---- The dialog ----

export type DiscardWay = "delete" | "refresh" | "close";

export interface DiscardProblemView {
  text: string;
  /** The button that is the way out. */
  way: DiscardWay;
  wayLabel: string;
  /** The second button («Закрыть»); false: the way is the only button. */
  canDismiss: boolean;
}

/** Why the delete did not happen, in plain words, and the button that leads on. */
export function discardProblemView(problem: Exclude<DiscardProblem, "">): DiscardProblemView {
  switch (problem) {
    case "changed":
      return { text: removalTexts.changed, way: "refresh", wayLabel: removalTexts.refresh, canDismiss: true };
    case "not_found":
      return { text: removalTexts.gone, way: "close", wayLabel: removalTexts.close, canDismiss: false };
    case "failed":
      return { text: removalTexts.failed, way: "delete", wayLabel: removalTexts.retry, canDismiss: true };
  }
}

export interface DialogInput {
  name: string;
  /** "loading" while the notes of the subject are being read. */
  counts: Counts | null | "loading";
  busy: boolean;
  problem: Exclude<DiscardProblem, ""> | "";
}

export interface DialogButton {
  label: string;
  disabled: boolean;
}

export interface DialogView {
  title: string;
  body: string;
  /** A small inline status (counting); "" for none. */
  status: string;
  /** The refusal; "" for none. */
  problem: string;
  /** The call runs: both buttons are off and Esc does nothing. */
  busy: boolean;
  /** The right button: «Удалить» (the only red one), or the way out of a refusal. */
  confirm: DialogButton & { action: DiscardWay; danger: boolean };
  /** The left button, which takes the focus; null when the way out is the only button. */
  cancel: DialogButton | null;
}

/** The dialog for any state: asking, counting, deleting, refused. */
export function dialogView(i: DialogInput): DialogView {
  const title = removeQuestion(i.name);
  if (i.problem !== "") {
    const p = discardProblemView(i.problem);
    return {
      title,
      body: removeBody(i.counts === "loading" ? null : i.counts),
      status: "",
      problem: p.text,
      busy: i.busy,
      confirm: { label: i.busy ? removalTexts.deleting : p.wayLabel, disabled: i.busy, action: p.way, danger: false },
      cancel: p.canDismiss ? { label: removalTexts.close, disabled: i.busy } : null,
    };
  }
  const loading = i.counts === "loading";
  return {
    title,
    body: i.counts === "loading" ? staysAndWindow : removeBody(i.counts),
    // «Удаление…» is on the button only; the status line is for the count.
    status: loading ? removalTexts.counting : "",
    problem: "",
    busy: i.busy,
    confirm: { label: i.busy ? removalTexts.deleting : removalTexts.confirm, disabled: i.busy || loading, action: "delete", danger: true },
    cancel: { label: removalTexts.cancel, disabled: i.busy },
  };
}

export type DiscardOutcome = { kind: "done"; discard: SubjectDiscard } | { kind: "problem"; problem: Exclude<DiscardProblem, ""> };

/** Runs «Удалить»; never throws. A reply with no problem but no usable version is a failure (nothing to restore with). */
export async function runDiscard(api: Pick<LibraryApi, "discardSubject">, subjectId: string, version: number): Promise<DiscardOutcome> {
  try {
    const d = await api.discardSubject(subjectId, version);
    return d.problem === "" ? { kind: "done", discard: d } : { kind: "problem", problem: d.problem };
  } catch {
    return { kind: "problem", problem: "failed" };
  }
}

// ---- The question as state (the transitions of the page's hook, kept pure) ----

/** What the dialog waits for: "reading" the subject and its notes again, "deleting" the call. */
export type AskBusy = "" | "reading" | "deleting";

/** The question that is open: the subject as the core last reported it (name and version), the notes, what runs. */
export interface Asking {
  id: string;
  name: string;
  /** Sent with «Удалить»; replaced by the fresh one when the dialog opens and at «Обновить». */
  version: number;
  notes: readonly NoteView[] | null | "loading";
  busy: AskBusy;
  problem: Exclude<DiscardProblem, ""> | "";
}

/** The question opens over what the page shows (maybe out of date); the fresh read follows at once. */
export function openAsking(subject: Pick<LibrarySubject, "id" | "name" | "version">): Asking {
  return { id: subject.id, name: subject.name, version: subject.version, notes: "loading", busy: "reading", problem: "" };
}

/** The subject as the list says it now. "unknown": the list could not be read, the page's data stays. */
export type SubjectRead = { kind: "found"; name: string; version: number } | { kind: "gone" } | { kind: "unknown" };

export interface Fresh {
  subject: SubjectRead;
  /** null: the notes could not be read (the warning names no numbers). */
  notes: readonly NoteView[] | null;
}

/** Reads the subject and its notes together; never rejects. */
export async function readFresh(
  library: Pick<LibraryApi, "subjects">,
  notes: Pick<NotesApi, "forSubject">,
  id: string,
): Promise<Fresh> {
  const subject = library.subjects().then(
    (list): SubjectRead => {
      const s = list.find((x) => x.id === id);
      return s === undefined ? { kind: "gone" } : { kind: "found", name: s.name, version: s.version };
    },
    (): SubjectRead => ({ kind: "unknown" }),
  );
  const own = notes.forSubject(id).then(
    (n): readonly NoteView[] | null => n,
    () => null,
  );
  return { subject: await subject, notes: await own };
}

/** The fresh read arrives: its version and name replace the shown ones; a subject that is gone is the «уже удалён» refusal. */
export function applyFresh<T extends Asking>(a: T, f: Fresh): T {
  const read = { ...a, notes: f.notes, busy: "" as const };
  switch (f.subject.kind) {
    case "found":
      return { ...read, name: f.subject.name, version: f.subject.version, problem: "" };
    case "gone":
      return { ...read, problem: "not_found" };
    case "unknown":
      return { ...read, problem: "" };
  }
}

/** «Удалить» / «Повторить»: the state while the call runs; null when it may not start (counting, running, other way out). */
export function startDiscard<T extends Asking>(a: T): T | null {
  if (a.busy !== "" || a.notes === "loading" || a.problem === "changed" || a.problem === "not_found") {
    return null;
  }
  return { ...a, busy: "deleting", problem: "" };
}

/** «Обновить»: the subject and notes are read again in place; null while something runs. */
export function startRefresh<T extends Asking>(a: T): T | null {
  return a.busy !== "" ? null : { ...a, notes: "loading", busy: "reading", problem: "" };
}

/** A subject the student removed on this visit: its «Вернуть» line stays until closed or the page is left. */
export interface RemovedSubject {
  id: string;
  name: string;
  /** The version of the removed subject, for restoreSubject (from the reply of the delete). */
  version: number;
}

/** The call ended: the question is gone and the line with «Вернуть» is born, or the refusal stays in the dialog. */
export function settleDiscard<T extends Asking>(a: T, out: DiscardOutcome): { asking: T | null; line: RemovedSubject | null } {
  if (out.kind === "done") {
    return { asking: null, line: { id: a.id, name: a.name, version: out.discard.version } };
  }
  return { asking: { ...a, busy: "", problem: out.problem }, line: null };
}

/** The lines of removed subjects: a subject removed again replaces its older line. */
export function withRemovedLine(list: readonly RemovedSubject[], line: RemovedSubject): RemovedSubject[] {
  return [...list.filter((r) => r.id !== line.id), line];
}

export function withoutRemovedLine(list: readonly RemovedSubject[], id: string): RemovedSubject[] {
  return list.filter((r) => r.id !== id);
}

/** The dialog for the open question. `materials`: the page's files of the subject ("loading" while they are read, null if they failed). */
export function askingDialog(a: Asking, materials: readonly LibraryMaterial[] | null | "loading"): DialogView {
  return dialogView({
    name: a.name,
    counts: askingCounts(a.notes, materials),
    busy: a.busy === "deleting",
    problem: a.problem,
  });
}

// ---- «Вернуть» ----

export type RestoreState =
  | { phase: "idle" }
  | { phase: "busy" }
  | { phase: "done" }
  | { phase: "problem"; problem: Exclude<RestoreProblem, ""> };

export const idleRestore: RestoreState = { phase: "idle" };
export const busyRestore: RestoreState = { phase: "busy" };

/** The state after the call: restored, or why not. */
export function restoreState(r: SubjectRestore): RestoreState {
  return r.problem === "" ? { phase: "done" } : { phase: "problem", problem: r.problem };
}

/** The call of a restore: the state it ended in and the subject that is back (null unless it is `done`). */
export interface RestoreCall {
  state: RestoreState;
  subject: SubjectRestore["subject"];
}

/** Runs «Вернуть»; never throws. */
export async function runRestore(api: Pick<LibraryApi, "restoreSubject">, subjectId: string, version: number): Promise<RestoreCall> {
  try {
    const r = await api.restoreSubject(subjectId, version);
    return { state: restoreState(r), subject: r.subject };
  } catch {
    return { state: { phase: "problem", problem: "failed" }, subject: null };
  }
}

/** After which refusal the lists are out of date (the page reads them again). */
export function restoreNeedsReload(s: RestoreState): boolean {
  return s.phase === "problem" && (s.problem === "changed" || s.problem === "not_found" || s.problem === "expired");
}

/** What the page does with the end of a «Вернуть» call: the subject that is back and whether the lists are read again. */
export function restoreEffects(call: RestoreCall): { restored: LibrarySubject | null; reload: boolean } {
  const restored = call.state.phase === "done" ? call.subject : null;
  return { restored, reload: restored !== null || restoreNeedsReload(call.state) };
}

/** What the place of a line offers besides «Вернуть». */
export interface RestorePlace {
  /** The place asked for «Закрыть» on the live line too (the line after «Удалить»). */
  canClose: boolean;
  /** The place can fetch a fresh version after `changed` (the create form runs the create again, the review reads again). */
  refetch: boolean;
  /** Typing the name again leads on (the create form), so the refusal says so. */
  retype: boolean;
}

export interface RestoreLineView {
  text: string;
  tone: "info" | "ok" | "fail";
  /** «Вернуть»; «Повторить» of a failed call; «Повторить» that fetches a fresh version after `changed`. */
  button: "restore" | "retry" | "refetch" | null;
  busy: boolean;
  /** «Закрыть» is offered: always after a refusal that cannot be repeated, else only where the place asked for it. */
  close: boolean;
}

/** The line of a removed subject for any state; `idle` and `done` are the texts of the place the line is shown in. */
export function restoreLineView(idle: string, done: string, s: RestoreState, place: RestorePlace): RestoreLineView {
  switch (s.phase) {
    case "idle":
      return { text: idle, tone: "info", button: "restore", busy: false, close: place.canClose };
    case "busy":
      return { text: idle, tone: "info", button: "restore", busy: true, close: place.canClose };
    case "done":
      return { text: done, tone: "ok", button: null, busy: false, close: false };
    case "problem":
      switch (s.problem) {
        case "expired":
          // The name may be held still, so no «ввести ещё раз» here.
          return { text: removalTexts.expired, tone: "info", button: null, busy: false, close: true };
        case "changed":
          return place.refetch
            ? { text: removalTexts.restoreChanged, tone: "fail", button: "refetch", busy: false, close: place.canClose }
            : { text: removalTexts.restoreChangedRetype, tone: "fail", button: null, busy: false, close: true };
        case "not_found": {
          const text = place.retype ? `${removalTexts.restoreGone} ${removalTexts.retypeHint}` : removalTexts.restoreGone;
          return { text, tone: "fail", button: null, busy: false, close: true };
        }
        case "failed":
          return { text: removalTexts.restoreFailed, tone: "fail", button: "retry", busy: false, close: place.canClose };
      }
  }
}

/** «Предмет «Физика» удалён»: the line that replaces the list after «Удалить». */
export function removedLineText(name: string): string {
  return `Предмет «${name}» удалён`;
}

/** A skipped row of the schedule review whose name a removed subject holds (the name is "" when the row has none). */
export interface RemovedRow {
  key: string;
  name: string;
  subjectId: string;
  version: number;
}

/** The text of such a row before «Вернуть»: the name first when it is known. */
export function removedRowText(row: RemovedRow): string {
  return row.name === "" ? removalTexts.nameRemoved : `«${row.name}» · ${removalTexts.nameRemoved}`;
}

/** The text of the row once the subject is back. */
export function restoredRowText(row: RemovedRow): string {
  return row.name === "" ? removalTexts.restored : `«${row.name}» · ${removalTexts.restored}`;
}
