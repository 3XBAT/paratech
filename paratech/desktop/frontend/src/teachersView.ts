// View logic of the block «Преподаватели» on the page of a subject (ADR-0030, TASK-238): three rows, a small form per
// row, «Как у лектора / семинариста» for the labs, and the flow of the buttons. Pure, no React. Names and addresses are
// typed text and personal data: passed through untouched, rendered as text nodes only, never logged or made a link.
// The core owns the rules (lengths, address syntax, versions); the page shows its codes in Russian.

import {
  type LibraryApi,
  type LibrarySubject,
  type LibraryTeachers,
  type TeacherResult,
  type TeacherRole,
  teacherRoles,
} from "./bindings/library.js";

export const teacherTexts = {
  title: "Преподаватели",
  notSet: "Не указан",
  set: "Указать",
  change: "Изменить",
  save: "Сохранить",
  saving: "Сохранение…",
  cancel: "Отмена",
  clear: "Убрать",
  clearing: "Удаление…",
  name: "Имя",
  email: "Почта",
  copyGroup: "Заполнить как у другого преподавателя",
  copyLecture: "Как у лектора",
  copyPractice: "Как у семинариста",
  lectureMissing: "лектор не указан",
  practiceMissing: "семинарист не указан",
  hint: "Почта преподавателя нужна, чтобы отправлять ему домашние задания.",
  nameEmpty: "Укажите имя преподавателя.",
  emailEmpty: "Укажите почту преподавателя.",
  nameInvalid: "Имя: от 1 до 200 символов, в одну строку.",
  emailInvalid: "Почта указана неверно: нужен адрес вида имя@домен.",
  changed: "Данные предмета изменились, список обновлён. Проверьте и повторите.",
  notFound: "Предмет не найден: обновите список.",
  saveFailed: "Не удалось сохранить. Данные обновлены, можно повторить.",
  clearFailed: "Не удалось убрать. Данные обновлены, можно повторить.",
  rereadFailed: "Не удалось обновить данные предмета. Откройте страницу заново.",
  saved: "Преподаватель сохранён.",
  cleared: "Преподаватель убран.",
} as const;

/** The titles of the three rows. */
export const rowLabels: Readonly<Record<TeacherRole, string>> = {
  lecture: "Лектор",
  practice: "Семинары и практика",
  lab: "Лабораторные",
};

/** The DOM id of the button that opens the form of a role: the focus returns to it when the form closes. */
export function triggerId(role: TeacherRole): string {
  return `teacher-open-${role}`;
}

export interface TeacherRow {
  role: TeacherRole;
  label: string;
  filled: boolean;
  /** "" when not filled. */
  name: string;
  email: string;
  /** «Указать» for an empty row, «Изменить» for a filled one. */
  action: string;
  /** The accessible name of the button: «Изменить: лектор». */
  actionLabel: string;
}

/** The three rows in the fixed order: лектор, семинары и практика, лабораторные. */
export function teacherRows(t: LibraryTeachers): TeacherRow[] {
  return teacherRoles.map((role) => {
    const teacher = t[role];
    const action = teacher === null ? teacherTexts.set : teacherTexts.change;
    const label = rowLabels[role];
    return {
      role,
      label,
      filled: teacher !== null,
      name: teacher?.name ?? "",
      email: teacher?.email ?? "",
      action,
      actionLabel: `${action}: ${label.toLowerCase()}`,
    };
  });
}

/** No teacher in any role: the block adds a one-line hint. */
export function noTeachers(t: LibraryTeachers): boolean {
  return teacherRoles.every((r) => t[r] === null);
}

export interface TeacherForm {
  role: TeacherRole;
  name: string;
  email: string;
}

/** The form of a role: filled with the teacher it has, empty otherwise. */
export function formFor(role: TeacherRole, teachers: LibraryTeachers): TeacherForm {
  const t = teachers[role];
  return { role, name: t?.name ?? "", email: t?.email ?? "" };
}

/** The rows a form can copy from: only the labs copy, from the lecturer and the seminar teacher. */
export type CopySource = "lecture" | "practice";

export function copySources(role: TeacherRole): CopySource[] {
  return role === "lab" ? ["lecture", "practice"] : [];
}

export const copyLabels: Readonly<Record<CopySource, { button: string; missing: string }>> = {
  lecture: { button: teacherTexts.copyLecture, missing: teacherTexts.lectureMissing },
  practice: { button: teacherTexts.copyPractice, missing: teacherTexts.practiceMissing },
};

/** «Как у лектора» works only while that row has a teacher. */
export function canCopy(source: CopySource, teachers: LibraryTeachers): boolean {
  return teachers[source] !== null;
}

/** A one-time copy of the name and the address of the source row into the form: a copy, not a link. */
export function copyInto(form: TeacherForm, source: CopySource, teachers: LibraryTeachers): TeacherForm {
  const t = teachers[source];
  return t === null ? form : { ...form, name: t.name, email: t.email };
}

export interface TeacherErrors {
  name: string;
  email: string;
  /** Not tied to a field: shown under the block. */
  general: string;
}

export const noErrors: TeacherErrors = { name: "", email: "", general: "" };

export interface TeacherState {
  /** The open form; at most one. */
  form: TeacherForm | null;
  /** A call runs: the buttons are off. */
  busy: "" | "save" | "clear";
  errors: TeacherErrors;
  /** The quiet result of the last call («Преподаватель сохранён.»). */
  note: string;
  /** The row whose button takes the focus once its form closed. */
  focusRole: TeacherRole | "";
}

export const idleTeacher: TeacherState = { form: null, busy: "", errors: noErrors, note: "", focusRole: "" };

/** The error of the field the core refused; a role or an unknown field is not the student's to fix. */
function fieldErrors(field: TeacherResult["field"], general: string): TeacherErrors {
  if (field === "name") {
    return { ...noErrors, name: teacherTexts.nameInvalid };
  }
  if (field === "email") {
    return { ...noErrors, email: teacherTexts.emailInvalid };
  }
  return { ...noErrors, general };
}

/** What the flow needs from the page that owns the list of subjects. */
export interface TeacherHost {
  /** The subject shown, with the version and the teachers of the last read; null when there is none. */
  subject(): LibrarySubject | null;
  /** The subject as a call returned it. */
  replace(subject: LibrarySubject): void;
  /** Reads the list of subjects again (Subjects(), not the overview); false when the read failed. */
  reread(): Promise<boolean>;
}

/**
 * The buttons of the block. One call at a time (a second click while one runs does nothing), and the version sent is
 * always the one of the last read, so a stale save is refused by the core («changed») and the list is read again.
 * A call that ends after reset() (another subject opened) changes the list but no longer the form.
 */
export class TeacherFlow {
  private current: TeacherState = idleTeacher;
  private pending = false;
  private disposed = false;
  private epoch = 0;

  constructor(
    private readonly api: Pick<LibraryApi, "setSubjectTeacher" | "clearSubjectTeacher">,
    private readonly host: TeacherHost,
    private readonly onChange: (s: TeacherState) => void,
  ) {}

  get state(): TeacherState {
    return this.current;
  }

  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(patch: Partial<TeacherState>): void {
    this.current = { ...this.current, ...patch };
    if (!this.disposed) {
      this.onChange(this.current);
    }
  }

  /** Another subject (or the list) is shown: the form, errors and note of the previous one go away. */
  reset(): void {
    this.epoch++;
    this.set(idleTeacher);
  }

  /** «Указать» / «Изменить»: opens the form of the role (instead of an open one). */
  open(role: TeacherRole): void {
    const subject = this.host.subject();
    if (this.pending || subject === null) {
      return;
    }
    this.set({ form: formFor(role, subject.teachers), errors: noErrors, note: "", focusRole: "" });
  }

  cancel(): void {
    const form = this.current.form;
    if (this.pending || form === null) {
      return;
    }
    this.set({ form: null, errors: noErrors, focusRole: form.role });
  }

  /** The student typed: the error of that field goes away. */
  type(patch: Partial<Pick<TeacherForm, "name" | "email">>): void {
    const form = this.current.form;
    if (form === null) {
      return;
    }
    const errors = {
      name: patch.name === undefined ? this.current.errors.name : "",
      email: patch.email === undefined ? this.current.errors.email : "",
      general: "",
    };
    this.set({ form: { ...form, ...patch }, errors });
  }

  /** «Как у лектора» / «Как у семинариста»: once, into the fields; saving is the usual «Сохранить». */
  copy(source: CopySource): void {
    const form = this.current.form;
    const subject = this.host.subject();
    if (this.pending || form === null || subject === null || !copySources(form.role).includes(source)) {
      return;
    }
    this.set({ form: copyInto(form, source, subject.teachers), errors: noErrors });
  }

  async save(): Promise<void> {
    const form = this.current.form;
    const subject = this.host.subject();
    if (this.pending || form === null || subject === null) {
      return;
    }
    const name = form.name.trim();
    const email = form.email.trim();
    if (name === "" || email === "") {
      this.set({ errors: { name: name === "" ? teacherTexts.nameEmpty : "", email: email === "" ? teacherTexts.emailEmpty : "", general: "" } });
      return;
    }
    await this.run("save", form.role, () => this.api.setSubjectTeacher(subject.id, subject.version, form.role, name, email));
  }

  /** «Убрать»: offered only for a filled role. */
  async clear(): Promise<void> {
    const form = this.current.form;
    const subject = this.host.subject();
    if (this.pending || form === null || subject === null || subject.teachers[form.role] === null) {
      return;
    }
    await this.run("clear", form.role, () => this.api.clearSubjectTeacher(subject.id, subject.version, form.role));
  }

  private async run(kind: "save" | "clear", role: TeacherRole, call: () => Promise<TeacherResult>): Promise<void> {
    this.pending = true;
    const epoch = this.epoch;
    this.set({ busy: kind, errors: noErrors, note: "" });
    try {
      let result: TeacherResult;
      try {
        result = await call();
      } catch {
        result = { problem: "failed", field: "", subject: null };
      }
      await this.finish(result, kind, role, epoch);
    } finally {
      this.pending = false;
    }
  }

  private async finish(result: TeacherResult, kind: "save" | "clear", role: TeacherRole, epoch: number): Promise<void> {
    const fresh = () => epoch === this.epoch;
    if (result.problem === "" && result.subject !== null) {
      this.host.replace(result.subject);
      if (fresh()) {
        this.set({
          form: null,
          busy: "",
          errors: noErrors,
          note: kind === "save" ? teacherTexts.saved : teacherTexts.cleared,
          focusRole: role,
        });
      }
      return;
    }
    const failed = kind === "save" ? teacherTexts.saveFailed : teacherTexts.clearFailed;
    if (result.problem === "invalid") {
      if (fresh()) {
        this.set({ busy: "", errors: fieldErrors(result.field, failed) });
      }
      return;
    }
    // The shown data is out of date or the call failed: read the list again so the next try goes over the real version.
    const read = await this.host.reread();
    if (!fresh()) {
      return;
    }
    const text = !read ? teacherTexts.rereadFailed : result.problem === "changed" ? teacherTexts.changed : result.problem === "not_found" ? teacherTexts.notFound : failed;
    this.set({
      busy: "",
      errors: { ...noErrors, general: text },
      ...(result.problem === "not_found" ? { form: null } : {}),
    });
  }
}

// ---- «Указать почту» from a refused send ----

/** A request to open the form of a role on the page of a subject; role "" opens the page only. */
export interface TeacherFocus {
  subjectId: string;
  role: TeacherRole | "";
}

/**
 * What the subject page does with a pending request: wait (the list is not read yet or another subject is open), drop it
 * (the subject is gone), or open the form of the role and be done. The request is taken once.
 */
export function teacherFocusStep(
  focus: TeacherFocus | null,
  openId: string,
  listed: boolean,
  known: boolean,
): { role: TeacherRole | null; done: boolean } {
  if (focus === null || focus.subjectId !== openId || !listed) {
    return { role: null, done: false };
  }
  if (!known) {
    return { role: null, done: true };
  }
  return { role: focus.role === "" ? null : focus.role, done: true };
}

/** The window keeps a request only while it still points at the subject it names. */
export function keepTeacherFocus(focus: TeacherFocus | null, route: { page: string; subject: string }): TeacherFocus | null {
  return focus !== null && route.page === "subjects" && route.subject === focus.subjectId ? focus : null;
}
