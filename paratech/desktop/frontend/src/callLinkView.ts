// View logic and the flow of the link to the online class kept on a subject (TASK-364, ADR-0032 addendum). Pure, no React.
// The core checks the link (http or https, length) and opens it; the page never builds a URL, sends only the subject ID to
// open, and shows a link as plain text (the host on «Сегодня», the full text on the page of the subject).
// The voice is impersonal. Subject names and links are typed or imported text: passed through untouched, text nodes only.

import {
  type CallLinkResult,
  type CreateWithCallLink,
  type LibraryApi,
  type LibraryCallLink,
  type LibrarySubject,
  type SubjectRestore,
} from "./bindings/library.js";
import type { TodayLesson } from "./bindings/today.js";

/** How long a confirming line («Ссылка на пару сохранена.») stays. */
export const confirmMs = 6000;

export const callLinkTexts = {
  join: "Подключиться",
  joining: "Открытие…",
  /** The tooltip of «Подключиться»: where it leads is on the card already. */
  joinTitle: "Открыть ссылку на пару (запись не начнётся)",
  add: "Добавить ссылку",
  change: "Изменить",
  clear: "Удалить",
  clearing: "Удаление…",
  undo: "Вернуть",
  restoring: "Возврат…",
  /** The main button of the field while a removed subject holds the name: the refused create is not run again. */
  revive: "Вернуть предмет и сохранить",
  save: "Сохранить",
  saveCreate: "Создать предмет и сохранить",
  saving: "Сохранение…",
  cancel: "Отмена",
  retry: "Повторить",
  openSubjects: "Открыть «Предметы»",
  /** The label of the field and of the block on the page of a subject. */
  field: "Ссылка на пару",
  placeholder: "https://…",
  none: "Ссылки на пару нет. С ней у пар этого предмета на «Сегодня» появится «Подключиться».",
  fieldEmpty: "Ссылка не вставлена.",
  invalid: "Нужна ссылка вида https://… (http или https).",
  saveFailed: "Не удалось сохранить ссылку. Можно повторить.",
  openFailed: "Не удалось открыть ссылку. Можно повторить.",
  clearFailed: "Не удалось удалить ссылку. Можно повторить.",
  gone: "Ссылки на пару уже нет. Можно добавить новую.",
  subjectGone: "Предмет не найден, данные обновлены.",
  saved: "Ссылка на пару сохранена.",
  opened: "Ссылка открыта.",
  cleared: "Ссылка на пару удалена.",
  restored: "Ссылка на пару возвращена.",
  restoreFailed: "Не удалось вернуть ссылку. Её можно добавить заново.",
  invalidName: "Название предмета в расписании не подходит: нужно от 1 до 200 символов.",
  createFailed: "Не удалось создать предмет. Можно повторить.",
  reviveFailed: "Не удалось вернуть предмет. Можно повторить.",
  reviveChanged: "Предмет изменился, вернуть его не удалось. Можно ещё раз нажать «Создать предмет и сохранить».",
  /** Once at the heading «Пары дня» when some pairs could not read their subjects (the cards say nothing). */
  linksUnavailable: "Ссылки на пары сейчас не загрузились.",
} as const;

/** After a restore: the subject is back, only the link is not kept. */
export function reviveLinkFailedText(name: string): string {
  return `Предмет «${name}» возвращён, но ссылку сохранить не удалось. Можно повторить.`;
}

/** «Ссылка хранится у предмета «Физика». Создать предмет и сохранить ссылку?» */
export function createQuestion(name: string): string {
  return `Ссылка хранится у предмета «${name}». Создать предмет и сохранить ссылку?`;
}

/** The field of «Изменить» on «Сегодня» is empty (the card never holds the link): the texts say what the new one does. */
export function replaceTexts(host: string): { placeholder: string; label: string; hint: string } {
  return host === ""
    ? { placeholder: callLinkTexts.placeholder, label: "Новая ссылка", hint: "Новая ссылка заменит прежнюю." }
    : { placeholder: callLinkTexts.placeholder, label: `Новая ссылка вместо ${host}`, hint: `Новая ссылка заменит ссылку на ${host}.` };
}

// ---- The card of a pair on «Сегодня» ----

/** What the card of a pair offers about the link; decided from the fields of the lesson only. */
export type CardLink =
  | { kind: "join"; host: string }
  | { kind: "add" }
  /** The schedule names a subject the library does not have: it can be created, then the link is kept. */
  | { kind: "create"; name: string }
  /** The pair names no subject at all: nowhere to keep a link. */
  | { kind: "nosubject" }
  /** The name fits several subjects: the card cannot pick one, the link is not kept here. */
  | { kind: "ambiguous"; name: string }
  /** The library could not be read just now. */
  | { kind: "unavailable" };

export function cardLink(l: Pick<TodayLesson, "subject" | "subjectState" | "subjectId" | "hasCallLink" | "callLinkHost">): CardLink {
  const name = l.subject.trim();
  switch (l.subjectState) {
    case "known":
      if (l.subjectId === "") {
        return { kind: "unavailable" };
      }
      return l.hasCallLink ? { kind: "join", host: l.callLinkHost } : { kind: "add" };
    case "unknown":
      return name === "" ? { kind: "nosubject" } : { kind: "create", name };
    case "ambiguous":
      return { kind: "ambiguous", name };
    case "none":
      return { kind: "nosubject" };
    default:
      return { kind: "unavailable" };
  }
}

/** The pair can open the field of a link (add, change, create the subject): the other kinds only say why not. */
export function canEditLink(card: CardLink): boolean {
  return card.kind === "join" || card.kind === "add" || card.kind === "create";
}

/** The key of a pair's link controls: its ID (the focus ids and the open form are kept by it). */
export function linkKey(l: Pick<TodayLesson, "id" | "start" | "title">): string {
  return l.id !== "" ? l.id : `${l.start}|${l.title}`;
}

/** The explanation above the field; "" when the card needs none. */
export function explainText(card: CardLink): string {
  return card.kind === "create" ? createQuestion(card.name) : "";
}

/**
 * The quiet one-line reason on a card whose link cannot be kept there; "" for the other cards. Every text is true to the
 * card. An unavailable card says nothing: the one line at the heading speaks for all of them (linksUnavailableNote).
 *
 * Ambiguous: the core matches the pair's string to the name or an alias of a subject (case, «ё», spaces ignored) and a
 * string with two owners matches none. The library does not let a subject take another's spelling, so the student fixes
 * it in «Предметы» by renaming a subject or removing the alias; then the pair resolves to one subject.
 */
export function cardNote(card: CardLink): string {
  switch (card.kind) {
    case "nosubject":
      return "У пары в расписании не указан предмет, поэтому ссылку негде сохранить.";
    case "ambiguous":
      return (
        `${card.name === "" ? "Название пары" : `Название «${card.name}»`} подходит к нескольким предметам, поэтому ссылку здесь не сохранить. ` +
        "Когда в «Предметы» это название (в том числе из «Также») останется только у одного предмета, здесь появится «Добавить ссылку»."
      );
    default:
      return "";
  }
}

/** One quiet line near «Пары дня» when at least one pair could not read its subject; "" otherwise. */
export function linksUnavailableNote(lessons: readonly Pick<TodayLesson, "subject" | "subjectState" | "subjectId" | "hasCallLink" | "callLinkHost">[]): string {
  return lessons.some((l) => cardLink(l).kind === "unavailable") ? callLinkTexts.linksUnavailable : "";
}

/** «Открыть «Предметы»» next to the note: the way to make the name belong to one subject. */
export function noteOffersSubjects(card: CardLink): boolean {
  return card.kind === "ambiguous";
}

export function saveLabel(card: CardLink): string {
  return card.kind === "create" ? callLinkTexts.saveCreate : callLinkTexts.save;
}

/** The accessible name of a button of a pair's card: the action and the pair. */
export function pairLabel(action: string, title: string): string {
  return `${action}: ${title}`;
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
          subjectState: "known",
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

// ---- The refusals of creating a subject with a link ----

/** The button next to the text under the field: «Открыть «Предметы»». A removed subject has no button of its own: the main one returns it. */
export type FieldAction = "" | "subjects";

/** A removed subject that holds the name: while it is set, «Вернуть предмет и сохранить» is restoreSubject(subjectId, version). */
export interface ReviveOffer {
  key: string;
  name: string;
  subjectId: string;
  version: number;
}

export interface CreateRefusal {
  text: string;
  action: FieldAction;
  offer: ReviveOffer | null;
}

/** What a refused createSubjectWithCallLink says under the field (nothing was created). */
export function createRefusal(res: CreateWithCallLink, name: string, key: string): CreateRefusal {
  switch (res.problem) {
    case "invalid":
      return { text: callLinkTexts.invalid, action: "", offer: null };
    case "invalid_name":
      return { text: callLinkTexts.invalidName, action: "", offer: null };
    case "name_taken":
      return { text: `Название «${name}» уже занято другим предметом.`, action: "subjects", offer: null };
    case "removed_name_taken":
      return res.removed === null
        ? { text: `Название «${name}» занято удалённым предметом.`, action: "subjects", offer: null }
        : {
            text: `Предмет «${name}» удалён. Его можно вернуть.`,
            action: "",
            offer: { key, name, subjectId: res.removed.subjectId, version: res.removed.version },
          };
    default:
      return { text: callLinkTexts.createFailed, action: "", offer: null };
  }
}

/**
 * Why a restore of a removed subject failed, in plain words, and the next step. Once the subject is gone for good (its
 * window ended, or it is not stored) the core frees the name (it erases an expired subject before checking a name), so a
 * new create goes through; the offer is dropped and the main button reads «Создать предмет и сохранить» again.
 */
export function reviveRefusalText(problem: Exclude<SubjectRestore["problem"], "">, name: string): string {
  switch (problem) {
    case "expired":
      return `Предмет «${name}» удалён окончательно. Можно создать новый: «${callLinkTexts.saveCreate}».`;
    case "changed":
      return callLinkTexts.reviveChanged;
    case "not_found":
      return `Предмет «${name}» не найден: возможно, он удалён окончательно. Можно создать новый: «${callLinkTexts.saveCreate}».`;
    default:
      return callLinkTexts.reviveFailed;
  }
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
  /** The button next to the text: repeat the open. */
  action: "" | "retry";
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
  /** A call runs: every control is off (one call at a time). "revive" brings a removed subject back. */
  busy: "" | "save" | "open" | "clear" | "restore" | "revive";
  busyKey: string;
  /** Under the field (the rule broke); the line below is for results. */
  fieldError: string;
  /** The button next to the error under the field, and the removed subject it brings back. */
  fieldAction: FieldAction;
  reviveOffer: ReviveOffer | null;
  line: LinkLine | null;
  removed: RemovedLink | null;
  /** The element to focus; `n` changes on every request so the same element can be asked again. */
  focus: { id: string; n: number };
}

export const idleLink: LinkState = {
  form: null,
  busy: "",
  busyKey: "",
  fieldError: "",
  fieldAction: "",
  reviveOffer: null,
  line: null,
  removed: null,
  focus: { id: "", n: 0 },
};

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

type FlowApi = Pick<
  LibraryApi,
  "createSubjectWithCallLink" | "restoreSubject" | "setSubjectCallLink" | "clearSubjectCallLink" | "openSubjectCallLink"
>;

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
    this.set({ form: { key, mode, text }, fieldError: "", fieldAction: "", reviveOffer: null, line: null, removed: null }, linkId(key, "field"));
  }

  /** The student typed: the error and the old result go away. */
  type(text: string): void {
    const form = this.current.form;
    if (form === null) {
      return;
    }
    this.set({ form: { ...form, text }, fieldError: "", fieldAction: "", reviveOffer: null, line: null });
  }

  /** Escape or «Отмена»: the focus returns to the button that opened the field. */
  cancel(): void {
    const form = this.current.form;
    if (this.pending || form === null) {
      return;
    }
    this.set({ form: null, fieldError: "", fieldAction: "", reviveOffer: null, line: null }, linkId(form.key, form.mode));
  }

  /** A result that only confirms («сохранена», «открыта») fades after a while; a failure and the undo offer stay. */
  dismiss(): void {
    if (!this.pending && this.current.line?.kind === "ok" && this.current.removed === null) {
      this.set({ line: null });
    }
  }

  private async guard(kind: LinkState["busy"], key: string, work: (fresh: () => boolean) => Promise<void>): Promise<void> {
    this.pending = true;
    const epoch = this.epoch;
    // «Вернуть предмет» works under the error it was offered with: that text stays while the call runs.
    const keep = kind === "revive";
    this.set({ busy: kind, busyKey: key, ...(keep ? {} : { fieldError: "", fieldAction: "", reviveOffer: null }), line: null });
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

  /** The end of a setSubjectCallLink: the card is told, or the field stays with the refusal and the typed text. */
  private finishSet(res: CallLinkResult, key: string, okText: string, fresh: () => boolean, failText: string = callLinkTexts.saveFailed): void {
    if (res.problem === "" && res.subject !== null) {
      this.host.applied(res.subject, key);
      if (fresh()) {
        const line: LinkLine = { key, kind: "ok", text: okText, action: "" };
        this.set({ busy: "", form: null, line, removed: null, fieldError: "", fieldAction: "", reviveOffer: null }, linkId(key, "join"));
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
      this.set({ busy: "", fieldError: callLinkTexts.invalid }, linkId(key, "field"));
    } else if (res.problem === "not_found") {
      const line: LinkLine = { key, kind: "fail", text: callLinkTexts.subjectGone, action: "" };
      this.set({ busy: "", form: null, line }, linkId(key, "add"));
    } else {
      const line: LinkLine = { key, kind: "fail", text: failText, action: "" };
      this.set({ busy: "", line }, linkId(key, "field"));
    }
  }

  /**
   * «Сохранить»: a pair with a library subject keeps the link there (the core checks it). A pair whose subject is not in
   * the library creates the subject and keeps the link in one call: the link is checked first, so a refusal creates
   * nothing and the field stays with the typed text and the reason under it.
   */
  async save(target: LinkTarget): Promise<void> {
    const form = this.current.form;
    if (this.pending || form === null || form.key !== target.key) {
      return;
    }
    // A removed subject holds the name: the create was refused, so the button and Enter bring the subject back instead.
    if (this.current.reviveOffer?.key === target.key) {
      return this.revive(target);
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
      if (target.subjectId !== "") {
        this.finishSet(await this.setLink(target.subjectId, text), target.key, callLinkTexts.saved, fresh);
        return;
      }
      let res: CreateWithCallLink;
      try {
        res = await this.api.createSubjectWithCallLink(target.name, text);
      } catch {
        res = { problem: "failed", subject: null, removed: null };
      }
      if (res.problem === "" && res.subject !== null) {
        this.host.applied(res.subject, target.key);
        if (fresh()) {
          const line: LinkLine = { key: target.key, kind: "ok", text: callLinkTexts.saved, action: "" };
          this.set({ busy: "", form: null, line, removed: null }, linkId(target.key, "join"));
        }
        return;
      }
      if (fresh()) {
        const refusal = createRefusal(res, target.name, target.key);
        this.set({ busy: "", fieldError: refusal.text, fieldAction: refusal.action, reviveOffer: refusal.offer }, linkId(target.key, "field"));
      }
    });
  }

  /**
   * «Вернуть предмет»: the removed subject that holds the name comes back, then the typed link is kept on it (the core
   * checks it as any other). A refusal stays under the field; a call that failed can be repeated.
   */
  async revive(target: LinkTarget): Promise<void> {
    const offer = this.current.reviveOffer;
    const form = this.current.form;
    if (this.pending || offer === null || form === null || form.key !== target.key || offer.key !== target.key) {
      return;
    }
    const text = form.text.trim();
    await this.guard("revive", target.key, async (fresh) => {
      let res: SubjectRestore;
      try {
        res = await this.api.restoreSubject(offer.subjectId, offer.version);
      } catch {
        res = { problem: "failed", subject: null };
      }
      if (res.problem !== "" || res.subject === null) {
        const problem = res.problem === "" ? "failed" : res.problem;
        if (problem !== "failed") {
          this.host.reread();
        }
        if (fresh()) {
          // Only a failed call is worth repeating with the same version; the other refusals leave the subject gone, so the
          // offer goes and the main button creates again.
          const again = problem === "failed";
          this.set({ busy: "", fieldError: reviveRefusalText(problem, offer.name), reviveOffer: again ? offer : null }, linkId(target.key, "field"));
        }
        return;
      }
      this.host.applied(res.subject, target.key);
      // The subject is back: the main button is a plain save now, also when only the link fails below.
      if (fresh()) {
        this.set({ reviveOffer: null, fieldError: "" });
      }
      this.finishSet(
        await this.setLink(res.subject.id, text),
        target.key,
        `Предмет «${offer.name}» возвращён, ссылка на пару сохранена.`,
        fresh,
        reviveLinkFailedText(offer.name),
      );
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
