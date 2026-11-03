// View logic and the flow of the link to the online class kept on a subject (TASK-364, ADR-0032 addendum). Pure, no React.
// The core checks the link (http or https, length) and opens it; the page never builds a URL, sends only the subject ID to
// open, and shows a link as plain text (the host on «Сегодня», host and full text on the page of the subject).
// The voice is impersonal. Subject names and links are typed or imported text: passed through untouched, text nodes only.

import {
  type CallLinkResult,
  genericError,
  type LibraryApi,
  type LibraryCallLink,
  type LibrarySubject,
  libraryErrorText,
  removedNameText,
} from "./bindings/library.js";
import type { TodayLesson } from "./bindings/today.js";

export const callLinkTexts = {
  join: "Подключиться",
  joining: "Открытие…",
  add: "Добавить ссылку",
  change: "Изменить",
  clear: "Удалить",
  clearing: "Удаление…",
  undo: "Вернуть",
  restoring: "Возврат…",
  save: "Сохранить",
  saveCreate: "Создать предмет и сохранить",
  saving: "Сохранение…",
  cancel: "Отмена",
  close: "Закрыть",
  retry: "Повторить",
  openSubjects: "Открыть «Предметы»",
  /** The label of the field and of the block on the page of a subject. */
  field: "Ссылка на пару",
  placeholder: "https://…",
  none: "Ссылки на пару нет",
  fieldEmpty: "Вставьте ссылку на пару.",
  invalid: "Нужна ссылка вида https://… (http или https).",
  saveFailed: "Не удалось сохранить ссылку. Попробуйте ещё раз.",
  openFailed: "Не удалось открыть ссылку. Попробуйте ещё раз.",
  clearFailed: "Не удалось удалить ссылку. Попробуйте ещё раз.",
  gone: "Ссылки на пару уже нет. Можно добавить новую.",
  subjectGone: "Предмет не найден, данные обновлены.",
  saved: "Ссылка на пару сохранена.",
  opened: "Ссылка открыта в браузере.",
  cleared: "Ссылка на пару удалена.",
  restored: "Ссылка на пару возвращена.",
  restoreFailed: "Не удалось вернуть ссылку. Её можно добавить заново.",
  createFailed: "Не удалось создать предмет. Попробуйте ещё раз.",
  noSubject: "У пары нет предмета: ссылку можно добавить на странице предмета в «Предметы».",
} as const;

/** The text of a refused create for a name that is taken; the core's message (library.go), pinned by a test. */
export const nameTakenText = "Предмет с таким названием уже есть.";

/** «Ссылка хранится у предмета «Физика». Создать предмет и сохранить ссылку?» */
export function createQuestion(name: string): string {
  return `Ссылка хранится у предмета «${name}». Создать предмет и сохранить ссылку?`;
}

/**
 * Why a subject cannot be made for the pair, in plain words. «Такое название уже есть» happens when the pair's subject
 * string fits several subjects (or an alias), so the pair cannot pick one: the student picks the subject in «Предметы».
 */
export function createRefusalText(err: unknown, name: string): string {
  const msg = libraryErrorText(err);
  if (msg === removedNameText) {
    return `Предмет «${name}» удалён. Вернуть его можно в «Предметы»: создайте предмет с тем же названием.`;
  }
  if (msg === nameTakenText) {
    return `Не удалось создать предмет «${name}»: такое название уже есть, а пара не знает, к какому предмету относится. Ссылку можно добавить у нужного предмета в «Предметы».`;
  }
  return msg === genericError ? callLinkTexts.createFailed : msg;
}

// ---- The card of a pair on «Сегодня» ----

/** What the card of a pair offers about the link; decided from the fields of the lesson only. */
export type CardLink =
  | { kind: "join"; host: string }
  | { kind: "add" }
  /** The pair names a subject the library does not have (or cannot pick): it can be created, then the link is kept. */
  | { kind: "create"; name: string }
  /** The pair names no subject at all. */
  | { kind: "nosubject" };

export function cardLink(l: Pick<TodayLesson, "subject" | "subjectId" | "hasCallLink" | "callLinkHost">): CardLink {
  if (l.subjectId === "") {
    const name = l.subject.trim();
    return name === "" ? { kind: "nosubject" } : { kind: "create", name };
  }
  return l.hasCallLink ? { kind: "join", host: l.callLinkHost } : { kind: "add" };
}

/** The key of a pair's link controls: its ID (the focus ids and the open form are kept by it). */
export function linkKey(l: Pick<TodayLesson, "id" | "start" | "title">): string {
  return l.id !== "" ? l.id : `${l.start}|${l.title}`;
}

/** The explanation above the field (or instead of it); "" when the card needs none. */
export function explainText(card: CardLink): string {
  return card.kind === "create" ? createQuestion(card.name) : card.kind === "nosubject" ? callLinkTexts.noSubject : "";
}

/** The pair has nowhere to keep a link: the form has no field, only the explanation and the way to «Предметы». */
export function hasNoField(card: CardLink): boolean {
  return card.kind === "nosubject";
}

export function saveLabel(card: CardLink): string {
  return card.kind === "create" ? callLinkTexts.saveCreate : callLinkTexts.save;
}

/** The accessible name of a button of a pair's card: the action and the pair. */
export function pairLabel(action: string, title: string): string {
  return `${action}: ${title}`;
}

/** The tooltip of «Подключиться»: where it leads (the host only). */
export function joinTitle(host: string): string {
  return host === "" ? "" : `Ссылка на пару: ${host}`;
}

/**
 * A subject as a call returned it (set, clear, create): the lessons of the day that belong to it follow at once, so the
 * card shows «Подключиться» without waiting for the next read. `key` is the pair that acted (it may have had no subject).
 */
export function applySubjectLink(lessons: readonly TodayLesson[], key: string, subject: LibrarySubject): TodayLesson[] {
  return lessons.map((l) =>
    linkKey(l) === key || (l.subjectId !== "" && l.subjectId === subject.id)
      ? {
          ...l,
          subjectId: subject.id,
          subjectName: subject.name,
          hasCallLink: subject.callLink !== null,
          callLinkHost: subject.callLink?.host ?? "",
        }
      : l,
  );
}

/** The link of a subject is gone (an open found none): its pairs offer «Добавить ссылку». */
export function withoutLink(lessons: readonly TodayLesson[], subjectId: string): TodayLesson[] {
  return lessons.map((l) => (l.subjectId === subjectId ? { ...l, hasCallLink: false, callLinkHost: "" } : l));
}

// ---- Element ids (the focus goes to them) ----

export type LinkPart = "join" | "add" | "change" | "clear" | "undo" | "field";

export function linkId(key: string, part: LinkPart): string {
  return `calllink-${part}-${key}`;
}

// ---- The flow ----

export type LinkMode = "add" | "change";

export interface LinkForm {
  key: string;
  mode: LinkMode;
  text: string;
}

/** The quiet result of the last call, shown at the pair/block it is about. */
export interface LinkLine {
  key: string;
  kind: "ok" | "fail";
  text: string;
  /** The button next to the text: repeat the open, or go to «Предметы». */
  action: "" | "retry" | "subjects";
}

/** A link the student just removed: «Вернуть» saves it back. Typed text, kept only in memory. */
export interface RemovedLink {
  key: string;
  subjectId: string;
  link: string;
}

export interface LinkState {
  /** The open form; at most one. */
  form: LinkForm | null;
  /** A call runs: every control is off (one call at a time). */
  busy: "" | "save" | "open" | "clear" | "restore";
  busyKey: string;
  /** Under the field (the rule broke); the line below is for results. */
  fieldError: string;
  line: LinkLine | null;
  removed: RemovedLink | null;
  /** The element to focus; `n` changes on every request so the same element can be asked again. */
  focus: { id: string; n: number };
}

export const idleLink: LinkState = { form: null, busy: "", busyKey: "", fieldError: "", line: null, removed: null, focus: { id: "", n: 0 } };

/** Where a pair's call goes. `name` is the subject string of the pair, used only to make the subject when there is none. */
export interface LinkTarget {
  key: string;
  subjectId: string;
  name: string;
}

/** What the flow needs from the page that shows the subjects. */
export interface LinkHost {
  /** The subject as a call returned it; `key` names the card/block that acted. */
  applied(subject: LibrarySubject, key: string): void;
  /** The subject keeps no link any more (an open found none). */
  linkGone(subjectId: string): void;
  /** The shown data is out of date (the subject is gone): read it again. */
  reread(): void;
}

type FlowApi = Pick<LibraryApi, "createSubject" | "setSubjectCallLink" | "clearSubjectCallLink" | "openSubjectCallLink">;

const failedResult: CallLinkResult = { problem: "failed", subject: null };

/**
 * The buttons of the link, one call at a time (a second click while one runs does nothing). A call that ends after
 * reset() (another page of a subject) still tells the host what it changed, but no longer touches the state.
 */
export class CallLinkFlow {
  private current: LinkState = idleLink;
  private pending = false;
  private disposed = false;
  private epoch = 0;

  constructor(
    private readonly api: FlowApi,
    private readonly host: LinkHost,
    private readonly onChange: (s: LinkState) => void,
  ) {}

  get state(): LinkState {
    return this.current;
  }

  attach(): void {
    this.disposed = false;
  }

  dispose(): void {
    this.disposed = true;
  }

  private set(patch: Partial<LinkState>, focusId = ""): void {
    const focus = focusId === "" ? this.current.focus : { id: focusId, n: this.current.focus.n + 1 };
    this.current = { ...this.current, ...patch, focus };
    if (!this.disposed) {
      this.onChange(this.current);
    }
  }

  /** Another subject (or screen) is shown: the form, the lines and the undo offer of the previous one go away. */
  reset(): void {
    this.epoch++;
    this.pending = false;
    this.set({ ...idleLink, focus: this.current.focus });
  }

  /** «Добавить ссылку» / «Изменить»: opens the field (with the current text for a change). */
  openForm(key: string, mode: LinkMode, text = ""): void {
    if (this.pending) {
      return;
    }
    this.set({ form: { key, mode, text }, fieldError: "", line: null, removed: null }, linkId(key, "field"));
  }

  /** The student typed: the error and the old result go away. */
  type(text: string): void {
    const form = this.current.form;
    if (form === null) {
      return;
    }
    this.set({ form: { ...form, text }, fieldError: "", line: null });
  }

  /** Escape or «Отмена»: the focus returns to the button that opened the field. */
  cancel(): void {
    const form = this.current.form;
    if (this.pending || form === null) {
      return;
    }
    this.set({ form: null, fieldError: "", line: null }, linkId(form.key, form.mode));
  }

  private async guard(kind: LinkState["busy"], key: string, work: (fresh: () => boolean) => Promise<void>): Promise<void> {
    this.pending = true;
    const epoch = this.epoch;
    this.set({ busy: kind, busyKey: key, fieldError: "", line: null });
    try {
      await work(() => epoch === this.epoch);
    } finally {
      // After reset() the flag belongs to the call of the new subject, not to this late one.
      if (epoch === this.epoch) {
        this.pending = false;
      }
    }
  }

  private async setLink(subjectId: string, text: string): Promise<CallLinkResult> {
    try {
      return await this.api.setSubjectCallLink(subjectId, text);
    } catch {
      return failedResult;
    }
  }

  /**
   * «Сохранить»: a pair without a library subject first makes the subject (a refusal says why and leads to «Предметы»),
   * then the link is checked and kept by the core. A refused link keeps the field with the typed text.
   */
  async save(target: LinkTarget): Promise<void> {
    const form = this.current.form;
    if (this.pending || form === null || form.key !== target.key) {
      return;
    }
    const text = form.text.trim();
    if (text === "") {
      this.set({ fieldError: callLinkTexts.fieldEmpty }, linkId(form.key, "field"));
      return;
    }
    if (target.subjectId === "" && target.name === "") {
      return;
    }
    await this.guard("save", target.key, async (fresh) => {
      let subjectId = target.subjectId;
      if (subjectId === "") {
        try {
          const created = await this.api.createSubject(target.name, "");
          this.host.applied(created, target.key);
          subjectId = created.id;
        } catch (err) {
          if (fresh()) {
            const line: LinkLine = { key: target.key, kind: "fail", text: createRefusalText(err, target.name), action: "subjects" };
            this.set({ busy: "", form: null, line }, linkId(target.key, "add"));
          }
          return;
        }
      }
      const res = await this.setLink(subjectId, text);
      if (res.problem === "" && res.subject !== null) {
        this.host.applied(res.subject, target.key);
        if (fresh()) {
          const line: LinkLine = { key: target.key, kind: "ok", text: callLinkTexts.saved, action: "" };
          this.set({ busy: "", form: null, line, removed: null }, linkId(target.key, "join"));
        }
        return;
      }
      if (res.problem === "not_found") {
        this.host.reread();
      }
      if (!fresh()) {
        return;
      }
      if (res.problem === "invalid") {
        this.set({ busy: "", fieldError: callLinkTexts.invalid }, linkId(target.key, "field"));
      } else if (res.problem === "not_found") {
        const line: LinkLine = { key: target.key, kind: "fail", text: callLinkTexts.subjectGone, action: "" };
        this.set({ busy: "", form: null, line }, linkId(target.key, "add"));
      } else {
        const line: LinkLine = { key: target.key, kind: "fail", text: callLinkTexts.saveFailed, action: "" };
        this.set({ busy: "", line }, linkId(target.key, "field"));
      }
    });
  }

  /** «Подключиться»: opens the link of the subject outside the app. Recording does not start. */
  async join(target: LinkTarget): Promise<void> {
    if (this.pending || target.subjectId === "") {
      return;
    }
    await this.guard("open", target.key, async (fresh) => {
      let problem: string;
      try {
        problem = (await this.api.openSubjectCallLink(target.subjectId)).problem;
      } catch {
        problem = "failed";
      }
      if (problem === "no_link") {
        this.host.linkGone(target.subjectId);
      } else if (problem === "not_found") {
        this.host.reread();
      }
      if (!fresh()) {
        return;
      }
      const line = (kind: LinkLine["kind"], text: string, action: LinkLine["action"] = ""): LinkLine => ({ key: target.key, kind, text, action });
      if (problem === "") {
        this.set({ busy: "", line: line("ok", callLinkTexts.opened) }, linkId(target.key, "join"));
      } else if (problem === "no_link") {
        // The link was removed meanwhile: the card now offers «Добавить ссылку».
        this.set({ busy: "", line: line("fail", callLinkTexts.gone) }, linkId(target.key, "add"));
      } else if (problem === "not_found") {
        this.set({ busy: "", line: line("fail", callLinkTexts.subjectGone) });
      } else {
        this.set({ busy: "", line: line("fail", callLinkTexts.openFailed, "retry") }, linkId(target.key, "join"));
      }
    });
  }

  /** «Удалить» (page of a subject): the link goes, «Вернуть» saves the removed text back. */
  async clear(target: LinkTarget, current: LibraryCallLink): Promise<void> {
    if (this.pending || target.subjectId === "") {
      return;
    }
    await this.guard("clear", target.key, async (fresh) => {
      let res: CallLinkResult;
      try {
        res = await this.api.clearSubjectCallLink(target.subjectId);
      } catch {
        res = failedResult;
      }
      if (res.problem === "" && res.subject !== null) {
        this.host.applied(res.subject, target.key);
        if (fresh()) {
          const line: LinkLine = { key: target.key, kind: "ok", text: callLinkTexts.cleared, action: "" };
          this.set({ busy: "", line, removed: { key: target.key, subjectId: target.subjectId, link: current.link } }, linkId(target.key, "undo"));
        }
        return;
      }
      if (res.problem === "not_found") {
        this.host.reread();
      }
      if (fresh()) {
        const text = res.problem === "not_found" ? callLinkTexts.subjectGone : callLinkTexts.clearFailed;
        this.set({ busy: "", line: { key: target.key, kind: "fail", text, action: "" } }, linkId(target.key, "clear"));
      }
    });
  }

  /** «Вернуть»: the removed link is saved again (the core checks it as any other). */
  async restore(): Promise<void> {
    const back = this.current.removed;
    if (this.pending || this.current.form !== null || back === null) {
      return;
    }
    await this.guard("restore", back.key, async (fresh) => {
      const res = await this.setLink(back.subjectId, back.link);
      if (res.problem === "" && res.subject !== null) {
        this.host.applied(res.subject, back.key);
        if (fresh()) {
          const line: LinkLine = { key: back.key, kind: "ok", text: callLinkTexts.restored, action: "" };
          this.set({ busy: "", line, removed: null }, linkId(back.key, "join"));
        }
        return;
      }
      if (res.problem === "not_found") {
        this.host.reread();
      }
      if (fresh()) {
        // A failed call can be repeated, so the offer stays; a refusal of the link or a lost subject cannot.
        const keep = res.problem === "failed" ? back : null;
        const line: LinkLine = { key: back.key, kind: "fail", text: callLinkTexts.restoreFailed, action: "" };
        this.set({ busy: "", line, removed: keep }, linkId(back.key, keep === null ? "add" : "undo"));
      }
    });
  }
}
