// Status chips (src/statusPresentation.ts). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { jobStatusView, recordingStatusView, showsProgress } from "../.test-build/statusPresentation.js";

const recordingStatuses = ["STARTING", "RECORDING", "STOPPING", "FINALIZING", "COMPLETED", "FAILED", "RECOVERY_REQUIRED"];
const jobStatuses = [
  "QUEUED", "RUNNING", "COMPLETED", "RETRY_WAIT", "CANCELLED", "FAILED",
  "WAITING_FOR_NETWORK", "WAITING_FOR_QUOTA", "WAITING_FOR_POWER",
];
const kinds = new Set(["rec", "run", "wait", "net", "ok", "fail", "neutral"]);
const rec = (status, over = {}) => ({ status, startedAt: "", durationSeconds: 0, hasDuration: false, ...over });
const job = (status, over = {}) => ({ type: "stt", status, waiting: status.startsWith("WAITING_"), progressPercent: 0, ...over });

test("every known status has a chip with a word and an icon (the recording one has the dot)", () => {
  const views = [...recordingStatuses.map((s) => recordingStatusView(rec(s), 0)), ...jobStatuses.map((s) => jobStatusView(job(s)))];
  for (const v of views) {
    assert.ok(kinds.has(v.kind), v.kind);
    assert.match(v.label, /[а-яё]/i);
    assert.ok(v.kind === "rec" ? v.icon === null : v.icon !== null, v.label);
  }
});

test("waiting jobs say what they wait for and that they go on by themselves", () => {
  assert.deepEqual(
    ["WAITING_FOR_POWER", "WAITING_FOR_NETWORK", "WAITING_FOR_QUOTA"].map((s) => {
      const v = jobStatusView(job(s));
      return [v.kind, v.label];
    }),
    [["wait", "Ждёт зарядку"], ["net", "Ждёт интернет"], ["wait", "Минуты обработки закончились"]],
  );
  assert.match(jobStatusView(job("WAITING_FOR_NETWORK")).hint, /само/);
  assert.match(jobStatusView(job("WAITING_FOR_POWER")).hint, /сама после подключения питания/);
});

test("a running job is named by its type and shows its progress", () => {
  assert.deepEqual(jobStatusView(job("RUNNING", { progressPercent: 64 })), {
    kind: "run", icon: "wave", label: "Расшифровка", value: "64 %", hint: "",
  });
  assert.equal(jobStatusView(job("RUNNING", { type: "summarize" })).label, "Пишется конспект");
  assert.equal(jobStatusView(job("RUNNING", { type: "summarize" })).value, "");
  assert.equal(jobStatusView(job("RUNNING", { type: "new_kind" })).label, "Выполняется");
  assert.equal(jobStatusView(job("COMPLETED", { type: "summarize" })).label, "Конспект готов");
  assert.equal(jobStatusView(job("COMPLETED")).label, "Готово");
  assert.equal(showsProgress({ status: "RUNNING", progressPercent: 1 }), true);
  assert.equal(showsProgress({ status: "RUNNING", progressPercent: 0 }), false);
  assert.equal(showsProgress({ status: "QUEUED", progressPercent: 50 }), false);
});

test("a running recording shows its time; unknown statuses are shown as is", () => {
  const start = Date.parse("2026-10-15T10:00:00Z");
  const v = recordingStatusView(rec("RECORDING", { startedAt: "2026-10-15T10:00:00Z" }), start + 61_000);
  assert.deepEqual([v.kind, v.label, v.value], ["rec", "Идёт запись", "0:01:01"]);
  assert.equal(recordingStatusView(rec("COMPLETED", { hasDuration: true, durationSeconds: 60 }), 0).value, "");
  assert.deepEqual(recordingStatusView(rec("PAUSED"), 0), { kind: "neutral", icon: null, hint: "", label: "PAUSED", value: "" });
  assert.deepEqual(jobStatusView(job("NEW")), { kind: "neutral", icon: null, hint: "", label: "NEW", value: "" });
  assert.equal(jobStatusView(job("WAITING_FOR_X", { waiting: true })).kind, "wait", "the core says it waits");
  assert.equal(jobStatusView(job("toString")).label, "toString");
});
