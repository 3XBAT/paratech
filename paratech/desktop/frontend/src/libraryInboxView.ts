// View logic of the actions on the files of the library (TASK-339): the row menu by state, the hint line, the panel
// «Разложить», the undo lines, the drag of a row to a subject. Pure, no React. Names of files, subjects and folders are
// typed or imported text: they are passed through untouched and shown as text nodes.

import { type LibraryImport, type LibraryMaterial, type LibrarySubject, refreshTexts } from "./bindings/library.js";
import type { PlacementHint } from "./bindings/libraryInbox.js";
import { folderTitle, orderedFolders, texts } from "./libraryView.js";

export const fileTexts = {
  open: "Открыть",
  place: "Разложить",
  /** In the «⋯» menu «Разложить» opens a panel, hence the dots; the visible button of the row stays «Разложить». */
  placeMenu: "Разложить…",
  move: "Переместить…",
  toInbox: "Вернуть во «Входящие»",
  remove: "Удалить",
  menuTitle: "Действия",
  subject: "Предмет",
  folder: "Папка",
  putHere: "Положить",
  cancel: "Отмена",
  noSubjects: "Предметов пока нет",
  createSubject: "Создать предмет",
  undo: "Вернуть",
  refresh: "Обновить список",
  retry: "Повторить",
  addFileTo: "Добавить файл",
  inboxPlace: "Входящие",
  pickSubject: "Выберите предмет",
  newSubject: "Новый предмет…",
  noFolder: "—",
  dragTitle: "Перетащите на карточку предмета",
  addToInboxTip: "Файл попадёт во «Входящие»",
  addToMaterialsTip: "Файл попадёт в «Материалы»",
} as const;

/** The accessible name of the «⋯» button of a file row. */
export function moreLabel(name: string): string {
  return `Ещё действия: ${name}`;
}

export type FileAction = "open" | "place" | "toInbox" | "remove";

export interface FileMenuItem {
  key: FileAction;
  label: string;
}

/**
 * The items of the «⋯» menu: a file of «Входящие» can be opened, placed and deleted; a placed one can also go back to
 * «Входящие», and «Разложить» is «Переместить…» for it.
 */
export function fileMenu(placed: boolean): FileMenuItem[] {
  const items: FileMenuItem[] = [
    { key: "open", label: fileTexts.open },
    { key: "place", label: placed ? fileTexts.move : fileTexts.placeMenu },
  ];
  if (placed) {
    items.push({ key: "toInbox", label: fileTexts.toInbox });
  }
  items.push({ key: "remove", label: fileTexts.remove });
  return items;
}

/** The one visible button of a row: «Разложить» in «Входящие», «Открыть» in a folder. */
export function primaryAction(placed: boolean): FileMenuItem {
  return placed ? { key: "open", label: fileTexts.open } : { key: "place", label: fileTexts.place };
}

// ---- Where a file lies ----

/** «Сети → Лабораторные»; the subject alone when the folder is unknown, "" when the subject is. */
export function placeLabel(subjects: readonly LibrarySubject[], subjectId: string, folderId: string): string {
  const s = subjects.find((x) => x.id === subjectId);
  if (!s) {
    return "";
  }
  const f = s.folders.find((x) => x.id === folderId);
  return f ? `${s.name} → ${folderTitle(f)}` : s.name;
}

/** «Положено в «Сети → Лабораторные».» */
export function placedNote(label: string): string {
  return label === "" ? "Файл положен." : `Положено в «${label}».`;
}

/** «Создан предмет «Сети», файл положен в «Лабораторные».» after a «create» hint. */
export function createdNote(subjectName: string, folderName: string): string {
  return `Создан предмет «${subjectName}», файл положен в «${folderName}».`;
}

/** «Удалено «имя».» */
export function removedNote(name: string): string {
  return `Удалено «${name}».`;
}

export const movedToInboxNote = "Файл возвращён во «Входящие».";

// ---- The hint where a file of «Входящие» goes ----

export interface HintLine {
  /** The quiet text before the button ("" when the button says it all). */
  text: string;
  /** The button; it is the only way the hint places anything. */
  action: string;
}

/** The line under a file of «Входящие»; null for no hint. The names are the core's, shown as text. */
export function hintLine(h: PlacementHint | undefined): HintLine | null {
  if (!h || h.kind === "none") {
    return null;
  }
  if (h.kind === "existing") {
    return { text: `Похоже на: ${h.subjectName} → ${h.folderName}`, action: fileTexts.putHere };
  }
  return { text: `Есть в расписании: «${h.createName}»`, action: `Создать предмет «${h.createName}» и положить` };
}

/** How many rows of «Входящие» are hinted (the hints are requested one by one). */
export const hintLimit = 20;

/** The files to ask a hint for: the first `limit` files of «Входящие» that have none yet. */
export function hintTargets(inbox: readonly LibraryMaterial[], known: ReadonlySet<string>, limit: number = hintLimit): string[] {
  return inbox
    .filter((m) => !m.removed)
    .slice(0, limit)
    .filter((m) => !known.has(m.id))
    .map((m) => m.id);
}

/** A hint is only valid for the subjects it was computed for: it is forgotten when they change. */
export function subjectsKey(subjects: readonly LibrarySubject[] | null): string {
  return subjects === null ? "" : subjects.map((s) => s.id).join("|");
}

// ---- The panel «Разложить» / «Переместить…» ----

export interface FolderChoice {
  id: string;
  title: string;
}

/** The folders of a subject for the select: the system ones first, as on the page. */
export function folderChoices(s: LibrarySubject | undefined): FolderChoice[] {
  return s ? orderedFolders(s.folders).map((f) => ({ id: f.id, title: folderTitle(f) })) : [];
}

/** The folder offered first: the wanted one when the subject has it, else «Материалы», else the first. */
export function defaultFolderId(s: LibrarySubject | undefined, wanted = ""): string {
  if (!s) {
    return "";
  }
  if (wanted !== "" && s.folders.some((f) => f.id === wanted)) {
    return wanted;
  }
  return (s.folders.find((f) => f.system === "materials") ?? orderedFolders(s.folders)[0])?.id ?? "";
}

export interface PanelChoice {
  subjectId: string;
  folderId: string;
}

/**
 * The subject and folder of the panel for the wished ones. A subject that is not wished (or not there) is not guessed:
 * the choice is empty ("Выберите предмет") and «Положить» stays off. A chosen subject opens on the wished folder, else
 * «Материалы».
 */
export function panelChoice(subjects: readonly LibrarySubject[], wantedSubject: string, wantedFolder: string): PanelChoice {
  const s = subjects.find((x) => x.id === wantedSubject);
  return { subjectId: s?.id ?? "", folderId: defaultFolderId(s, wantedFolder) };
}

/** The value of the last option of the subject select, «Новый предмет…» (never a real subject id). */
export const newSubjectValue = "__new-subject__";

/** What the panel holds: the wished subject and folder (""), and whether the name of a new subject is being typed. */
export interface PanelPick {
  subjectId: string;
  folderId: string;
  creating: boolean;
}

export function panelStart(subjectId: string, folderId: string): PanelPick {
  return { subjectId, folderId, creating: false };
}

/** The subject select changed: «Новый предмет…» turns it into the name form, any other value is the new subject (folder reset). */
export function panelPickSubject(prev: PanelPick, value: string): PanelPick {
  return value === newSubjectValue ? { ...prev, creating: true } : { subjectId: value, folderId: "", creating: false };
}

/** The new subject was made: it is selected (the file is not placed yet), the form closes. */
export function panelCreated(subjectId: string): PanelPick {
  return { subjectId, folderId: "", creating: false };
}

/** The refusals after which the list is out of date ("refresh") or the call may simply be repeated ("retry"). */
export function errorWay(text: string): "refresh" | "retry" {
  return refreshTexts.includes(text) ? "refresh" : "retry";
}

// ---- Undo ----

/** How a change is taken back. */
export type UndoBack = { kind: "inbox"; removeSubject?: string } | { kind: "place"; subjectId: string; folderId: string } | { kind: "restore" };

export interface UndoItem {
  id: number;
  materialId: string;
  text: string;
  back: UndoBack;
  /** Epoch ms after which the line goes. */
  expires: number;
  /** The refusal of «Вернуть», shown in the line; the line then stays until it expires. */
  error: string;
}

/** Where a file lies now, as the way back for «Вернуть». */
export function backOf(m: LibraryMaterial): UndoBack {
  return m.inbox || m.subjectId === "" ? { kind: "inbox" } : { kind: "place", subjectId: m.subjectId, folderId: m.folderId };
}

export const undoMs = 10_000;
export const undoMax = 3;

/**
 * Adds a line. Each line keeps its own ten seconds, so a second delete does not take the first undo away. A new line for
 * the same file replaces the old one (the old undo would be wrong), and past undoMax lines the oldest goes.
 */
export function undoPush(list: readonly UndoItem[], item: Omit<UndoItem, "expires" | "error">, now: number): UndoItem[] {
  const alive = undoPrune(list, now).filter((x) => x.materialId !== item.materialId);
  return [...alive, { ...item, expires: now + undoMs, error: "" }].slice(-undoMax);
}

/** The lines still alive at `now`. */
export function undoPrune(list: readonly UndoItem[], now: number): UndoItem[] {
  return list.filter((x) => x.expires > now);
}

export function undoDrop(list: readonly UndoItem[], id: number): UndoItem[] {
  return list.filter((x) => x.id !== id);
}

export function undoFail(list: readonly UndoItem[], id: number, error: string): UndoItem[] {
  return list.map((x) => (x.id === id ? { ...x, error } : x));
}

/** Milliseconds to the next line that goes; null without lines. */
export function undoNextDelay(list: readonly UndoItem[], now: number): number | null {
  return list.length === 0 ? null : Math.max(0, Math.min(...list.map((x) => x.expires)) - now);
}

// ---- The drag of a row to a subject ----

/** The type of the internal drag; it carries only the ID of the file, and a drag of files from the disk never has it. */
export const materialDragType = "application/x-paratech-material";

export function isMaterialDrag(transfer: { types?: ArrayLike<string> } | null | undefined): boolean {
  const types = transfer?.types;
  for (let i = 0; types && i < types.length; i++) {
    if (types[i] === materialDragType) {
      return true;
    }
  }
  return false;
}

/** The ID in a drop when it is the ID of a file of «Входящие» known to the page; null for anything else. */
export function readMaterialDrop(raw: unknown, inboxIds: ReadonlySet<string>): string | null {
  return typeof raw === "string" && raw !== "" && raw.length <= 128 && inboxIds.has(raw) ? raw : null;
}

/** The cue on a subject card while a row is dragged: the drop lands in «Материалы», so the cue says it. */
export function dropCue(subjectName: string): string {
  return `Положить в «${subjectName} → Материалы»`;
}

// ---- «Добавить файл» on the page of a subject ----

/** The folder a target means: "" is «Материалы» of the subject. */
export function targetFolderId(subjects: readonly LibrarySubject[], subjectId: string, folderId: string): string {
  return folderId !== "" ? folderId : defaultFolderId(subjects.find((s) => s.id === subjectId));
}

/** «Добавить файл в папку «Лекции»» */
export function addToFolderLabel(folderName: string): string {
  return `Добавить файл в папку «${folderName}»`;
}

/** The title of the folder a target means ("" when the subject or the folder is gone). */
function targetFolderTitle(subjects: readonly LibrarySubject[], subjectId: string, folderId: string): string {
  const here = targetFolderId(subjects, subjectId, folderId);
  const f = subjects.find((s) => s.id === subjectId)?.folders.find((x) => x.id === here);
  return f ? folderTitle(f) : "";
}

/** «Добавить файл» ended with a file the core kept but did not put into the target; it lies in «Входящие». */
export function notPlacedNote(folderName: string): string {
  const where = folderName === "" ? "нужную папку" : `«${folderName}»`;
  return `Файл добавлен во «Входящие»: положить его в ${where} не получилось.`;
}

/** The file of an import that was known already and lies elsewhere than the target, with the offers that follow. */
export interface ImportElsewhere {
  material: LibraryMaterial;
  /** «Физика → Лекции» or «Входящие»; "" when the place is unknown to the page. */
  label: string;
  /** The subject to open; "" for «Входящие» or a subject the page does not know. */
  openSubjectId: string;
}

export interface ImportOutcome {
  /** The line to show; "" when the dialog was cancelled. */
  note: string;
  elsewhere: ImportElsewhere | null;
  /** The file lies in «Входящие» unplaced: the line offers to go there. */
  toInbox: boolean;
  /** With `toInbox`: the file and the wished place (the folder resolved), for «Разложить» in the line. */
  unplaced?: { material: LibraryMaterial; subjectId: string; folderId: string };
}

/**
 * What «Добавить файл» on the page of a subject says: nothing when the dialog was cancelled; that the file did not land
 * in its folder (it lies in «Входящие»); where the file already lies when it was known and so was not moved, with the
 * way to bring it here or to look at it there.
 */
export function importOutcome(
  r: Pick<LibraryImport, "cancelled" | "existing" | "material"> & { notPlaced?: boolean },
  target: { subjectId: string; folderId: string },
  subjects: readonly LibrarySubject[],
): ImportOutcome {
  const none = { elsewhere: null, toInbox: false };
  if (r.cancelled) {
    return { note: "", ...none };
  }
  const here = targetFolderId(subjects, target.subjectId, target.folderId);
  const m = r.material;
  if (r.notPlaced === true) {
    return {
      note: notPlacedNote(targetFolderTitle(subjects, target.subjectId, target.folderId)),
      elsewhere: null,
      toInbox: true,
      ...(m === null ? {} : { unplaced: { material: m, subjectId: target.subjectId, folderId: here } }),
    };
  }
  if (!r.existing || m === null) {
    const where = placeLabel(subjects, target.subjectId, here);
    return { note: where === "" ? texts.fileAdded : `Файл добавлен в «${where}».`, ...none };
  }
  if (m.subjectId === target.subjectId && m.folderId === here) {
    return { note: texts.fileExisting, ...none };
  }
  const inbox = m.inbox || m.subjectId === "";
  const label = inbox ? fileTexts.inboxPlace : placeLabel(subjects, m.subjectId, m.folderId);
  const open = !inbox && subjects.some((s) => s.id === m.subjectId) ? m.subjectId : "";
  return {
    note: label === "" ? texts.fileExisting : `Этот файл уже есть: «${label}».`,
    elsewhere: { material: m, label, openSubjectId: open },
    toInbox: false,
  };
}

/** The line only, see importOutcome. */
export function importToNote(
  r: Pick<LibraryImport, "cancelled" | "existing" | "material"> & { notPlaced?: boolean },
  target: { subjectId: string; folderId: string },
  subjects: readonly LibrarySubject[],
): string {
  return importOutcome(r, target, subjects).note;
}

export const addFileTexts = {
  moveHere: "Переместить сюда",
  openSubject: "Открыть предмет",
  openInbox: "Перейти во «Входящие»",
  place: "Разложить",
} as const;

type Scrollable = { getElementById(id: string): { scrollIntoView?: (o: { block: "start" }) => void } | null };

/** The anchor of the section «Входящие» on the list page. */
export const inboxAnchor = "inbox-section";

/** Scrolls the section «Входящие» into view; nothing without a document or the section. */
export function scrollToInbox(doc: Scrollable | undefined): void {
  doc?.getElementById(inboxAnchor)?.scrollIntoView?.({ block: "start" });
}

/**
 * «Перейти во «Входящие»» opens the list page first, so the scroll waits for it: true when the list is shown and loaded
 * and a scroll is pending (the page then scrolls and clears the flag).
 */
export function inboxScrollDue(pending: boolean, openId: string, listLoaded: boolean): boolean {
  return pending && openId === "" && listLoaded;
}

/** The anchor of the section of a folder on the page of a subject (the tree scrolls to it). */
export function folderAnchor(folderId: string): string {
  return `folder-${folderId}`;
}

/** Scrolls the section of a folder into view; nothing without a document (the tests) or without the section. */
export function scrollToFolder(
  doc: { getElementById(id: string): { scrollIntoView?: (o: { block: "start" }) => void } | null } | undefined,
  folderId: string,
): void {
  doc?.getElementById(folderAnchor(folderId))?.scrollIntoView?.({ block: "start" });
}
