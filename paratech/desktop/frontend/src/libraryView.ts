// View logic of «Предметы»: semesters, folders and file lines. Pure, no React. Names are typed or imported text and are
// passed through untouched (the page renders them as text nodes).

import type {
  LessonStatus,
  LibraryApi,
  LibraryFolder,
  LibraryMaterial,
  LibrarySubject,
  SubjectLesson,
  SubjectOverview,
  SubjectSlot,
} from "./bindings/library.js";
import type { IconName } from "./ui/Icon.js";
import type { ChipKind } from "./ui/StatusChip.js";

export const texts = {
  upcoming: "Ближайшее",
  lessons: "Занятия",
  fresh: "новое",
  overviewFailed: "Не удалось загрузить сведения о предмете.",
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

/** The line after «Добавить файл»: nothing when the student cancelled the dialog. */
export function importNote(r: { cancelled: boolean; existing: boolean }): string {
  if (r.cancelled) {
    return "";
  }
  return r.existing ? texts.fileExisting : texts.fileAdded;
}

/** The title of a folder: the fixed name of a system folder, the typed name of an own one. */
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

/** The count next to a folder in the tree: «—» for an empty folder or while the files are being read. */
export function treeCount(n: number | undefined): string {
  return n === undefined || n === 0 ? "—" : String(n);
}

/** Files per folder of the open subject (removed files and «Входящие» do not count). */
export function folderCounts(folders: readonly LibraryFolder[], materials: readonly LibraryMaterial[]): Map<string, number> {
  return new Map(folders.map((f) => [f.id, filesOfFolder(materials, f.id).length]));
}

export function folderCountText(n: number): string {
  const last = n % 10;
  const tens = n % 100;
  const word = tens >= 11 && tens <= 14 ? "файлов" : last === 1 ? "файл" : last >= 2 && last <= 4 ? "файла" : "файлов";
  return `${n} ${word}`;
}

// ---- The page of a subject (TASK-227) ----

const weekdayNames = ["пн", "вт", "ср", "чт", "пт", "сб", "вс"];

/** «пн» for 1 (Monday) to «вс» for 7; "" out of range. */
export function weekdayShort(weekday: number): string {
  return weekdayNames[weekday - 1] ?? "";
}

const kindNames: Readonly<Record<string, string>> = {
  lecture: "Лекции",
  seminar: "Семинары",
  practice: "Практики",
  lab: "Лабораторные",
};

/** «Лекции — вт, 10:30 · Семинары — пн, 12:10»; "" without slots. */
export function scheduleLine(slots: readonly SubjectSlot[]): string {
  const parts = slots
    .filter((s) => weekdayShort(s.weekday) !== "")
    .map((s) => `${kindNames[s.kind] ?? "Занятия"} — ${weekdayShort(s.weekday)}, ${s.start}`);
  return [...new Set(parts)].join(" · ");
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** «27.10» in the local zone; "" for an unreadable date. */
export function dayMonth(rfc3339: string): string {
  const d = new Date(rfc3339);
  return Number.isNaN(d.getTime()) ? "" : `${pad(d.getDate())}.${pad(d.getMonth() + 1)}`;
}

/** The weekday of a date in the local zone («вт»); "" for an unreadable date. */
export function weekdayOf(rfc3339: string): string {
  const d = new Date(rfc3339);
  return Number.isNaN(d.getTime()) ? "" : weekdayNames[(d.getDay() + 6) % 7];
}

function plural(n: number, one: string, few: string, many: string): string {
  const last = n % 10;
  const tens = n % 100;
  return tens >= 11 && tens <= 14 ? many : last === 1 ? one : last >= 2 && last <= 4 ? few : many;
}

export interface DaysChip {
  kind: ChipKind;
  label: string;
}

/** «сегодня», «завтра», «N дней»; an overdue deadline is «просрочено». */
export function daysChip(daysLeft: number): DaysChip {
  if (daysLeft < 0) {
    return { kind: "fail", label: "просрочено" };
  }
  if (daysLeft === 0) {
    return { kind: "wait", label: "сегодня" };
  }
  if (daysLeft === 1) {
    return { kind: "wait", label: "завтра" };
  }
  return { kind: "neutral", label: `${daysLeft} ${plural(daysLeft, "день", "дня", "дней")}` };
}

/** The title of a lesson row: its title, or the date when the recording has none. */
export function lessonTitle(l: SubjectLesson): string {
  if (l.title.trim() !== "") {
    return l.title;
  }
  const day = dayMonth(l.startedAt);
  return day === "" ? "Занятие" : `Занятие ${day}`;
}

/** «1:30:12» or «42:05»; "" for an unknown duration. */
export function durationText(seconds: number): string {
  if (seconds <= 0) {
    return "";
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

/** «вт, 13.10 · 1:30:12». */
export function lessonMeta(l: SubjectLesson): string {
  const when = [weekdayOf(l.startedAt), dayMonth(l.startedAt)].filter((x) => x !== "");
  return [when.join(", "), durationText(l.durationSeconds)].filter((x) => x !== "").join(" · ");
}

export interface LessonChip {
  kind: ChipKind;
  icon: IconName | null;
  label: string;
}

const lessonChips: Readonly<Record<LessonStatus, LessonChip>> = {
  recorded: { kind: "neutral", icon: "rec", label: "Записано" },
  transcribed: { kind: "neutral", icon: "doc", label: "Расшифровано" },
  summaryReady: { kind: "ok", icon: "ok", label: "Конспект готов" },
  failed: { kind: "fail", icon: "warn", label: "Ошибка" },
};

export function lessonChip(status: LessonStatus): LessonChip {
  return lessonChips[status];
}

/** The «новое» marks of one visit: folders holding fresh files, and those files. */
export interface FreshMarks {
  folders: ReadonlySet<string>;
  materials: ReadonlySet<string>;
}

export const noFresh: FreshMarks = { folders: new Set(), materials: new Set() };

export function freshMarks(o: SubjectOverview | null): FreshMarks {
  if (o === null || o.newMaterials.length === 0) {
    return noFresh;
  }
  return {
    folders: new Set(o.newMaterials.map((m) => m.folderId)),
    materials: new Set(o.newMaterials.map((m) => m.materialId)),
  };
}

export interface OverviewState {
  status: "idle" | "loading" | "ready" | "failed";
  overview: SubjectOverview | null;
  error: string;
}

export const idleOverview: OverviewState = { status: "idle", overview: null, error: "" };

/**
 * Loads the overview of a subject when it is opened and, once it is in hand, records the opening — exactly once per
 * `open`. A failed overview shows a line and records nothing; a failed record is silent. A superseded or closed
 * `open` is ignored, so a late reply of the previous subject never shows.
 */
export class SubjectOverviewFlow {
  private token = 0;

  constructor(
    private readonly api: Pick<LibraryApi, "subjectOverview" | "markSubjectOpened">,
    private readonly onChange: (s: OverviewState) => void,
  ) {}

  open(subjectId: string): void {
    const mine = ++this.token;
    this.onChange({ status: "loading", overview: null, error: "" });
    this.api.subjectOverview(subjectId).then(
      (overview) => {
        if (mine !== this.token) {
          return;
        }
        this.onChange({ status: "ready", overview, error: "" });
        void this.mark(subjectId);
      },
      () => {
        if (mine === this.token) {
          this.onChange({ status: "failed", overview: null, error: texts.overviewFailed });
        }
      },
    );
  }

  close(): void {
    this.token++;
  }

  private async mark(subjectId: string): Promise<void> {
    try {
      await this.api.markSubjectOpened(subjectId);
    } catch {
      // Silent: the next visit simply marks a little more.
    }
  }
}
