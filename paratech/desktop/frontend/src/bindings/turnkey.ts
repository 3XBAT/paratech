// Bindings of the turnkey work (TASK-132): calls into internal/input/wails/turnkey.go. The page never talks to the Hub
// and keeps nothing in browser storage. Topic, section titles, paragraphs, notes, sources and messages are model or
// student text: show them as text nodes only. Errors of the binding are fixed Russian texts, shown as they are.

import type { CallByName } from "./settings";

const service = "paratech/desktop/internal/input/wails.TurnkeyService";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  work: `${service}.Work`,
  startWriting: `${service}.StartWriting`,
  approveOutline: `${service}.ApproveOutline`,
  editOutline: `${service}.EditOutline`,
  editOutlineAt: `${service}.EditOutlineAt`,
  stopWriting: `${service}.StopWriting`,
  regenerate: `${service}.Regenerate`,
  // TASK-130: questions, «Как понято задание», the helper.
  workOfAssignment: `${service}.WorkOfAssignment`,
  // TASK-185: «Сделать под ключ» on the assignment's page.
  startForAssignment: `${service}.StartForAssignment`,
  answerQuestions: `${service}.AnswerQuestions`,
  skipQuestions: `${service}.SkipQuestions`,
  editUnderstanding: `${service}.EditUnderstanding`,
  confirmRequirements: `${service}.ConfirmRequirements`,
  retry: `${service}.Retry`,
  askStep: `${service}.AskStep`,
} as const;

export type WorkStatus = "triage" | "questions" | "review" | "confirmed" | "writing" | "done" | "helper" | "failed";

export const workStatuses: readonly WorkStatus[] = ["triage", "questions", "review", "confirmed", "writing", "done", "helper", "failed"];

/** outline: the plan is written; approve: it waits for «Утвердить план»; blocked: a step used its attempts; none: done. */
export type WritingStep = "outline" | "approve" | "section" | "assemble" | "blocked" | "none";

export const writingSteps: readonly WritingStep[] = ["outline", "approve", "section", "assemble", "blocked", "none"];

/** State of the running writing job; "" when no job runs. */
export type WritingJobState = "" | "queued" | "running" | "waiting_network" | "waiting_quota" | "waiting_power" | "retrying";

export const writingJobStates: readonly WritingJobState[] = ["", "queued", "running", "waiting_network", "waiting_quota", "waiting_power", "retrying"];

export interface WorkSource {
  text: string;
  materialId: string;
  /** Named by the model, not taken from the student's materials. */
  verify: boolean;
}

export interface WorkSection {
  /** 1-based number of a section of the plan. */
  number: number;
  title: string;
  words: number;
  note: string;
  written: boolean;
  paragraphs: string[];
  sources: WorkSource[];
}

export interface WorkWriting {
  step: WritingStep;
  generation: number;
  topic: string;
  sections: WorkSection[];
  done: number;
  total: number;
  subjectId: string;
  materialId: string;
  result: string;
  job: string;
  /** A writing job runs for the work («Остановить»). */
  active: boolean;
  jobState: WritingJobState;
}

/** One question of the reader: a choice (options, default index) or a text (defaultText; "" may be skipped). */
export interface WorkQuestion {
  kind: "choice" | "text";
  text: string;
  options: string[];
  default: number;
  defaultText: string;
  why: string;
}

/** «Как я понял задание»: the type of the work and one line per requirement, "" unknown. */
export interface WorkCardFields {
  workType: string;
  volume: string;
  fileFormat: string;
  formatting: string;
  variant: string;
  deadline: string;
  sources: string;
}

export interface WorkHelper {
  steps: string[];
  submit: string;
  pitfalls: string[];
  readyParts: string[];
  placeholders: string[];
}

/** A question of an earlier round with the answer ("" skipped; default: the default was taken). */
export interface WorkAnswered {
  round: number;
  question: string;
  answer: string;
  default: boolean;
}

/** The student's answer to question number `question` (from 0): `option` for a choice, `text` for a text question. */
export interface QuestionAnswer {
  question: number;
  option: number;
  text: string;
}

/** The part of TurnkeyWork the cards read. Every text but `message` was written by the reader or the student: text nodes only. */
export interface TurnkeyWork {
  id: string;
  status: WorkStatus;
  version: number;
  writing: WorkWriting | null;
  /** card.workType of the Go view ("" without a card); "presentation" is not written as a paper. */
  workType: string;
  problem: string;
  message: string;
  questionRound: number;
  questions: WorkQuestion[];
  hasCard: boolean;
  card: WorkCardFields;
  /** Wire names of the fields the student changed (work_type, volume, ...). */
  edited: string[];
  feasibility: "" | "full" | "partial" | "impossible";
  feasibilityReason: string;
  partial: boolean;
  assumptions: string[];
  helper: WorkHelper | null;
  answered: WorkAnswered[];
  /** The topic of a work on a topic (student's or agent's words: text only); "" for a work of an assignment. */
  topic: string;
  /** The library subject of the work; "" none, or a subject removed since. */
  subjectId: string;
  /** «Отправить преподавателю» is offered: a work of an assignment only. */
  sendable: boolean;
}

/** A section of the plan the student sends; number 0 is a new section. */
export interface OutlineItem {
  number: number;
  title: string;
  words: number;
}

export const notFoundText = "Работа не найдена: возможно, её отменили.";

/** The fixed Russian texts of the Go binding (turnkey.go); anything else gets the generic one. */
export const errorTexts: readonly string[] = [
  notFoundText,
  "Это действие сейчас недоступно: обновите карточку.",
  "Вопросы по шагу пока недоступны.",
  "Не удалось выполнить. Попробуйте ещё раз.",
  "Работа ещё пишется: дождитесь окончания.",
  "Работа уже написана: измените план или сгенерируйте раздел заново.",
  "Работа изменилась: обновите карточку.",
  "Не удалось остановить написание. Попробуйте ещё раз.",
  "Задание не найдено: возможно, его удалили.",
  // AskStep (TASK-192): no reader wired, offline, quota, refused answer.
  "Нет связи с paratech: вопрос не отправлен. Повторите позже.",
  "Лимит запросов исчерпан: ответить на вопрос сейчас нельзя.",
  "Ответ на вопрос не прошёл проверку. Спросите ещё раз или переформулируйте вопрос.",
];

export const genericError = "Не удалось выполнить. Попробуйте ещё раз.";

function errorMessage(err: unknown): string {
  if (typeof err === "string") {
    return err;
  }
  if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    return (err as { message: string }).message;
  }
  return "";
}

/** The core's text of a refused call when it is one of the known texts; a generic text otherwise. */
export function turnkeyErrorText(err: unknown): string {
  const msg = errorMessage(err);
  return errorTexts.includes(msg) ? msg : genericError;
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

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
    throw new Error(`${what}: ${key} is not a count`);
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

/** A list of the Go side may come as null (an empty slice of an older build); anything but a list is an error. */
function list<T>(r: Record<string, unknown>, key: string, what: string, parse: (raw: unknown) => T): T[] {
  const v = r[key];
  if (v === null || v === undefined) {
    return [];
  }
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

function parseSource(raw: unknown): WorkSource {
  const w = "work source";
  const r = record(raw, w);
  return { text: str(r, "text", w), materialId: str(r, "materialId", w), verify: bool(r, "verify", w) };
}

function parseSection(raw: unknown): WorkSection {
  const w = "work section";
  const r = record(raw, w);
  return {
    number: count(r, "number", w),
    title: str(r, "title", w),
    words: count(r, "words", w),
    note: str(r, "note", w),
    written: bool(r, "written", w),
    paragraphs: list(r, "paragraphs", w, (p) => {
      if (typeof p !== "string") {
        throw new Error(`${w}: paragraph is not a string`);
      }
      return p;
    }),
    sources: list(r, "sources", w, parseSource),
  };
}

function parseWriting(raw: unknown): WorkWriting {
  const w = "work writing";
  const r = record(raw, w);
  return {
    step: oneOf(r, "step", writingSteps, w),
    generation: count(r, "generation", w),
    topic: str(r, "topic", w),
    sections: list(r, "sections", w, parseSection),
    done: count(r, "done", w),
    total: count(r, "total", w),
    subjectId: str(r, "subjectId", w),
    materialId: str(r, "materialId", w),
    result: str(r, "result", w),
    job: str(r, "job", w),
    active: optBool(r, "active", w),
    jobState: r.jobState === undefined || r.jobState === null ? "" : oneOf(r, "jobState", writingJobStates, w),
  };
}

// A field an older build did not send is the default; a field of a wrong type is an error.
function optStr(r: Record<string, unknown>, key: string, what: string): string {
  return r[key] === undefined || r[key] === null ? "" : str(r, key, what);
}

function optBool(r: Record<string, unknown>, key: string, what: string): boolean {
  return r[key] === undefined || r[key] === null ? false : bool(r, key, what);
}

function optCount(r: Record<string, unknown>, key: string, what: string): number {
  return r[key] === undefined || r[key] === null ? 0 : count(r, key, what);
}

function strings(r: Record<string, unknown>, key: string, what: string): string[] {
  return list(r, key, what, (p) => {
    if (typeof p !== "string") {
      throw new Error(`${what}: ${key} item is not a string`);
    }
    return p;
  });
}

function parseQuestion(raw: unknown): WorkQuestion {
  const w = "work question";
  const r = record(raw, w);
  return {
    kind: oneOf(r, "kind", ["choice", "text"] as const, w),
    text: str(r, "text", w),
    options: strings(r, "options", w),
    default: optCount(r, "default", w),
    defaultText: optStr(r, "defaultText", w),
    why: optStr(r, "why", w),
  };
}

function parseCardFields(raw: unknown): WorkCardFields {
  const w = "work card";
  const r = record(raw, w);
  return {
    workType: str(r, "workType", w),
    volume: optStr(r, "volume", w),
    fileFormat: optStr(r, "fileFormat", w),
    formatting: optStr(r, "formatting", w),
    variant: optStr(r, "variant", w),
    deadline: optStr(r, "deadline", w),
    sources: optStr(r, "sources", w),
  };
}

const emptyCard: WorkCardFields = { workType: "", volume: "", fileFormat: "", formatting: "", variant: "", deadline: "", sources: "" };

function parseHelper(raw: unknown): WorkHelper {
  const w = "work helper";
  const r = record(raw, w);
  return {
    steps: strings(r, "steps", w),
    submit: str(r, "submit", w),
    pitfalls: strings(r, "pitfalls", w),
    readyParts: strings(r, "readyParts", w),
    placeholders: strings(r, "placeholders", w),
  };
}

function parseAnswered(raw: unknown): WorkAnswered {
  const w = "work answered";
  const r = record(raw, w);
  return { round: optCount(r, "round", w), question: str(r, "question", w), answer: str(r, "answer", w), default: optBool(r, "default", w) };
}

/** Checks the shape of a work; a malformed reply is an error, not a default. */
export function parseWork(raw: unknown): TurnkeyWork {
  const w = "turnkey work";
  const r = record(raw, w);
  const writing = r.writing === null || r.writing === undefined ? null : parseWriting(r.writing);
  const card = r.card === null || r.card === undefined ? emptyCard : parseCardFields(r.card);
  const feasibility = r.feasibility === undefined || r.feasibility === null || r.feasibility === "" ? "" : oneOf(r, "feasibility", ["full", "partial", "impossible"] as const, w);
  return {
    id: str(r, "id", w),
    status: oneOf(r, "status", workStatuses, w),
    version: count(r, "version", w),
    writing,
    workType: card.workType,
    problem: str(r, "problem", w),
    message: str(r, "message", w),
    questionRound: optCount(r, "questionRound", w),
    questions: list(r, "questions", w, parseQuestion),
    hasCard: optBool(r, "hasCard", w),
    card,
    edited: strings(r, "edited", w),
    feasibility,
    feasibilityReason: optStr(r, "feasibilityReason", w),
    partial: optBool(r, "partial", w),
    assumptions: strings(r, "assumptions", w),
    helper: r.helper === null || r.helper === undefined ? null : parseHelper(r.helper),
    answered: list(r, "answered", w, parseAnswered),
    topic: optStr(r, "topic", w),
    subjectId: optStr(r, "subjectId", w),
    sendable: optBool(r, "sendable", w),
  };
}

export interface TurnkeyApi {
  /** The work of a work card. */
  work(workId: string): Promise<TurnkeyWork>;
  /** «Написать работу» on a confirmed work (a paid job). */
  startWriting(workId: string): Promise<TurnkeyWork>;
  /** «Утвердить план»: the sections are written. */
  approveOutline(workId: string): Promise<TurnkeyWork>;
  /** Saves the plan: the items in their order, number 0 is a new section. */
  editOutline(workId: string, items: OutlineItem[]): Promise<TurnkeyWork>;
  /** editOutline for the plan as shown at `version`; a changed work is refused (stale). */
  editOutlineAt(workId: string, version: number, items: OutlineItem[]): Promise<TurnkeyWork>;
  /** «Остановить»: cancels the writing job, the written sections stay. */
  stopWriting(workId: string): Promise<TurnkeyWork>;
  /** Section `number` again with the student's note; 0 is every section (or the plan of a blocked outline). */
  regenerate(workId: string, number: number, note: string): Promise<TurnkeyWork>;
  /** The work of an assignment; a missing one is the not-found error. */
  workOfAssignment(assignmentId: string): Promise<TurnkeyWork>;
  /**
   * «Сделать под ключ»: starts the triage of an assignment (a paid reader call; the click is the consent). An existing
   * work comes back without a new call; a call that did not go through comes back as the work with its problem.
   */
  startForAssignment(assignmentId: string): Promise<TurnkeyWork>;
  /** Sends the answers of the questions card; the next round is asked at once (a paid call). */
  answerQuestions(workId: string, answers: QuestionAnswer[]): Promise<TurnkeyWork>;
  /** «Пропустить»: every question takes its default. */
  skipQuestions(workId: string): Promise<TurnkeyWork>;
  /** Saves the student's card; unchanged fields follow the reader. */
  editUnderstanding(workId: string, card: WorkCardFields): Promise<TurnkeyWork>;
  /** «Подтвердить» on the card. */
  confirmRequirements(workId: string): Promise<TurnkeyWork>;
  /** Asks the waiting reader call again. */
  retry(workId: string): Promise<TurnkeyWork>;
  /** A question about helper step `step` (from 0); the answer is plain text. A refusal carries one of the fixed texts. */
  askStep(workId: string, step: number, question: string): Promise<string>;
}

/** Builds the turnkey API over a call function (the Wails runtime in the app, a fake in tests). */
export function turnkeyApi(call: CallByName): TurnkeyApi {
  return {
    work: async (id) => parseWork(await call(methods.work, id)),
    startWriting: async (id) => parseWork(await call(methods.startWriting, id)),
    approveOutline: async (id) => parseWork(await call(methods.approveOutline, id)),
    editOutline: async (id, items) =>
      parseWork(await call(methods.editOutline, id, items.map((i) => ({ number: i.number, title: i.title, words: i.words })))),
    editOutlineAt: async (id, version, items) =>
      parseWork(await call(methods.editOutlineAt, id, version, items.map((i) => ({ number: i.number, title: i.title, words: i.words })))),
    stopWriting: async (id) => parseWork(await call(methods.stopWriting, id)),
    regenerate: async (id, number, note) => parseWork(await call(methods.regenerate, id, number, note)),
    workOfAssignment: async (id) => parseWork(await call(methods.workOfAssignment, id)),
    startForAssignment: async (id) => parseWork(await call(methods.startForAssignment, id)),
    answerQuestions: async (id, answers) =>
      parseWork(await call(methods.answerQuestions, id, answers.map((a) => ({ question: a.question, option: a.option, text: a.text })))),
    skipQuestions: async (id) => parseWork(await call(methods.skipQuestions, id)),
    editUnderstanding: async (id, card) =>
      parseWork(
        await call(methods.editUnderstanding, id, {
          workType: card.workType,
          volume: card.volume,
          fileFormat: card.fileFormat,
          formatting: card.formatting,
          variant: card.variant,
          deadline: card.deadline,
          sources: card.sources,
        }),
      ),
    confirmRequirements: async (id) => parseWork(await call(methods.confirmRequirements, id)),
    retry: async (id) => parseWork(await call(methods.retry, id)),
    askStep: async (id, step, question) => {
      const r = record(await call(methods.askStep, id, step, question), "step answer");
      return str(r, "text", "step answer");
    },
  };
}
