// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  errorMessages,
  methods,
  parsePromptView,
  parseRecordingStart,
  promptApi,
  shownEvent,
} from "../.test-build/bindings/prompt.js";
import {
  actionErrorText,
  buttonHints,
  buttonLabels,
  headings,
  noPromptNote,
  popupModel,
  problemText,
  problemTexts,
  staleNote,
  startingLabel,
  stateNotes,
  timeRange,
} from "../.test-build/popup/view.js";

// Mirror domain/recording/prompt and internal/input/wails/prompt.go. SNOOZED and WITHDRAWN are gone with the snooze
// (TASK-076, owner's decision 2026-10-18).
const states = ["OFFERED", "STARTING", "STARTED", "SKIPPED", "EXPIRED"];
const problems = ["unavailable", "in_progress", "recovering", "source_gone", "failed"];

const lesson = {
  id: "p1", kind: "lesson", title: "Матанализ <b>", start: "10:00", end: "11:30",
  state: "OFFERED", problem: "",
};
const adhoc = { id: "p2", kind: "adhoc", title: "", start: "", end: "", state: "OFFERED", problem: "" };

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

test("the api calls the bound Go methods with the prompt id", async () => {
  const { call, calls } = fakeCall(lesson);
  const api = promptApi(call);
  assert.deepEqual(await api.current(), lesson);
  assert.deepEqual(await api.start("p1", "zoom.exe"), lesson);
  await api.start("p1", "");
  await api.skip("p1");
  assert.deepEqual(calls, [
    ["paratech/desktop/internal/input/wails.PromptService.Current"],
    ["paratech/desktop/internal/input/wails.PromptService.Start", "p1", "zoom.exe"],
    ["paratech/desktop/internal/input/wails.PromptService.Start", "p1", ""],
    ["paratech/desktop/internal/input/wails.PromptService.Skip", "p1"],
  ]);
  // Snooze is no longer bound (TASK-076); selectSource and startRecording are the main window's (TASK-081).
  assert.equal(Object.keys(methods).length, 6);
  assert.equal("snooze" in api, false);
  assert.equal(shownEvent, "recordingprompt:shown");
});

// TASK-081: the picker of the diagnostics page and «Начать запись» of «Записи и задачи».
test("the main window remembers a source and starts a recording without the popup", async () => {
  const started = { started: true, problem: "", recordingId: "rec-1", linked: true };
  const { call, calls } = fakeCall(started);
  const api = promptApi(call);
  assert.equal(await api.selectSource("zoom.exe"), undefined);
  await api.selectSource("");
  assert.deepEqual(await api.startRecording("zoom.exe"), started);
  assert.deepEqual(calls, [
    ["paratech/desktop/internal/input/wails.PromptService.SelectSource", "zoom.exe"],
    ["paratech/desktop/internal/input/wails.PromptService.SelectSource", ""],
    ["paratech/desktop/internal/input/wails.PromptService.StartRecording", "zoom.exe"],
  ]);
});

test("a refused start resolves with its problem", async () => {
  const busy = { started: false, problem: "in_progress", recordingId: "", linked: false };
  assert.deepEqual(await promptApi(fakeCall(busy).call).startRecording(""), busy);
  for (const p of problems) {
    assert.equal(parseRecordingStart({ ...busy, problem: p }).problem, p);
  }
});

test("a malformed start reply is an error", async () => {
  const ok = { started: true, problem: "", recordingId: "rec-1", linked: false };
  for (const bad of [
    null, "x", [], { ...ok, started: "yes" }, { ...ok, linked: 1 }, { ...ok, recordingId: 7 },
    { ...ok, problem: "failed" }, { ...ok, started: false },
  ]) {
    assert.throws(() => parseRecordingStart(bad), undefined, JSON.stringify(bad));
  }
  await assert.rejects(promptApi(fakeCall({ started: true }).call).startRecording(""));
});

test("no current prompt is null", async () => {
  assert.equal(await promptApi(fakeCall(null).call).current(), null);
  assert.equal(await promptApi(fakeCall(undefined).call).current(), null);
});

test("a malformed prompt is an error", async () => {
  for (const bad of ["x", 1, { ...lesson, id: 1 }, { ...lesson, kind: "other" }]) {
    assert.throws(() => parsePromptView(bad), undefined, JSON.stringify(bad));
  }
  const { title: _t, ...noTitle } = lesson;
  await assert.rejects(promptApi(fakeCall(noTitle).call).start("p1", ""));
});

test("every problem code has its own text, an unknown one the generic failure", () => {
  assert.deepEqual(Object.keys(problemTexts).sort(), [...problems].sort());
  const texts = problems.map(problemText);
  assert.equal(new Set(texts).size, texts.length);
  assert.equal(problemText("unavailable"), "Запись пока недоступна в этой сборке.");
  assert.equal(problemText("in_progress"), "Уже идёт другая запись — её можно остановить в «Записях и задачах».");
  assert.equal(problemText("recovering"), "Восстанавливаются прерванные записи. Повторите через минуту.");
  assert.equal(problemText("failed"), "Не удалось начать запись.");
  assert.equal(problemText(""), "");
  assert.equal(problemText("toString"), problemTexts.failed);
});

// Since TASK-076 the popup has only «Начать запись» and «Не записывать»: the snooze button and canSnooze are gone.
test("an offered lesson shows title, time and its two buttons", () => {
  const m = popupModel(lesson, null);
  assert.equal(m.heading, "Начинается занятие");
  assert.equal(m.title, "Матанализ <b>");
  assert.equal(m.time, "10:00–11:30");
  assert.deepEqual(m.buttons, ["start", "skip"]);
  assert.equal(m.startLabel, "Начать запись");
  assert.equal(m.disabled, false);
  assert.equal(m.note, "");
  assert.equal(m.problem, "");
  // A stale canSnooze from an older core changes nothing.
  assert.deepEqual(popupModel({ ...lesson, canSnooze: true }, null).buttons, ["start", "skip"]);
  assert.deepEqual([buttonLabels.start, buttonLabels.skip], ["Начать запись", "Не записывать"]);
  assert.deepEqual(Object.keys(buttonLabels).sort(), ["close", "skip", "start"]);
});

test("every button has a hint that says what it does", () => {
  assert.deepEqual(Object.keys(buttonHints).sort(), Object.keys(buttonLabels).sort());
  assert.equal(buttonHints.start, "Записать звук этого занятия");
  assert.equal(buttonHints.skip, "Закрыть, это занятие не записывается");
  for (const id of Object.keys(buttonHints)) {
    assert.notEqual(buttonHints[id], buttonLabels[id], id);
  }
});

test("an ad-hoc prompt has its own heading and no lesson details", () => {
  const m = popupModel({ ...adhoc, title: "ignored", start: "10:00", end: "11:00" }, null);
  assert.equal(m.heading, "Запись без занятия");
  assert.equal(m.title, "");
  assert.equal(m.time, "");
  assert.deepEqual(m.buttons, ["start", "skip"]);
  assert.notEqual(headings.lesson, headings.adhoc);
});

test("a start in progress reads Запуск… and disables every button", () => {
  for (const [view, pending] of [[{ ...lesson, state: "STARTING" }, null], [lesson, "start"]]) {
    const m = popupModel(view, pending);
    assert.equal(m.startLabel, startingLabel);
    assert.equal(startingLabel, "Запуск…");
    assert.equal(m.disabled, true);
    assert.deepEqual(m.buttons, ["start", "skip"]);
  }
  // The problem of the previous attempt is hidden while a new one runs.
  assert.equal(popupModel({ ...lesson, state: "STARTING", problem: "failed" }, null).problem, "");
  assert.equal(popupModel({ ...lesson, problem: "failed" }, "start").problem, "");
});

test("another pending action disables the buttons without Запуск…", () => {
  const m = popupModel(lesson, "skip");
  assert.equal(m.disabled, true);
  assert.equal(m.startLabel, "Начать запись");
});

test("a failed start stays on offer with the problem text", () => {
  for (const p of problems) {
    const m = popupModel({ ...lesson, problem: p }, null);
    assert.equal(m.problem, problemTexts[p]);
    assert.deepEqual(m.buttons, ["start", "skip"]);
    assert.equal(m.disabled, false);
  }
});

test("every state maps to buttons; ended states only offer Закрыть with a note", () => {
  assert.deepEqual(Object.keys(stateNotes).sort(), states.filter((s) => s !== "OFFERED" && s !== "STARTING").sort());
  for (const state of states) {
    const m = popupModel({ ...lesson, state, problem: "failed" }, null);
    if (state === "OFFERED" || state === "STARTING") {
      assert.deepEqual(m.buttons, ["start", "skip"], state);
      assert.equal(m.note, "", state);
    } else {
      assert.deepEqual(m.buttons, ["close"], state);
      assert.equal(m.note, stateNotes[state], state);
      assert.equal(m.problem, "", state);
      assert.equal(m.disabled, false, state);
    }
  }
  const unknown = popupModel({ ...lesson, state: "PAUSED" }, null);
  assert.deepEqual(unknown.buttons, ["close"]);
  assert.equal(unknown.note, staleNote);
  assert.equal(buttonLabels.close, "Закрыть");
});

test("no prompt shows a note and Закрыть", () => {
  const m = popupModel(null, null);
  assert.equal(m.note, noPromptNote);
  assert.deepEqual(m.buttons, ["close"]);
  assert.equal(m.title, "");
  assert.equal(m.time, "");
  assert.equal(m.problem, "");
});

test("time range tolerates missing parts", () => {
  assert.equal(timeRange("10:00", "11:30"), "10:00–11:30");
  assert.equal(timeRange("10:00", ""), "10:00");
  assert.equal(timeRange("", ""), "");
});

test("binding errors get fixed Russian texts; raw text is never shown", () => {
  assert.equal(actionErrorText(new Error(errorMessages.gone)), "Это предложение уже не актуально.");
  assert.equal(actionErrorText({ message: errorMessages.refused }), "Это действие сейчас недоступно.");
  assert.equal(actionErrorText(errorMessages.invalidId), "Неверный идентификатор предложения.");
  assert.equal(actionErrorText(new Error(errorMessages.actionFailed)), "Не удалось выполнить действие.");
  assert.equal(actionErrorText(new Error(errorMessages.invalidSource)), "Этот источник звука нельзя выбрать.");
  assert.equal(actionErrorText(new Error("C:\\secret\\path failed")), "Не удалось выполнить действие.");
  assert.equal(actionErrorText(undefined), "Не удалось выполнить действие.");
});
