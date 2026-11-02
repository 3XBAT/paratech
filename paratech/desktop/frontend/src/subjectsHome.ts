// What «Предметы» shows on arrival (TASK-341): the tree and the subject right away, not a page of tiles. Pure, no React.
// The route keeps the student's choice: "" is «no choice yet» and shows the last opened subject (else the first one)
// without an entry of its own in the history; inboxRoute is «Входящие»; any other value is a subject id.

import type { LibraryMaterial, LibrarySubject } from "./bindings/library.js";
import { groupBySemester } from "./libraryView.js";

/** The route value of «Входящие». Subject ids are made by the core and are never this. */
export const inboxRoute = "__inbox__";

/** Where the last opened subject is kept: the webview's localStorage, like the theme (ADR-0012). */
export const lastSubjectKey = "paratech.subjects.last";

const maxIdLength = 128;

/** The part of localStorage the page needs (a fake in the tests). */
export interface KeyValueStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** The storage of the window; null where there is none or the browser refuses it. */
export function browserStore(): KeyValueStore | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

/** True for the id of a real subject in a route (not "" and not «Входящие»). */
export function isSubjectRoute(route: string): boolean {
  return route !== "" && route !== inboxRoute;
}

/** The remembered subject; "" when nothing usable is stored or the storage fails. The id may be gone from the list. */
export function readLastSubject(store: KeyValueStore | null): string {
  if (store === null) {
    return "";
  }
  try {
    const v: unknown = store.getItem(lastSubjectKey);
    return typeof v === "string" && v.length <= maxIdLength && isSubjectRoute(v) ? v : "";
  } catch {
    return "";
  }
}

/** Remembers a subject as the last opened one. «Входящие» and "" are not subjects; a failing storage is ignored. */
export function rememberSubject(store: KeyValueStore | null, id: string): void {
  if (store === null || !isSubjectRoute(id)) {
    return;
  }
  try {
    store.setItem(lastSubjectKey, id);
  } catch {
    // Quota or a blocked storage: the next arrival simply opens the first subject.
  }
}

/** The subject at the top of the tree (the latest semester, the first by name); "" without subjects. */
export function firstSubjectId(subjects: readonly LibrarySubject[]): string {
  return groupBySemester(subjects)[0]?.subjects[0]?.id ?? "";
}

/** The subject an arrival opens: the remembered one while it is still listed, else the first. */
export function homeSubjectId(subjects: readonly LibrarySubject[], remembered: string): string {
  return subjects.some((s) => s.id === remembered) ? remembered : firstSubjectId(subjects);
}

/** The remembered subject as read for one arrival (`visit` counts the rail clicks, see landingFor). */
export interface Landing {
  visit: number;
  id: string;
}

/**
 * "" must show the same subject for as long as the section stays open, or «Назад» to it would show the subject just left.
 * So the remembered id is read once per rail click, not at every render. A click on the rail while a subject is open
 * reads it again, so it lands on that subject.
 */
export function landingFor(prev: Landing | null, visit: number, store: KeyValueStore | null): Landing {
  return prev !== null && prev.visit === visit ? prev : { visit, id: readLastSubject(store) };
}

export type SubjectsView =
  | { kind: "loading" }
  /** No subjects at all: the tiles with the ways to make the first ones. */
  | { kind: "empty" }
  | { kind: "inbox" }
  | { kind: "subject"; id: string };

/**
 * What the section shows for a route, the subjects and the remembered subject of this arrival (see landingFor). A route to
 * a subject that is gone shows what "" shows: nothing is navigated, so «Назад» is not trapped by a redirect.
 */
export function resolveView(route: string, subjects: readonly LibrarySubject[] | null, landing: string): SubjectsView {
  if (subjects === null) {
    return { kind: "loading" };
  }
  if (subjects.length === 0) {
    return { kind: "empty" };
  }
  if (route === inboxRoute) {
    return { kind: "inbox" };
  }
  return { kind: "subject", id: subjects.some((s) => s.id === route) ? route : homeSubjectId(subjects, landing) };
}

/** The tiles are only for a library with no subjects. */
export function showsTiles(view: SubjectsView): boolean {
  return view.kind === "empty";
}

/** The subject on screen ("" for «Входящие», the tiles and the waiting states). */
export function shownSubjectId(view: SubjectsView): string {
  return view.kind === "subject" ? view.id : "";
}

/** A click on a subject in the tree is a new place unless that subject is on screen already. */
export function opensNewPlace(view: SubjectsView, target: string): boolean {
  return target === inboxRoute ? view.kind !== "inbox" : !(view.kind === "subject" && view.id === target);
}

/** The first item of the tree. */
export interface InboxEntry {
  label: string;
  /** The number of files; "" when there are none (the item stays, quiet). */
  count: string;
  /** No files: drawn muted. */
  quiet: boolean;
  current: boolean;
  /** «Входящие · 3» for the tooltip and the screen reader. */
  title: string;
}

export const inboxLabel = "Входящие";

/** The files of «Входящие» that are not deleted. */
export function inboxFiles(inbox: readonly LibraryMaterial[]): number {
  return inbox.filter((m) => !m.removed).length;
}

/**
 * `known` is false while «Входящие» is not read yet or failed to load: then nothing is claimed (no number, not quiet),
 * so an unknown state does not look like an empty inbox.
 */
export function inboxEntry(files: number, current: boolean, known = true): InboxEntry {
  const empty = known && files <= 0;
  const counted = known && files > 0;
  return {
    label: inboxLabel,
    count: counted ? String(files) : "",
    quiet: empty,
    current,
    title: counted ? `${inboxLabel} · ${files}` : inboxLabel,
  };
}

/** What «Входящие» says while it holds nothing, and how files get there. */
export const emptyInboxText =
  "Здесь собираются файлы без предмета: добавленные кнопкой «Добавить файл» или перетащенные в окно. Отсюда их можно разложить по предметам.";

/** «Входящие» did not load: said instead of an empty list. */
export const inboxFailedText = "Не удалось загрузить «Входящие».";

/** The state that tells the page a new view started (the form, the notes and the loaded files of the previous one go). */
export function viewKey(view: SubjectsView, route: string): string {
  if (view.kind === "subject") {
    return view.id;
  }
  return view.kind === "inbox" ? inboxRoute : route;
}
