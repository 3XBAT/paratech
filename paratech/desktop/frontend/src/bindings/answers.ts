// Answer card bindings (TASK-296): calls into the Go core (internal/input/wails/answers.go). The card reads a stored
// answer and lists its sources. Every text (question, claims, quotes, labels, titles) is model, lesson or imported
// text: plain text only. A note is opened by ids, never by a path.

import { type KnowledgeSection, knowledgeSections, type NoteLocatorKind, noteLocatorKinds } from "./knowledgeBase.js";
import type { CallByName } from "./settings.js";

/** Bound method names: "<Go package path>.<type>.<method>". */
export const methods = {
  answer: "paratech/desktop/internal/input/wails.AnswerService.Answer",
} as const;

/** Messages of the errors the Go binding returns (answers.go); the card shows them as they are. */
export const errorMessages = {
  notFound: "Ответ не найден.",
  unavailable: "Не удалось открыть ответ. Попробуйте ещё раз.",
} as const;

/**
 * The place a citation opens, as a KnowledgeHit. recordingId, startMs and time are empty for the student's own notes
 * (section "ownNote"), whose noteId, materialId, locatorKind, locatorN and label name the place. A removed source
 * does not open: its label is «источник удалён».
 */
export interface AnswerSource {
  recordingId: string;
  section: KnowledgeSection;
  index: number;
  startMs: number;
  /** "12:05" for a lecture block or a transcript segment, "" otherwise. */
  time: string;
  /** The lesson as it was when the answer was made; "" for an ad-hoc recording. */
  title: string;
  /** RFC 3339 in UTC; "" when unknown. */
  startedAt: string;
  noteId: string;
  materialId: string;
  locatorKind: NoteLocatorKind;
  locatorN: number;
  label: string;
  removed: boolean;
}

/** A checked quote and the index of its place in AnswerView.sources. */
export interface AnswerCite {
  quote: string;
  source: number;
}

export interface AnswerClaim {
  text: string;
  /** False: the statement is not from the lessons and is never shown as if it were. */
  fromLessons: boolean;
  cites: AnswerCite[];
}

export interface AnswerView {
  id: string;
  question: string;
  /** "" for a question about the whole base. */
  subjectId: string;
  /** RFC 3339 in UTC. */
  createdAt: string;
  claims: AnswerClaim[];
  /** The parts the lessons do not answer. */
  missing: string[];
  /** In the core's order; a citation points here by index. */
  sources: AnswerSource[];
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

function count(r: Record<string, unknown>, key: string, what: string): number {
  const v = r[key];
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
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

function oneOf<T extends string>(r: Record<string, unknown>, key: string, allowed: readonly T[], what: string): T {
  const v = r[key];
  if (typeof v !== "string" || !allowed.includes(v as T)) {
    throw new Error(`${what}: ${key} is not one of ${allowed.join(", ")}`);
  }
  return v as T;
}

function items<T>(r: Record<string, unknown>, key: string, what: string, parse: (raw: unknown) => T): T[] {
  const v = r[key];
  if (!Array.isArray(v)) {
    throw new Error(`${what}: ${key} is not a list`);
  }
  return v.map(parse);
}

/** Checks one source; a malformed one is an error. A removed source is not checked against its section (it never opens). */
export function parseSource(raw: unknown): AnswerSource {
  const w = "answer source";
  const r = record(raw, w);
  const section = oneOf(r, "section", knowledgeSections, w);
  const noteId = str(r, "noteId", w);
  const materialId = str(r, "materialId", w);
  const locatorKind = oneOf(r, "locatorKind", noteLocatorKinds, w);
  const locatorN = count(r, "locatorN", w);
  const label = str(r, "label", w);
  const removed = bool(r, "removed", w);
  const own = section === "ownNote";
  const fits = removed
    ? true
    : own
      ? noteId !== "" && materialId !== "" && locatorKind !== "" && locatorN > 0 && label !== ""
      : noteId === "" && materialId === "" && locatorKind === "" && locatorN === 0 && label === "";
  if (!fits) {
    throw new Error(`${w}: the note place does not fit the section`);
  }
  return {
    recordingId: str(r, "recordingId", w),
    section,
    index: count(r, "index", w),
    startMs: count(r, "startMs", w),
    time: str(r, "time", w),
    title: str(r, "title", w),
    startedAt: str(r, "startedAt", w),
    noteId,
    materialId,
    locatorKind,
    locatorN,
    label,
    removed,
  };
}

function parseCite(raw: unknown): AnswerCite {
  const w = "answer citation";
  const r = record(raw, w);
  return { quote: str(r, "quote", w), source: count(r, "source", w) };
}

function parseClaim(raw: unknown): AnswerClaim {
  const w = "answer claim";
  const r = record(raw, w);
  return { text: str(r, "text", w), fromLessons: bool(r, "fromLessons", w), cites: items(r, "cites", w, parseCite) };
}

function parseMissing(raw: unknown): string {
  if (typeof raw !== "string") {
    throw new Error("answer: a missing part is not a string");
  }
  return raw;
}

/** Checks the whole reply; a malformed reply is an error, not a default. */
export function parseAnswer(raw: unknown): AnswerView {
  const w = "answer";
  const r = record(raw, w);
  const sources = items(r, "sources", w, parseSource);
  const claims = items(r, "claims", w, parseClaim);
  for (const c of claims) {
    if (c.cites.some((ct) => ct.source >= sources.length)) {
      throw new Error(`${w}: a citation points past the sources`);
    }
  }
  return {
    id: str(r, "id", w),
    question: str(r, "question", w),
    subjectId: str(r, "subjectId", w),
    createdAt: str(r, "createdAt", w),
    claims,
    missing: items(r, "missing", w, parseMissing),
    sources,
  };
}

export interface AnswersApi {
  /** The answer with the given id for its card. Errors: errorMessages. */
  answer(id: string): Promise<AnswerView>;
}

/** Builds the answers API over a call function (the Wails runtime in the app, a fake in tests). */
export function answersApi(call: CallByName): AnswersApi {
  return {
    answer: async (id) => parseAnswer(await call(methods.answer, id)),
  };
}

const knownErrors: ReadonlySet<string> = new Set(Object.values(errorMessages));

/** The core's fixed text of a failed call as it is; anything else (a malformed reply) gets the generic text. */
export function errorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return knownErrors.has(msg) ? msg : errorMessages.unavailable;
}
