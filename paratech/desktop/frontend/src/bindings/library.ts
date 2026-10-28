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
  openMaterial: `${service}.OpenMaterial`,
  // TASK-225: the page of a subject.
  subjectOverview: `${service}.SubjectOverview`,
  markSubjectOpened: `${service}.MarkSubjectOpened`,
  // TASK-236, TASK-285: the teachers of a subject.
  setSubjectTeacher: `${service}.SetSubjectTeacher`,
  clearSubjectTeacher: `${service}.ClearSubjectTeacher`,
  // TASK-311: subjects from the schedule.
  scheduleSubjects: `${service}.ScheduleSubjects`,
  createFromSchedule: `${service}.CreateFromSchedule`,
  undoFromSchedule: `${service}.UndoFromSchedule`,
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
  "У предмета есть конспекты, в том числе удалённые: такой предмет удалить нельзя.",
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
  /** Passed back to setSubjectTeacher / clearSubjectTeacher; 0 when the reply has none. */
  version: number;
  /** The teacher of each role; null for a role without one. */
  teachers: LibraryTeachers;
}

/** A teacher of a subject: personal data typed by the student, text nodes only. */
export interface LibraryTeacher {
  name: string;
  email: string;
}

/** The roles of the teachers of a subject (TASK-285): лектор, семинары и практика, лабораторные. */
export type TeacherRole = "lecture" | "practice" | "lab";

export const teacherRoles: readonly TeacherRole[] = ["lecture", "practice", "lab"];

export type LibraryTeachers = Record<TeacherRole, LibraryTeacher | null>;

export type TeacherProblem = "" | "invalid" | "changed" | "not_found" | "failed";

export const teacherProblems: readonly TeacherProblem[] = ["", "invalid", "changed", "not_found", "failed"];

/** The reply of setSubjectTeacher / clearSubjectTeacher; the page owns the texts (TASK-238). */
export interface TeacherResult {
  problem: TeacherProblem;
  /** "name", "email" or "role" for "invalid"; "" otherwise. */
  field: "" | "name" | "email" | "role";
  /** Set only when problem is "". */
  subject: LibrarySubject | null;
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
    version: typeof r.version === "number" && Number.isSafeInteger(r.version) && r.version >= 0 ? r.version : 0,
    teachers: parseTeachers(r.teachers),
  };
}

/** The teachers of a subject; a missing or malformed reply is null in every role. */
export function parseTeachers(raw: unknown): LibraryTeachers {
  const r = typeof raw === "object" && raw !== null && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
  return { lecture: parseTeacher(r.lecture), practice: parseTeacher(r.practice), lab: parseTeacher(r.lab) };
}

/** A teacher of a subject; null for none or for a reply without both texts. */
export function parseTeacher(raw: unknown): LibraryTeacher | null {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return null;
  }
  const r = raw as Record<string, unknown>;
  const name = optStr(r, "name");
  const email = optStr(r, "email");
  return name !== "" && email !== "" ? { name, email } : null;
}

/** Reads the reply of a teacher call; an unknown or malformed reply is "failed" without a subject. */
export function parseTeacherResult(raw: unknown): TeacherResult {
  const failed: TeacherResult = { problem: "failed", field: "", subject: null };
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return failed;
  }
  const r = raw as Record<string, unknown>;
  const problem = r.problem;
  if (typeof problem !== "string" || !teacherProblems.includes(problem as TeacherProblem)) {
    return failed;
  }
  if (problem === "") {
    try {
      return { problem: "", field: "", subject: parseSubject(r.subject) };
    } catch {
      return failed;
    }
  }
  const field = problem === "invalid" && (r.field === "name" || r.field === "email" || r.field === "role") ? r.field : "";
  return { problem: problem as TeacherProblem, field, subject: null };
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

/** The outcome of «Открыть в Word / PowerPoint». */
export type OpenStatus = "opened" | "not_found" | "refused" | "failed";

export interface LibraryOpen {
  status: OpenStatus;
  /** The Russian text to show (never a path). */
  message: string;
}

/** The texts of OpenMaterial by status; test/libraryOpen.test.mjs pins them to library_open.go. */
export const openTexts: Readonly<Record<OpenStatus, string>> = {
  opened: "Файл открывается в программе по умолчанию.",
  not_found: "Файл не найден: возможно, его удалили.",
  refused: "Этот файл нельзя открыть отсюда.",
  failed: "Не удалось открыть файл. Попробуйте ещё раз.",
};

function isOpenStatus(v: unknown): v is OpenStatus {
  return typeof v === "string" && Object.prototype.hasOwnProperty.call(openTexts, v);
}

/** Reads the reply of OpenMaterial; an unknown reply is "failed". The text shown is always one of openTexts. */
export function parseOpen(raw: unknown): LibraryOpen {
  const r = typeof raw === "object" && raw !== null && !Array.isArray(raw) ? (raw as Record<string, unknown>) : {};
  const status: OpenStatus = isOpenStatus(r.status) ? r.status : "failed";
  return { status, message: openTexts[status] };
}

/** A line of «Ближайшее». Title is model or student text: a text node only. */
export interface SubjectDeadline {
  assignmentId: string;
  title: string;
  /** RFC 3339. */
  due: string;
  /** Calendar days from today; 0 is today. */
  daysLeft: number;
}

/** A weekly place of the subject's lessons in the schedule; weekday 1 (Monday) to 7 (Sunday), times "HH:MM". */
export interface SubjectSlot {
  kind: string;
  weekday: number;
  start: string;
  end: string;
}

export type LessonStatus = "recorded" | "transcribed" | "summaryReady" | "failed";

export const lessonStatuses: readonly LessonStatus[] = ["recorded", "transcribed", "summaryReady", "failed"];

export interface SubjectLesson {
  recordingId: string;
  /** "" for an ad-hoc recording (the page names it by its date). Text node only. */
  title: string;
  kind: string;
  startedAt: string;
  /** 0 when unknown. */
  durationSeconds: number;
  status: LessonStatus;
  summaryNew: boolean;
}

export interface SubjectNewMaterial {
  materialId: string;
  folderId: string;
}

export interface SubjectOverview {
  subjectId: string;
  name: string;
  openedAt: string;
  deadlines: SubjectDeadline[];
  schedule: SubjectSlot[];
  lessons: SubjectLesson[];
  newMaterials: SubjectNewMaterial[];
}

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a count`);
  }
  return v;
}

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

export function parseDeadline(raw: unknown): SubjectDeadline {
  const w = "deadline";
  const r = record(raw, w);
  const days = r.daysLeft;
  if (typeof days !== "number" || !Number.isInteger(days)) {
    throw new Error(`${w}: daysLeft is not an integer`);
  }
  return { assignmentId: str(r, "assignmentId", w), title: str(r, "title", w), due: str(r, "due", w), daysLeft: days };
}

export function parseSlot(raw: unknown): SubjectSlot {
  const w = "slot";
  const r = record(raw, w);
  const weekday = count(r, "weekday", w);
  if (weekday < 1 || weekday > 7) {
    throw new Error(`${w}: weekday is out of range`);
  }
  return { kind: str(r, "kind", w), weekday, start: str(r, "start", w), end: str(r, "end", w) };
}

export function parseLesson(raw: unknown): SubjectLesson {
  const w = "lesson";
  const r = record(raw, w);
  const status = r.status;
  if (typeof status !== "string" || !lessonStatuses.includes(status as LessonStatus)) {
    throw new Error(`${w}: status is not one of ${lessonStatuses.join(", ")}`);
  }
  return {
    recordingId: str(r, "recordingId", w),
    title: str(r, "title", w),
    kind: optStr(r, "kind"),
    startedAt: str(r, "startedAt", w),
    durationSeconds: count(r, "durationSeconds", w),
    status: status as LessonStatus,
    summaryNew: bool(r, "summaryNew", w),
  };
}

function parseNewMaterial(raw: unknown): SubjectNewMaterial {
  const w = "new material";
  const r = record(raw, w);
  return { materialId: str(r, "materialId", w), folderId: str(r, "folderId", w) };
}

/** Checks the shape of the page data; a malformed reply is an error, not a default. */
export function parseOverview(raw: unknown): SubjectOverview {
  const w = "subject overview";
  const r = record(raw, w);
  return {
    subjectId: str(r, "subjectId", w),
    name: str(r, "name", w),
    openedAt: optStr(r, "openedAt"),
    deadlines: list(r.deadlines, w, parseDeadline),
    schedule: list(r.schedule, w, parseSlot),
    lessons: list(r.lessons, w, parseLesson),
    newMaterials: list(r.newMaterials, w, parseNewMaterial),
  };
}

export interface LibraryApi {
  /** The data of the page of a subject; it changes nothing (see markSubjectOpened). */
  subjectOverview(subjectId: string): Promise<SubjectOverview>;
  /** Records the opening once the overview is shown, so the next visit marks only what is newer. */
  markSubjectOpened(subjectId: string): Promise<void>;
  subjects(): Promise<LibrarySubject[]>;
  /** semester is a code or "" for the current one. */
  createSubject(name: string, semester: string): Promise<LibrarySubject>;
  createFolder(subjectId: string, name: string): Promise<LibraryFolder>;
  /** The files of a subject, or of «Входящие» for "". */
  materials(subjectId: string): Promise<LibraryMaterial[]>;
  /** «Добавить файл»: the native open dialog. */
  importFile(): Promise<LibraryImport>;
  /** «Открыть в Word / PowerPoint»: only the ID crosses; a failed call is "failed". */
  openMaterial(id: string): Promise<LibraryOpen>;
  /** Sets the teacher of role while the subject is at version (the one shown); a failed call is "failed". */
  setSubjectTeacher(subjectId: string, version: number, role: TeacherRole, name: string, email: string): Promise<TeacherResult>;
  /** Removes the teacher of role while the subject is at version; a failed call is "failed". */
  clearSubjectTeacher(subjectId: string, version: number, role: TeacherRole): Promise<TeacherResult>;
}

export function libraryApi(call: CallByName): LibraryApi {
  return {
    subjectOverview: async (subjectId) => parseOverview(await call(methods.subjectOverview, subjectId)),
    markSubjectOpened: async (subjectId) => {
      await call(methods.markSubjectOpened, subjectId);
    },
    subjects: async () => list(await call(methods.subjects), "subjects", parseSubject),
    createSubject: async (name, semester) => parseSubject(await call(methods.createSubject, name, [], semester)),
    createFolder: async (subjectId, name) => parseFolder(await call(methods.createFolder, subjectId, name)),
    materials: async (subjectId) => list(await call(methods.materials, subjectId), "materials", parseMaterial),
    importFile: async () => parseImport(await call(methods.importFile)),
    openMaterial: async (id) => {
      try {
        return parseOpen(await call(methods.openMaterial, id));
      } catch {
        return parseOpen(null);
      }
    },
    setSubjectTeacher: async (subjectId, version, role, name, email) => {
      try {
        return parseTeacherResult(await call(methods.setSubjectTeacher, subjectId, version, role, name, email));
      } catch {
        return parseTeacherResult(null);
      }
    },
    clearSubjectTeacher: async (subjectId, version, role) => {
      try {
        return parseTeacherResult(await call(methods.clearSubjectTeacher, subjectId, version, role));
      } catch {
        return parseTeacherResult(null);
      }
    },
  };
}

// Subjects from the schedule (TASK-311, ADR-0033). The names, spellings and aliases are schedule text (untrusted):
// plain text nodes only, never logged and never put into an error message. The parsers are strict: any unknown code,
// wrong type, missing field or null list is an error (the page shows its generic text), not a default.

export type ScheduleKind = "lecture" | "seminar" | "lab" | "unknown";

export const scheduleKinds: readonly ScheduleKind[] = ["lecture", "seminar", "lab", "unknown"];

/** Kind words for a count of lessons; «пары» is a lesson of an unknown kind. */
export const scheduleKindWords: Readonly<Record<ScheduleKind, string>> = {
  lecture: "лекции",
  seminar: "семинары",
  lab: "лабораторные",
  unknown: "пары",
};

export type ScheduleLink = "same" | "abbreviation" | "typo";

export const scheduleLinks: readonly ScheduleLink[] = ["same", "abbreviation", "typo"];

export type ScheduleGroupAction = "create" | "alias_of";

export const scheduleGroupActions: readonly ScheduleGroupAction[] = ["create", "alias_of"];

export type ScheduleChoiceAction = "create" | "alias_of" | "skip";

export type ScheduleMemberAction = "keep" | "separate" | "skip";

export type ScheduleSkipReason = "name_taken" | "too_many_aliases" | "subject_gone";

export const scheduleSkipReasons: readonly ScheduleSkipReason[] = ["name_taken", "too_many_aliases", "subject_gone"];

export type ScheduleKeptReason = "has_files" | "has_notes";

export const scheduleKeptReasons: readonly ScheduleKeptReason[] = ["has_files", "has_notes"];

export type ScheduleListProblem = "" | "unavailable";

export type ScheduleCreateProblem = "" | "stale" | "invalid" | "name_taken" | "busy" | "unavailable";

export type ScheduleUndoProblem = "" | "invalid" | "busy" | "unavailable";

export const scheduleListProblems: readonly ScheduleListProblem[] = ["", "unavailable"];

export const scheduleCreateProblems: readonly ScheduleCreateProblem[] = ["", "stale", "invalid", "name_taken", "busy", "unavailable"];

export const scheduleUndoProblems: readonly ScheduleUndoProblem[] = ["", "invalid", "busy", "unavailable"];

export interface ScheduleKindCount {
  kind: ScheduleKind;
  lessons: number;
}

/** An obvious group inside a proposed group; "abbreviation" and "typo" links are the doubtful ones. */
export interface ScheduleMember {
  key: string;
  name: string;
  spellings: string[];
  kinds: ScheduleKindCount[];
  lessons: number;
  recordings: number;
  doubtful: boolean;
  link: ScheduleLink;
}

/** A proposed subject; action is the default: a new subject «name» or the spellings added to the subject subjectId. */
export interface ScheduleGroup {
  key: string;
  name: string;
  aliases: string[];
  kinds: ScheduleKindCount[];
  lessons: number;
  recordings: number;
  doubtful: boolean;
  members: ScheduleMember[];
  action: ScheduleGroupAction;
  /** "" for a group to create. */
  subjectId: string;
  subjectName: string;
}

export interface ScheduleSubjectsReply {
  /** Goes back to createFromSchedule and undoFromSchedule. */
  token: string;
  groups: ScheduleGroup[];
  /** The least used strings left out. */
  omitted: number;
  problem: ScheduleListProblem;
}

export interface ScheduleMemberChoice {
  key: string;
  action: ScheduleMemberAction;
  /** A new name for "separate"; "" keeps the proposed one. */
  name: string;
}

export interface ScheduleChoice {
  group: string;
  action: ScheduleChoiceAction;
  /** A new name for "create"; "" keeps the proposed one. */
  name: string;
  members: ScheduleMemberChoice[];
}

export interface ScheduleCreated {
  id: string;
  key: string;
  name: string;
}

export interface ScheduleAliased {
  subjectId: string;
  aliases: string[];
}

export interface ScheduleSkipped {
  key: string;
  subjectId: string;
  reason: ScheduleSkipReason;
}

/** With a problem the lists still hold what was done before it; undoFromSchedule takes that back. */
export interface CreateFromScheduleReply {
  created: ScheduleCreated[];
  aliased: ScheduleAliased[];
  skipped: ScheduleSkipped[];
  problem: ScheduleCreateProblem;
}

export interface ScheduleKept {
  id: string;
  name: string;
  reason: ScheduleKeptReason;
}

export interface ScheduleKeptAliases {
  subjectId: string;
  aliases: string[];
  reason: "stale";
}

export interface UndoFromScheduleReply {
  /** IDs of the removed subjects. */
  removed: string[];
  kept: ScheduleKept[];
  unaliased: ScheduleAliased[];
  keptAliases: ScheduleKeptAliases[];
  problem: ScheduleUndoProblem;
}

/** A required list: unlike list(), null or a missing list is an error. */
function strictList<T>(r: Record<string, unknown>, key: string, what: string, parse: (item: unknown) => T): T[] {
  const v = r[key];
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

function strings(r: Record<string, unknown>, key: string, what: string): string[] {
  return strictList(r, key, what, (s) => {
    if (typeof s !== "string") {
      throw new Error(`${what}: ${key} holds a non-string`);
    }
    return s;
  });
}

function oneOf<T extends string>(r: Record<string, unknown>, key: string, what: string, allowed: readonly T[]): T {
  const v = r[key];
  if (typeof v !== "string" || !allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not a known code`);
  }
  return v as T;
}

function parseKindCount(raw: unknown): ScheduleKindCount {
  const w = "schedule kind";
  const r = record(raw, w);
  return { kind: oneOf(r, "kind", w, scheduleKinds), lessons: count(r, "lessons", w) };
}

export function parseScheduleMember(raw: unknown): ScheduleMember {
  const w = "schedule member";
  const r = record(raw, w);
  return {
    key: str(r, "key", w),
    name: str(r, "name", w),
    spellings: strings(r, "spellings", w),
    kinds: strictList(r, "kinds", w, parseKindCount),
    lessons: count(r, "lessons", w),
    recordings: count(r, "recordings", w),
    doubtful: bool(r, "doubtful", w),
    link: oneOf(r, "link", w, scheduleLinks),
  };
}

export function parseScheduleGroup(raw: unknown): ScheduleGroup {
  const w = "schedule group";
  const r = record(raw, w);
  return {
    key: str(r, "key", w),
    name: str(r, "name", w),
    aliases: strings(r, "aliases", w),
    kinds: strictList(r, "kinds", w, parseKindCount),
    lessons: count(r, "lessons", w),
    recordings: count(r, "recordings", w),
    doubtful: bool(r, "doubtful", w),
    members: strictList(r, "members", w, parseScheduleMember),
    action: oneOf(r, "action", w, scheduleGroupActions),
    subjectId: str(r, "subjectId", w),
    subjectName: str(r, "subjectName", w),
  };
}

export function parseScheduleSubjects(raw: unknown): ScheduleSubjectsReply {
  const w = "schedule subjects";
  const r = record(raw, w);
  return {
    token: str(r, "token", w),
    groups: strictList(r, "groups", w, parseScheduleGroup),
    omitted: count(r, "omitted", w),
    problem: oneOf(r, "problem", w, scheduleListProblems),
  };
}

function parseAliased(raw: unknown): ScheduleAliased {
  const w = "schedule aliases";
  const r = record(raw, w);
  return { subjectId: str(r, "subjectId", w), aliases: strings(r, "aliases", w) };
}

export function parseCreateFromSchedule(raw: unknown): CreateFromScheduleReply {
  const w = "schedule create";
  const r = record(raw, w);
  return {
    created: strictList(r, "created", w, (c) => {
      const cw = "schedule created";
      const cr = record(c, cw);
      return { id: str(cr, "id", cw), key: str(cr, "key", cw), name: str(cr, "name", cw) };
    }),
    aliased: strictList(r, "aliased", w, parseAliased),
    skipped: strictList(r, "skipped", w, (s) => {
      const sw = "schedule skipped";
      const sr = record(s, sw);
      return { key: str(sr, "key", sw), subjectId: str(sr, "subjectId", sw), reason: oneOf(sr, "reason", sw, scheduleSkipReasons) };
    }),
    problem: oneOf(r, "problem", w, scheduleCreateProblems),
  };
}

export function parseUndoFromSchedule(raw: unknown): UndoFromScheduleReply {
  const w = "schedule undo";
  const r = record(raw, w);
  return {
    removed: strings(r, "removed", w),
    kept: strictList(r, "kept", w, (k) => {
      const kw = "schedule kept";
      const kr = record(k, kw);
      return { id: str(kr, "id", kw), name: str(kr, "name", kw), reason: oneOf(kr, "reason", kw, scheduleKeptReasons) };
    }),
    unaliased: strictList(r, "unaliased", w, parseAliased),
    keptAliases: strictList(r, "keptAliases", w, (k) => {
      const kw = "schedule kept aliases";
      const kr = record(k, kw);
      return {
        subjectId: str(kr, "subjectId", kw),
        aliases: strings(kr, "aliases", kw),
        reason: oneOf(kr, "reason", kw, ["stale"] as const),
      };
    }),
    problem: oneOf(r, "problem", w, scheduleUndoProblems),
  };
}

/** The proposal of subjects from the schedule; it changes nothing. A malformed reply rejects. */
export async function scheduleSubjects(call: CallByName): Promise<ScheduleSubjectsReply> {
  return parseScheduleSubjects(await call(methods.scheduleSubjects));
}

/** Applies the choices to the proposal of token (the one «Да»); the choices go to the core as they are. */
export async function createFromSchedule(call: CallByName, token: string, choices: ScheduleChoice[]): Promise<CreateFromScheduleReply> {
  return parseCreateFromSchedule(await call(methods.createFromSchedule, token, choices));
}

/** «Отменить всё»: only the token crosses; the core keeps what it did and takes that back. */
export async function undoFromSchedule(call: CallByName, token: string): Promise<UndoFromScheduleReply> {
  return parseUndoFromSchedule(await call(methods.undoFromSchedule, token));
}
