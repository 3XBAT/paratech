// Recordings and jobs bindings: calls into the Go core (internal/input/wails/activity.go). The core decides what is
// offered ("stoppable", "runNowOffered"); the page only shows it.

import type { CallByName } from "./settings";

/** Step of the whole chain of a recording (TASK-076), decided by the Go core. */
export type ChainStage = "recording" | "saving" | "queued" | "transcribing" | "summarizing" | "done" | "failed";
/** The step a queued or failed chain stopped at; "" otherwise. */
export type ChainStep = "" | "transcription" | "summary";
/** Why a queued chain waits; "" otherwise. */
export type QueueReason =
  | ""
  | "inLine"
  | "retry"
  | "power"
  | "network"
  | "quota"
  | "serviceNotConfigured"
  | "serviceUnavailable";
/** What failed; "" otherwise. */
export type ChainErrorCode = "" | "recordingFailed" | "transcriptionFailed" | "summaryFailed" | "homeworkFailed";
/** What the row offers besides Stop; runNow calls runJobNow(actionJobId). */
export type ChainAction = "" | "runNow" | "openModels" | "openConnections" | "checkSound";

/** One recording with the status of its whole chain, newest first. Times are RFC 3339 in UTC, "" when unknown. */
export interface RecordingItem {
  id: string;
  lessonId: string;
  /** Lesson title: imported, untrusted text, render as plain text only. "" without a lesson: show «Запись <дата, время>». */
  title: string;
  status: string;
  startedAt: string;
  stoppedAt: string;
  /** Final duration; meaningful only when hasDuration (the recording is COMPLETED). */
  durationSeconds: number;
  hasDuration: boolean;
  problem: string;
  stoppable: boolean;
  stage: ChainStage;
  step: ChainStep;
  /** Transcription progress, 0..100. */
  progressPercent: number;
  queueReason: QueueReason;
  errorCode: ChainErrorCode;
  /** Last error of the job the stage refers to (failed or retrying); "" otherwise. */
  jobError: string;
  action: ChainAction;
  actionJobId: string;
  /** Approximate time left of a queued or running transcription («≈ N мин осталось», TASK-084). */
  remainingSeconds: number;
  /** false: no time left to show. */
  hasRemaining: boolean;
}

/**
 * One job, newest first. For a waiting job (WAITING_FOR_*) the status is the waiting reason. Since TASK-076 the list
 * has neither the service jobs nor the jobs of the recording chain (stt, summarize, extract_homework): the recording
 * rows show those.
 */
export interface JobItem {
  id: string;
  type: string;
  status: string;
  waiting: boolean;
  attempts: number;
  maxAttempts: number;
  progressPercent: number;
  lastError: string;
  createdAt: string;
  updatedAt: string;
  nextRunAt: string;
  /** AC-22: "Run now" is offered (a job waiting for AC power). */
  runNowOffered: boolean;
}

/** Bound method names: "<Go package path>.<type>.<method>" (checked by a Go test). */
export const methods = {
  listRecordings: "paratech/desktop/internal/input/wails.ActivityService.ListRecordings",
  listJobs: "paratech/desktop/internal/input/wails.ActivityService.ListJobs",
  stopRecording: "paratech/desktop/internal/input/wails.ActivityService.StopRecording",
  runJobNow: "paratech/desktop/internal/input/wails.ActivityService.RunJobNow",
} as const;

/** Messages of the errors the Go binding returns (checked by a Go test); the page may match them. */
export const errorMessages = {
  unavailable: "recordings and jobs are unavailable",
  invalidId: "invalid id",
  notFound: "not found",
  recordingNotRunning: "the recording is not running in this session",
  cannotRunNow: "the job cannot be run now",
  actionFailed: "the action failed",
} as const;

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${what}: reply is not an object`);
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

function bool(r: Record<string, unknown>, key: string, what: string): boolean {
  const v = r[key];
  if (typeof v !== "boolean") {
    throw new Error(`${what}: ${key} is not a boolean`);
  }
  return v;
}

function oneOf<T extends string>(r: Record<string, unknown>, key: string, what: string, allowed: readonly T[]): T {
  const v = str(r, key, what);
  if (!(allowed as readonly string[]).includes(v)) {
    throw new Error(`${what}: unknown ${key}`);
  }
  return v as T;
}

const stages = ["recording", "saving", "queued", "transcribing", "summarizing", "done", "failed"] as const;
const steps = ["", "transcription", "summary"] as const;
const queueReasons = [
  "",
  "inLine",
  "retry",
  "power",
  "network",
  "quota",
  "serviceNotConfigured",
  "serviceUnavailable",
] as const;
const errorCodes = ["", "recordingFailed", "transcriptionFailed", "summaryFailed", "homeworkFailed"] as const;
const actions = ["", "runNow", "openModels", "openConnections", "checkSound"] as const;

function list<T>(raw: unknown, what: string, parse: (item: unknown) => T): T[] {
  if (!Array.isArray(raw)) {
    throw new Error(`${what}: reply is not a list`);
  }
  return raw.map(parse);
}

/** Checks the shape of one recording; a malformed reply is an error, not a default. */
export function parseRecording(raw: unknown): RecordingItem {
  const w = "recording";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    lessonId: str(r, "lessonId", w),
    title: str(r, "title", w),
    status: str(r, "status", w),
    startedAt: str(r, "startedAt", w),
    stoppedAt: str(r, "stoppedAt", w),
    durationSeconds: num(r, "durationSeconds", w),
    hasDuration: bool(r, "hasDuration", w),
    problem: str(r, "problem", w),
    stoppable: bool(r, "stoppable", w),
    stage: oneOf(r, "stage", w, stages),
    step: oneOf(r, "step", w, steps),
    progressPercent: num(r, "progressPercent", w),
    queueReason: oneOf(r, "queueReason", w, queueReasons),
    errorCode: oneOf(r, "errorCode", w, errorCodes),
    jobError: str(r, "jobError", w),
    action: oneOf(r, "action", w, actions),
    actionJobId: str(r, "actionJobId", w),
    remainingSeconds: num(r, "remainingSeconds", w),
    hasRemaining: bool(r, "hasRemaining", w),
  };
}

/** Checks the shape of one job; a malformed reply is an error, not a default. */
export function parseJob(raw: unknown): JobItem {
  const w = "job";
  const r = record(raw, w);
  return {
    id: str(r, "id", w),
    type: str(r, "type", w),
    status: str(r, "status", w),
    waiting: bool(r, "waiting", w),
    attempts: num(r, "attempts", w),
    maxAttempts: num(r, "maxAttempts", w),
    progressPercent: num(r, "progressPercent", w),
    lastError: str(r, "lastError", w),
    createdAt: str(r, "createdAt", w),
    updatedAt: str(r, "updatedAt", w),
    nextRunAt: str(r, "nextRunAt", w),
    runNowOffered: bool(r, "runNowOffered", w),
  };
}

export interface ActivityApi {
  listRecordings(): Promise<RecordingItem[]>;
  listJobs(): Promise<JobItem[]>;
  stopRecording(id: string): Promise<void>;
  runJobNow(id: string): Promise<void>;
}

/** Builds the recordings and jobs API over a call function (the Wails runtime in the app, a fake in tests). */
export function activityApi(call: CallByName): ActivityApi {
  return {
    listRecordings: async () => list(await call(methods.listRecordings), "recordings", parseRecording),
    listJobs: async () => list(await call(methods.listJobs), "jobs", parseJob),
    stopRecording: async (id) => {
      await call(methods.stopRecording, id);
    },
    runJobNow: async (id) => {
      await call(methods.runJobNow, id);
    },
  };
}
