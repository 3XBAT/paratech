// View logic of «Предметы»: semesters, folders and file lines. Pure, no React. Names are typed or imported text and are
// passed through untouched (the page renders them as text nodes).

import { type KnowledgeBaseApi, type KnowledgeFolderLesson, type KnowledgeLesson, lessonFolderSystems } from "./bindings/knowledgeBase.js";
import type { LibraryApi, LibraryFolder, LibraryMaterial, LibrarySubject, SubjectOverview, SubjectSlot } from "./bindings/library.js";
import { durationText } from "./durationEstimate.js";
import type { ChipKind } from "./ui/StatusChip.js";

export const texts = {
  openAssignment: "Открыть задание",
  fresh: "новое",
  overviewFailed: "Не удалось загрузить сведения о предмете.",
  title: "Предметы",
  addSubject: "Добавить предмет",
  addFolder: "Новая папка",
  addFile: "Добавить файл",
  fileAdded: "Файл добавлен.",
  fileExisting: "Файл уже есть в списке.",
  knowledge: "База знаний",
  subjectName: "Название предмета",
  folderName: "Название папки",
  create: "Создать",
  cancel: "Отмена",
  loading: "Загрузка…",
  loadFailed: "Не удалось загрузить предметы.",
  filesFailed: "Не удалось загрузить файлы предмета.",
  emptyTitle: "Предметов пока нет",
  emptyText: "Создайте первый предмет: в нём появятся папки для лекций, семинаров, материалов и заданий.",
  emptyFolders: "Пусто:",
  folderEmpty: "Пока пусто",
  inbox: "Входящие",
  inboxHint:
    "Файлы, которые ещё не разложены по предметам. Разложить можно кнопкой справа или перетащив значок файла на предмет слева.",
  folders: "Папки",
  files: "Файлы",
  aliases: "Также",
} as const;

/** The one muted line under the name of a subject: «Осень 2026 · Также: матан, МА»; "" when both are empty. */
export function subjectSubLine(s: Pick<LibrarySubject, "semester" | "semesterLabel" | "aliases">): string {
  const parts = [s.semesterLabel || s.semester];
  if (s.aliases.length > 0) {
    parts.push(`${texts.aliases}: ${s.aliases.join(", ")}`);
  }
  return parts.filter((p) => p !== "").join(" · ");
}

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

// ---- Recorded lessons in the folders of their kind (TASK-430, ADR-0034 amendment 2026-11-10) ----

/**
 * The recorded lessons of the open subject as the core placed them (see knowledgeBase.folderLessons). `null` is
 * «not known»: not read yet or the read failed. Where a parameter below takes it, `[]` means «known: none» and is the
 * default, so a caller that knows of no link rows (a test, an older screen) sees only files.
 */
export type FolderLessons = readonly KnowledgeFolderLesson[] | null;

/** The system folders that hold link rows of lessons («Лекции», «Семинары», «Лабораторные»). */
export function isLessonFolder(f: Pick<LibraryFolder, "system">): boolean {
  return (lessonFolderSystems as readonly string[]).includes(f.system);
}

/** The link rows of one folder, in the core's order (newest first). */
export function lessonsOfFolder(lessons: FolderLessons | undefined, folderId: string): KnowledgeFolderLesson[] {
  return (lessons ?? []).filter((l) => l.folderId === folderId);
}

/**
 * Files and link rows per folder of the open subject (removed files and «Входящие» do not count). While the link rows
 * are unknown (`null`) the three lesson folders have no entry at all: neither «0» nor «—» may claim them empty, and a
 * number that leaves the lessons out would be wrong.
 */
export function folderCounts(folders: readonly LibraryFolder[], materials: readonly LibraryMaterial[], lessons: FolderLessons = []): Map<string, number> {
  const counts = new Map<string, number>();
  for (const f of folders) {
    if (lessons === null && isLessonFolder(f)) {
      continue;
    }
    counts.set(f.id, filesOfFolder(materials, f.id).length + lessonsOfFolder(lessons, f.id).length);
  }
  return counts;
}

/**
 * The folders of the page of a subject split for the screen (TASK-347): the ones with files or link rows (or whose
 * contents are not known yet: no «пусто» claim then) get a section each; the empty ones fold into one line «Пусто: ...».
 * Order is kept. `keepOpen` holds the ids of folders that stay sections while empty (a folder just created: it must not
 * vanish into the line); they say «Пока пусто» themselves. While `lessons` is unknown (`null`) the three lesson folders
 * are never called empty.
 */
export function foldEmptyFolders(
  folders: readonly LibraryFolder[],
  materials: readonly LibraryMaterial[] | null,
  keepOpen: ReadonlySet<string> = new Set(),
  lessons: FolderLessons = [],
): { shown: LibraryFolder[]; empty: LibraryFolder[] } {
  const ordered = orderedFolders(folders);
  if (materials === null) {
    return { shown: ordered, empty: [] };
  }
  const isEmpty = (f: LibraryFolder) =>
    filesOfFolder(materials, f.id).length === 0 &&
    lessonsOfFolder(lessons, f.id).length === 0 &&
    !(lessons === null && isLessonFolder(f)) &&
    !keepOpen.has(f.id);
  return { shown: ordered.filter((f) => !isEmpty(f)), empty: ordered.filter(isEmpty) };
}

/** «1 пара», «2 пары», «5 пар». */
export function pairCountText(n: number): string {
  return `${n} ${plural(n, "пара", "пары", "пар")}`;
}

/** After the name in the head of a section: «2 пары · 1 файл»; only the non-zero parts, "" when both are zero. */
export function folderHeadCount(pairs: number, files: number): string {
  return [pairs > 0 ? pairCountText(pairs) : "", files > 0 ? folderCountText(files) : ""].filter((p) => p !== "").join(" · ");
}

export const folderLessonTexts = {
  failed: "Не удалось загрузить пары предмета.",
  retry: "Повторить",
  loading: "Загрузка пар…",
  openSummary: "Открыть конспект",
  openRecording: "Открыть запись",
} as const;

/** The title of a link row: the title of the recording, else «Запись 21.10» (just «Запись» when the date is unknown). */
export function folderLessonTitle(l: Pick<KnowledgeLesson, "title" | "startedAt">): string {
  const t = l.title.trim();
  if (t !== "") {
    return t;
  }
  const day = dayMonth(l.startedAt);
  return day === "" ? "Запись" : `Запись ${day}`;
}

/** The muted line of a link row: «21.10 · 1 ч 20 мин»; the parts that are unknown are left out. */
export function folderLessonMeta(l: Pick<KnowledgeLesson, "startedAt" | "durationSeconds">): string {
  return [dayMonth(l.startedAt), l.durationSeconds > 0 ? durationText(l.durationSeconds) : ""].filter((p) => p !== "").join(" · ");
}

/** What the page knows of the link rows of a subject; `subjectId` is the subject the rows were read for. */
export interface FolderLessonsState {
  subjectId: string;
  rows: FolderLessons;
  /** The last read failed: the page says so with «Повторить»; `rows` is null then. */
  failed: boolean;
}

export const noFolderLessons: FolderLessonsState = { subjectId: "", rows: null, failed: false };

/** The state as far as it belongs to `subjectId`: what was read for another subject is «not known yet», never shown. */
export function folderLessonsFor(state: FolderLessonsState, subjectId: string): FolderLessonsState {
  return state.subjectId === subjectId ? state : { subjectId, rows: null, failed: false };
}

/**
 * Reads the link rows of one subject. `open` starts with «not known» and reads; `reread` reads again and keeps what is
 * shown meanwhile (a change on the page, a lesson screen left); a failed read makes the rows unknown again and says so.
 * A reply that came after a newer read, or after `close`, is dropped, so a late reply of the previous subject never shows.
 */
export class FolderLessonsFlow {
  private token = 0;

  constructor(
    private readonly api: Pick<KnowledgeBaseApi, "folderLessons">,
    readonly subjectId: string,
    private readonly onChange: (s: FolderLessonsState) => void,
  ) {}

  open(): void {
    this.onChange({ subjectId: this.subjectId, rows: null, failed: false });
    this.read(++this.token);
  }

  reread(): void {
    this.read(++this.token);
  }

  close(): void {
    this.token++;
  }

  private read(mine: number): void {
    this.api.folderLessons(this.subjectId).then(
      (rows) => {
        if (mine === this.token) {
          this.onChange({ subjectId: this.subjectId, rows, failed: false });
        }
      },
      () => {
        if (mine === this.token) {
          this.onChange({ subjectId: this.subjectId, rows: null, failed: true });
        }
      },
    );
  }
}

/** «Семинары»: Файл добавлен. — names the folder in the result of one that has no section of its own (it sits in the «Пусто» line). */
export function inFolderText(folder: string, text: string): string {
  return text === "" ? "" : `«${folder}»: ${text}`;
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
    .map((s) => `${kindNames[s.kind] ?? "Пары"} — ${weekdayShort(s.weekday)}, ${s.start}`);
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
  /** The last re-read after a change on the page failed: `overview` is the previous one, still shown. */
  rereadFailed?: boolean;
}

export const idleOverview: OverviewState = { status: "idle", overview: null, error: "" };

/**
 * Loads the overview of a subject when it is opened and, once it is in hand, records the opening — exactly once per
 * `open`. A failed overview shows a line and records nothing; a failed record is silent. A superseded or closed
 * `open` is ignored, so a late reply of the previous subject never shows.
 */
export class SubjectOverviewFlow {
  private token = 0;
  private refreshes = 0;
  /** The overview of this opening, once read: a failed re-read keeps it. */
  private loaded: SubjectOverview | null = null;

  constructor(
    private readonly api: Pick<LibraryApi, "subjectOverview" | "markSubjectOpened">,
    private readonly onChange: (s: OverviewState) => void,
  ) {}

  open(subjectId: string): void {
    const mine = ++this.token;
    this.loaded = null;
    this.onChange({ status: "loading", overview: null, error: "" });
    this.api.subjectOverview(subjectId).then(
      (overview) => {
        if (mine !== this.token) {
          return;
        }
        this.loaded = overview;
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

  /**
   * Reads the overview again after a change made on the page (an assignment added): no «Загрузка…», no new
   * opening is recorded. A failed read keeps what is shown and says so (`rereadFailed`), so the change is never lost
   * silently. A late reply of a superseded read never shows.
   */
  refresh(subjectId: string): void {
    const mine = ++this.refreshes;
    const opened = this.token;
    this.api.subjectOverview(subjectId).then(
      (overview) => {
        if (mine === this.refreshes && opened === this.token) {
          this.loaded = overview;
          this.onChange({ status: "ready", overview, error: "" });
        }
      },
      () => {
        if (mine === this.refreshes && opened === this.token) {
          this.onChange(
            this.loaded === null
              ? { status: "failed", overview: null, error: texts.overviewFailed }
              : { status: "ready", overview: this.loaded, error: "", rereadFailed: true },
          );
        }
      },
    );
  }

  /** «Повторить»: a first reading when there is nothing yet (it failed), else the re-read that failed. */
  retry(subjectId: string): void {
    if (this.loaded === null) {
      this.open(subjectId);
    } else {
      this.refresh(subjectId);
    }
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
