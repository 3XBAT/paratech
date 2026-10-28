// Bindings of the files of the library (TASK-339): «Разложить», «Вернуть во «Входящие»», «Удалить» with undo, the hint where
// a file of «Входящие» goes, and «Добавить файл» into a subject. The Go side is internal/input/wails/library.go and
// library_inbox.go. Names are typed or imported text: show them as text nodes only. Errors are the fixed texts of
// libraryErrorText (bindings/library.ts).

import type { CallByName } from "./settings";
import { type LibraryImport, type LibraryMaterial, parseImport, parseMaterial } from "./library.js";

const service = "paratech/desktop/internal/input/wails.LibraryService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const inboxMethods = {
  place: `${service}.Place`,
  moveToInbox: `${service}.MoveToInbox`,
  removeMaterial: `${service}.RemoveMaterial`,
  restoreMaterial: `${service}.RestoreMaterial`,
  placementHint: `${service}.PlacementHint`,
  importFileTo: `${service}.ImportFileTo`,
} as const;

export type PlacementKind = "none" | "existing" | "create";

/**
 * Where a file of «Входящие» may go (from its name, no AI). It places nothing: the page offers it and the student clicks.
 * "existing": Place(material, subjectId, folderId). "create": CreateSubject(createName, createAliases, ""), then Place into
 * the folder of the new subject whose `system` is folderKind. createName is the name without the lesson-kind mark;
 * createAliases are the schedule's other spellings (never null). Names are plain text.
 */
export interface PlacementHint {
  kind: PlacementKind;
  subjectId: string;
  subjectName: string;
  folderId: string;
  folderName: string;
  folderKind: string;
  createName: string;
  createAliases: string[];
}

export const noHint: PlacementHint = {
  kind: "none",
  subjectId: "",
  subjectName: "",
  folderId: "",
  folderName: "",
  folderKind: "",
  createName: "",
  createAliases: [],
};

function text(r: Record<string, unknown>, key: string): string {
  return typeof r[key] === "string" ? (r[key] as string) : "";
}

/** The strings of a list; a missing or non-list value is empty, non-string items are left out. */
function texts(raw: unknown): string[] {
  return Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : [];
}

/** Reads a hint defensively: anything unknown or incomplete is "none" (no hint), never a half-filled offer. */
export function parsePlacementHint(raw: unknown): PlacementHint {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return noHint;
  }
  const r = raw as Record<string, unknown>;
  const hint: PlacementHint = {
    kind: r.kind === "existing" || r.kind === "create" ? r.kind : "none",
    subjectId: text(r, "subjectId"),
    subjectName: text(r, "subjectName"),
    folderId: text(r, "folderId"),
    folderName: text(r, "folderName"),
    folderKind: text(r, "folderKind"),
    createName: text(r, "createName"),
    createAliases: texts(r.createAliases),
  };
  if (hint.kind === "existing" && (hint.subjectId === "" || hint.subjectName === "" || hint.folderId === "" || hint.folderName === "")) {
    return noHint;
  }
  if (hint.kind === "create" && (hint.createName.trim() === "" || hint.folderKind === "")) {
    return noHint;
  }
  return hint;
}

export interface LibraryInboxApi {
  /** Puts a file into a folder of a subject; folderId "" is «Материалы». */
  place(materialId: string, subjectId: string, folderId: string): Promise<LibraryMaterial>;
  moveToInbox(materialId: string): Promise<LibraryMaterial>;
  /** A soft delete: the file is kept and restoreMaterial brings it back to where it was. */
  removeMaterial(materialId: string): Promise<LibraryMaterial>;
  restoreMaterial(materialId: string): Promise<LibraryMaterial>;
  /** Read-only. */
  placementHint(materialId: string): Promise<PlacementHint>;
  /** «Добавить файл» on the page of a subject: the native dialog; folderId "" is «Материалы». */
  importFileTo(subjectId: string, folderId: string): Promise<LibraryImport>;
}

export function libraryInboxApi(call: CallByName): LibraryInboxApi {
  return {
    place: async (materialId, subjectId, folderId) => parseMaterial(await call(inboxMethods.place, materialId, subjectId, folderId)),
    moveToInbox: async (materialId) => parseMaterial(await call(inboxMethods.moveToInbox, materialId)),
    removeMaterial: async (materialId) => parseMaterial(await call(inboxMethods.removeMaterial, materialId)),
    restoreMaterial: async (materialId) => parseMaterial(await call(inboxMethods.restoreMaterial, materialId)),
    placementHint: async (materialId) => parsePlacementHint(await call(inboxMethods.placementHint, materialId)),
    importFileTo: async (subjectId, folderId) => parseImport(await call(inboxMethods.importFileTo, subjectId, folderId)),
  };
}
