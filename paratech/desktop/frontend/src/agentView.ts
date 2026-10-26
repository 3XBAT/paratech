// View logic of the agent panel: context label, Ctrl+K, the Russian wording of the cards and which buttons a request
// offers. The core decides statuses and validates cards (ARCH-44); here they are only put into words. The voice is
// impersonal («Создан предмет…», not «Я создал…»). Model text (summary, notes, questions, options, labels, replies) is
// passed through untouched and must be rendered as a text node only, never as HTML.

import type { AnswerInput, ArgView, AskInput, CallProblem, CardView, PlanStepView, QuestionView } from "./bindings/agent.js";
import { type Entry, isBusyRefused } from "./agentState.js";
import { type PageId, pageById } from "./nav.js";

/** The agent screen of the core (values.Screen) for a section of the window; the core refuses unknown screens. */
const screens: Record<PageId, string> = {
  today: "today",
  subjects: "subjects",
  knowledge: "subjects",
  tickets: "subjects",
  homework: "assignments",
  schedule: "schedule",
  journal: "journal",
  settings: "settings",
  models: "settings",
  connections: "settings",
  diagnostics: "settings",
  profile: "settings",
  appearance: "settings",
};

export const noSubjectLabel = "Без предмета";

/** The context chip of the panel and the ids a request is asked with. */
export interface AgentContext {
  label: string;
  /** The label can be removed (the request is then asked «Без предмета»). */
  removable: boolean;
  ask: Omit<AskInput, "text">;
}

/**
 * The context by the current screen. The window has no subject selection yet, so the label is the section; removing it
 * drops the ids and the thread becomes «Без предмета». The page opens an assignment: it is part of the context.
 */
export function contextFor(page: PageId, assignmentId: string, removed: boolean): AgentContext {
  const screen = screens[page];
  if (removed) {
    return { label: noSubjectLabel, removable: false, ask: { subjectId: "", lessonId: "", assignmentId: "", screen } };
  }
  return {
    label: pageById(page).label,
    removable: true,
    ask: { subjectId: "", lessonId: "", assignmentId: page === "homework" ? assignmentId : "", screen },
  };
}

export function askInput(ctx: AgentContext, text: string): AskInput {
  return { ...ctx.ask, text };
}

/** True for Ctrl+K (Cmd+K on a Mac keyboard layout), the shortcut that focuses the panel input. */
export function isFocusPanelKey(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }): boolean {
  return (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k";
}

/** A request text to send: trimmed, null when empty. */
export function requestText(raw: string): string | null {
  const t = raw.trim();
  return t === "" ? null : t;
}

// ---- Texts ----

export const texts = {
  title: "paratech",
  hint: "Ctrl+K",
  send: "Отправить",
  /** Visually hidden text of the waiting dots. */
  waiting: "Запрос выполняется",
  removeContext: "Убрать контекст",
  offlineBanner: "Нет связи с paratech",
  empty: "Можно задать вопрос или попросить что-нибудь сделать.",
  planCaption: "Понял так",
  progressCaption: "Работа идёт",
  resultCaption: "Готово",
  answerCaption: "Ответ по материалам",
  replyCaption: "Ответ",
  cannotDoCaption: "Целиком не получится",
  yourPart: "Остаётся на вашей стороне",
  ourPart: "Берёт на себя paratech",
  confirmCaption: "Нужно подтверждение",
  confirmPending: "Подтверждение пока недоступно.",
  actionDone: "Действие выполнено",
  undo: "Отменить",
  undoAll: "Отменить всё",
  undoBusy: "Отмена…",
  undoDone: "Отменено",
  skip: "Пропустить — сделай как считаешь",
  submitAnswers: "Ответить",
  reviseLabel: "Что изменить в плане",
  reviseSend: "Отправить",
  textAnswerLabel: "Свой вариант",
  skipTextAnswer: "Как считаешь нужным",
  noArtifact: "Файл недоступен для показа",
  answerReady: "Ответ с источниками подготовлен.",
  stale: "нет связи — может быть неактуально",
  threadNote: "Последняя беседа",
  rerunTitle: "Запустить просьбу заново",
} as const;

/** Button ids of a request; the panel maps them to the calls. */
export type ButtonId = "approve" | "revise" | "cancel" | "stop" | "continue" | "retry" | "restart" | "rerun" | "resend";

export const buttonLabels: Record<ButtonId, string> = {
  approve: "Да",
  revise: "Изменить",
  cancel: "Отмена",
  stop: "Стоп",
  continue: "Продолжить",
  retry: "Повторить",
  restart: "Начать заново",
  rerun: "Повторить",
  resend: "Отправить ещё раз",
};

const callProblemTexts: Record<Exclude<CallProblem, "">, string> = {
  invalid: "Просьба не принята: проверьте текст.",
  offline: "Нет связи с paratech.",
  closed: "Приложение закрывается.",
  unavailable: "Пока недоступно.",
  busy: "Действие уже меняется, попробуйте ещё раз.",
  failed: "Не удалось отправить просьбу.",
};

/** The wording of a refused call. */
export function callProblemText(p: CallProblem): string {
  return p === "" ? "" : callProblemTexts[p];
}

const updateProblemTexts: Record<string, string> = {
  busy: "Предыдущая просьба ещё выполняется.",
  stale: "Просьба устарела: нужно задать её заново.",
  invalid: "Просьба не принята: проверьте текст.",
  budget: "Остановлено: лимит просьбы.",
  retry_required: "Шаг не удался.",
  restart_required: "Просьба устарела.",
  rerun_blocked: "Повтор недоступен: просьба уже изменила данные. Можно отменить действия или задать вопрос заново.",
  failed: "Не удалось выполнить просьбу.",
};

/** Why a request stopped: the text of the stop card and the buttons it offers. */
const stopTexts: Record<string, { text: string; buttons: ButtonId[] }> = {
  offline: { text: "Не завершено: нет связи", buttons: ["continue", "cancel"] },
  user: { text: "Остановлено", buttons: ["continue", "cancel"] },
  quota: { text: "Не завершено: исчерпан лимит", buttons: ["continue", "cancel"] },
  failed: { text: "Не получилось выполнить шаг", buttons: ["retry"] },
  expired: { text: "Просьба устарела", buttons: ["restart"] },
  budget: { text: "Остановлено: лимит просьбы", buttons: [] },
};

// ---- Plan ----

/** Impersonal names of the steps of a plan. */
const toolTexts: Record<string, string> = {
  list_subjects: "Просмотр предметов",
  find_lessons: "Поиск занятий",
  list_assignments: "Просмотр заданий",
  job_status: "Проверка хода работы",
  delivery_status: "Проверка отправки письма",
  search: "Поиск по материалам",
  answer_question: "Ответ по материалам",
  inspect_upload: "Просмотр загруженного файла",
  create_subject: "Создание предмета",
  create_folder: "Создание папки",
  place_upload: "Размещение файла",
  link_material: "Привязка материала",
  set_lesson_info: "Уточнение данных занятия",
  make_summary: "Подготовка конспекта",
  make_lecture_text: "Подготовка текста лекции",
  find_homework: "Поиск домашнего задания",
  generate_draft: "Подготовка решения",
  edit_due: "Изменение срока",
  triage_assignment: "Разбор задания",
  write_work: "Подготовка работы",
  propose_deadline: "Предложение срока",
  propose_send: "Подготовка письма",
  propose_delete_recording: "Предложение удалить запись",
};

const idKindTexts: Record<string, string> = {
  subject: "предмет",
  folder: "папка",
  lesson: "занятие",
  recording: "запись",
  material: "материал",
  upload: "файл",
  assignment: "задание",
  work: "работа",
  version: "версия",
};

/** YYYY-MM-DD as DD.MM.YYYY; anything else stays as it came (shown as text). */
export function dateText(v: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(v);
  return m ? `${m[3]}.${m[2]}.${m[1]}` : v;
}

function argText(a: ArgView): string {
  switch (a.type) {
    case "date":
      return `срок ${dateText(a.value)}`;
    case "label":
    case "enum":
      return a.value;
    case "id":
      return idKindTexts[a.idKind] ?? "";
    case "ids":
      return a.values.length > 0 ? `${idKindTexts[a.idKind] ?? "объекты"}: ${a.values.length}` : "";
    default:
      return "";
  }
}

/** «Создание предмета · <метка>»: the tool's name, then its labels, dates and values. An unknown tool is generic. */
export function stepText(step: PlanStepView): string {
  const head = toolTexts[step.tool] ?? "Шаг";
  const parts = step.args.map(argText).filter((p) => p !== "");
  const later = step.deferred.length > 0 ? ["после предыдущих шагов"] : [];
  return [head, ...parts, ...later].join(" · ");
}

// ---- Artifacts ----

const artifactKinds: Record<string, string> = {
  recordings: "Запись",
  transcripts: "Расшифровка",
  exports: "Файл",
  materials: "Материал",
};

export interface ArtifactLabel {
  /** «Запись», «Расшифровка», «Файл» or «Материал». */
  kind: string;
  /** The file name only, never the directories. */
  name: string;
}

/**
 * Reads the artifact reference "<kind>/<path>" of a result card into a label. It is only a label: the panel opens
 * nothing by a path. A kind outside the known ones or a path that is absolute, climbs up or has control characters
 * gives null.
 */
export function parseArtifact(ref: string): ArtifactLabel | null {
  const i = ref.indexOf("/");
  if (i <= 0) {
    return null;
  }
  const kind = artifactKinds[ref.slice(0, i)];
  const path = ref.slice(i + 1);
  const bad =
    kind === undefined ||
    path === "" ||
    path.startsWith("/") ||
    path.includes("\\") ||
    path.includes(":") ||
    /[\u0000-\u001f\u007f]/.test(path) ||
    path.split("/").some((seg) => seg === ".." || seg === "." || seg === "");
  if (bad) {
    return null;
  }
  return { kind, name: path.slice(path.lastIndexOf("/") + 1) };
}

// ---- Questions ----

/** The answers of «Пропустить — сделай как считаешь»: the preselected option of a choice, a neutral text otherwise. */
export function skipAnswers(questions: readonly QuestionView[]): AnswerInput[] {
  return questions.map((q, i) =>
    q.kind === "choice"
      ? { question: i, option: q.default, text: "" }
      : { question: i, option: 0, text: texts.skipTextAnswer },
  );
}

/** The typed answers; null while a text question is empty (the form stays closed). */
export function collectAnswers(
  questions: readonly QuestionView[],
  picked: Readonly<Record<number, number>>,
  typed: Readonly<Record<number, string>>,
): AnswerInput[] | null {
  const out: AnswerInput[] = [];
  for (const [i, q] of questions.entries()) {
    if (q.kind === "choice") {
      out.push({ question: i, option: picked[i] ?? q.default, text: "" });
      continue;
    }
    const t = (typed[i] ?? "").trim();
    if (t === "") {
      return null;
    }
    out.push({ question: i, option: 0, text: t });
  }
  return out;
}

/** «Вопросы · раунд 1 из 2». */
export function roundCaption(round: number): string {
  return `Уточнение · раунд ${round} из 2`;
}

// ---- Cards ----

export interface ButtonModel {
  id: ButtonId;
  label: string;
}

export type CardModel =
  | { type: "plan"; caption: string; summary: string; steps: string[]; notes: string[]; buttons: ButtonModel[] }
  | { type: "questions"; caption: string; questions: QuestionView[]; active: boolean; skip: string; submit: string }
  | { type: "progress"; caption: string; detail: string; buttons: ButtonModel[] }
  | { type: "waiting"; buttons: ButtonModel[] }
  | { type: "result"; caption: string; artifact: ArtifactLabel | null; fallback: string }
  | { type: "answer"; caption: string; text: string }
  | { type: "reply"; caption: string; text: string }
  | { type: "cannot_do"; caption: string; yourTitle: string; yourPart: string[]; ourTitle: string; ourPart: string[] }
  | { type: "confirm"; caption: string; text: string; target: { kind: "proposal" | "preview"; id: string } | null }
  | { type: "action"; text: string; actionId: string; undo: { label: string; enabled: boolean } }
  | { type: "stopped"; text: string; buttons: ButtonModel[] }
  | { type: "work"; workId: string }
  | { type: "problem"; text: string; buttons: ButtonModel[] };

function buttons(ids: ButtonId[]): ButtonModel[] {
  return ids.map((id) => ({ id, label: buttonLabels[id] }));
}

/** The target a confirm card names; anything but a proposal or a deletion preview has no card to show. */
function confirmTarget(c: CardView): { kind: "proposal" | "preview"; id: string } | null {
  const t = c.target;
  return t !== null && (t.kind === "proposal" || t.kind === "preview") && t.id !== "" ? { kind: t.kind, id: t.id } : null;
}

const activeStatuses = new Set(["PLANNING", "EXECUTING"]);

function cardModel(c: CardView, status: string): CardModel | null {
  switch (c.kind) {
    case "plan":
      if (!c.plan) {
        return null;
      }
      return {
        type: "plan",
        caption: texts.planCaption,
        summary: c.plan.summary,
        steps: c.plan.steps.map(stepText),
        notes: c.plan.notes,
        buttons: status === "AWAITING_PLAN" ? buttons(["approve", "revise", "cancel"]) : [],
      };
    case "questions":
      if (!c.questions) {
        return null;
      }
      return {
        type: "questions",
        caption: roundCaption(c.questions.round),
        questions: c.questions.questions,
        active: status === "AWAITING_ANSWERS",
        skip: texts.skip,
        submit: texts.submitAnswers,
      };
    case "progress":
      return { type: "progress", caption: texts.progressCaption, detail: "", buttons: activeStatuses.has(status) ? buttons(["stop"]) : [] };
    case "result":
      return { type: "result", caption: texts.resultCaption, artifact: parseArtifact(c.artifact), fallback: texts.noArtifact };
    case "answer":
      return { type: "answer", caption: texts.answerCaption, text: texts.answerReady };
    case "reply":
      return { type: "reply", caption: texts.replyCaption, text: c.text };
    case "cannot_do":
      if (!c.cannotDo) {
        return null;
      }
      return {
        type: "cannot_do",
        caption: texts.cannotDoCaption,
        yourTitle: texts.yourPart,
        yourPart: c.cannotDo.yourPart,
        ourTitle: texts.ourPart,
        ourPart: c.cannotDo.ourPart,
      };
    case "confirm":
      return { type: "confirm", caption: texts.confirmCaption, text: texts.confirmPending, target: confirmTarget(c) };
    case "action_line":
      return {
        type: "action",
        text: texts.actionDone,
        actionId: c.actionId,
        undo: { label: texts.undo, enabled: c.actionId !== "" },
      };
    case "work":
      // The card reads the work itself (TurnkeyService.Work); a work card without an id has nothing to read.
      return c.workId !== "" ? { type: "work", workId: c.workId } : null;
    case "stopped":
      return null; // Built once from the stop reason below.
  }
}

/** What «Отменить» of an action line did: running, done, or the Russian text of a refusal. */
export interface UndoNote {
  busy: boolean;
  done: boolean;
  text: string;
}

/** The action lines of a request that offer «Отменить» (for «Отменить всё»). */
export function undoableActions(cards: readonly CardModel[]): string[] {
  return cards.flatMap((c) => (c.type === "action" && c.undo.enabled ? [c.actionId] : []));
}

/** What a request shows: the student's text, the cards in order, and the state line while nothing else says it. */
export interface EntryModel {
  key: string;
  /** What the student typed; "" for a request from the history. */
  userText: string;
  /** The refusal text when the call failed, "" otherwise. */
  callLine: string;
  /** The call runs and nothing has come back yet: the waiting dots show instead of a line. */
  waiting: boolean;
  cards: CardModel[];
  /** «Повторить» is offered under the request. */
  rerun: boolean;
}

/** What the thread says about a request that the request alone does not know. */
export interface EntryContext {
  /** The request is the last one of the thread. */
  last: boolean;
  /** A running request blocks this refused one, and «Стоп» can stop it. */
  blocked: boolean;
}

const finishedStatuses = new Set(["DONE", "FAILED", "CANCELLED", "STOPPED"]);

export function entryModel(e: Entry, ctx: EntryContext = { last: false, blocked: false }): EntryModel {
  const callLine = e.callProblem !== "" ? callProblemText(e.callProblem) : "";
  const base = { key: e.key, userText: e.text, callLine, waiting: e.callProblem === "" && e.update === null, rerun: false };
  const u = e.update;
  if (u === null) {
    return { ...base, cards: [] };
  }

  const cards: CardModel[] = [];
  for (const c of u.cards) {
    const m = cardModel(c, u.status);
    if (m) {
      cards.push(m);
    }
  }
  if (!cards.some((c) => c.type === "progress") && activeStatuses.has(u.status)) {
    cards.push({ type: "waiting", buttons: buttons(["stop"]) });
  }

  // One stop card, from the stopped card or the update's own reason; the problem adds a button for what to do next.
  const stopCard = u.cards.find((c) => c.kind === "stopped");
  const reason = stopCard?.stopReason || u.stopReason;
  const stop = reason !== "" ? (stopTexts[reason] ?? { text: "Остановлено", buttons: [] }) : null;
  if (stop) {
    cards.push({ type: "stopped", text: stop.text, buttons: buttons(stop.buttons) });
  }
  if (u.problem !== "") {
    let next: ButtonId[] = u.problem === "retry_required" ? ["retry"] : u.problem === "restart_required" ? ["restart"] : [];
    // A request refused as busy has no thread: it can stop the one that blocks it, or be sent again. Only the last one.
    if (isBusyRefused(e) && ctx.last) {
      next = [...(ctx.blocked ? (["stop"] as const) : []), ...(e.text !== "" && e.scope !== null ? (["resend"] as const) : [])];
    }
    cards.push({ type: "problem", text: updateProblemTexts[u.problem] ?? updateProblemTexts.failed, buttons: buttons(next) });
  } else if (!stop && u.status === "CANCELLED") {
    cards.push({ type: "stopped", text: "Отменено", buttons: [] });
  }

  // «Повторить» only under the last request, and not where the stop card or the problem already offers the next step.
  const offered = [...(stop?.buttons ?? []), ...(u.problem === "retry_required" ? ["retry" as const] : []), ...(u.problem === "restart_required" ? ["restart" as const] : [])];
  const rerun =
    ctx.last &&
    e.threadId !== "" &&
    finishedStatuses.has(u.status) &&
    u.problem !== "rerun_blocked" &&
    !offered.includes("retry") &&
    !offered.includes("restart");
  return { ...base, cards, rerun };
}
