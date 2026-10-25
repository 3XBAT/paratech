// Agent panel bindings: calls into the Go core (internal/input/wails/agent.go) and the shape of the "agent:update"
// event (agentpanel.go). The core validates every card; texts of the model (reply text, plan summary, notes, question
// texts, options, labels) are untrusted: show them as plain text nodes, never as HTML.
// An Ask runs in the background: the call only says whether it started, the outcome comes as updates.

import type { CallByName } from "./settings";

/** The Wails event of the panel; its data is one AgentUpdate. */
export const updateEvent = "agent:update";

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
  cancel: `${service}.Cancel`,
  undo: `${service}.Undo`,
  undoRequest: `${service}.UndoRequest`,
  history: `${service}.History`,
} as const;

/** Problems of a call (AgentResult.problem); "" when the operation started. */
export type CallProblem = "" | "invalid" | "offline" | "closed" | "unavailable" | "busy" | "failed";

export const callProblems: readonly CallProblem[] = ["", "invalid", "offline", "closed", "unavailable", "busy", "failed"];

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
  | "work";

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
];

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
  stopReason: string;
  /** A `work` card: the work whose texts TurnkeyService.Work returns; "" for other cards. */
  workId: string;
  /** A `work` card: questions, review, helper, pending, failed or confirmed. */
  outcome: string;
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
  /** "", busy, stale, invalid, budget, retry_required, restart_required or failed. */
  problem: string;
  /** A step went to the Hub: no cards; apply the status only and keep the cards shown. False from an older core. */
  progress: boolean;
  cards: CardView[];
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
    stopReason: str(r, "stopReason", w),
    // An older core does not send them; only a `work` card needs its workId (checked where the card is built).
    workId: optStr(r, "workId", w),
    outcome: optStr(r, "outcome", w),
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

export interface AgentApi {
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
  cancel(ref: TurnRef): Promise<CallProblem>;
  /** «Отменить» of an action line. */
  undo(actionId: string): Promise<UndoView>;
  /** «Отменить всё» of a request: every action of it that can still be undone. */
  undoRequest(ref: TurnRef): Promise<UndoRequestView>;
  /** «Журнал»: actions and processing jobs of the last days, newest first. */
  history(filter: HistoryFilter): Promise<HistoryView>;
}

/** Builds the agent API over a call function (the Wails runtime in the app, a fake in tests). */
export function agentApi(call: CallByName): AgentApi {
  const turn = (method: string) => async (ref: TurnRef) => parseCallProblem(await call(method, ref));
  return {
    state: async () => parseState(await call(methods.state)),
    ask: async (input) => parseCallProblem(await call(methods.ask, input)),
    approvePlan: turn(methods.approvePlan),
    revisePlan: async (ref, wish) => parseCallProblem(await call(methods.revisePlan, ref, wish)),
    answer: async (ref, answers) => parseCallProblem(await call(methods.answer, ref, answers)),
    stop: turn(methods.stop),
    continueTurn: turn(methods.continueTurn),
    retry: turn(methods.retry),
    restart: turn(methods.restart),
    cancel: turn(methods.cancel),
    undo: async (actionId) => parseUndo(await call(methods.undo, actionId)),
    undoRequest: async (ref) => parseUndoRequest(await call(methods.undoRequest, ref)),
    history: async (filter) => parseHistory(await call(methods.history, filter)),
  };
}
