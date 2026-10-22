// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { activityApi, errorMessages, methods, parseJob, parseRecording } from "../.test-build/bindings/activity.js";
import {
  actionErrorText,
  formatDuration,
  formatTime,
  jobStatusLabel,
  jobStatusLabels,
  jobTypeLabel,
  loadErrorText,
  recordingDurationText,
  recordingStatusLabel,
  recordingStatusLabels,
  waitingStatuses,
} from "../.test-build/activityView.js";

// Mirrors domain/recording/recording and domain/jobs/job.
const recordingStatuses = ["STARTING", "RECORDING", "STOPPING", "FINALIZING", "COMPLETED", "FAILED", "RECOVERY_REQUIRED"];
const jobStatuses = [
  "QUEUED", "RUNNING", "COMPLETED", "RETRY_WAIT", "CANCELLED", "FAILED",
  "WAITING_FOR_NETWORK", "WAITING_FOR_QUOTA", "WAITING_FOR_POWER",
];

const recording = {
  id: "r1", lessonId: "l1", title: "История", subject: "", kind: "", teacher: "",
  status: "COMPLETED", startedAt: "2026-10-15T10:00:00Z",
  stoppedAt: "2026-10-15T11:00:00Z", durationSeconds: 3600, hasDuration: true, problem: "", stoppable: false,
  stage: "queued", step: "transcription", progressPercent: 0, partsDone: 0, parts: 0, queueReason: "power", errorCode: "",
  jobError: "", action: "runNow", actionJobId: "j1", retryWork: "", canRedoSummary: false, lectureTextReady: false,
  remainingSeconds: 1500, hasRemaining: true,
  advice: null,
};
const job = {
  id: "j1", type: "stt", status: "WAITING_FOR_POWER", waiting: true, attempts: 0, maxAttempts: 3, progressPercent: 0,
  lastError: "", createdAt: "2026-10-15T10:00:00Z", updatedAt: "2026-10-15T10:00:00Z", nextRunAt: "", runNowOffered: true,
  // TASK-094: the assignment of a draft job; "" for the others.
  assignmentId: "", assignmentTitle: "", assignmentSubject: "", label: "",
};

test("every waiting status has its own non-empty reason", () => {
  assert.deepEqual([...waitingStatuses].sort(), jobStatuses.filter((s) => s.startsWith("WAITING_")).sort());
  const reasons = waitingStatuses.map(jobStatusLabel);
  for (const [i, reason] of reasons.entries()) {
    assert.ok(reason.trim() !== "" && reason !== waitingStatuses[i], waitingStatuses[i]);
  }
  assert.equal(new Set(reasons).size, reasons.length);
  assert.equal(jobStatusLabel("WAITING_FOR_POWER"), "Ждёт зарядку");
  assert.equal(jobStatusLabel("WAITING_FOR_NETWORK"), "Ждёт интернет");
  assert.equal(jobStatusLabel("WAITING_FOR_QUOTA"), "Минуты обработки закончились");
});

test("every status has a distinct Russian label and unknown ones are shown raw", () => {
  assert.deepEqual(Object.keys(recordingStatusLabels).sort(), [...recordingStatuses].sort());
  assert.deepEqual(Object.keys(jobStatusLabels).sort(), [...jobStatuses].sort());
  for (const [labels, of] of [[recordingStatuses.map(recordingStatusLabel), recordingStatuses], [jobStatuses.map(jobStatusLabel), jobStatuses]]) {
    assert.equal(new Set(labels).size, labels.length);
    labels.forEach((l, i) => assert.notEqual(l, of[i]));
  }
  assert.equal(recordingStatusLabel("PAUSED"), "PAUSED");
  assert.equal(jobStatusLabel("toString"), "toString");
  assert.equal(jobTypeLabel("stt"), "Распознавание речи");
  assert.equal(jobTypeLabel("calendar_sync"), "Синхронизация календаря");
  // TASK-076: send_email and homework_draft are the jobs the page still lists, so they have labels now.
  assert.equal(jobTypeLabel("send_email"), "Отправка письма");
  assert.equal(jobTypeLabel("homework_draft"), "Решение домашнего задания");
  assert.equal(jobTypeLabel("new_kind"), "new_kind");
});

test("duration is h:mm:ss", () => {
  assert.equal(formatDuration(0), "0:00:00");
  assert.equal(formatDuration(59.9), "0:00:59");
  assert.equal(formatDuration(3661), "1:01:01");
  assert.equal(formatDuration(36000 + 600 + 5), "10:10:05");
  assert.equal(formatDuration(-1), "—");
  assert.equal(formatDuration(Number.NaN), "—");
});

test("an empty or bad time is a dash", () => {
  assert.equal(formatTime(""), "—");
  assert.equal(formatTime("yesterday"), "—");
  assert.notEqual(formatTime("2026-10-15T10:00:00Z"), "—");
});

test("errors of the core map to Russian text, unknown ones to a generic text", () => {
  for (const msg of Object.values(errorMessages)) {
    assert.match(actionErrorText(new Error(msg)), /[а-яё]/i);
  }
  assert.equal(actionErrorText(new Error(errorMessages.cannotRunNow)), "Эту задачу сейчас нельзя запустить.");
  // TASK-086: the retry and the redo of the summary; an invalid work is a bug of the page, so the generic text.
  assert.equal(actionErrorText(new Error("the work is already running")), "Уже выполняется — дождитесь окончания.");
  assert.equal(actionErrorText(new Error("nothing to retry")), "Повторять нечего — уже готово.");
  assert.equal(actionErrorText(new Error(errorMessages.invalidWork)), actionErrorText(new Error("unknown")));
  assert.equal(actionErrorText({ message: errorMessages.notFound }), actionErrorText(errorMessages.notFound));
  const generic = actionErrorText(new Error("<b>boom</b> C:\\secret"));
  assert.equal(generic, "Не удалось выполнить действие.");
  assert.equal(actionErrorText(undefined), generic);
  assert.equal(actionErrorText(new Error("constructor")), generic);
  assert.equal(loadErrorText(new Error(errorMessages.unavailable), "jobs"), "Записи и задачи сейчас недоступны.");
  assert.equal(loadErrorText(new Error("x"), "recordings"), "Не удалось загрузить записи.");
  assert.equal(loadErrorText(new Error("x"), "jobs"), "Не удалось загрузить задачи.");
});

test("a running recording shows the time elapsed by the client clock", () => {
  const start = Date.parse("2026-10-15T10:00:00Z");
  const running = { ...recording, status: "RECORDING", stoppedAt: "", durationSeconds: 0, hasDuration: false };
  assert.equal(recordingDurationText(running, start + 3661_000), "1:01:01");
  assert.equal(recordingDurationText(running, start - 5000), "0:00:00");
  assert.equal(recordingDurationText({ ...running, startedAt: "" }, start), "—");
  assert.equal(recordingDurationText({ ...running, startedAt: "bad" }, start), "—");
  assert.equal(recordingDurationText({ ...running, status: "STOPPING" }, start + 60_000), "—");
  assert.equal(recordingDurationText(recording, start + 99_999_000), "1:00:00");
});

test("parse accepts well-formed items and drops extra fields", () => {
  assert.deepEqual(parseRecording({ ...recording, path: "C:\\x.wav" }), recording);
  assert.deepEqual(parseJob({ ...job, payload: "{}" }), job);
  for (const queueReason of ["serviceNotConfigured", "serviceUnavailable"]) {
    assert.equal(parseRecording({ ...recording, queueReason }).queueReason, queueReason);
  }
  // TASK-086: «Повторить» and «Сделать конспект заново».
  const failed = { ...recording, stage: "failed", action: "retry", actionJobId: "", retryWork: "homework", canRedoSummary: true };
  assert.deepEqual(parseRecording(failed), failed);
  assert.equal(parseRecording({ ...failed, action: "openConnections", retryWork: "summary" }).retryWork, "summary");
});

test("parse reads the lesson snapshot and an older core without it as empty (TASK-088)", () => {
  const lesson = { subject: "Матанализ", kind: "lecture", teacher: "Иванов И. И." };
  assert.deepEqual(parseRecording({ ...recording, ...lesson }), { ...recording, ...lesson });
  const { subject, kind, teacher, ...older } = recording;
  assert.deepEqual(parseRecording(older), recording);
  for (const bad of [{ subject: 1 }, { kind: null }, { teacher: ["x"] }]) {
    assert.throws(() => parseRecording({ ...recording, ...bad }), /recording/, JSON.stringify(bad));
  }
});

test("parse rejects malformed items", () => {
  for (const raw of [null, "x", [], {}, { ...recording, stoppable: "yes" }, { ...recording, durationSeconds: Number.NaN }, { ...recording, id: 1 },
    { ...recording, title: undefined }, { ...recording, stage: "paused" }, { ...recording, queueReason: "rain" },
    { ...recording, action: "delete" }, { ...recording, errorCode: 1 },
    { ...recording, remainingSeconds: undefined }, { ...recording, hasRemaining: "yes" },
    { ...recording, retryWork: "transcription" }, { ...recording, retryWork: undefined },
    { ...recording, canRedoSummary: "true" }, { ...recording, canRedoSummary: undefined }]) {
    assert.throws(() => parseRecording(raw), /recording/);
  }
  for (const raw of [null, 5, {}, { ...job, runNowOffered: 1 }, { ...job, attempts: "0" }, { ...job, status: null }]) {
    assert.throws(() => parseJob(raw), /job/);
  }
});

test("the api calls the bound methods and checks the replies", async () => {
  const calls = [];
  const replies = { [methods.listRecordings]: [recording], [methods.listJobs]: [job] };
  const api = activityApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return replies[method] ?? null;
  });
  assert.deepEqual(await api.listRecordings(), [recording]);
  assert.deepEqual(await api.listJobs(), [job]);
  await api.stopRecording("r1");
  await api.runJobNow("j1");
  await api.retryPostProcessing("r1", "homework");
  await api.redoSummary("r2");
  assert.deepEqual(calls, [
    [methods.listRecordings], [methods.listJobs], [methods.stopRecording, "r1"], [methods.runJobNow, "j1"],
    [methods.retryPostProcessing, "r1", "homework"], [methods.redoSummary, "r2"],
  ]);
  assert.equal(methods.retryPostProcessing, "paratech/desktop/internal/input/wails.ActivityService.RetryPostProcessing");
  assert.equal(methods.redoSummary, "paratech/desktop/internal/input/wails.ActivityService.RedoSummary");
  await assert.rejects(activityApi(async () => { throw new Error(errorMessages.alreadyRunning); }).redoSummary("r1"), /already running/);
  await assert.rejects(activityApi(async () => ({})).listJobs(), /jobs: reply is not a list/);
  await assert.rejects(activityApi(async () => [{}]).listRecordings(), /recording/);
});

test("a draft job row is named by the core and opens its assignment (TASK-094)", async () => {
  const { jobAssignment, jobTitle } = await import("../.test-build/activityView.js");
  const draft = {
    ...job, type: "homework_draft", assignmentId: "a1", assignmentTitle: "Задача 1", assignmentSubject: "Матанализ",
    label: "ДЗ: Задача 1 · Матанализ",
  };
  assert.deepEqual(parseJob(draft), draft);
  assert.equal(jobTitle(draft), "ДЗ: Задача 1 · Матанализ");
  assert.equal(jobAssignment(draft), "a1");
  // Without a label (another job, or the assignment is gone) the row keeps its type and opens nothing.
  assert.equal(jobTitle({ ...draft, label: "" }), "Решение домашнего задания");
  assert.equal(jobTitle(job), jobTypeLabel("stt"));
  assert.equal(jobAssignment(job), "");
  for (const key of ["assignmentId", "assignmentTitle", "assignmentSubject", "label"]) {
    assert.throws(() => parseJob({ ...draft, [key]: null }), /job/, key);
  }

  const { readFileSync } = await import("node:fs");
  // The task row (TASK-098: JobRow next to RecordingRow) is titled by jobTitle and opens the assignment it names.
  const jobRow = readFileSync(new URL("../src/pages/RecordingRow.tsx", import.meta.url), "utf8");
  assert.match(jobRow, /const title = jobTitle\(j\);/);
  assert.match(jobRow, /title=\{title\}/);
  assert.match(jobRow, /\} else if \(assignment !== "" && onOpenAssignment\) \{\s*onOpenAssignment\(assignment\);/);
  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /onOpenAssignment=\{onOpenAssignment\}/);
  const app = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
  // «Сегодня» holds the recordings page until TASK-125 replaces it (pages/TodayPage.tsx).
  const today = readFileSync(new URL("../src/pages/TodayPage.tsx", import.meta.url), "utf8");
  assert.match(today, /<RecordingsPage onOpen=\{props\.onOpen\} onOpenAssignment=\{props\.onOpenAssignment\} \/>/);
  assert.match(app, /<TodayPage onOpen=\{props\.onOpen\} onOpenAssignment=\{props\.onOpenAssignment\} \/>/);
  assert.match(app, /onOpenAssignment=\{openAssignment\}/);
});
