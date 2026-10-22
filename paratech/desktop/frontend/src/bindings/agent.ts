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
  history: `${service}.History`,
} as const;

/** Problems of a call (AgentResult.problem); "" when the operation started. */
export type CallProblem = "" | "invalid" | "offline" | "closed" | "unavailable" | "failed";

export const callProblems: readonly CallProblem[] = ["", "invalid", "offline", "closed", "unavailable", "failed"];

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
  | "stopped";

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
  cards: CardView[];
}

export interface HistoryView {
  problem: CallProblem;
  requests: UpdateView[];
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

export function parseHistory(raw: unknown): HistoryView {
  const w = "agent history";
  const r = record(raw, w);
  return {
    problem: parseCallProblem(r),
    requests: items(r.requests, w, parseUpdate),
  };
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
  /** «Отменить» of an action line: "unavailable" until the journal exists (TASK-123). */
  undo(actionId: string): Promise<CallProblem>;
  /** Stored requests of the thread of subjectId ("" «Без предмета»): "unavailable" until TASK-123. */
  history(subjectId: string): Promise<HistoryView>;
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
    undo: async (actionId) => parseCallProblem(await call(methods.undo, actionId)),
    history: async (subjectId) => parseHistory(await call(methods.history, subjectId)),
  };
}
