// One row per recording with the status of its whole chain, and the one-second clock (TASK-076). Runs on the modules
// compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { formatTime } from "../.test-build/activityView.js";
import { nextTickDelay, SecondTicker, tickMs } from "../.test-build/clock.js";
import { JobRow, RecordingRow, StatusLine } from "../.test-build/pages/RecordingRow.js";
import { jobStatusView } from "../.test-build/statusPresentation.js";
import {
  actionLabels,
  actionPages,
  anyRecording,
  chainStatusView,
  errorTexts,
  jobActions,
  jobDetails,
  jobItemLabels,
  jobMeta,
  latestDrafts,
  lessonKindTexts,
  lessonLine,
  pageGroups,
  queueLine,
  queueReasonHints,
  queueReasonLabels,
  queueReasonTexts,
  recordingRowView,
  rowAction,
  rowActions,
  retryHints,
  retryLabels,
  rowRetry,
  rowSecondaryAction,
  rowTitle,
  stageLabels,
} from "../.test-build/recordingRowView.js";

const { renderToStaticMarkup } = server;

// Mirror the chain codes of internal/cases/activity/types.go.
const stages = ["recording", "saving", "queued", "transcribing", "summarizing", "lectureText", "done", "failed"];
// The service reasons have their own chip (TASK-083); the others read «В очереди · <reason>».
const suffixedReasons = ["inLine", "retry", "power", "network", "quota"];
const ownLabelReasons = ["serviceNotConfigured", "serviceUnavailable"];
const reasons = [...suffixedReasons, ...ownLabelReasons];
const errorCodes = ["recordingFailed", "transcriptionFailed", "summaryFailed", "homeworkFailed", "lectureTextFailed"];
const actions = ["runNow", "openModels", "openConnections", "checkSound", "retry"];

const started = "2026-10-18T17:19:35Z";
const startMs = Date.parse(started);
const row = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", subject: "", kind: "", teacher: "",
  status: "COMPLETED", startedAt: started, stoppedAt: "",
  durationSeconds: 0, hasDuration: false, problem: "", stoppable: false, stage: "done", step: "", progressPercent: 0,
  partsDone: 0, parts: 0, queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "", retryWork: "",
  canRedoSummary: false, lectureTextReady: false,
  remainingSeconds: 0, hasRemaining: false, advice: null, ...over,
});

test("every chain code has its own Russian text", () => {
  assert.deepEqual(Object.keys(stageLabels), stages);
  assert.deepEqual(Object.keys(queueReasonTexts), suffixedReasons);
  assert.deepEqual(Object.keys(queueReasonLabels), ownLabelReasons);
  assert.deepEqual(Object.keys(queueReasonHints), reasons);
  assert.deepEqual(Object.keys(errorTexts), errorCodes);
  assert.deepEqual(Object.keys(actionLabels), actions);
  for (const texts of [stageLabels, queueReasonTexts, queueReasonLabels, queueReasonHints, errorTexts, actionLabels]) {
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
    const label = ownLabelReasons.includes(reason) ? queueReasonLabels[reason] : `В очереди · ${queueReasonTexts[reason]}`;
    assert.equal(v.label, label, reason);
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
  assert.deepEqual([v.status.label, v.status.kind, v.status.icon], ["Сервис конспектов не настроен", "wait", "warn"]);
  assert.equal(
    v.status.hint,
    "Дальше краткое содержание. Укажите адрес и ключ в «Подключения» → «Конспекты и задания». Запись сохранена.",
  );
  assert.deepEqual(v.action, { kind: "open", page: "connections", label: "Открыть подключения" });
  const offHtml = render(off);
  assert.ok(offHtml.includes("Сервис конспектов не настроен"));
  assert.ok(!offHtml.includes("В очереди"));
  assert.ok(offHtml.includes("Открыть подключения"));
  assert.ok(!offHtml.includes("нет сети"));

  const down = recordingRowView(row({ stage: "queued", step: "summary", queueReason: "serviceUnavailable", jobError: "hub: unavailable" }), 0);
  assert.deepEqual([down.status.label, down.status.kind, down.status.icon], ["Сервис конспектов недоступен", "wait", "clock"]);
  assert.equal(down.status.hint, "Дальше краткое содержание. Повторим автоматически.");
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

test("a failed post-processing offers «Повторить», also next to the connections after an LLM call failure (TASK-086)", () => {
  const retry = row({ stage: "failed", step: "summary", errorCode: "summaryFailed", action: "retry", retryWork: "summary" });
  assert.deepEqual(rowAction(retry), { kind: "retry", work: "summary", label: "Повторить" });
  assert.equal(rowSecondaryAction(retry), null);
  assert.equal(chainStatusView(retry, 0).hint, `${errorTexts.summaryFailed} ${retryHints.retry}`);
  assert.ok(!chainStatusView(retry, 0).hint.includes("подключени"), "an unreadable answer is no connection problem");
  assert.equal(rowAction({ ...retry, retryWork: "" }), null, "no work to retry");

  const llm = row({ stage: "failed", step: "summary", errorCode: "homeworkFailed", action: "openConnections", retryWork: "homework" });
  assert.deepEqual(rowAction(llm), { kind: "open", page: "connections", label: "Открыть подключения" });
  assert.deepEqual(rowSecondaryAction(llm), { kind: "retry", work: "homework", label: "Повторить" });
  assert.equal(chainStatusView(llm, 0).hint, `${errorTexts.homeworkFailed} ${retryHints.afterConnections}`);
  assert.equal(rowSecondaryAction({ ...llm, retryWork: "" }), null);
  assert.equal(chainStatusView({ ...llm, retryWork: "" }, 0).hint, errorTexts.homeworkFailed);

  // A queued service problem keeps its link only; running or queued work is never retried.
  assert.equal(rowSecondaryAction(row({ stage: "queued", queueReason: "serviceNotConfigured", action: "openConnections" })), null);
  assert.equal(recordingRowView(row({ stage: "summarizing" }), 0).retry, null);

  // The retry is the round icon button next to the chip (TASK-096): its name is in aria-label and title.
  const label = 'aria-label="Повторить краткое содержание"';
  const html = render(retry, { onRetry: () => undefined });
  assert.ok(html.includes(label));
  assert.ok(!html.includes(">Повторить<"), "no text button");
  assert.ok(!html.includes("Открыть подключения"));
  const both = render(llm, { onRetry: () => undefined });
  assert.ok(both.includes('aria-label="Повторить поиск заданий"'));
  assert.ok(both.indexOf("Повторить поиск заданий") < both.indexOf("Открыть подключения"), "the retry by the chip, the link in the actions");
  assert.ok(!render(retry).includes(label), "no retry without onRetry");
  assert.ok(!render(row({ canRedoSummary: true }), { onRetry: () => undefined }).includes("заново"), "the redo is only in the closed «⋯» menu");
});

test("«Повторить» is a round icon button by the chip, named by the work, busy while it runs (TASK-096)", () => {
  assert.deepEqual(retryLabels, {
    summary: "Повторить краткое содержание",
    homework: "Повторить поиск заданий",
    lectureText: "Повторить текст лекции",
  });
  const failed = (work, action = "retry") => row({ stage: "failed", step: "summary", errorCode: "summaryFailed", action, retryWork: work });
  for (const work of ["summary", "homework", "lectureText"]) {
    assert.deepEqual(rowRetry(failed(work)), { work, label: retryLabels[work] });
    assert.deepEqual(rowRetry(failed(work, "openConnections")), { work, label: retryLabels[work] });
  }
  assert.equal(rowRetry(failed("")), null);
  assert.equal(rowRetry(failed("summary", "openModels")), null);

  const picked = [];
  const html = render(failed("lectureText"), { onRetry: (w) => picked.push(w) });
  const button = html.match(/<button[^>]*aria-label="Повторить текст лекции"[^>]*>/)?.[0] ?? "";
  assert.ok(button.includes('type="button"'), button);
  assert.ok(button.includes('class="btn icon-btn"'), button);
  assert.ok(button.includes('title="Повторить текст лекции"'), button);
  assert.ok(!button.includes("disabled") && !button.includes("aria-busy"), button);
  assert.ok(html.includes('<use href="#i-retry"></use>'));
  // Right next to the chip, inside the status cell.
  assert.match(html, /<span class="st-head"><span class="chip s-fail"[^]*?<\/span><\/span><button[^>]*aria-label="Повторить текст лекции"/);

  const busy = render(failed("summary"), { onRetry: () => undefined, retrying: true })
    .match(/<button[^>]*aria-label="Повторить краткое содержание"[^>]*>/)?.[0] ?? "";
  assert.ok(busy.includes("disabled") && busy.includes('aria-busy="true"'), busy);
});

test("one main button per state; the rest goes to «⋯» (TASK-096)", () => {
  const texts = { texts: true, redo: true };
  const keys = (over, offers = texts) => {
    const a = rowActions(row(over), offers);
    return [a.main?.key ?? null, a.menu.map((i) => i.key)];
  };
  // A ready summary leads; the lecture text, the transcript and the redo are in the menu.
  assert.deepEqual(keys({ canRedoSummary: true }), ["summary", ["transcript", "redo"]]);
  assert.deepEqual(keys({ canRedoSummary: true, lectureTextReady: true }), ["summary", ["lectureText", "transcript", "redo"]]);
  // A ready summary and a failed homework search behind the connections: the link goes to the menu.
  assert.deepEqual(keys({ stage: "failed", step: "summary", errorCode: "homeworkFailed", action: "openConnections", retryWork: "homework" }),
    ["summary", ["transcript", "open"]]);
  // No ready summary: the lecture text leads, else the transcript; the fix link stays in the menu.
  assert.deepEqual(keys({ stage: "failed", step: "summary", errorCode: "summaryFailed", action: "openConnections", retryWork: "summary" }),
    ["transcript", ["summary", "open"]]);
  assert.deepEqual(keys({ stage: "failed", step: "summary", errorCode: "summaryFailed", action: "openConnections", retryWork: "summary", lectureTextReady: true }),
    ["lectureText", ["summary", "transcript", "open"]]);
  // A failed transcription keeps its recognised part, so the transcript leads there too.
  assert.deepEqual(keys({ stage: "failed", step: "transcription", errorCode: "transcriptionFailed", action: "openModels" }),
    ["transcript", ["open"]]);
  // No text at all: the fix link is the fallback main button.
  assert.deepEqual(keys({ stage: "failed", step: "", errorCode: "recordingFailed", action: "checkSound" }), ["open", []]);
  assert.deepEqual(keys({ stage: "failed", step: "summary", errorCode: "summaryFailed", action: "retry", retryWork: "summary" }),
    ["transcript", ["summary"]]);
  assert.deepEqual(keys({ stage: "summarizing", lectureTextReady: true }), ["lectureText", ["summary", "transcript"]]);
  assert.deepEqual(keys({ stage: "transcribing" }), ["transcript", []]);
  assert.deepEqual(keys({ stage: "failed", errorCode: "silentRecording", action: "checkSound" }), ["open", []]);
  // Stop and «Запустить сейчас» stay visible: they are the state's own action.
  assert.deepEqual(keys({ stage: "recording", status: "RECORDING", stoppable: true }), ["stop", []]);
  assert.deepEqual(keys({ stage: "queued", step: "summary", queueReason: "power", action: "runNow", actionJobId: "j1" }),
    ["runNow", ["summary", "transcript"]]);
  // The page decides what it offers: no texts and no redo without their handlers.
  assert.deepEqual(keys({ canRedoSummary: true }, { texts: false, redo: false }), [null, []]);
  assert.deepEqual(keys({ stage: "queued", step: "summary", queueReason: "serviceNotConfigured", action: "openConnections" }, { texts: false, redo: false }),
    ["open", []]);

  // The row renders the one main button and the «⋯» button; the menu items show only when it opens.
  const noop = () => undefined;
  const page = { onOpenSummary: noop, onOpenTranscript: noop, onRedoSummary: noop, onDelete: noop };
  const html = render(row({ canRedoSummary: true, lectureTextReady: true }), page);
  assert.ok(html.includes(">Открыть конспект<"));
  assert.ok(!html.includes("Открыть текст лекции") && !html.includes("Открыть расшифровку") && !html.includes("Исходная расшифровка"));
  assert.equal(html.split('class="btn"').length - 1, 1, "one main button");
  assert.ok(html.includes('aria-label="Ещё действия: Матанализ"'));
  // Every row keeps the «⋯» cell, so the actions column stays aligned.
  assert.ok(render(row({ stage: "transcribing" })).includes('<div class="mmenu"></div>'));
});

test("the row shows no technical error text; the details are in the chip's tooltip (TASK-096)", () => {
  const failed = row({
    stage: "failed", step: "summary", errorCode: "summaryFailed", action: "retry", retryWork: "summary",
    problem: "", jobError: "permanent failure: hub: answer does not match the schema",
  });
  const v = recordingRowView(failed, 0);
  assert.deepEqual(v.notes, []);
  assert.equal(v.details, "permanent failure: hub: answer does not match the schema");
  const html = render(failed, { onRetry: () => undefined });
  assert.ok(html.includes(errorTexts.summaryFailed));
  assert.ok(!html.includes("row-problem"));
  assert.match(html, /<span class="chip s-fail" title="permanent failure: hub: answer does not match the schema">/);

  // The core's Russian note (the capture switch) stays in the row; its English reasons do not.
  const note = "Захват выбранного приложения не запустился (activation failed) — записывается весь звук компьютера.";
  assert.deepEqual(recordingRowView(row({ problem: note }), 0).notes, [note]);
  assert.deepEqual(recordingRowView(row({ problem: `the capture ended unexpectedly; ${note}` }), 0).notes, [note]);
  assert.deepEqual(recordingRowView(row({ problem: "the capture could not start" }), 0).notes, []);
  assert.equal(recordingRowView(row({ problem: "the capture could not start", jobError: "x" }), 0).details, "the capture could not start\nx");
  assert.ok(!render(row({ problem: "the capture could not start" })).includes(">the capture"));
  assert.ok(!render(row()).includes("title="), "no tooltip without details");
});

test("the core's details are shown once each", () => {
  assert.equal(recordingRowView(row({ problem: "ended early", jobError: "model missing" }), 0).details, "ended early\nmodel missing");
  assert.equal(recordingRowView(row({ problem: "same", jobError: "same" }), 0).details, "same");
  assert.equal(recordingRowView(row(), 0).details, "");
  assert.deepEqual(recordingRowView(row({ problem: "Запись прервалась.; Запись прервалась." }), 0).notes, ["Запись прервалась."]);
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

  const noop = () => undefined;
  assert.ok(render(row(), { onOpenSummary: noop, onOpenTranscript: noop }).includes(">Открыть конспект<"), "the texts when the page opens them");
  assert.ok(!render(row()).includes("Открыть"), "no texts without the page's handlers");
  const src = readFileSync(new URL("../src/pages/RecordingRow.tsx", import.meta.url), "utf8");
  assert.ok(!src.includes("dangerouslySetInnerHTML"));
});

test("the lesson line names the known subject, kind and teacher (TASK-088)", () => {
  assert.deepEqual(lessonKindTexts, { lecture: "лекция", seminar: "семинар", practice: "практика", lab: "лабораторная" });
  const line = (over) => lessonLine({ title: "Тестовый урок", subject: "", kind: "", teacher: "", ...over });
  for (const [over, want] of [
    [{ subject: "Матанализ", kind: "lecture", teacher: "Иванов И. И." }, "Матанализ · лекция · Иванов И. И."],
    [{ subject: "Матанализ", kind: "lecture" }, "Матанализ · лекция"],
    [{ kind: "seminar" }, "семинар"],
    [{ kind: "practice", teacher: "Петрова" }, "практика · Петрова"],
    [{ subject: "Физика", kind: "lab" }, "Физика · лабораторная"],
    [{ subject: "  Физика  ", teacher: " Петрова " }, "Физика · Петрова"],
    [{ subject: "Физика", kind: "other" }, "Физика"],
    [{ subject: "Физика", kind: "unknown" }, "Физика"],
    [{ subject: "Физика", kind: "exam" }, "Физика"],
    [{ subject: "Физика", kind: "toString" }, "Физика"],
    [{ subject: "Физика", kind: "LECTURE" }, "Физика"],
    // The subject that repeats the title is not shown twice.
    [{ title: "Матанализ", subject: " матанализ ", kind: "lecture" }, "лекция"],
    [{ title: "", subject: "Матанализ" }, "Матанализ"],
    [{ subject: "   ", teacher: "  " }, ""],
    [{}, ""],
  ]) {
    assert.equal(line(over), want, JSON.stringify(over));
  }
  assert.equal(recordingRowView(row({ subject: "Матанализ", kind: "lecture" }), 0).lesson, "лекция");
  assert.equal(recordingRowView(row(), 0).lesson, "");
});

test("the row shows the lesson line as text, and nothing extra without it (TASK-088)", () => {
  const html = render(row({ title: "Тестовый урок", subject: "Матанализ", kind: "lecture", teacher: "Иванов И. И." }));
  assert.ok(html.includes('<span class="row-lesson">Матанализ · лекция · Иванов И. И.</span>'), html);

  const empty = render(row({ title: "Тестовый урок" }));
  assert.ok(!empty.includes("row-lesson"));
  assert.ok(!empty.includes(" · "), empty);
  // Without a lesson line and without meta there is no empty meta span either.
  assert.ok(!render(row({ title: "" })).includes('class="small muted"></span>'));

  const unsafe = render(row({ subject: "<img src=x onerror=alert(1)>", teacher: "<a href=x>t</a>" }));
  assert.ok(!unsafe.includes("<img") && !unsafe.includes("<a "));
  assert.ok(unsafe.includes("&lt;img src=x onerror=alert(1)&gt; · &lt;a href=x&gt;t&lt;/a&gt;"));
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
  lastError: "", createdAt: started, updatedAt: started, nextRunAt: "", runNowOffered: false,
  assignmentId: "", assignmentTitle: "", assignmentSubject: "", label: "", ...over,
});
const at = (min) => new Date(startMs + min * 60_000).toISOString();

// TASK-098: «Записи» and «Задачи» are separate sections, each with its own «Идут» and «Завершённые», newest first.
test("recordings and other jobs are separate sections, each split into in progress and finished", () => {
  const recordings = [
    row({ id: "r-old", stage: "failed", errorCode: "summaryFailed", startedAt: at(0) }),
    row({ id: "r-run", stage: "recording", status: "RECORDING", stoppable: true }),
    row({ id: "r-new", startedAt: at(30) }),
  ];
  const jobs = [
    job({ id: "j-queued" }),
    job({ id: "j-failed", status: "FAILED", lastError: "smtp: 550", createdAt: at(10) }),
    job({ id: "j-cancelled", type: "calendar_sync", status: "CANCELLED", createdAt: at(40) }),
    job({ id: "j-runnow", status: "FAILED", runNowOffered: true }),
  ];
  const g = pageGroups(recordings, jobs);

  assert.deepEqual(g.recordings.active.map((r) => r.id), ["r-run"]);
  assert.deepEqual(g.recordings.finished.map((r) => r.id), ["r-new", "r-old"]);
  assert.deepEqual(g.tasks.active.map((j) => j.id), ["j-queued", "j-runnow"], "a job with Run now stays in «Идут»");
  assert.deepEqual(g.tasks.finished.map((j) => j.id), ["j-cancelled", "j-failed"]);
  assert.equal(g.tasks.finished.find((j) => j.id === "j-failed").lastError, "smtp: 550", "a failed job keeps its error");

  // A bad time sorts last; empty lists give empty sections.
  const bad = pageGroups([row({ id: "x", startedAt: "" }), row({ id: "y" })], []);
  assert.deepEqual(bad.recordings.finished.map((r) => r.id), ["y", "x"]);
  assert.deepEqual(pageGroups([], []), { recordings: { active: [], finished: [] }, tasks: { active: [], finished: [] } });
});

test("only the latest draft job of an assignment is shown (TASK-098)", () => {
  const draft = (over) => job({ type: "homework_draft", assignmentId: "a1", status: "COMPLETED", ...over });
  const failedOld = draft({ id: "d-old", status: "FAILED", createdAt: at(0), updatedAt: at(1) });
  const doneNew = draft({ id: "d-new", createdAt: at(5), updatedAt: at(6) });
  const other = draft({ id: "d-other", assignmentId: "a2", createdAt: at(2) });
  const email = job({ id: "e1", assignmentId: "a1" });
  const loose = job({ id: "d-loose", type: "homework_draft" });

  // The input order does not matter; other jobs, other assignments and drafts without one stay, in order.
  assert.deepEqual(latestDrafts([failedOld, email, doneNew, other, loose]).map((j) => j.id), ["e1", "d-new", "d-other", "d-loose"]);
  assert.deepEqual(latestDrafts([doneNew, failedOld]).map((j) => j.id), ["d-new"]);
  // Equal creation: the later update wins, then the greater id.
  const a = draft({ id: "d-a", createdAt: at(5), updatedAt: at(9) });
  assert.deepEqual(latestDrafts([doneNew, a]).map((j) => j.id), ["d-a"]);
  const b = draft({ id: "d-b", createdAt: at(5), updatedAt: at(9) });
  assert.deepEqual(latestDrafts([b, a]).map((j) => j.id), ["d-b"]);
  assert.deepEqual(latestDrafts([a, b]).map((j) => j.id), ["d-b"]);
  // A draft with a readable time beats one without.
  assert.deepEqual(latestDrafts([draft({ id: "d-z", createdAt: "" }), failedOld]).map((j) => j.id), ["d-old"]);

  // The page groups only the latest: the old failed draft is in neither group.
  const g = pageGroups([], [failedOld, doneNew]);
  assert.deepEqual([...g.tasks.active, ...g.tasks.finished].map((j) => j.id), ["d-new"]);

  // Only finished older drafts are hidden: one still in progress, waiting or offering Run now stays in «Идут».
  const runningOld = draft({ id: "d-run", status: "RUNNING", createdAt: at(0) });
  const waitingOld = draft({ id: "d-wait", status: "WAITING_FOR_NETWORK", waiting: true, createdAt: at(1) });
  const runNowOld = draft({ id: "d-runnow", status: "FAILED", runNowOffered: true, createdAt: at(2) });
  assert.deepEqual(latestDrafts([runningOld, waitingOld, runNowOld, doneNew]).map((j) => j.id), ["d-run", "d-wait", "d-runnow", "d-new"]);
  const kept = pageGroups([], [runningOld, doneNew]);
  assert.deepEqual(kept.tasks.active.map((j) => j.id), ["d-run"], "an older running draft stays in view");
  assert.deepEqual(kept.tasks.finished.map((j) => j.id), ["d-new"]);
  const queuedOld = draft({ id: "d-queued", status: "QUEUED", createdAt: at(0) });
  const inLine = pageGroups([], [queuedOld, doneNew]);
  assert.equal(queueLine([], [...inLine.tasks.active, ...inLine.tasks.finished]), "В очереди 1 задача", "the queue line counts the kept draft");
  // An older failed draft behind a newer done one, and an older done one behind a newer failed one, are hidden.
  assert.deepEqual(latestDrafts([failedOld, doneNew]).map((j) => j.id), ["d-new"]);
  const doneOld = draft({ id: "d-done-old", createdAt: at(0) });
  const failedNew = draft({ id: "d-failed-new", status: "FAILED", createdAt: at(5) });
  assert.deepEqual(latestDrafts([doneOld, failedNew]).map((j) => j.id), ["d-failed-new"]);
});

test("a task row has one main button and the rest in «⋯»; attempts and errors only in the tooltip (TASK-098)", () => {
  assert.deepEqual(jobActions(job(), { assignment: true }), { main: null, menu: [] });
  assert.deepEqual(jobActions(job({ assignmentId: "a1" }), { assignment: true }), { main: { key: "assignment", label: "Открыть задание" }, menu: [] });
  assert.deepEqual(jobActions(job({ assignmentId: "a1" }), { assignment: false }), { main: null, menu: [] });
  assert.deepEqual(jobActions(job({ assignmentId: "a1", runNowOffered: true }), { assignment: true }), {
    main: { key: "runNow", label: "Запустить сейчас" },
    menu: [{ key: "assignment", label: "Открыть задание" }],
  });
  assert.equal(jobItemLabels.runNow, actionLabels.runNow);

  assert.equal(jobDetails(job({ lastError: " smtp: 550 ", attempts: 2, maxAttempts: 3 })), "smtp: 550\nПопыток: 2 из 3");
  assert.equal(jobDetails(job({ maxAttempts: 0 })), "");
  // To the minute, like «17.09.2026, 10:12», without the seconds of formatTime.
  const d = new Date(started);
  const two = (n) => String(n).padStart(2, "0");
  assert.equal(jobMeta(job()), `Обновлена ${two(d.getDate())}.${two(d.getMonth() + 1)}.${d.getFullYear()}, ${two(d.getHours())}:${two(d.getMinutes())}`);
  assert.equal(jobMeta(job({ updatedAt: "not a time" })), "");
  assert.equal(jobMeta(job({ updatedAt: "" })), "");
});

function renderJob(item, props = {}) {
  return renderToStaticMarkup(h(JobRow, { item, starting: false, onRunNow: () => undefined, ...props }));
}

// The actions cell of a row: the main button (if any), then the «⋯» cell, in the shared .arows grid.
const actsOf = (html) => html.slice(html.indexOf('<div class="acts">'));

test("a task row renders no technical text and keeps the «⋯» cell, empty without a menu (TASK-098)", () => {
  const noop = () => undefined;
  const failed = job({
    type: "homework_draft", status: "FAILED", lastError: "llm: <b>bad</b>", attempts: 3, maxAttempts: 3,
    assignmentId: "a1", label: "Черновик: <i>Эссе</i> · История",
  });
  const html = renderJob(failed, { onOpenAssignment: noop });
  assert.equal(html.split("опыток").length - 1, 1, "the attempts only once: in the chip's title");
  assert.ok(html.includes('title="llm: &lt;b&gt;bad&lt;/b&gt;\nПопыток: 3 из 3"'), "the error and attempts are in the chip's title");
  assert.ok(html.includes("Черновик: &lt;i&gt;Эссе&lt;/i&gt; · История") && !html.includes("<i>"), "model text as text only");
  assert.match(actsOf(html), /^<div class="acts"><button type="button" class="btn">.*Открыть задание<\/button><div class="mmenu"><\/div><\/div>/);

  const plain = renderJob(job({ status: "COMPLETED", type: "calendar_sync" }));
  assert.match(actsOf(plain), /^<div class="acts"><div class="mmenu"><\/div><\/div>/, "no button, the «⋯» cell still there");

  const both = renderJob(job({ status: "WAITING_FOR_POWER", runNowOffered: true, assignmentId: "a1" }), { onOpenAssignment: noop });
  assert.match(actsOf(both), /^<div class="acts"><button[^>]*>.*Запустить сейчас.*<\/button><div class="mmenu"><button[^>]*aria-label="Ещё действия: /);
});

test("recording and task rows share one grid: fixed status, button and «⋯» tracks in every card (TASK-098)", () => {
  // A recording row has the same cells: the main button, then the «⋯» cell.
  assert.match(actsOf(render(row({ stage: "recording", status: "RECORDING", stoppable: true }))), /^<div class="acts"><button[^>]*class="btn danger"[^>]*>.*<\/button><div class="mmenu">/);
  assert.match(actsOf(render(row({ stage: "queued", queueReason: "inLine" }))), /^<div class="acts"><div class="mmenu"><\/div><\/div>/);

  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.equal(page.split('className="card arows"').length - 1, 1, "every group of both sections uses the one card");
  assert.ok(!page.includes('"card rows"'), "no per-card subgrid");
  assert.match(page, /renderContainer=\{card\}/);
  assert.match(page, /<h2 className="group-title">Записи<\/h2>/);
  assert.match(page, /<h2 className="group-title">Задачи<\/h2>/);
  assert.match(page, /sectionGroups\("recordings", "записи", groups\.recordings, recordingRow\)/);
  assert.match(page, /sectionGroups\("tasks", "задачи", groups\.tasks, jobRow\)/);
  // The two «Завершённые» toggles have distinct accessible names: a hidden word after the title.
  assert.match(page, /Завершённые<span className="vh"> \{name\}<\/span>/);
  // No «Записи» heading right above the empty state's own heading.
  assert.match(page, /\{!noRecordings && <h2 className="group-title">Записи<\/h2>\}/);
  assert.match(page, /\) : noRecordings \? \(\s*<EmptyState/);
  assert.match(page, /<h3 className="arows-sub">Идут<\/h3>/);
  assert.match(page, /<p className="muted arows-empty">\{tasksEmptyText\}<\/p>/, "an empty «Задачи» has a short hint");
  assert.ok(!page.includes("attempts"), "the page shows no attempts");

  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  const tracks = [...css.matchAll(/\.arows > \.row \{ grid-template-columns: ([^;]+); \}/g)].map((m) => m[1]);
  assert.deepEqual(tracks, [
    "minmax(0, 1fr) var(--arow-st) calc(var(--arow-btn) + 4px + var(--arow-menu))",
    "minmax(0, 1fr) calc(var(--arow-btn) + 4px + var(--arow-menu))",
    "minmax(0, 1fr)",
  ], "only the title flexes, wide and narrow; one column when very narrow");
  assert.match(css, /\.arows \{ --arow-st: 17rem; --arow-btn: 14rem; --arow-menu: 32px;/);
  assert.match(css, /\.arows > \.row > \.acts \{ display: grid; grid-template-columns: var\(--arow-btn\) var\(--arow-menu\);/);
  assert.match(css, /\.arows > \.row > \.acts > \.btn, \.arows > \.row > \.acts > \.btn-with-reason \{ grid-column: 1; width: 100%; \}/, "one button width");
  assert.match(css, /\.arows > \.row > \.acts > \.mmenu \{ grid-column: 2; \}/, "«⋯» in its own slot, also without a button");
  // Only the row's own actions cell: a descendant rule would also stack the advice's «Переключиться»/«Оставить».
  assert.ok(!/\.arows\s+\.acts/.test(css), "no descendant .arows .acts rule");
  assert.match(css, /@container app \(max-width: 1000px\) \{\s*\.arows > \.row \{[^}]*\}\s*\.arows > \.row > \.row-main \{ grid-column: 1 \/ -1; \}/);
  assert.match(css, /@container app \(max-width: 720px\) \{\s*\.arows > \.row \{ grid-template-columns: minmax\(0, 1fr\); \}\s*\.arows > \.row > \.acts \{ justify-self: end; \}/, "very narrow: one column");
});

test("«Сделать конспект заново» from «⋯» asks with the summary screen's confirmation first (TASK-096)", () => {
  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /onRedoSummary=\{\(\) => setRedoAsk\(r\.id\)\}/, "the menu only opens the confirmation");
  assert.equal(page.split("api.redoSummary(").length - 1, 1, "one call site");
  assert.match(page, /function redo\(\) \{[^]*?api\.redoSummary\(id\)/);
  assert.match(page, /<Dialog\s+open=\{redoOffered\}\s+title=\{redoTexts\.title\}[^]*?onConfirm=\{redo\}/);
});

test("a job row keeps its technical error in the chip's tooltip, not as row text (TASK-096)", () => {
  const src = readFileSync(new URL("../src/pages/RecordingRow.tsx", import.meta.url), "utf8");
  assert.match(src, /<StatusLine view=\{v\} notes=\{\[\]\} details=\{jobDetails\(j\)\}>/);
  assert.ok(!src.includes("[j.lastError]"), "the job error is never a row note");
  assert.match(src, /<Button icon="doc" onClick=\{\(\) => run\("assignment"\)\}>/, "same style as a recording row's main button");

  const err = "permanent failure: the LLM answer does not match the schema";
  const html = renderToStaticMarkup(h(StatusLine, { view: jobStatusView(job({ type: "homework_draft", status: "FAILED" })), notes: [], details: err }));
  assert.ok(html.includes(`title="${err}"`), "the error is in the chip's title");
  assert.equal(html.split(err).length - 1, 1, "and nowhere else");
});

test("the retry hints name the round arrow by the status (TASK-096)", () => {
  assert.equal(retryHints.retry, "Нажмите круглую стрелку «Повторить» у статуса.");
  assert.equal(retryHints.afterConnections, "Проверьте подключения (меню «⋯») и нажмите круглую стрелку «Повторить».");
});
