// Agent panel bindings: calls into the Go core (internal/input/wails/agent.go) and the shape of the "agent:update"
// event (agentpanel.go). The core validates every card; texts of the model (reply text, plan summary, notes, question
// texts, options, labels) are untrusted: show them as plain text nodes, never as HTML.
// An Ask runs in the background: the call only says whether it started, the outcome comes as updates.

import type { CallByName } from "./settings";

/** The Wails event of the panel; its data is one AgentUpdate. */
export const updateEvent = "agent:update";

/** The Wails event of a draft answer; its data is one DraftView. */
export const draftEvent = "agent:draft";

const service = "paratech/desktop/internal/input/wails.AgentService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  state: `${service}.State`,
  ask: `${service}.Ask`,
  approvePlan: `${service}.ApprovePlan`,
  revisePlan: `${service}.RevisePlan`,
  answer: `${service}.Answer`,
  stop: `${service}.Stop`,
  continueTurn: `${service}.Continue`,
  retry: `${service}.Retry`,
  restart: `${service}.Restart`,
  rerun: `${service}.Rerun`,
  cancel: `${service}.Cancel`,
  undo: `${service}.Undo`,
  undoRequest: `${service}.UndoRequest`,
  history: `${service}.History`,
  freeWorkCard: `${service}.FreeWorkCard`,
  freeWork: `${service}.FreeWork`,
  chats: `${service}.Chats`,
  newChat: `${service}.NewChat`,
  renameChat: `${service}.RenameChat`,
  archiveChat: `${service}.ArchiveChat`,
  restoreChat: `${service}.RestoreChat`,
  chatEarlier: `${service}.ChatEarlier`,
  mentionCandidates: `${service}.MentionCandidates`,
  chatCurrent: `${service}.ChatCurrent`,
} as const;

export type MentionKind = "subject" | "lesson" | "material" | "assignment";

const mentionKinds: readonly MentionKind[] = ["subject", "lesson", "material", "assignment"];

/** The most candidates the core returns (MaxMentionCandidates). */
export const maxMentionCandidates = 20;

/** An object the «@» search offers; label and hint are untrusted plain text of one line. */
export interface MentionCandidate {
  kind: MentionKind;
  id: string;
  label: string;
  /** Subject and date; may be empty. */
  hint: string;
}

export type MentionsProblem = "" | "unavailable" | "failed";

export interface MentionsView {
  problem: MentionsProblem;
  candidates: MentionCandidate[];
}

/** Problems of a call (AgentResult.problem); "" when the operation started. */
export type CallProblem = "" | "invalid" | "offline" | "closed" | "unavailable" | "busy" | "failed";

export const callProblems: readonly CallProblem[] = ["", "invalid", "offline", "closed", "unavailable", "busy", "failed"];

/**
 * Problems of a chat call (chat list, rename, archive, restore, earlier requests); not_found: the chat is gone. Kept apart
 * from CallProblem so the request texts stay exhaustive; an unknown code reads as "failed".
 */
export type ChatProblem = "" | "invalid" | "busy" | "failed" | "not_found";

export const chatProblems: readonly ChatProblem[] = ["", "invalid", "busy", "failed", "not_found"];

/** Why the panel is read-only: "ready" is the only state that accepts a request. */
export type LinkState = "ready" | "no_hub" | "no_key" | "no_key_store" | "unknown";

export const linkStates: readonly LinkState[] = ["ready", "no_hub", "no_key", "no_key_store", "unknown"];

export interface AgentState {
  link: LinkState;
  readOnly: boolean;
}

/** The context a request is asked in. Empty ids mean none; an empty subjectId is the thread «Без предмета». */
export interface AskInput {
  text: string;
  subjectId: string;
  lessonId: string;
  assignmentId: string;
  /** "", today, subjects, assignments, schedule, journal or settings. */
  screen: string;
  /**
   * The chat; absent or "" is the current chat (a new one when there is none). A chat that no longer exists comes back
   * as an update with problem "failed" (the update problems have no not_found).
   */
  threadId?: string;
  /** The «@» objects of the text, at most 8 (values.MaxMentions; the core checks); absent means none. */
  mentions?: MentionView[];
}

/** An «@» object: kind is subject, lesson, material or assignment; label is the name shown at mention time (plain text). */
export interface MentionView {
  kind: string;
  id: string;
  label: string;
}

/** The request a button belongs to. */
export interface TurnRef {
  threadId: string;
  turn: number;
}

/** Answer to question `question`: an option of a choice, or the text the student typed. */
export interface AnswerInput {
  question: number;
  option: number;
  text: string;
}

export interface RefView {
  kind: string;
  id: string;
}

export interface ArgView {
  name: string;
  /** id, ids, date, enum or label. */
  type: string;
  idKind: string;
  value: string;
  values: string[];
}

export interface PlanStepView {
  tool: string;
  args: ArgView[];
  /** Names of id arguments an earlier step creates. */
  deferred: string[];
}

export interface PlanView {
  summary: string;
  steps: PlanStepView[];
  notes: string[];
}

export interface QuestionView {
  kind: "choice" | "text";
  text: string;
  options: string[];
  /** Index of the preselected option of a choice. */
  default: number;
  citeRef: string;
}

export interface QuestionsView {
  /** 1 or 2: the panel asks at most two rounds. */
  round: number;
  questions: QuestionView[];
}

export interface CannotDoView {
  yourPart: string[];
  ourPart: string[];
}

/**
 * An assignment a reply lists. The core reads it from the local data, never from the model; the title is still shown
 * as plain text. subjectId and due ("YYYY-MM-DD") are "" when there is none.
 */
export interface AssignmentLineView {
  id: string;
  title: string;
  subjectId: string;
  due: string;
}

/**
 * The assignment an `action_line` of create_assignment added (catalog v6). The core reads it from the local data,
 * never from the model; the title is still shown as plain text. subjectId, due ("YYYY-MM-DD") and time ("HH:MM";
 * "23:59" when the student named no time) are "" when there is none.
 */
export interface AddedAssignmentView {
  id: string;
  title: string;
  subjectId: string;
  due: string;
  time: string;
}

export type CardKind =
  | "plan"
  | "questions"
  | "progress"
  | "result"
  | "confirm"
  | "action_line"
  | "answer"
  | "cannot_do"
  | "reply"
  | "stopped"
  | "work"
  | "free_work";

export const cardKinds: readonly CardKind[] = [
  "plan",
  "questions",
  "progress",
  "result",
  "confirm",
  "action_line",
  "answer",
  "cannot_do",
  "reply",
  "stopped",
  "work",
  "free_work",
];

/**
 * A `free_work` card «Такого задания нет — могу сделать как свободную работу» (TASK-188): the call that waits on it.
 * The page words the card itself; the topic never comes to the page and is never sent back.
 */
export interface FreeWorkRef {
  threadId: string;
  turn: number;
  callId: string;
}

/**
 * Where a free-work card stands: waiting («Сделать» / «Не надо»; «Не надо» stores nothing, so a declined card reads as
 * waiting again), done (workId: show the work card), undone (removed with «Отменить») or failed.
 */
export type FreeWorkStateKind = "waiting" | "done" | "undone" | "failed";

export const freeWorkStates: readonly FreeWorkStateKind[] = ["waiting", "done", "undone", "failed"];

export interface FreeWorkState {
  state: FreeWorkStateKind;
  /** "" unless done or undone. */
  workId: string;
}

/** The fixed Russian texts of the free-work card errors (agent.go, checked by a Go test). */
export const freeWorkErrors = {
  notFound: "Карточка устарела: обновите панель.",
  unavailable: "Свободная работа пока недоступна.",
  failed: "Не удалось начать свободную работу.",
} as const;

/** The core's text of a refused free-work call when it is a known one; the generic «failed» text otherwise. */
export function freeWorkErrorText(err: unknown): string {
  const msg =
    typeof err === "string"
      ? err
      : typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string"
        ? (err as { message: string }).message
        : "";
  return (Object.values(freeWorkErrors) as string[]).includes(msg) ? msg : freeWorkErrors.failed;
}

/** One card; `kind` says which field is set. */
export interface CardView {
  kind: CardKind;
  plan: PlanView | null;
  questions: QuestionsView | null;
  jobId: string;
  /** "<kind>/<path>" of the artifact store. */
  artifact: string;
  target: RefView | null;
  actionId: string;
  answerId: string;
  cannotDo: CannotDoView | null;
  text: string;
  /** A `reply` card: the assignments it lists, from the student's own data; empty for other cards. */
  assignments: AssignmentLineView[];
  /** An `action_line` of create_assignment: the assignment it added; null for other cards, when gone or from an older core. */
  assignment: AddedAssignmentView | null;
  stopReason: string;
  /** A `work` card: the work whose texts TurnkeyService.Work returns; "" for other cards. */
  workId: string;
  /** A `work` card: questions, review, helper, pending, failed or confirmed. */
  outcome: string;
  /** A `free_work` card: the call it names (AgentApi.freeWork); null for other cards. */
  freeWork: FreeWorkRef | null;
}

/**
 * One update of a request. threadId is "" when an Ask failed before its thread existed: match it by scope. Updates of
 * different operations may arrive in any order.
 */
export interface UpdateView {
  threadId: string;
  turn: number;
  subjectId: string;
  lessonId: string;
  assignmentId: string;
  screen: string;
  /** PLANNING, AWAITING_PLAN, AWAITING_ANSWERS, EXECUTING, DONE, STOPPED, FAILED or CANCELLED. */
  status: string;
  /** "", budget, offline, user, quota, failed or expired. */
  stopReason: string;
  /**
   * "", busy, stale, invalid, budget, retry_required, restart_required, rerun_blocked («Повторить» over effects of the
   * request: «Отменить всё» first) or failed.
   */
  problem: string;
  /** A step went to the Hub: no cards; apply the status only and keep the cards shown. False from an older core. */
  progress: boolean;
  cards: CardView[];
}

/**
 * The draft of an answer being read by answer_question callId of a request (agentdraft.go): the claim texts so far,
 * at most 10 a second, the last texts winning. Unchecked model text: show it as plain text marked «черновик»; the
 * update with the call's answer card, or the request stopped, replaces it.
 */
export interface DraftView {
  threadId: string;
  turn: number;
  callId: string;
  texts: string[];
}

/** What «Журнал» reads: a line is an action of paratech or a processing job. Empty fields match everything. */
export interface HistoryFilter {
  /** "", action or processing. */
  kind: string;
  subjectId: string;
  /** An action status (DONE, UNDONE, UNDO_REFUSED, ...) or a job status; "" for any. */
  status: string;
  /** 1..90; 0 means 90. */
  days: number;
  /** Only the lines that offer «Отменить». */
  undoable: boolean;
}

export const emptyFilter: HistoryFilter = { kind: "", subjectId: "", status: "", days: 0, undoable: false };

/** One line of «Журнал»: ids and enums only, the page words them. Times are RFC 3339 (UTC). */
export interface JournalLine {
  kind: string;
  at: string;
  updatedAt: string;
  subjectId: string;
  actionId: string;
  threadId: string;
  /** The student's chat title of an action line (plain text); "" when the chat cannot be found or from an older core. */
  chatTitle: string;
  turn: number;
  tool: string;
  jobId: string;
  jobType: string;
  status: string;
  /** «Отменить» is offered. */
  undoable: boolean;
  /** «Отменить нельзя»: a proposal or a sent email. */
  cannotUndo: boolean;
  /** Why there is no «Отменить»: running, failed, undone, irreversible, changed, result_ready, cannot_stop, sent, ... */
  noUndo: string;
}

export interface HistoryView {
  problem: CallProblem;
  lines: JournalLine[];
}

/** The outcome of «Отменить» of one action: a problem, or the status after the call, or a refusal. */
export interface UndoView {
  problem: CallProblem;
  actionId: string;
  status: string;
  refusal: string;
}

/** The outcome of «Отменить всё» of a request. */
export interface UndoRequestView {
  problem: CallProblem;
  actions: UndoView[];
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

function num(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isFinite(v)) {
    throw new Error(`${what}: ${key} is not a number`);
  }
  return v;
}

function oneOf<T extends string>(r: Record<string, unknown>, key: string, allowed: readonly T[], what: string): T {
  const v = r[key];
  if (typeof v !== "string" || !allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not one of ${allowed.join(", ")}`);
  }
  return v as T;
}

/** Go sends nil slices as null for fields the core does not fill; a missing list is an empty one. */
function strings(r: Record<string, unknown>, key: string, what: string): string[] {
  const v = r[key];
  if (v === null || v === undefined) {
    return [];
  }
  if (!Array.isArray(v) || v.some((s) => typeof s !== "string")) {
    throw new Error(`${what}: ${key} is not a list of strings`);
  }
  return v as string[];
}

function items<T>(raw: unknown, what: string, parse: (item: unknown) => T): T[] {
  if (raw === null || raw === undefined) {
    return [];
  }
  if (!Array.isArray(raw)) {
    throw new Error(`${what}: not a list`);
  }
  return raw.map(parse);
}

function parseArg(raw: unknown): ArgView {
  const w = "plan argument";
  const r = record(raw, w);
  return {
    name: str(r, "name", w),
    type: str(r, "type", w),
    idKind: str(r, "idKind", w),
    value: str(r, "value", w),
    values: strings(r, "values", w),
  };
}

function parseStep(raw: unknown): PlanStepView {
  const w = "plan step";
  const r = record(raw, w);
  return { tool: str(r, "tool", w), args: items(r.args, w, parseArg), deferred: strings(r, "deferred", w) };
}

function parsePlan(raw: unknown): PlanView {
  const w = "plan";
  const r = record(raw, w);
  return { summary: str(r, "summary", w), steps: items(r.steps, w, parseStep), notes: strings(r, "notes", w) };
}

function parseQuestion(raw: unknown): QuestionView {
  const w = "question";
  const r = record(raw, w);
  return {
    kind: oneOf(r, "kind", ["choice", "text"], w),
    text: str(r, "text", w),
    options: strings(r, "options", w),
    default: num(r, "default", w),
    citeRef: str(r, "citeRef", w),
  };
}

function parseQuestions(raw: unknown): QuestionsView {
  const w = "questions";
  const r = record(raw, w);
  return { round: num(r, "round", w), questions: items(r.questions, w, parseQuestion) };
}

function parseRef(raw: unknown): RefView {
  const w = "ref";
  const r = record(raw, w);
  return { kind: str(r, "kind", w), id: str(r, "id", w) };
}

function parseCannotDo(raw: unknown): CannotDoView {
  const w = "cannot do";
  const r = record(raw, w);
  return { yourPart: strings(r, "yourPart", w), ourPart: strings(r, "ourPart", w) };
}

function parseAssignmentLine(raw: unknown): AssignmentLineView {
  const w = "assignment line";
  const r = record(raw, w);
  return { id: str(r, "id", w), title: str(r, "title", w), subjectId: str(r, "subjectId", w), due: str(r, "due", w) };
}

function parseAddedAssignment(raw: unknown): AddedAssignmentView {
  const w = "added assignment";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    title: str(r, "title", w),
    subjectId: str(r, "subjectId", w),
    due: str(r, "due", w),
    time: str(r, "time", w),
  };
}

function parseFreeWorkRef(raw: unknown): FreeWorkRef {
  const w = "free work";
  const r = record(raw, w);
  return { threadId: str(r, "threadId", w), turn: num(r, "turn", w), callId: str(r, "callId", w) };
}

export function parseFreeWorkState(raw: unknown): FreeWorkState {
  const w = "free work state";
  const r = record(raw, w);
  return { state: oneOf(r, "state", freeWorkStates, w), workId: str(r, "workId", w) };
}

function nested<T>(r: Record<string, unknown>, key: string, parse: (raw: unknown) => T): T | null {
  const v = r[key];
  return v === null || v === undefined ? null : parse(v);
}

/** A card the panel does not know is an error: the caller drops that card, not the update. */
export function parseCard(raw: unknown): CardView {
  const w = "card";
  const r = record(raw, w);
  return {
    kind: oneOf(r, "kind", cardKinds, w),
    plan: nested(r, "plan", parsePlan),
    questions: nested(r, "questions", parseQuestions),
    jobId: str(r, "jobId", w),
    artifact: str(r, "artifact", w),
    target: nested(r, "target", parseRef),
    actionId: str(r, "actionId", w),
    answerId: str(r, "answerId", w),
    cannotDo: nested(r, "cannotDo", parseCannotDo),
    text: str(r, "text", w),
    // null on other cards and from an older core.
    assignments: items(r.assignments, w, parseAssignmentLine),
    assignment: nested(r, "assignment", parseAddedAssignment),
    stopReason: str(r, "stopReason", w),
    // An older core does not send them; only a `work` card needs its workId (checked where the card is built).
    workId: optStr(r, "workId", w),
    outcome: optStr(r, "outcome", w),
    freeWork: nested(r, "freeWork", parseFreeWorkRef),
  };
}

/** Checks one update; a malformed update is an error, a malformed or unknown card is dropped. */
export function parseUpdate(raw: unknown): UpdateView {
  const w = "agent update";
  const r = record(raw, w);
  const cards: CardView[] = [];
  for (const c of items(r.cards, w, (x) => x)) {
    try {
      cards.push(parseCard(c));
    } catch {
      // The core already drops cards it cannot show; one the panel cannot read is dropped the same way.
    }
  }
  return {
    threadId: str(r, "threadId", w),
    turn: num(r, "turn", w),
    subjectId: str(r, "subjectId", w),
    lessonId: str(r, "lessonId", w),
    assignmentId: str(r, "assignmentId", w),
    screen: str(r, "screen", w),
    status: str(r, "status", w),
    stopReason: str(r, "stopReason", w),
    problem: str(r, "problem", w),
    progress: flag(r, "progress"),
    cards,
  };
}

/** Checks one draft; a malformed draft is an error (the caller drops it). */
export function parseDraft(raw: unknown): DraftView {
  const w = "agent draft";
  const r = record(raw, w);
  return {
    threadId: str(r, "threadId", w),
    turn: num(r, "turn", w),
    callId: str(r, "callId", w),
    texts: strings(r, "texts", w),
  };
}

export function parseState(raw: unknown): AgentState {
  const w = "agent state";
  const r = record(raw, w);
  if (typeof r.readOnly !== "boolean") {
    throw new Error(`${w}: readOnly is not a boolean`);
  }
  return { link: oneOf(r, "link", linkStates, w), readOnly: r.readOnly };
}

/** An unknown problem reads as "failed". */
export function parseCallProblem(raw: unknown): CallProblem {
  const w = "agent result";
  const r = record(raw, w);
  const p = str(r, "problem", w);
  return (callProblems as readonly string[]).includes(p) ? (p as CallProblem) : "failed";
}

function flag(r: Record<string, unknown>, key: string): boolean {
  return r[key] === true;
}

/** Text fields of a journal line are optional for the page: a missing one reads as "". */
function optStr(r: Record<string, unknown>, key: string, what: string): string {
  return r[key] === undefined || r[key] === null ? "" : str(r, key, what);
}

export function parseJournalLine(raw: unknown): JournalLine {
  const w = "journal line";
  const r = record(raw, w);
  return {
    kind: str(r, "kind", w),
    at: optStr(r, "at", w),
    updatedAt: optStr(r, "updatedAt", w),
    subjectId: optStr(r, "subjectId", w),
    actionId: optStr(r, "actionId", w),
    threadId: optStr(r, "threadId", w),
    chatTitle: optStr(r, "chatTitle", w),
    turn: typeof r.turn === "number" && Number.isFinite(r.turn) ? r.turn : 0,
    tool: optStr(r, "tool", w),
    jobId: optStr(r, "jobId", w),
    jobType: optStr(r, "jobType", w),
    status: optStr(r, "status", w),
    undoable: flag(r, "undoable"),
    cannotUndo: flag(r, "cannotUndo"),
    noUndo: optStr(r, "noUndo", w),
  };
}

export function parseHistory(raw: unknown): HistoryView {
  const w = "agent history";
  const r = record(raw, w);
  return { problem: parseCallProblem(r), lines: items(r.lines, w, parseJournalLine) };
}

export function parseUndo(raw: unknown): UndoView {
  const w = "agent undo";
  const r = record(raw, w);
  return {
    problem: parseCallProblem(r),
    actionId: optStr(r, "actionId", w),
    status: optStr(r, "status", w),
    refusal: optStr(r, "refusal", w),
  };
}

export function parseUndoRequest(raw: unknown): UndoRequestView {
  const w = "agent undo request";
  const r = record(raw, w);
  return { problem: parseCallProblem(r), actions: items(r.actions, w, parseUndo) };
}

/** A line of the chat list; lastActive is RFC 3339 (UTC); running: the chat's request runs. */
export interface ChatView {
  threadId: string;
  title: string;
  archived: boolean;
  lastActive: string;
  running: boolean;
}

/** The chat list: unarchived chats newest first, then the archive. */
export interface ChatsView {
  problem: ChatProblem;
  chats: ChatView[];
}

/** The outcome of a chat call; chat is the chat after the call, null unless problem is "". */
export interface ChatResult {
  problem: ChatProblem;
  chat: ChatView | null;
}

/** A request of a chat, read-only; current marks the only one with buttons. */
export interface EarlierRequestView {
  turn: number;
  text: string;
  mentions: MentionView[];
  /** What the student was shown (plain text); "" when no card was shown. */
  reply: string;
  current: boolean;
}

export interface EarlierView {
  problem: ChatProblem;
  requests: EarlierRequestView[];
}

/** An unknown or missing problem reads as "failed". */
function chatProblemOf(r: Record<string, unknown>): ChatProblem {
  const p = r.problem;
  return typeof p === "string" && (chatProblems as readonly string[]).includes(p) ? (p as ChatProblem) : "failed";
}

/** Titles and labels are optional for the page: a missing one reads as "". */
export function parseChat(raw: unknown): ChatView {
  const w = "chat";
  const r = record(raw, w);
  return {
    threadId: str(r, "threadId", w),
    title: optStr(r, "title", w),
    archived: flag(r, "archived"),
    lastActive: optStr(r, "lastActive", w),
    running: flag(r, "running"),
  };
}

/** Malformed data reads as a failed call with no chats. */
export function parseChats(raw: unknown): ChatsView {
  try {
    const r = record(raw, "chats");
    const problem = chatProblemOf(r);
    return { problem, chats: problem === "" ? items(r.chats, "chats", parseChat) : [] };
  } catch {
    return { problem: "failed", chats: [] };
  }
}

/** Malformed data, or a missing chat on success, reads as a failed call. */
export function parseChatResult(raw: unknown): ChatResult {
  try {
    const r = record(raw, "chat result");
    const problem = chatProblemOf(r);
    if (problem !== "") {
      return { problem, chat: null };
    }
    return { problem, chat: parseChat(r.chat) };
  } catch {
    return { problem: "failed", chat: null };
  }
}

function parseMention(raw: unknown): MentionView {
  const w = "mention";
  const r = record(raw, w);
  return { kind: str(r, "kind", w), id: str(r, "id", w), label: optStr(r, "label", w) };
}

function parseMentionCandidate(raw: unknown): MentionCandidate | null {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return null;
  }
  const r = raw as Record<string, unknown>;
  const { kind, id, label, hint } = r;
  if (typeof kind !== "string" || !(mentionKinds as readonly string[]).includes(kind)) {
    return null;
  }
  if (typeof id !== "string" || id === "" || typeof label !== "string" || label === "") {
    return null;
  }
  return { kind: kind as MentionKind, id, label, hint: typeof hint === "string" ? hint : "" };
}

/** Bad items are dropped, at most 20 are kept; malformed data or an unknown problem reads as failed. */
export function parseMentions(raw: unknown): MentionsView {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return { problem: "failed", candidates: [] };
  }
  const r = raw as Record<string, unknown>;
  const p = r.problem;
  if (p !== "" && p !== "unavailable") {
    return { problem: "failed", candidates: [] };
  }
  const list = Array.isArray(r.candidates) ? r.candidates : [];
  const candidates: MentionCandidate[] = [];
  for (const item of list) {
    const c = parseMentionCandidate(item);
    if (c !== null) {
      candidates.push(c);
      if (candidates.length === maxMentionCandidates) {
        break;
      }
    }
  }
  return { problem: p, candidates };
}

function parseEarlierRequest(raw: unknown): EarlierRequestView {
  const w = "earlier request";
  const r = record(raw, w);
  return {
    turn: num(r, "turn", w),
    text: str(r, "text", w),
    mentions: items(r.mentions, w, parseMention),
    reply: optStr(r, "reply", w),
    current: flag(r, "current"),
  };
}

/** Malformed data reads as a failed call with no requests. */
export function parseEarlier(raw: unknown): EarlierView {
  try {
    const r = record(raw, "earlier requests");
    const problem = chatProblemOf(r);
    return { problem, requests: problem === "" ? items(r.requests, "earlier requests", parseEarlierRequest) : [] };
  } catch {
    return { problem: "failed", requests: [] };
  }
}

/** The current state of a chat; update is null when the chat has no request or its request ended. */
export interface CurrentView {
  problem: ChatProblem;
  update: UpdateView | null;
}

/** The update is parsed like the "agent:update" event; malformed data reads as a failed call with no update. */
export function parseCurrent(raw: unknown): CurrentView {
  try {
    const r = record(raw, "chat current");
    const problem = chatProblemOf(r);
    if (problem !== "") {
      return { problem, update: null };
    }
    return { problem, update: r.update === null || r.update === undefined ? null : parseUpdate(r.update) };
  } catch {
    return { problem: "failed", update: null };
  }
}

export interface AgentApi {
  /** The current state of a chat as an update, to restore its cards; reads only the local data. */
  chatCurrent(threadId: string): Promise<CurrentView>;
  /** The «@» search: objects whose name contains the query, newest first; reads only the local data. */
  mentionCandidates(query: string): Promise<MentionsView>;
  /** The chat list; reads only the local data. */
  chats(): Promise<ChatsView>;
  /** «Новый чат»: the current chat when it is still empty, else a new one. */
  newChat(): Promise<ChatResult>;
  renameChat(threadId: string, title: string): Promise<ChatResult>;
  archiveChat(threadId: string): Promise<ChatResult>;
  restoreChat(threadId: string): Promise<ChatResult>;
  /** The requests of a chat as text, oldest first, the current one last. */
  chatEarlier(threadId: string): Promise<EarlierView>;
  /** The Hub link of the panel; calls no Hub. */
  state(): Promise<AgentState>;
  /** Starts a request; the outcome comes as updates. */
  ask(input: AskInput): Promise<CallProblem>;
  approvePlan(ref: TurnRef): Promise<CallProblem>;
  revisePlan(ref: TurnRef, wish: string): Promise<CallProblem>;
  answer(ref: TurnRef, answers: AnswerInput[]): Promise<CallProblem>;
  stop(ref: TurnRef): Promise<CallProblem>;
  continueTurn(ref: TurnRef): Promise<CallProblem>;
  retry(ref: TurnRef): Promise<CallProblem>;
  restart(ref: TurnRef): Promise<CallProblem>;
  /** «Повторить» of the last finished or stopped request: a new request with its text and scope; "offline" like ask. */
  rerun(ref: TurnRef): Promise<CallProblem>;
  cancel(ref: TurnRef): Promise<CallProblem>;
  /** «Отменить» of an action line. */
  undo(actionId: string): Promise<UndoView>;
  /** «Отменить всё» of a request: every action of it that can still be undone. */
  undoRequest(ref: TurnRef): Promise<UndoRequestView>;
  /** «Журнал»: actions and processing jobs of the last days, newest first. */
  history(filter: HistoryFilter): Promise<HistoryView>;
  /** The state of a free-work card; starts nothing. Rejects with a freeWorkErrors text. */
  freeWorkCard(ref: FreeWorkRef): Promise<FreeWorkState>;
  /**
   * The answer on a free-work card: accept is «Сделать» (once: a second press returns the same work), otherwise «Не
   * надо»; dontShowAgain is «Больше не показывать». Rejects with a freeWorkErrors text.
   */
  freeWork(ref: FreeWorkRef, accept: boolean, dontShowAgain: boolean): Promise<FreeWorkState>;
}

/** Only the three fields of the ref go to the core. */
function freeWorkArg(ref: FreeWorkRef): FreeWorkRef {
  return { threadId: ref.threadId, turn: ref.turn, callId: ref.callId };
}

/** Builds the agent API over a call function (the Wails runtime in the app, a fake in tests). */
export function agentApi(call: CallByName): AgentApi {
  const turn = (method: string) => async (ref: TurnRef) => parseCallProblem(await call(method, ref));
  return {
    mentionCandidates: async (query) => parseMentions(await call(methods.mentionCandidates, query)),
    chats: async () => parseChats(await call(methods.chats)),
    newChat: async () => parseChatResult(await call(methods.newChat)),
    renameChat: async (threadId, title) => parseChatResult(await call(methods.renameChat, threadId, title)),
    archiveChat: async (threadId) => parseChatResult(await call(methods.archiveChat, threadId)),
    restoreChat: async (threadId) => parseChatResult(await call(methods.restoreChat, threadId)),
    chatEarlier: async (threadId) => parseEarlier(await call(methods.chatEarlier, threadId)),
    chatCurrent: async (threadId) => parseCurrent(await call(methods.chatCurrent, threadId)),
    state: async () => parseState(await call(methods.state)),
    ask: async (input) => parseCallProblem(await call(methods.ask, input)),
    approvePlan: turn(methods.approvePlan),
    revisePlan: async (ref, wish) => parseCallProblem(await call(methods.revisePlan, ref, wish)),
    answer: async (ref, answers) => parseCallProblem(await call(methods.answer, ref, answers)),
    stop: turn(methods.stop),
    continueTurn: turn(methods.continueTurn),
    retry: turn(methods.retry),
    restart: turn(methods.restart),
    rerun: turn(methods.rerun),
    cancel: turn(methods.cancel),
    undo: async (actionId) => parseUndo(await call(methods.undo, actionId)),
    undoRequest: async (ref) => parseUndoRequest(await call(methods.undoRequest, ref)),
    history: async (filter) => parseHistory(await call(methods.history, filter)),
    freeWorkCard: async (ref) => parseFreeWorkState(await call(methods.freeWorkCard, freeWorkArg(ref))),
    freeWork: async (ref, accept, dontShowAgain) =>
      parseFreeWorkState(await call(methods.freeWork, freeWorkArg(ref), accept, dontShowAgain)),
  };
}
