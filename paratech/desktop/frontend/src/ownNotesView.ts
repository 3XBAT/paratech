// «Мой конспект» on a lecture page (TASK-154): texts and the arrangement of what the core sends. Note titles, file
// names and pasted text are the student's or imported: they stay plain strings here and the components show them as
// text nodes only. The student's notes and paratech's «Конспект» are never merged (ADR-0027, Coexistence).

import type { LibrarySubjectState } from "./bindings/knowledgeBase.js";
import { notesErrorText, pageUrl, type NoteFile, type NotesApi,type NotePart, type NotesAdd, type NoteTarget, type NoteView } from "./bindings/notes.js";
import { plural } from "./plural.js";

/** Every label of the feature in one place: the owner's naming question is a one-line change here. */
export const ownNotesTexts = {
  title: "Мой конспект",
  ownChip: "ваш конспект",
  paratechChip: "paratech",
  paratechBlock: "Конспект paratech",
  addFiles: "Добавить файлы",
  paste: "Вставить текст",
  pasteLabel: "Текст конспекта",
  pasteSave: "Добавить текст",
  pasteCancel: "Закрыть",
  dropHint: "Файлы можно перетащить сюда: DOCX, PPTX, PDF или текст.",
  empty: "Пока ничего не добавлено.",
  loading: "Загрузка…",
  alreadyAdded: "уже добавлено",
  replace: "Заменить",
  moveUp: "Выше",
  moveDown: "Ниже",
  remove: "Удалить",
  undo: "Отменить",
  removedLine: "Конспект удалён.",
  replacedLine: "Файл заменён.",
  reorderedLine: "Порядок изменён.",
  restoredLine: "Конспект восстановлен.",
  undoneLine: "Действие отменено.",
  added: "добавлено",
  noSubject: "Для этого занятия не найден предмет в библиотеке: добавьте предмет, чтобы прикреплять конспект.",
  subjectAmbiguous: "У нескольких предметов в библиотеке одно название — добавить заметку нельзя.",
  subjectNone: "Предмет занятия не найден в библиотеке — добавьте его в «Предметах».",
  cancelled: "Выбор отменён.",
  statusLabel: "Состояние конспекта",
  partsLabel: "Части конспекта",
  removedPart: "удалён из библиотеки",
  missingPart: "файла больше нет в библиотеке",
  pending: "текст ещё извлекается",
  failed: "текст не извлечён",
  pdfNotExtracted: "текст PDF не извлечён",
  untitled: "Без названия",
  tooLong: "Текст больше 1 МБ.",
  limitPrefix: "Не больше",
  megabytes: "МБ",
  kilobytes: "КБ",
  counterOf: "из",
  noText: "текст не распознан",
  pagePasteLabel: "Текст страницы",
  pagePasteHint: "Ctrl+Enter сохраняет, Esc закрывает.",
  pageViewerOpen: "Открыть страницу",
  viewerPrev: "Назад",
  viewerNext: "Вперёд",
  viewerClose: "Закрыть",
  pageWord: "Страница",
  pageTextWord: "Текст страницы",
  viewerDialog: "Просмотр страниц",
  sectionOne: "раздел",
  sectionFew: "раздела",
  sectionMany: "разделов",
  pagesShort: "стр.",
  fragmentOne: "фрагмент",
  fragmentFew: "фрагмента",
  fragmentMany: "фрагментов",
} as const;

/** «3 стр. без текста» in the note header; "" when every page has text. */
export function noTextPagesText(n: number): string {
  return n > 0 ? `${n} стр. без текста` : "";
}

/** The alt text of a page image: the page number only, never the file name. */
export function pageAlt(number: number, noText: boolean): string {
  return `${ownNotesTexts.pageWord} ${number}: фото${noText ? `, ${ownNotesTexts.noText}` : ""}`;
}

/** The live-region line of the viewer. */
export function viewerAnnounce(number: number, total: number): string {
  return `${ownNotesTexts.pageWord} ${number} ${ownNotesTexts.counterOf} ${total}`;
}

/** The accessible name of the thumbnail button. */
export function thumbLabel(number: number, total: number): string {
  return `${ownNotesTexts.pageViewerOpen} ${number} ${ownNotesTexts.counterOf} ${total}`;
}

export interface PhotoPage {
  materialId: string;
  /** The only image source: the core's same-origin address. */
  src: string;
  /** 1-based position among the photo parts. */
  number: number;
  noText: boolean;
  alt: string;
}

/** The photo parts of a note in order, numbered by their position among photo parts. */
export function photoPages(note: Pick<NoteView, "parts">): PhotoPage[] {
  const out: PhotoPage[] = [];
  for (const p of note.parts) {
    const src = pageUrl(p.pageUrl ?? "");
    if (src === "") {
      continue;
    }
    const noText = p.textStatus === "no_text";
    const number = out.length + 1;
    out.push({ materialId: p.materialId, src, number, noText, alt: pageAlt(number, noText) });
  }
  return out;
}

export type ViewerKey = "ArrowLeft" | "ArrowRight" | "Home" | "End";

/** The page index after a key or a button; clamped to the first and the last page, never wraps. */
export function stepPage(index: number, total: number, key: ViewerKey): number {
  if (total <= 0) {
    return 0;
  }
  let to = index;
  switch (key) {
    case "ArrowLeft":
      to = index - 1;
      break;
    case "ArrowRight":
      to = index + 1;
      break;
    case "Home":
      to = 0;
      break;
    case "End":
      to = total - 1;
      break;
  }
  return Math.min(Math.max(to, 0), total - 1);
}

/** The longest pasted text the core keeps (cases/notes MaxPastedBytes). */
export const maxPasteBytes = 1024 * 1024;

export function pasteLimitText(): string {
  return `${ownNotesTexts.limitPrefix} ${maxPasteBytes / (1024 * 1024)} ${ownNotesTexts.megabytes}.`;
}

export type PasteCheck = { kind: "empty" } | { kind: "long" } | { kind: "ok" };

/** The pasted text as the core would count it: bytes of UTF-8. The core checks again. */
export function checkPaste(text: string): PasteCheck {
  if (text.trim() === "") {
    return { kind: "empty" };
  }
  return new TextEncoder().encode(text).length > maxPasteBytes ? { kind: "long" } : { kind: "ok" };
}

/** «12 из 1024 КБ» under the textarea. */
export function pasteCounterText(text: string): string {
  const used = new TextEncoder().encode(text).length;
  const kb = Math.ceil(used / 1024);
  return `${kb} ${ownNotesTexts.counterOf} ${maxPasteBytes / 1024} ${ownNotesTexts.kilobytes}`;
}

const maxNameLength = 120;

/** A file or note name for one line: long names are cut, the full text stays in the title attribute. */
export function shortName(name: string): string {
  return name.length > maxNameLength ? `${name.slice(0, maxNameLength)}…` : name;
}

/** «DOCX», «PDF»; "" when unknown. */
export function partTypeLabel(type: string): string {
  return type === "" ? "" : type.toUpperCase();
}

/** The state of a part's text, "" when it is fine. */
export function partState(p: Pick<NotePart, "textStatus" | "removed" | "missing">): string {
  if (p.missing) {
    return ownNotesTexts.missingPart;
  }
  if (p.removed) {
    return ownNotesTexts.removedPart;
  }
  switch (p.textStatus) {
    case "pending":
      return ownNotesTexts.pending;
    case "failed":
      return ownNotesTexts.failed;
    case "pdf_not_extracted":
      return ownNotesTexts.pdfNotExtracted;
    // A photo page without text has its own label and button.
    case "no_text":
    default:
      return "";
  }
}

export interface PartRow {
  materialId: string;
  name: string;
  /** «DOCX · текст ещё извлекается»: type and state. */
  meta: string;
  /** «3 раздела» / «12 стр.» / «5 фрагментов»; "" when the core gave no count. */
  pieces: string;
  canUp: boolean;
  canDown: boolean;
  /** The photo page of the part, null for any other part. */
  page: PhotoPage | null;
  /** Number of photo pages in the note (for «N из M»). */
  pageTotal: number;
}

/** «3 раздела», «12 стр.», «5 фрагментов» for a part; sections win over pages, pages over fragments; "" when none. */
export function partPiecesText(p: Partial<Pick<NotePart, "sections" | "pages" | "fragments">>): string {
  const n = (v: unknown) => (typeof v === "number" && Number.isInteger(v) && v > 0 ? v : 0);
  const { sections, pages, fragments } = { sections: n(p.sections), pages: n(p.pages), fragments: n(p.fragments) };
  if (sections > 0) {
    return `${sections} ${plural(sections, ownNotesTexts.sectionOne, ownNotesTexts.sectionFew, ownNotesTexts.sectionMany)}`;
  }
  if (pages > 0) {
    return `${pages} ${ownNotesTexts.pagesShort}`;
  }
  if (fragments > 0) {
    return `${fragments} ${plural(fragments, ownNotesTexts.fragmentOne, ownNotesTexts.fragmentFew, ownNotesTexts.fragmentMany)}`;
  }
  return "";
}

/** The parts of a note as listed, with the moves each may make. */
export function partRows(note: Pick<NoteView, "parts">): PartRow[] {
  const n = note.parts.length;
  const pages = photoPages(note);
  return note.parts.map((p, i) => ({
    materialId: p.materialId,
    name: p.name === "" ? ownNotesTexts.untitled : p.name,
    meta: [partTypeLabel(p.type), partState(p)].filter((s) => s !== "").join(" · "),
    pieces: partPiecesText(p),
    canUp: i > 0,
    canDown: i < n - 1,
    page: pages.find((pg) => pg.materialId === p.materialId) ?? null,
    pageTotal: pages.length,
  }));
}

/** «3 части» under the note's title. */
export function partCountText(n: number): string {
  return `${n} ${plural(n, "часть", "части", "частей")}`;
}

/** The order of part ids after moving the part at index by delta (-1 up, +1 down); null when it cannot move. */
export function movedOrder(order: readonly string[], index: number, delta: -1 | 1): string[] | null {
  const to = index + delta;
  if (index < 0 || index >= order.length || to < 0 || to >= order.length) {
    return null;
  }
  const out = [...order];
  [out[index], out[to]] = [out[to], out[index]];
  return out;
}

/**
 * The order with newId moved to right after pageId; null when nothing has to change: the page or the new part is
 * not in the order, they are the same, or the new part is already next to the page.
 */
export function orderAfter(order: readonly string[], pageId: string, newId: string): string[] | null {
  const page = order.indexOf(pageId);
  const at = order.indexOf(newId);
  if (page < 0 || at < 0 || pageId === newId || at === page + 1) {
    return null;
  }
  const out = order.filter((id) => id !== newId);
  out.splice(out.indexOf(pageId) + 1, 0, newId);
  return out;
}

/** «Текст страницы 2 добавлен» after a typed text is stored next to its page. */
export const pageTextAddedText = (number: number): string => `${ownNotesTexts.pageTextWord} ${number} добавлен`;

export const orderOf =(note: Pick<NoteView, "parts">): string[] => note.parts.map((p) => p.materialId);

/** The notes shown: removed ones stay only while their «Отменить» is offered. */
export function visibleNotes(notes: readonly NoteView[], undoNoteId: string): NoteView[] {
  return notes.filter((n) => !n.removed || n.id === undoNoteId);
}

/** What «Отменить» does; one step back, the latest action only. */
export type UndoAction =
  | { kind: "remove"; noteId: string }
  | { kind: "replace"; noteId: string; currentId: string; previousId: string }
  | { kind: "reorder"; noteId: string; order: string[] };

/** The line shown with «Отменить» after the action. */
export function undoneText(a: UndoAction): string {
  switch (a.kind) {
    case "remove":
      return ownNotesTexts.removedLine;
    case "replace":
      return ownNotesTexts.replacedLine;
    case "reorder":
      return ownNotesTexts.reorderedLine;
  }
}

export interface ResultLine {
  ok: boolean;
  text: string;
}

function fileLine(f: NoteFile): ResultLine {
  const name = shortName(f.name);
  const prefix = name === "" ? "" : `${name}: `;
  if (f.error !== "") {
    return { ok: false, text: `${prefix}${f.error}` };
  }
  if (f.alreadyAdded) {
    return { ok: true, text: `${prefix}${ownNotesTexts.alreadyAdded}` };
  }
  return { ok: true, text: `${prefix}${ownNotesTexts.added}` };
}

/** The report of an add, paste or drop: one line per file, or the refusal of the whole drop. */
export function addLines(add: NotesAdd): ResultLine[] {
  if (add.cancelled) {
    return [{ ok: true, text: ownNotesTexts.cancelled }];
  }
  if (add.error !== "") {
    return [{ ok: false, text: add.error }];
  }
  return add.files.map(fileLine);
}

/** The core's refusal text of a failed call; the page shows only known texts. */
export const refusalText = notesErrorText;

/** The note put in the list: replaced when it is there already, else added. */
export function withNote(notes: readonly NoteView[], note: NoteView): NoteView[] {
  const i = notes.findIndex((n) => n.id === note.id);
  return i < 0 ? [...notes, note] : notes.map((n, j) => (j === i ? note : n));
}

/** The subject of the notes: the one of an existing note, else the library subject the core matched to the lesson. */
export function pickSubjectId(
  notes: readonly Pick<NoteView, "subjectId">[],
  librarySubjectId: string,
  state: LibrarySubjectState,
): string {
  if (notes.length > 0) {
    return notes[0].subjectId;
  }
  return state === "matched" ? librarySubjectId : "";
}

/** Why adding is off: the reason the core gave for the lesson's subject. */
export function noSubjectText(state: LibrarySubjectState): string {
  switch (state) {
    case "ambiguous":
      return ownNotesTexts.subjectAmbiguous;
    case "none":
      return ownNotesTexts.subjectNone;
    default:
      return ownNotesTexts.noSubject;
  }
}

export type UndoApi = Pick<NotesApi, "restore" | "reorder" | "undoReplace">;

/** Performs the undo; null when the core did not give the note back (the undo is then dropped). */
export async function undoCall(api: UndoApi, a: UndoAction): Promise<NoteView | null> {
  switch (a.kind) {
    case "remove":
      return api.restore(a.noteId);
    case "reorder":
      return api.reorder(a.noteId, a.order);
    case "replace":
      return (await api.undoReplace(a.noteId, a.currentId, a.previousId)).note;
  }
}

/** The lecture the files go to. */
export function recordingTarget(recordingId: string, subjectId: string, noteId = ""): NoteTarget {
  return { noteId, subjectId, kind: "recording", recordingId, lessonId: "", date: "" };
}
