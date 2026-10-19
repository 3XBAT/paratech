// Protection from empty recordings (TASK-082): the silent-source advice in the row and the popup, readable app names
// and the «звук идёт» mark in the picker, the preselection by sound and «Запись без звука». Runs on the modules
// compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  activityApi,
  errorMessages,
  methods,
  parseAdvice,
  parseRecording,
  parseSourceSwitch,
} from "../.test-build/bindings/activity.js";
import { methods as promptMethods, parseSourcesView, promptApi } from "../.test-build/bindings/prompt.js";
import {
  adviceErrorText,
  adviceKey,
  adviceLabels,
  adviceModel,
  keepSource,
  switchProblemText,
  switchProblemTexts,
  switchToSuggestion,
} from "../.test-build/sourcePicker/advice.js";
import { SourceAdvice } from "../.test-build/sourcePicker/SourceAdvice.js";
import { SourceNotes, SourcePicker } from "../.test-build/sourcePicker/SourcePicker.js";
import {
  autoSelectedNote,
  chooseSource,
  initialSelection,
  nextSelection,
  pickerModel,
  savedSelection,
  sourceMarks,
} from "../.test-build/sourcePicker/view.js";
import { chainStatusView, recordingRowView, silentTexts } from "../.test-build/recordingRowView.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";
import { rowOffersTranscript } from "../.test-build/transcriptView.js";

const { renderToStaticMarkup } = server;
const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");
const noop = () => undefined;

// Mirror AdviceView of internal/input/wails/activity.go (the owner's case: a video in Comet, Yandex Browser chosen).
const advice = {
  recordingId: "r1", silentApp: "browser.exe", silentName: "Яндекс Браузер", suggestApp: "comet.exe", suggestName: "Comet",
};
const whole = { ...advice, suggestApp: "", suggestName: "" };

const recording = {
  id: "r1", lessonId: "", title: "", subject: "", kind: "", teacher: "",
  status: "RECORDING", startedAt: "2026-10-19T10:00:00Z", stoppedAt: "",
  durationSeconds: 0, hasDuration: false, problem: "", stoppable: true, stage: "recording", step: "", progressPercent: 0,
  partsDone: 0, parts: 0, queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "", retryWork: "",
  canRedoSummary: false, lectureTextReady: false,
  remainingSeconds: 0, hasRemaining: false, advice,
};

// Mirror SourcesView of internal/input/wails/prompt.go.
const yandex = {
  app: "browser.exe", name: "Яндекс Браузер", title: "", playing: true, sounding: false, muted: false, browser: true, icon: "",
};
const comet = {
  app: "comet.exe", name: "Comet", title: "Лекция — YouTube", playing: true, sounding: true, muted: false, browser: true, icon: "",
};
const sources = (over = {}) => ({
  apps: [comet, yandex], selected: "comet.exe", autoSelected: true, remembered: "browser.exe", listError: false, ...over,
});

test("the row carries its advice; a malformed advice is an error", () => {
  assert.deepEqual(parseRecording(recording).advice, advice);
  assert.equal(parseRecording({ ...recording, advice: null }).advice, null);
  assert.deepEqual(parseAdvice({ ...whole, extra: "x" }), whole);
  for (const bad of [
    undefined, "x", 1, [], { ...advice, silentName: 1 }, { ...advice, suggestApp: null }, { ...advice, recordingId: "" },
    { ...advice, silentApp: "" }, { ...advice, suggestName: undefined },
  ]) {
    assert.throws(() => parseRecording({ ...recording, advice: bad }), /advice/, JSON.stringify(bad));
  }
  const { advice: _a, ...noAdvice } = recording;
  assert.throws(() => parseRecording(noAdvice), /advice/);
});

test("a silent recording is a known chain code", () => {
  const silent = { ...recording, status: "COMPLETED", stage: "failed", errorCode: "silentRecording", action: "checkSound", advice: null };
  assert.equal(parseRecording(silent).errorCode, "silentRecording");
  assert.throws(() => parseRecording({ ...silent, errorCode: "quiet" }), /recording/);
});

test("switchSource and dismissAdvice call the bound methods and check the answer", async () => {
  const calls = [];
  const api = activityApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return method === methods.switchSource ? { switched: true, problem: "" } : null;
  });
  assert.deepEqual(await api.switchSource("r1", "comet.exe"), { switched: true, problem: "" });
  await api.switchSource("r1", "");
  assert.equal(await api.dismissAdvice("r1"), undefined);
  assert.deepEqual(calls, [
    ["paratech/desktop/internal/input/wails.ActivityService.SwitchSource", "r1", "comet.exe"],
    ["paratech/desktop/internal/input/wails.ActivityService.SwitchSource", "r1", ""],
    ["paratech/desktop/internal/input/wails.ActivityService.DismissAdvice", "r1"],
  ]);
  assert.equal(Object.keys(methods).length, 8); // +RetryPostProcessing, RedoSummary (TASK-086)
  assert.equal(errorMessages.invalidSource, "invalid source");

  for (const problem of ["source_gone", "refused", "failed"]) {
    assert.deepEqual(parseSourceSwitch({ switched: false, problem }), { switched: false, problem });
  }
  for (const bad of [
    null, "x", {}, { switched: "yes", problem: "" }, { switched: true, problem: "refused" }, { switched: false, problem: "" },
    { switched: false, problem: "later" }, { switched: false, problem: 1 },
  ]) {
    assert.throws(() => parseSourceSwitch(bad), /switch/, JSON.stringify(bad));
  }
  await assert.rejects(activityApi(async () => ({ switched: 1 })).switchSource("r1", ""), /switch/);
});

test("the popup reads the advice: null when none, checked when there is one", async () => {
  const calls = [];
  let reply = null;
  const api = promptApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  });
  assert.equal(await api.advice(), null);
  reply = undefined;
  assert.equal(await api.advice(), null);
  reply = whole;
  assert.deepEqual(await api.advice(), whole);
  assert.deepEqual(calls[0], ["paratech/desktop/internal/input/wails.PromptService.Advice"]);
  assert.equal(promptMethods.advice, "paratech/desktop/internal/input/wails.PromptService.Advice");
  reply = { ...advice, silentName: 5 };
  await assert.rejects(api.advice(), /advice/);
});

test("the advice names the silent app and the one that sounds, or the whole computer", () => {
  assert.deepEqual(adviceModel(advice), {
    text: "В Яндекс Браузер нет звука, звук идёт из Comet. Переключиться?",
    hint: "browser.exe → comet.exe",
  });
  assert.deepEqual(adviceModel(whole), {
    text: "В Яндекс Браузер нет звука, звук идёт из других приложений. Записывать весь звук компьютера?",
    hint: "browser.exe → весь звук компьютера",
  });
  // A missing readable name falls back to the exe name.
  assert.equal(
    adviceModel({ ...advice, silentName: " ", suggestName: "" }).text,
    "В browser.exe нет звука, звук идёт из comet.exe. Переключиться?",
  );
  assert.deepEqual([adviceLabels.switch, adviceLabels.keep], ["Переключиться", "Оставить"]);
  assert.notEqual(adviceKey(advice), adviceKey(whole), "a new suggestion is another advice");
});

test("«Переключиться» and «Оставить»: done, or a problem shown next to the kept advice", async () => {
  const calls = [];
  const fake = (answer, fail) => ({
    switchSource: async (id, app) => {
      calls.push(["switch", id, app]);
      if (fail) throw fail;
      return answer;
    },
    dismissAdvice: async (id) => {
      calls.push(["dismiss", id]);
      if (fail) throw fail;
    },
  });
  assert.deepEqual(await switchToSuggestion(fake({ switched: true, problem: "" }), advice), { kind: "done" });
  assert.deepEqual(await switchToSuggestion(fake({ switched: true, problem: "" }), whole), { kind: "done" });
  assert.deepEqual(calls, [["switch", "r1", "comet.exe"], ["switch", "r1", ""]]);
  assert.deepEqual(await switchToSuggestion(fake({ switched: false, problem: "source_gone" }), advice), {
    kind: "problem", text: switchProblemTexts.source_gone,
  });
  assert.deepEqual(await switchToSuggestion(fake({ switched: false, problem: "refused" }), advice), {
    kind: "problem", text: switchProblemTexts.refused,
  });
  assert.deepEqual(await switchToSuggestion(fake({ switched: false, problem: "failed" }), advice), {
    kind: "problem", text: "Не удалось переключиться: источник не запустился. Запись идёт как раньше.",
  });
  assert.deepEqual(await keepSource(fake(), advice), { kind: "done" });
  assert.deepEqual(calls.at(-1), ["dismiss", "r1"]);

  // Errors get fixed texts; the raw text is never shown.
  const secret = new Error("C:\\Users\\student\\secret.db is locked");
  assert.deepEqual(await switchToSuggestion(fake(null, secret), advice), { kind: "problem", text: "Не удалось выполнить действие." });
  assert.deepEqual(await keepSource(fake(null, new Error(errorMessages.recordingNotRunning)), advice), {
    kind: "problem", text: "Эта запись уже не идёт.",
  });
  for (const msg of Object.values(errorMessages)) {
    assert.match(adviceErrorText(new Error(msg)), /[а-яё]/i, msg);
  }
  assert.equal(switchProblemText({ switched: true, problem: "" }), "");
  assert.equal(switchProblemText({ switched: false, problem: "toString" }), "Не удалось выполнить действие.");
});

test("the advice renders untrusted names as text, with both buttons and the problem", () => {
  const evil = { ...advice, silentName: "<img src=x onerror=alert(1)>", suggestName: "<b>Comet</b>" };
  const html = renderToStaticMarkup(h(SourceAdvice, { advice: evil, pending: null, problem: "", onSwitch: noop, onKeep: noop }));
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(html.includes("&lt;b&gt;Comet&lt;/b&gt;"));
  assert.ok(!html.includes("<img") && !html.includes("<b>"));
  assert.ok(html.includes('role="status"'), "a polite note, no dialog");
  assert.ok(html.includes("s-wait"));
  assert.ok(html.includes('title="browser.exe → comet.exe"'));
  assert.ok(html.includes("Переключиться") && html.includes("Оставить"));
  assert.ok(!html.includes("autofocus") && !html.includes("<dialog"));

  const busy = renderToStaticMarkup(
    h(SourceAdvice, { advice, pending: "switch", problem: switchProblemTexts.refused, onSwitch: noop, onKeep: noop }),
  );
  assert.ok(busy.includes(switchProblemTexts.refused));
  assert.ok(busy.includes(adviceLabels.switching));
  assert.equal(busy.split("disabled").length - 1, 2, "both buttons wait for the call");
  for (const file of ["../src/sourcePicker/SourceAdvice.tsx", "../src/sourcePicker/advice.ts"]) {
    assert.ok(!src(file).includes("dangerouslySetInnerHTML"), file);
  }
});

test("the row of the running recording shows the advice under it", () => {
  const props = {
    now: Date.parse(recording.startedAt), stopping: false, starting: false, onStop: noop, onRunNow: noop, onOpen: noop,
  };
  const html = renderToStaticMarkup(h(RecordingRow, { ...props, item: recording, onSwitchSource: noop, onKeepSource: noop }));
  assert.ok(html.includes("В Яндекс Браузер нет звука, звук идёт из Comet. Переключиться?"));
  assert.ok(html.includes('class="row-below"'));
  assert.ok(html.includes("Остановить"));
  const none = renderToStaticMarkup(h(RecordingRow, { ...props, item: { ...recording, advice: null }, onSwitchSource: noop, onKeepSource: noop }));
  assert.ok(!none.includes("row-below"));

  const page = src("../src/pages/RecordingsPage.tsx");
  assert.match(page, /switchToSuggestion\(api, a\)/);
  assert.match(page, /keepSource\(api, a\)/);
});

test("the popup shows the advice first, read with the prompt when shown, and steals no focus", () => {
  const popup = src("../src/popup/Popup.tsx");
  assert.match(popup, /api\.advice\(\)/);
  assert.match(popup, /onEvent\(shownEvent/);
  assert.match(popup, /switchToSuggestion\(activity, advice\)/);
  assert.match(popup, /keepSource\(activity, advice\)/);
  assert.match(popup, /<Banner kind="wait" role="status">/);
  for (const banned of ["autoFocus", ".focus(", "dangerouslySetInnerHTML"]) {
    assert.ok(!popup.includes(banned), banned);
  }
});

test("the picker labels apps by their readable names; the exe name is only in the tooltip", () => {
  const raw = sources({ apps: [{ ...comet, name: "<b>Comet</b>" }, yandex] });
  const parsed = parseSourcesView(raw);
  assert.deepEqual(parsed.apps[0], { ...comet, name: "<b>Comet</b>" });
  assert.equal(parsed.autoSelected, true);
  // An empty name (the core never sends one) falls back to the exe name.
  assert.equal(parseSourcesView(sources({ apps: [{ ...comet, name: "" }] })).apps[0].name, "comet.exe");
  for (const bad of [
    sources({ autoSelected: "yes" }), sources({ autoSelected: undefined }), sources({ apps: [{ ...comet, name: 1 }] }),
    sources({ apps: [{ ...comet, name: undefined }] }), sources({ apps: [{ ...comet, sounding: "yes" }] }),
    sources({ apps: [{ ...comet, sounding: undefined }] }),
  ]) {
    assert.throws(() => parseSourcesView(bad), /prompt/, JSON.stringify(bad));
  }

  const picker = pickerModel(parsed, chooseSource("comet.exe"));
  const open = renderToStaticMarkup(
    h(SourcePicker, { picker, disabled: false, open: true, onToggle: noop, onClose: noop, onChoose: noop }),
  );
  assert.ok(open.includes('<span class="name">&lt;b&gt;Comet&lt;/b&gt;</span>'));
  assert.ok(open.includes('<span class="name">Яндекс Браузер</span>'));
  assert.ok(!open.includes('class="name">comet.exe') && !open.includes('class="name">browser.exe'));
  assert.ok(open.includes('title="&lt;b&gt;Comet&lt;/b&gt; — Лекция — YouTube (comet.exe)"'));
  assert.ok(open.includes('title="Яндекс Браузер (browser.exe)"'));
  // Only the sounding app is marked, once in the closed row and once in the list; the core's order is kept.
  assert.equal(open.split(sourceMarks.sounding).length - 1, 2);
  assert.ok(open.includes('class="mark sounding"'));
  assert.deepEqual(picker.options.map((o) => o.app), ["", "comet.exe", "browser.exe"]);
});

test("the app that sounds is preselected over a silent remembered one, and the picker says why", () => {
  // The owner's case: Yandex Browser remembered, the video plays in Comet.
  let sel = nextSelection(initialSelection, sources());
  assert.deepEqual(sel, { app: "comet.exe", explicit: false });
  let m = pickerModel(sources(), sel);
  assert.equal(m.autoNote, autoSelectedNote);
  assert.equal(autoSelectedNote, "Выбрано: звук идёт отсюда.");
  const notes = renderToStaticMarkup(h(SourceNotes, { picker: m }));
  assert.ok(notes.includes(autoSelectedNote));

  // A pause in Comet's sound does not switch back to the silent app while Comet is open.
  sel = nextSelection(sel, sources({ selected: "browser.exe", autoSelected: false }));
  assert.equal(sel.app, "comet.exe");
  assert.equal(pickerModel(sources({ selected: "browser.exe", autoSelected: false }), sel).autoNote, autoSelectedNote);
  // Comet closed: back to the remembered app, without the note.
  sel = nextSelection(sel, sources({ apps: [yandex], selected: "browser.exe", autoSelected: false }));
  assert.deepEqual(sel, { app: "browser.exe", explicit: false });
  assert.equal(pickerModel(sources({ apps: [yandex], selected: "browser.exe", autoSelected: false }), sel).autoNote, "");

  // The user's choice wins and has no note; «Диагностика» shows the remembered source, without the note.
  m = pickerModel(sources(), nextSelection(chooseSource("browser.exe"), sources()));
  assert.equal(m.chosen.app, "browser.exe");
  assert.equal(m.autoNote, "");
  assert.equal(pickerModel(sources(), chooseSource("comet.exe")).autoNote, "");
  assert.equal(pickerModel(sources(), savedSelection(sources(), null)).autoNote, "");
  assert.equal(pickerModel(sources({ listError: true, apps: [] }), { app: "comet.exe", explicit: false }).autoNote, "");
});

test("a silent recording says «Запись без звука» and offers the sound check, not the transcript", () => {
  const silent = {
    ...recording, status: "COMPLETED", stoppable: false, stage: "failed", errorCode: "silentRecording", action: "checkSound",
    advice: null, hasDuration: true, durationSeconds: 3600,
  };
  const v = recordingRowView(silent, 0);
  assert.deepEqual([v.status.label, v.status.kind, v.status.icon], ["Запись без звука", "wait", "warn"]);
  assert.equal(
    v.status.hint,
    "В записи нет звука — скорее всего, был выбран не тот источник. Проверьте звук перед следующим занятием.",
  );
  assert.equal(chainStatusView(silent, 0).label, silentTexts.label);
  assert.deepEqual(v.action, { kind: "open", page: "diagnostics", label: "Проверить звук" });
  assert.equal(rowOffersTranscript(silent), false);
  assert.equal(rowOffersTranscript({ ...silent, errorCode: "transcriptionFailed" }), true);

  const html = renderToStaticMarkup(
    h(RecordingRow, { item: silent, now: 0, stopping: false, starting: false, onStop: noop, onRunNow: noop, onOpen: noop }),
  );
  assert.ok(html.includes("Запись без звука"));
  assert.ok(html.includes("Проверить звук"));
  assert.ok(!html.includes("Ошибка"));
});
