// Library bindings («Предметы»): calls into the Go core (internal/input/wails/library.go). Names of subjects, folders and
// files are typed or imported text: show them as text nodes only. The Go errors are fixed Russian texts; only a text
// from the list below is shown, anything else gets a generic one (a path or a cause never reaches the page).

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.LibraryService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  subjects: `${service}.Subjects`,
  createSubject: `${service}.CreateSubject`,
  createFolder: `${service}.CreateFolder`,
  materials: `${service}.Materials`,
  importFile: `${service}.ImportFile`,
} as const;

/** The error texts of the Go binding (library.go); a Go test pins the same strings. */
export const errorTexts: readonly string[] = [
  "Неверный идентификатор: обновите список.",
  "Название предмета: от 1 до 200 символов.",
  "Недопустимое имя папки: до 64 символов, без \\ / : * ? \" < > |, не CON, NUL и т. п., без точки в конце.",
  "Предмет с таким названием уже есть.",
  "Папка с таким именем уже есть.",
  "Предмет не найден: обновите список.",
  "Папка не найдена: обновите список.",
  "Файл не найден: обновите список.",
  "Занятие не найдено в расписании.",
  "Сначала уберите отсюда все файлы, в том числе удалённые.",
  "Эту папку удалить нельзя.",
  "Достигнут предел: слишком много папок, названий или связей.",
  "Файл удалён: сначала восстановите его.",
  "Файл пустой.",
  "Файл больше 100 МБ.",
  "Подходят только DOCX, PPTX, PDF и текст в UTF-8.",
  "Не удалось прочитать файл.",
  "Данные сейчас меняются. Попробуйте ещё раз.",
  "Не удалось прочитать библиотеку. Попробуйте ещё раз.",
  "Этот файл добавить нельзя: перетащите файл с диска этого компьютера.",
  "За раз можно перетащить не больше 10 файлов.",
];

export const genericError = "Не удалось выполнить действие.";

function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}

/** The core's text of a refused call as it is when it is one of the known texts; a generic text otherwise. */
export function libraryErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) ? msg : genericError;
}

/** System folders of a subject by their code. */
export type SystemFolder = "lectures" | "seminars" | "labs" | "materials" | "homework" | "tickets";

export interface LibraryFolder {
  id: string;
  name: string;
  /** The code of a system folder, "" for an own folder. */
  system: string;
}

export interface LibrarySubject {
  id: string;
  name: string;
  aliases: string[];
  /** The code ("2026-autumn"). */
  semester: string;
  /** The text shown («Осень 2026»). */
  semesterLabel: string;
  folders: LibraryFolder[];
}

export interface LibraryMaterial {
  id: string;
  name: string;
  /** docx, pptx, txt or pdf. */
  type: string;
  sizeBytes: number;
  subjectId: string;
  folderId: string;
  inbox: boolean;
  removed: boolean;
  textStatus: string;
  textNote: string;
  createdAt: string;
}

export interface LibraryImport {
  cancelled: boolean;
  existing: boolean;
  material: LibraryMaterial | null;
}

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new Error(`${what}: not an object`);
  }
  return raw as Record<string, unknown>;
}

function str(r: Record<string, unknown>, key: string, what: string): string {
  const v = r[key];
  if (typeof v !== "string") {
    throw new Error(`${what}: ${key} is not a string`);
  }
  return v;
}

function optStr(r: Record<string, unknown>, key: string): string {
  return typeof r[key] === "string" ? (r[key] as string) : "";
}

function list<T>(raw: unknown, what: string, parse: (item: unknown) => T): T[] {
  if (raw === null || raw === undefined) {
    return [];
  }
  if (!Array.isArray(raw)) {
    throw new Error(`${what}: not a list`);
  }
  return raw.map(parse);
}

export function parseFolder(raw: unknown): LibraryFolder {
  const w = "folder";
  const r = record(raw, w);
  return { id: str(r, "id", w), name: str(r, "name", w), system: optStr(r, "system") };
}

export function parseSubject(raw: unknown): LibrarySubject {
  const w = "subject";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    name: str(r, "name", w),
    aliases: list(r.aliases, w, (a) => String(a)),
    semester: optStr(r, "semester"),
    semesterLabel: optStr(r, "semesterLabel"),
    folders: list(r.folders, w, parseFolder),
  };
}

export function parseMaterial(raw: unknown): LibraryMaterial {
  const w = "material";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    name: str(r, "name", w),
    type: optStr(r, "type"),
    sizeBytes: typeof r.sizeBytes === "number" && Number.isFinite(r.sizeBytes) ? r.sizeBytes : 0,
    subjectId: optStr(r, "subjectId"),
    folderId: optStr(r, "folderId"),
    inbox: r.inbox === true,
    removed: r.removed === true,
    textStatus: optStr(r, "textStatus"),
    textNote: optStr(r, "textNote"),
    createdAt: optStr(r, "createdAt"),
  };
}

export function parseImport(raw: unknown): LibraryImport {
  const w = "import";
  const r = record(raw, w);
  return {
    cancelled: r.cancelled === true,
    existing: r.existing === true,
    material: r.material === null || r.material === undefined ? null : parseMaterial(r.material),
  };
}

export interface LibraryApi {
  subjects(): Promise<LibrarySubject[]>;
  /** semester is a code or "" for the current one. */
  createSubject(name: string, semester: string): Promise<LibrarySubject>;
  createFolder(subjectId: string, name: string): Promise<LibraryFolder>;
  /** The files of a subject, or of «Входящие» for "". */
  materials(subjectId: string): Promise<LibraryMaterial[]>;
  /** «Добавить файл»: the native open dialog. */
  importFile(): Promise<LibraryImport>;
}

export function libraryApi(call: CallByName): LibraryApi {
  return {
    subjects: async () => list(await call(methods.subjects), "subjects", parseSubject),
    createSubject: async (name, semester) => parseSubject(await call(methods.createSubject, name, [], semester)),
    createFolder: async (subjectId, name) => parseFolder(await call(methods.createFolder, subjectId, name)),
    materials: async (subjectId) => list(await call(methods.materials, subjectId), "materials", parseMaterial),
    importFile: async () => parseImport(await call(methods.importFile)),
  };
}
