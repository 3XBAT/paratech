// Source picker of the recording popup (TASK-059). Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseSourcesView, promptApi } from "../.test-build/bindings/prompt.js";
import {
  adviceTexts,
  chooseSource,
  collapseAdvice,
  initialSelection,
  nextSelection,
  pickerModel,
  problemText,
  refreshMs,
  shouldRefresh,
  sourceMarks,
  sourceOptions,
  SourcesPoller,
  wholeComputerName,
} from "../.test-build/popup/view.js";

// Mirror SourcesView of internal/input/wails/prompt.go.
const icon = "data:image/png;base64,iVBORw0KGgo=";
const chrome = { app: "chrome.exe", title: "Лекция <b> — YouTube", playing: true, muted: false, browser: true, icon };
const zoom = { app: "zoom.exe", title: "", playing: false, muted: true, browser: false, icon: "" };
const reply = (over = {}) => ({ apps: [chrome, zoom], selected: "", remembered: "", listError: false, ...over });

const lesson = {
  id: "p1", kind: "lesson", title: "Матанализ", start: "10:00", end: "11:30", state: "OFFERED", problem: "", canSnooze: true,
};
const states = ["OFFERED", "STARTING", "STARTED", "SNOOZED", "SKIPPED", "EXPIRED", "WITHDRAWN"];

test("sources calls the bound Go method and keeps the reply", async () => {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply({ selected: "zoom.exe", remembered: "zoom.exe" });
  };
  assert.deepEqual(await promptApi(call).sources(), reply({ selected: "zoom.exe", remembered: "zoom.exe" }));
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.PromptService.Sources"]]);
});

test("a malformed sources reply or a bad icon is an error", async () => {
  const bad = [
    null, "x", [], {}, reply({ apps: null }), reply({ apps: {} }), reply({ listError: "no" }),
    reply({ selected: null }), reply({ remembered: 1 }), reply({ apps: ["chrome.exe"] }),
    reply({ apps: [{ ...chrome, app: "" }] }),
    reply({ apps: [{ ...chrome, title: undefined }] }),
    reply({ apps: [{ ...chrome, playing: "yes" }] }),
    reply({ apps: [{ ...chrome, muted: 0 }] }),
    reply({ apps: [{ ...chrome, browser: undefined }] }),
    reply({ apps: [{ ...chrome, icon: 5 }] }),
    reply({ apps: [{ ...chrome, icon: "https://evil.example/x.png" }] }),
    reply({ apps: [{ ...chrome, icon: "data:image/svg+xml;base64,PHN2Zz4=" }] }),
    reply({ apps: [{ ...chrome, icon: "javascript:alert(1)" }] }),
    reply({ apps: [{ ...chrome, icon: 'data:image/png;base64,AAAA" onerror="x' }] }),
    reply({ apps: [{ ...chrome, icon: "data:image/png;base64," }] }),
    reply({ apps: [{ ...chrome, icon: " data:image/png;base64,AAAA" }] }),
  ];
  for (const b of bad) {
    assert.throws(() => parseSourcesView(b), undefined, JSON.stringify(b));
  }
  await assert.rejects(promptApi(async () => reply({ apps: [{ ...chrome, icon: "x" }] })).sources());
  assert.deepEqual(parseSourcesView(reply({ apps: [] })).apps, []);
});

test("the whole computer comes first, then the apps by title or exe name, a letter when there is no icon", () => {
  const opts = sourceOptions(reply());
  assert.deepEqual(
    opts.map((o) => [o.app, o.name]),
    [["", wholeComputerName], ["chrome.exe", "Лекция <b> — YouTube"], ["zoom.exe", "zoom.exe"]],
  );
  assert.equal(wholeComputerName, "Весь звук компьютера");
  assert.equal(opts[1].icon, icon);
  assert.equal(opts[1].hint, "Лекция <b> — YouTube (chrome.exe)");
  assert.equal(opts[2].icon, "");
  assert.equal(opts[2].letter, "Z");
  assert.equal(sourceOptions(reply({ apps: [{ ...zoom, title: "  " }] }))[1].name, "zoom.exe");
  assert.deepEqual(sourceOptions(null).map((o) => o.app), [""]);
  assert.deepEqual(
    [sourceMarks.playing, sourceMarks.muted, sourceMarks.gone],
    ["звучит", "звук выключен в микшере", "приложение закрыто"],
  );
});

test("preselection: the user's choice wins, else the remembered app as soon as it is listed, else selected", () => {
  assert.deepEqual(nextSelection(initialSelection, reply()), { app: "", explicit: false });

  // Remembered but not open yet; then it appears; it stays chosen while the app restarts.
  let sel = nextSelection(initialSelection, reply({ apps: [chrome], remembered: "zoom.exe" }));
  assert.equal(sel.app, "");
  sel = nextSelection(sel, reply({ remembered: "zoom.exe" }));
  assert.deepEqual(sel, { app: "zoom.exe", explicit: false });
  sel = nextSelection(sel, reply({ apps: [chrome], remembered: "zoom.exe" }));
  assert.equal(sel.app, "zoom.exe");

  assert.equal(nextSelection(initialSelection, reply({ selected: "chrome.exe" })).app, "chrome.exe");

  // The user's choice, the whole computer included, is never replaced by a refresh.
  for (const app of ["", "chrome.exe"]) {
    const next = nextSelection(chooseSource(app), reply({ selected: "zoom.exe", remembered: "zoom.exe" }));
    assert.deepEqual(next, { app, explicit: true });
  }
});

test("a chosen app that closed stays chosen and is marked closed; without a list it is not marked", () => {
  const m = pickerModel(reply({ apps: [chrome] }), chooseSource("zoom.exe"));
  assert.equal(m.chosen.app, "zoom.exe");
  assert.equal(m.chosen.name, "zoom.exe");
  assert.equal(m.chosen.gone, true);
  assert.equal(pickerModel(reply(), chooseSource("zoom.exe")).chosen.gone, false);
  assert.equal(pickerModel(reply({ apps: [], listError: true }), chooseSource("zoom.exe")).chosen.gone, false);
  assert.equal(pickerModel(null, chooseSource("zoom.exe")).chosen.gone, false);
  assert.equal(problemText("source_gone"), "Выбранное приложение не запущено. Откройте его или выберите другое.");
});

test("advice: the general one always; browser, muted and list error only when they apply", () => {
  const whole = pickerModel(reply(), initialSelection);
  assert.equal(whole.chosen.name, wholeComputerName);
  assert.deepEqual(whole.warnings, []);
  assert.equal(whole.advice, adviceTexts.always);
  assert.match(adviceTexts.always, /^Чтобы не слышать лекцию, убавьте громкость Windows или наушников\./);

  assert.deepEqual(pickerModel(reply(), chooseSource("chrome.exe")).warnings, [adviceTexts.browser]);
  assert.deepEqual(pickerModel(reply(), chooseSource("zoom.exe")).warnings, [adviceTexts.muted]);
  const mutedBrowser = reply({ apps: [{ ...chrome, muted: true }] });
  assert.deepEqual(pickerModel(mutedBrowser, chooseSource("chrome.exe")).warnings, [adviceTexts.muted, adviceTexts.browser]);

  const failed = pickerModel(reply({ apps: [], listError: true }), initialSelection);
  assert.deepEqual(failed.warnings, [adviceTexts.listError]);
  assert.deepEqual(failed.options.map((o) => o.app), [""]);
  assert.equal(new Set(Object.values(adviceTexts)).size, 4);
});

test("the general advice collapses only next to two or more other messages", () => {
  const error = problemText("in_progress");
  assert.equal(collapseAdvice([], ""), false);
  assert.equal(collapseAdvice([adviceTexts.browser], ""), false);
  assert.equal(collapseAdvice([adviceTexts.muted], ""), false);
  assert.equal(collapseAdvice([], error), false);
  assert.equal(collapseAdvice([adviceTexts.browser], error), true);
  assert.equal(collapseAdvice([adviceTexts.listError], error), true);
  assert.equal(collapseAdvice([adviceTexts.muted, adviceTexts.browser], ""), true);
  assert.equal(collapseAdvice([adviceTexts.muted, adviceTexts.browser], error), true);

  // The worst case of the review: a browser muted in the mixer; the advice text itself stays verbatim.
  const m = pickerModel(reply({ apps: [{ ...chrome, muted: true }] }), chooseSource("chrome.exe"));
  assert.equal(collapseAdvice(m.warnings, error), true);
  assert.equal(m.advice, adviceTexts.always);
});

test("the list refreshes only while visible, on offer, idle and closed", () => {
  assert.equal(refreshMs, 3000);
  assert.equal(shouldRefresh(lesson, true, null, false), true);
  assert.equal(shouldRefresh({ ...lesson, kind: "adhoc" }, true, null, false), true);
  assert.equal(shouldRefresh(lesson, false, null, false), false, "hidden");
  assert.equal(shouldRefresh(lesson, true, "start", false), false, "starting");
  assert.equal(shouldRefresh(lesson, true, "skip", false), false, "skipping");
  assert.equal(shouldRefresh(lesson, true, null, true), false, "list open");
  assert.equal(shouldRefresh(null, true, null, false), false, "no prompt");
  for (const state of states.filter((s) => s !== "OFFERED")) {
    assert.equal(shouldRefresh({ ...lesson, state }, true, null, false), false, state);
  }
});

// fakeTimers runs planned callbacks only when the test fires them.
function fakeTimers() {
  let next = 1;
  const planned = new Map();
  return {
    planned,
    set(fn, ms) {
      planned.set(next, { fn, ms });
      return next++;
    },
    clear(h) {
      planned.delete(h);
    },
    fire() {
      const [[h, { fn }]] = planned;
      planned.delete(h);
      fn();
    },
  };
}

const settle = () => new Promise((r) => setImmediate(r));

test("the poller loads at once, then an interval after each load, never two loads at a time", async () => {
  const timers = fakeTimers();
  let running = 0;
  let max = 0;
  let loads = 0;
  let release = () => {};
  const load = () => {
    loads++;
    running++;
    max = Math.max(max, running);
    return new Promise((r) => {
      release = () => {
        running--;
        r();
      };
    });
  };
  const p = new SourcesPoller(load, timers, 3000);
  p.refreshNow();
  assert.equal(loads, 0, "inactive: no load");

  p.setActive(true);
  assert.equal(loads, 1);
  p.refreshNow();
  assert.equal(loads, 1, "no overlapping load");
  assert.equal(timers.planned.size, 0, "nothing planned while a load runs");
  release();
  await settle();
  assert.equal(loads, 2, "a refreshNow during a load runs right after it");
  release();
  await settle();
  assert.equal(timers.planned.size, 1);
  assert.equal([...timers.planned.values()][0].ms, 3000);

  timers.fire();
  assert.equal(loads, 3);
  release();
  await settle();
  assert.equal(timers.planned.size, 1);

  // Stopped (hidden, started): the plan is dropped and a load that settles late plans nothing.
  timers.fire();
  assert.equal(loads, 4);
  p.setActive(false);
  release();
  await settle();
  assert.equal(timers.planned.size, 0);
  assert.equal(loads, 4);
  assert.equal(max, 1);

  // Active again: loads at once.
  p.setActive(true);
  assert.equal(loads, 5);
  p.setActive(false);
  release();
  await settle();
  assert.equal(timers.planned.size, 0);
});

test("a failed load does not stop the poller", async () => {
  const timers = fakeTimers();
  let loads = 0;
  const p = new SourcesPoller(async () => {
    loads++;
    throw new Error("boom");
  }, timers);
  p.setActive(true);
  await settle();
  assert.equal(loads, 1);
  assert.equal(timers.planned.size, 1);
  assert.equal([...timers.planned.values()][0].ms, refreshMs);
  p.setActive(false);
  assert.equal(timers.planned.size, 0);
});
