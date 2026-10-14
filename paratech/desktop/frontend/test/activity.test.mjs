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
  id: "r1", lessonId: "l1", status: "COMPLETED", startedAt: "2026-10-15T10:00:00Z", stoppedAt: "2026-10-15T11:00:00Z",
  durationSeconds: 3600, hasDuration: true, problem: "", stoppable: false,
};
const job = {
  id: "j1", type: "stt", status: "WAITING_FOR_POWER", waiting: true, attempts: 0, maxAttempts: 3, progressPercent: 0,
  lastError: "", createdAt: "2026-10-15T10:00:00Z", updatedAt: "2026-10-15T10:00:00Z", nextRunAt: "", runNowOffered: true,
};

test("every waiting status has its own non-empty reason", () => {
  assert.deepEqual([...waitingStatuses].sort(), jobStatuses.filter((s) => s.startsWith("WAITING_")).sort());
  const reasons = waitingStatuses.map(jobStatusLabel);
  for (const [i, reason] of reasons.entries()) {
    assert.ok(reason.trim() !== "" && reason !== waitingStatuses[i], waitingStatuses[i]);
  }
  assert.equal(new Set(reasons).size, reasons.length);
  assert.equal(jobStatusLabel("WAITING_FOR_POWER"), "Ждёт питания от сети");
  assert.equal(jobStatusLabel("WAITING_FOR_NETWORK"), "Ждёт подключения к сети");
  assert.equal(jobStatusLabel("WAITING_FOR_QUOTA"), "Квота исчерпана — ждёт пополнения");
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
  assert.equal(jobTypeLabel("send_email"), "send_email");
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
});

test("parse rejects malformed items", () => {
  for (const raw of [null, "x", [], {}, { ...recording, stoppable: "yes" }, { ...recording, durationSeconds: Number.NaN }, { ...recording, id: 1 }]) {
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
  assert.deepEqual(calls, [[methods.listRecordings], [methods.listJobs], [methods.stopRecording, "r1"], [methods.runJobNow, "j1"]]);
  await assert.rejects(activityApi(async () => ({})).listJobs(), /jobs: reply is not a list/);
  await assert.rejects(activityApi(async () => [{}]).listRecordings(), /recording/);
});
