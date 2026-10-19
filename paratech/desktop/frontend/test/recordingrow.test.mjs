// One row per recording with the status of its whole chain, and the one-second clock (TASK-076). Runs on the modules
// compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { formatTime } from "../.test-build/activityView.js";
import { nextTickDelay, SecondTicker, tickMs } from "../.test-build/clock.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";
import {
  actionLabels,
  actionPages,
  anyRecording,
  chainStatusView,
  errorTexts,
  finishedKey,
  pageGroups,
  queueLine,
  queueReasonHints,
  queueReasonTexts,
  recordingRowView,
  rowAction,
  rowTitle,
  stageLabels,
} from "../.test-build/recordingRowView.js";

const { renderToStaticMarkup } = server;

// Mirror the chain codes of internal/cases/activity/types.go.
const stages = ["recording", "saving", "queued", "transcribing", "summarizing", "done", "failed"];
const reasons = ["inLine", "retry", "power", "network", "quota", "serviceNotConfigured", "serviceUnavailable"];
const errorCodes = ["recordingFailed", "transcriptionFailed", "summaryFailed", "homeworkFailed"];
const actions = ["runNow", "openModels", "openConnections", "checkSound"];

const started = "2026-10-18T17:19:35Z";
const startMs = Date.parse(started);
const row = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", status: "COMPLETED", startedAt: started, stoppedAt: "",
  durationSeconds: 0, hasDuration: false, problem: "", stoppable: false, stage: "done", step: "", progressPercent: 0,
  queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "", ...over,
});

test("every chain code has its own Russian text", () => {
  assert.deepEqual(Object.keys(stageLabels), stages);
  assert.deepEqual(Object.keys(queueReasonTexts), reasons);
  assert.deepEqual(Object.keys(queueReasonHints), reasons);
  assert.deepEqual(Object.keys(errorTexts), errorCodes);
  assert.deepEqual(Object.keys(actionLabels), actions);
  for (const texts of [stageLabels, queueReasonTexts, queueReasonHints, errorTexts, actionLabels]) {
    const values = Object.values(texts);
    assert.equal(new Set(values).size, values.length);
    values.forEach((v) => assert.match(v, /[а-яё]/i));
  }
});

test("the row is titled by the lesson, a manual recording by its start", () => {
  assert.deepEqual(rowTitle(row()), { text: "Матанализ", isTime: false });
  assert.deepEqual(rowTitle(row({ title: "  " })), { text: `Запись ${formatTime(started)}`, isTime: true });
  assert.deepEqual(rowTitle(row({ title: "", startedAt: "" })), { text: "Запись", isTime: true });

  // With a lesson title the start time moves to the meta line; the final duration follows it.
  assert.equal(recordingRowView(row({ hasDuration: true, durationSeconds: 5400 }), 0).meta, `${formatTime(started)} · Длительность 1:30:00`);
  assert.equal(recordingRowView(row({ title: "", hasDuration: true, durationSeconds: 61 }), 0).meta, "Длительность 0:01:01");
  assert.equal(recordingRowView(row({ title: "" }), 0).meta, "");
});

test("one status per stage, in the order of the chain", () => {
  const at = (over) => chainStatusView(row(over), startMs + 42_000);
  assert.deepEqual(at({ stage: "recording", status: "RECORDING" }), {
    kind: "rec", icon: null, label: "Идёт запись", value: "0:00:42", hint: "Окно можно закрыть — запись продолжится.",
  });
  assert.equal(at({ stage: "recording", status: "STARTING" }).value, "", "no time before the capture runs");
  assert.equal(at({ stage: "saving", status: "FINALIZING" }).label, "Сохраняется");
  assert.match(at({ stage: "saving", status: "RECOVERY_REQUIRED" }).hint, /восстановится/);
  assert.deepEqual([at({ stage: "transcribing", progressPercent: 64 }).label, at({ stage: "transcribing", progressPercent: 64 }).value],
    ["Расшифровка", "64 %"]);
  assert.equal(at({ stage: "transcribing" }).value, "");
  assert.equal(at({ stage: "summarizing" }).label, "Краткое содержание");
  assert.deepEqual([at({ stage: "done" }).label, at({ stage: "done" }).kind], ["Конспект готов", "ok"]);
  assert.equal(recordingRowView(row({ stage: "transcribing", progressPercent: 64 }), 0).progress, 64);
  assert.equal(recordingRowView(row({ stage: "queued", progressPercent: 64 }), 0).progress, null);
});

test("a queued chain says why it waits and which step is next", () => {
  for (const reason of reasons) {
    const v = chainStatusView(row({ stage: "queued", step: "summary", queueReason: reason }), 0);
    assert.equal(v.label, `В очереди · ${queueReasonTexts[reason]}`, reason);
    assert.equal(v.hint, `Дальше краткое содержание. ${queueReasonHints[reason]}`, reason);
  }
  const power = chainStatusView(row({ stage: "queued", step: "transcription", queueReason: "power" }), 0);
  assert.deepEqual([power.label, power.kind, power.icon], ["В очереди · ждёт питания от сети", "wait", "plug"]);
  assert.match(power.hint, /^Дальше расшифровка\./);
  assert.equal(chainStatusView(row({ stage: "queued", queueReason: "network" }), 0).kind, "net");
});

test("a summary service problem is named by the core's reason, not by no network (TASK-083)", () => {
  const off = row({ stage: "queued", step: "summary", queueReason: "serviceNotConfigured", action: "openConnections", jobError: "network unavailable" });
  const v = recordingRowView(off, 0);
  assert.deepEqual([v.status.label, v.status.kind, v.status.icon], ["В очереди · сервис не настроен", "wait", "warn"]);
  assert.equal(
    v.status.hint,
    "Дальше краткое содержание. Сервис конспектов не настроен. Укажите адрес и ключ в «Подключения» → «Конспекты и задания». Запись сохранена.",
  );
  assert.deepEqual(v.action, { kind: "open", page: "connections", label: "Открыть подключения" });
  const offHtml = render(off);
  assert.ok(offHtml.includes("Сервис конспектов не настроен."));
  assert.ok(offHtml.includes("Открыть подключения"));
  assert.ok(!offHtml.includes("нет сети"));

  const down = recordingRowView(row({ stage: "queued", step: "summary", queueReason: "serviceUnavailable", jobError: "hub: unavailable" }), 0);
  assert.deepEqual([down.status.label, down.status.kind, down.status.icon], ["В очереди · сервис недоступен", "wait", "clock"]);
  assert.equal(down.status.hint, "Дальше краткое содержание. Сервис конспектов недоступен, повторим автоматически.");
  assert.equal(down.action, null);

  // Only the reason decides: a network error text does not turn a service reason into no network, nor the reverse.
  const net = chainStatusView(row({ stage: "queued", step: "summary", queueReason: "network", jobError: "hub: not configured" }), 0);
  assert.deepEqual([net.label, net.kind], ["В очереди · нет сети", "net"]);
  assert.equal(net.hint, "Дальше краткое содержание. Продолжится само, как только появится сеть.");
});

test("a failed chain names what failed and offers the core's action", () => {
  for (const code of errorCodes) {
    const v = chainStatusView(row({ stage: "failed", errorCode: code }), 0);
    assert.deepEqual([v.label, v.kind, v.hint], ["Ошибка", "fail", errorTexts[code]], code);
  }
  assert.deepEqual(rowAction(row({ action: "runNow", actionJobId: "j7" })), { kind: "runNow", jobId: "j7", label: "Запустить сейчас" });
  assert.equal(rowAction(row({ action: "runNow", actionJobId: "" })), null, "no job to run");
  assert.deepEqual(rowAction(row({ action: "openModels" })), { kind: "open", page: "models", label: "Открыть модели речи" });
  assert.deepEqual(rowAction(row({ action: "openConnections" })), { kind: "open", page: "connections", label: "Открыть подключения" });
  assert.deepEqual(rowAction(row({ action: "checkSound" })), { kind: "open", page: "diagnostics", label: "Проверить звук" });
  assert.deepEqual(Object.keys(actionPages), ["openModels", "openConnections", "checkSound"]);
  assert.equal(rowAction(row()), null);
});

test("the core's notes are shown once each", () => {
  assert.deepEqual(recordingRowView(row({ problem: "ended early", jobError: "model missing" }), 0).notes, ["ended early", "model missing"]);
  assert.deepEqual(recordingRowView(row({ problem: "same", jobError: "same" }), 0).notes, ["same"]);
  assert.deepEqual(recordingRowView(row(), 0).notes, []);
});

test("the queue line counts rows and jobs waiting for their turn; the clock ticks only while a recording runs", () => {
  const rows = [row({ stage: "queued", queueReason: "inLine" }), row({ stage: "queued", queueReason: "power" }), row()];
  assert.equal(queueLine(rows, [{ status: "QUEUED" }, { status: "RUNNING" }]), "В очереди 2 задачи");
  assert.equal(queueLine([], []), "");
  assert.equal(anyRecording(rows), false);
  assert.equal(anyRecording([row({ stage: "recording", status: "STARTING" })]), false);
  assert.equal(anyRecording([...rows, row({ stage: "recording", status: "RECORDING" })]), true);
});

function render(item, props = {}) {
  return renderToStaticMarkup(
    h(RecordingRow, {
      item, now: startMs + 3_661_000, stopping: false, starting: false,
      onStop: () => undefined, onRunNow: () => undefined, onOpen: () => undefined, ...props,
    }),
  );
}

test("the row renders the lesson title and notes as text and only the offered buttons", () => {
  const html = render(row({ title: "<img src=x onerror=alert(1)>", jobError: "<b>x</b>", stage: "failed", errorCode: "summaryFailed", action: "openConnections" }));
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("&lt;b&gt;x&lt;/b&gt;"));
  assert.ok(html.includes("Открыть подключения"));
  assert.ok(!html.includes("Остановить"));
  assert.ok(!html.includes("Запустить сейчас"));

  const running = render(row({ stage: "recording", status: "RECORDING", stoppable: true }));
  assert.ok(running.includes("Идёт запись"));
  assert.ok(running.includes("1:01:01"));
  assert.ok(running.includes("Остановить"));

  const waiting = render(row({ stage: "queued", step: "transcription", queueReason: "power", action: "runNow", actionJobId: "j1" }));
  assert.ok(waiting.includes("Запустить сейчас"));
  assert.ok(waiting.includes("В очереди · ждёт питания от сети"));

  assert.ok(render(row(), { extraActions: h("button", null, "Ещё") }).includes(">Ещё</button>"), "room for TASK-079");
  const src = readFileSync(new URL("../src/pages/RecordingRow.tsx", import.meta.url), "utf8");
  assert.ok(!src.includes("dangerouslySetInnerHTML"));
});

test("the next tick comes just after the next whole second", () => {
  assert.equal(tickMs, 1000);
  for (const [now, lo, hi] of [[startMs, 1000, 1050], [startMs + 999, 1, 50], [startMs + 400, 600, 650]]) {
    const d = nextTickDelay(now);
    assert.ok(d >= lo && d <= hi, `${now % 1000}: ${d}`);
    assert.equal((now + d) % 1000 > 0 && (now + d) % 1000 < 50, true);
  }
});

// Fake timers with a fake clock: run() moves the clock to the planned time and fires the timer.
function fakeTimers(start) {
  const t = { clock: start, planned: new Map(), next: 1 };
  t.timers = {
    set: (fn, ms) => {
      const id = t.next++;
      t.planned.set(id, { fn, at: t.clock + ms });
      return id;
    },
    clear: (id) => t.planned.delete(id),
  };
  t.run = () => {
    const [id, p] = [...t.planned.entries()][0];
    t.planned.delete(id);
    t.clock = p.at;
    p.fn();
  };
  return t;
}

test("the elapsed time advances by one second per tick, not by the 3 s poll step", () => {
  const t = fakeTimers(startMs + 35_250);
  const shown = [];
  const ticker = new SecondTicker((now) => shown.push(chainStatusView(row({ stage: "recording", status: "RECORDING" }), now).value), t.timers, () => t.clock);

  ticker.setActive(true);
  for (let i = 0; i < 5; i++) {
    t.run();
  }
  assert.deepEqual(shown, ["0:00:35", "0:00:36", "0:00:37", "0:00:38", "0:00:39", "0:00:40"]);
  assert.equal(t.planned.size, 1, "one timer at a time");

  ticker.setActive(true);
  assert.equal(shown.length, 6, "turning on twice does not tick twice");
  ticker.setActive(false);
  assert.equal(t.planned.size, 0, "the timer is cleared");
  assert.equal(ticker.active, false);
  ticker.setActive(false);

  // A timer that fires late still shows every second once.
  const late = fakeTimers(startMs);
  const values = [];
  const lateTicker = new SecondTicker((now) => values.push(chainStatusView(row({ stage: "recording", status: "RECORDING" }), now).value), late.timers, () => late.clock);
  lateTicker.setActive(true);
  for (let i = 0; i < 3; i++) {
    const [, p] = [...late.planned.entries()][0];
    p.at += 30;
    late.run();
  }
  assert.deepEqual(values, ["0:00:00", "0:00:01", "0:00:02", "0:00:03"]);
  lateTicker.setActive(false);
});

const job = (over = {}) => ({
  id: "j1", type: "send_email", status: "QUEUED", waiting: false, attempts: 1, maxAttempts: 3, progressPercent: 0,
  lastError: "", createdAt: started, updatedAt: started, nextRunAt: "", runNowOffered: false, ...over,
});

// A failed email or homework draft must stay visible: it joins the recordings in the one «Завершённые» group.
test("finished recordings and finished other jobs share one «Завершённые» group, newest first", () => {
  const at = (min) => new Date(startMs + min * 60_000).toISOString();
  const recordings = [
    row({ id: "r-new", startedAt: at(30) }),
    row({ id: "r-run", stage: "recording", status: "RECORDING", stoppable: true }),
    row({ id: "r-old", stage: "failed", errorCode: "summaryFailed", startedAt: at(0) }),
  ];
  const jobs = [
    job({ id: "j-queued" }),
    job({ id: "j-failed", status: "FAILED", lastError: "smtp: 550", createdAt: at(10) }),
    job({ id: "j-cancelled", type: "homework_draft", status: "CANCELLED", createdAt: at(40) }),
    job({ id: "j-runnow", status: "FAILED", runNowOffered: true }),
  ];
  const g = pageGroups(recordings, jobs);

  assert.deepEqual(g.recordings.map((r) => r.id), ["r-run"]);
  assert.deepEqual(g.otherJobs.map((j) => j.id), ["j-queued", "j-runnow"], "a job with Run now stays in «Другие задачи»");
  assert.deepEqual(g.finished.map(finishedKey), ["job:j-cancelled", "recording:r-new", "job:j-failed", "recording:r-old"]);
  assert.equal(g.finished.find((e) => e.item.id === "j-failed").item.lastError, "smtp: 550");

  // Same id in both lists still gives two keys; a bad time sorts last; empty lists give empty groups.
  const same = pageGroups([row({ id: "x", startedAt: "" })], [job({ id: "x", status: "COMPLETED" })]);
  assert.deepEqual(same.finished.map(finishedKey), ["job:x", "recording:x"]);
  assert.deepEqual(pageGroups([], []), { recordings: [], otherJobs: [], finished: [] });
});
