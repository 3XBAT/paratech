// View logic of «Предметы»: semesters, folders and file lines. Pure, no React. Names are typed or imported text and are
// passed through untouched (the page renders them as text nodes).

import type { LibraryFolder, LibraryMaterial, LibrarySubject } from "./bindings/library.js";

export const texts = {
  title: "Предметы",
  addSubject: "Предмет",
  addFolder: "Папка",
  addFile: "Добавить файл",
  fileAdded: "Файл добавлен.",
  fileExisting: "Файл уже есть в списке.",
  knowledge: "База знаний",
  back: "Все предметы",
  subjectName: "Название предмета",
  folderName: "Название папки",
  create: "Создать",
  cancel: "Отмена",
  loading: "Загрузка…",
  loadFailed: "Не удалось загрузить предметы.",
  filesFailed: "Не удалось загрузить файлы.",
  emptyTitle: "Предметов пока нет",
  emptyText: "Создайте первый предмет: в нём появятся папки для лекций, семинаров, материалов и заданий.",
  emptyFolder: "В этой папке пока нет файлов.",
  inbox: "Входящие",
  inboxHint: "Файлы, которые ещё не разложены по предметам.",
  folders: "Папки",
  files: "Файлы",
  aliases: "Другие названия",
} as const;

/** Russian names of the system folders by their code. */
const systemNames: Readonly<Record<string, string>> = {
  lectures: "Лекции",
  seminars: "Семинары",
  labs: "Лабораторные",
  materials: "Материалы",
  homework: "ДЗ",
  tickets: "Билеты",
};

const systemOrder = ["lectures", "seminars", "labs", "materials", "homework", "tickets"];

/** The title of a folder: the fixed name of a system folder, the typed name of an own one. */
/** The line after «Добавить файл»: nothing when the student cancelled the dialog. */
export function importNote(r: { cancelled: boolean; existing: boolean }): string {
  if (r.cancelled) {
    return "";
  }
  return r.existing ? texts.fileExisting : texts.fileAdded;
}

export function folderTitle(f: LibraryFolder): string {
  return systemNames[f.system] ?? f.name;
}

/** System folders in their fixed order, then the own folders in the order the core sent them. */
export function orderedFolders(folders: readonly LibraryFolder[]): LibraryFolder[] {
  const system = folders
    .filter((f) => f.system !== "")
    .sort((a, b) => systemOrder.indexOf(a.system) - systemOrder.indexOf(b.system));
  return [...system, ...folders.filter((f) => f.system === "")];
}

export interface SemesterGroup {
  code: string;
  label: string;
  subjects: LibrarySubject[];
}

const seasonRank: Readonly<Record<string, number>> = { spring: 1, summer: 2, autumn: 3, winter: 4 };

function semesterKey(code: string): [number, number] {
  const m = /^(\d{4})-([a-z]+)$/.exec(code);
  return m ? [Number(m[1]), seasonRank[m[2]] ?? 0] : [0, 0];
}

/** Subjects grouped by semester, the latest semester first; subjects keep the order of the core (by name). */
export function groupBySemester(subjects: readonly LibrarySubject[]): SemesterGroup[] {
  const groups = new Map<string, SemesterGroup>();
  for (const s of subjects) {
    const g = groups.get(s.semester) ?? { code: s.semester, label: s.semesterLabel || s.semester, subjects: [] };
    g.subjects.push(s);
    groups.set(s.semester, g);
  }
  return [...groups.values()].sort((a, b) => {
    const [ya, ra] = semesterKey(a.code);
    const [yb, rb] = semesterKey(b.code);
    return yb - ya || rb - ra;
  });
}

/** The active files of one folder of a subject. */
export function filesOfFolder(materials: readonly LibraryMaterial[], folderId: string): LibraryMaterial[] {
  return materials.filter((m) => !m.removed && !m.inbox && m.folderId === folderId);
}

export function typeLabel(type: string): string {
  return type === "" ? "" : type.toUpperCase();
}

/** «1,5 МБ», «320 КБ», «12 Б». */
export function sizeText(bytes: number): string {
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1).replace(".", ",")} МБ`;
  }
  if (bytes >= 1024) {
    return `${Math.round(bytes / 1024)} КБ`;
  }
  return `${Math.max(0, Math.round(bytes))} Б`;
}

export function folderCountText(n: number): string {
  const last = n % 10;
  const tens = n % 100;
  const word = tens >= 11 && tens <= 14 ? "файлов" : last === 1 ? "файл" : last >= 2 && last <= 4 ? "файла" : "файлов";
  return `${n} ${word}`;
}
