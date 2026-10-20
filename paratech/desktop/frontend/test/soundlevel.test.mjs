// The sound level capsule of a running recording (TASK-099): states, the «слышно» mark, the CSS for reduced motion
// and forced colours, the popup's recording state and the polling. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { activityApi, methods, parseRecordingSound } from "../.test-build/bindings/activity.js";
import { heardMarkPercent, levelPercent } from "../.test-build/diagnosticsView.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";
import { elapsedText, recordingInfo, recordingTexts, showsRecording } from "../.test-build/popup/view.js";
import {
  soundLevelModel,
  soundMarkPercent,
  soundKeepOnFailures,
  SoundPoller,
  soundPollMs,
  soundRetryMs,
  soundTexts,
} from "../.test-build/soundLevelView.js";
import { SoundLevel } from "../.test-build/ui/SoundLevel.js";

const { renderToStaticMarkup } = server;

// Mirror RecordingSound of internal/input/wails/activity.go.
const idle = { recording: false, recordingId: "", level: 0, heard: false, quietMs: 0, quietHint: false };
const heard = (level = 0.2) => ({ recording: true, recordingId: "r1", level, heard: true, quietMs: 0, quietHint: false });
const quiet = (quietMs = 30000, quietHint = false) => ({
  recording: true, recordingId: "r1", level: 0.001, heard: false, quietMs, quietHint,
});

test("recordingSound calls the bound Go method and checks the reply", async () => {
  const calls = [];
  const api = activityApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return heard(0.5);
  });
  assert.deepEqual(await api.recordingSound(), heard(0.5));
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.ActivityService.RecordingSound"]]);
  assert.equal(methods.recordingSound, "paratech/desktop/internal/input/wails.ActivityService.RecordingSound");
  assert.deepEqual(parseRecordingSound(idle), idle);
  for (const bad of [null, {}, { ...heard(), level: 2 }, { ...heard(), recordingId: "" }, { ...idle, recordingId: "r1" }]) {
    assert.throws(() => parseRecordingSound(bad), /sound/, JSON.stringify(bad));
  }
});

test("no recording: nothing to show", () => {
  assert.equal(soundLevelModel(null), null);
  assert.equal(soundLevelModel(idle), null);
  assert.equal(renderToStaticMarkup(h(SoundLevel, { sound: idle })), "");
});

test("heard: filled by the level on the dB scale of «Диагностика», with the word", () => {
  for (const level of [0.01, 0.2, 1]) {
    const m = soundLevelModel(heard(level));
    assert.equal(m.state, "heard");
    assert.equal(m.percent, levelPercent(level));
    assert.equal(m.word, "Звук идёт");
    assert.equal(m.label, "Уровень звука: звук идёт");
    assert.equal(m.hint, "");
  }
  const html = renderToStaticMarkup(h(SoundLevel, { sound: heard(1) }));
  assert.match(html, /class="lvl on"/);
  assert.match(html, /role="img"/);
  assert.match(html, /aria-label="Уровень звука: звук идёт"/);
  assert.match(html, /transform:scaleX\(1\)/);
  assert.match(html, />Звук идёт</);
  assert.match(renderToStaticMarkup(h(SoundLevel, { sound: heard(), large: true })), /class="lvl on lg"/);
});

test("quiet: an empty grey capsule and «Тихо», no hint before the core asks", () => {
  const m = soundLevelModel(quiet(90000));
  assert.deepEqual(m, { state: "quiet", percent: 0, word: "Тихо", label: "Уровень звука: тихо", hint: "" });
  const html = renderToStaticMarkup(h(SoundLevel, { sound: quiet(90000), hint: true }));
  assert.match(html, /class="lvl quiet"/);
  assert.doesNotMatch(html, /scaleX/, "the quiet sliver is drawn by CSS, not by the level");
  assert.doesNotMatch(html, /lvl-hint/);
});

test("quietHint: the minutes and a calm line, decided by the core only", () => {
  const m = soundLevelModel(quiet(125000, true));
  assert.equal(m.state, "hint");
  assert.equal(m.word, "Тихо 2 мин");
  assert.equal(m.label, "Уровень звука: звука нет 2 минуты");
  assert.equal(m.hint, soundTexts.hint);
  assert.equal(soundLevelModel(quiet(5 * 60000, true)).label, "Уровень звука: звука нет 5 минут");
  assert.equal(soundLevelModel(quiet(21 * 60000, true)).label, "Уровень звука: звука нет 21 минуту");
  // No own threshold: a long quiet without the core's hint stays «Тихо».
  assert.equal(soundLevelModel(quiet(10 * 60000, false)).state, "quiet");
  const html = renderToStaticMarkup(h(SoundLevel, { sound: quiet(125000, true), hint: true }));
  assert.match(html, /class="lvl-hint">Если занятие идёт/);
  assert.doesNotMatch(renderToStaticMarkup(h(SoundLevel, { sound: quiet(125000, true) })), /lvl-hint/);
});

test("the «слышно» mark is the one of «Диагностика»", () => {
  assert.equal(soundMarkPercent, heardMarkPercent);
  assert.match(renderToStaticMarkup(h(SoundLevel, { sound: heard() })), new RegExp(`left:${heardMarkPercent}%`));
});

test("the CSS is scoped and handles reduced motion and forced colours", () => {
  const css = readFileSync(new URL("../src/ui/soundLevel.css", import.meta.url), "utf8");
  assert.match(css, /\.lvl-bar \{[^}]*width: 56px; height: 4px;/);
  assert.match(css, /transition: transform 160ms linear/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\) \{\s*\.lvl-bar b \{ transition: none; \}/);
  assert.match(css, /@media \(forced-colors: active\) \{[^@]*outline: 1px solid CanvasText[^@]*Highlight/);
  assert.doesNotMatch(css, /animation/, "the real level moves the fill: no breathing");
  for (const rule of css.match(/^[^@\s/}][^{]*\{/gm) ?? []) {
    assert.match(rule, /^\.lvl/, `rule not scoped: ${rule}`);
  }
  // Both windows load it.
  for (const entry of ["../src/main.tsx", "../src/popup/main.tsx"]) {
    assert.match(readFileSync(new URL(entry, import.meta.url), "utf8"), /ui\/soundLevel\.css/);
  }
});

const started = "2026-10-20T10:00:00Z";
const row = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", subject: "", kind: "", teacher: "",
  status: "RECORDING", startedAt: started, stoppedAt: "", durationSeconds: 0, hasDuration: false, problem: "",
  stoppable: true, stage: "recording", step: "", progressPercent: 0, partsDone: 0, parts: 0, queueReason: "",
  errorCode: "", jobError: "", action: "", actionJobId: "", retryWork: "", canRedoSummary: false,
  lectureTextReady: false, remainingSeconds: 0, hasRemaining: false, advice: null, ...over,
});
const rowHtml = (item, sound) =>
  renderToStaticMarkup(
    h(RecordingRow, {
      item, now: Date.parse(started) + 1000, stopping: false, starting: false,
      onStop() {}, onRunNow() {}, onOpen() {}, sound,
    }),
  );

test("the «Идёт запись» row shows the capsule under the chip, only for its own running recording", () => {
  const html = rowHtml(row(), heard());
  assert.match(html, /Идёт запись[\s\S]*class="lvl on"/);
  assert.doesNotMatch(rowHtml(row(), undefined), /class="lvl/);
  assert.doesNotMatch(rowHtml(row(), { ...heard(), recordingId: "r2" }), /class="lvl/);
  assert.doesNotMatch(rowHtml(row({ stage: "saving", status: "FINALIZING", stoppable: false }), heard()), /class="lvl/);
  assert.match(rowHtml(row(), quiet(130000, true)), /lvl-hint/);
});

const lesson = { id: "p1", kind: "lesson", title: "Матанализ", start: "10:00", end: "11:30", state: "OFFERED", problem: "" };

test("the popup shows the running recording unless a lesson is on offer or a start runs", () => {
  assert.equal(showsRecording(null, false, null), false);
  assert.equal(showsRecording(null, true, null), true);
  assert.equal(showsRecording(null, true, "start"), false);
  for (const state of ["STARTED", "SKIPPED", "EXPIRED"]) {
    assert.equal(showsRecording({ ...lesson, state }, true, null), true, state);
  }
  assert.equal(showsRecording(lesson, true, null), false, "a lesson offer stays");
  assert.equal(showsRecording({ ...lesson, state: "STARTING" }, true, null), false);
  assert.equal(showsRecording({ ...lesson, kind: "adhoc", title: "" }, true, null), true, "the tray offer is replaced");
  assert.equal(recordingTexts.stop, "Остановить");
  assert.equal(recordingTexts.hide, "Скрыть");
});

function fakeTimers() {
  let next = 0;
  const planned = new Map();
  return {
    planned,
    set(fn, ms) {
      planned.set(++next, { fn, ms });
      return next;
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

test("the poller reads every 250 ms while active, never two reads at a time, and stops when inactive", async () => {
  const timers = fakeTimers();
  const shown = [];
  let reads = 0;
  let running = 0;
  let max = 0;
  let release = () => {};
  const read = () => {
    reads++;
    running++;
    max = Math.max(max, running);
    return new Promise((r) => {
      release = (v) => {
        running--;
        r(v);
      };
    });
  };
  const p = new SoundPoller(read, (s) => shown.push(s), timers);
  p.refreshNow();
  assert.equal(reads, 0, "inactive: no read");

  p.setActive(true);
  assert.equal(reads, 1);
  p.refreshNow();
  assert.equal(reads, 1, "no overlapping read");
  release(heard());
  await settle();
  assert.equal(reads, 2, "a refresh during a read runs right after it");
  release(heard(0.5));
  await settle();
  assert.deepEqual(shown, [heard(), heard(0.5)]);
  assert.equal(timers.planned.size, 1);
  assert.equal([...timers.planned.values()][0].ms, soundPollMs);
  assert.equal(soundPollMs, 250);

  // Without untilIdle a reply without a recording keeps polling (the list may still show it running).
  timers.fire();
  release(idle);
  await settle();
  assert.equal(timers.planned.size, 1);

  // Inactive: the plan is dropped, nothing is shown, and a late reply is dropped.
  timers.fire();
  assert.equal(reads, 4);
  p.setActive(false);
  assert.equal(shown.at(-1), null);
  release(heard());
  await settle();
  assert.equal(timers.planned.size, 0);
  assert.equal(shown.at(-1), null, "a late reply is not shown");
  assert.equal(reads, 4);
  assert.equal(max, 1);
});

// Changed in the review: a failure no longer hides the capsule at once nor pauses untilIdle polling (the popup fell back
// to the prompt view while recording); it keeps the last reading for two failures and retries every second.
test("failed reads keep the last reading for two failures and retry; untilIdle pauses only after no recording", async () => {
  const timers = fakeTimers();
  const shown = [];
  let reply = heard();
  let fail = false;
  let reads = 0;
  const p = new SoundPoller(
    async () => {
      reads++;
      if (fail) {
        throw new Error("boom");
      }
      return reply;
    },
    (s) => shown.push(s),
    timers,
    true,
  );
  p.setActive(true);
  await settle();
  assert.equal(timers.planned.size, 1, "recording: polling goes on");

  fail = true;
  for (let i = 1; i <= soundKeepOnFailures; i++) {
    timers.fire();
    await settle();
    assert.deepEqual(shown.at(-1), heard(), `failure ${i} keeps the last reading`);
    assert.equal(timers.planned.size, 1, "untilIdle: a failure keeps polling");
    assert.equal([...timers.planned.values()][0].ms, soundRetryMs, "slower retry");
  }
  timers.fire();
  await settle();
  assert.equal(shown.at(-1), null, "a third failure in a row shows nothing");
  assert.equal(timers.planned.size, 1, "and still retries");
  assert.equal(soundRetryMs, 1000);

  fail = false;
  timers.fire();
  await settle();
  assert.deepEqual(shown.at(-1), heard(), "a good read shows again");
  assert.equal([...timers.planned.values()][0].ms, soundPollMs);

  // One failure after a good read keeps it again: the count starts over.
  fail = true;
  timers.fire();
  await settle();
  assert.deepEqual(shown.at(-1), heard());

  fail = false;
  reply = idle;
  timers.fire();
  await settle();
  assert.deepEqual(shown.at(-1), idle);
  assert.equal(timers.planned.size, 0, "untilIdle: no recording pauses");
  const before = reads;

  reply = heard();
  p.refreshNow();
  await settle();
  assert.equal(reads, before + 1);
  assert.equal(timers.planned.size, 1, "a refresh with a recording resumes");
  p.setActive(false);
  assert.equal(timers.planned.size, 0);
});

test("without untilIdle a failure retries too", async () => {
  const timers = fakeTimers();
  const shown = [];
  const p = new SoundPoller(async () => { throw new Error("boom"); }, (s) => shown.push(s), timers);
  p.setActive(true);
  await settle();
  assert.deepEqual(shown, [], "nothing shown before the third failure");
  assert.equal([...timers.planned.values()][0].ms, soundRetryMs);
  p.setActive(false);
});

test("the popup's recording state: title and timer from one list read, hh:mm:ss ticking locally", () => {
  assert.deepEqual(recordingInfo([row({ id: "r0" }), row()], "r1"), { title: "Матанализ", startedAt: started });
  assert.deepEqual(recordingInfo([row({ title: "  " })], "r1"), { title: "Запись без занятия", startedAt: started });
  assert.equal(recordingInfo([row()], "r2"), null);
  const t0 = Date.parse(started);
  assert.equal(elapsedText(started, t0 + (12 * 60 + 4) * 1000 + 999), "00:12:04");
  assert.equal(elapsedText(started, t0 + (25 * 3600 + 1) * 1000), "25:00:01");
  assert.equal(elapsedText(started, t0 - 5000), "00:00:00", "clock skew is clamped");
  assert.equal(elapsedText("", t0), "");
  assert.equal(elapsedText("bad", t0), "");

  const popup = readFileSync(new URL("../src/popup/Popup.tsx", import.meta.url), "utf8");
  // Read once per recording shown (keyed by its id), not with every sound read.
  assert.match(popup, /activity\.listRecordings\(\)[\s\S]*\}, \[activity, liveId\]\);/);
  assert.equal(popup.match(/listRecordings/g).length, 1);
  assert.match(popup, /useNow\(liveInfo !== null/);
  // The hint keeps the info icon of the mockup; «Остановка…» does not change the button width.
  assert.match(popup, /<p className="p-note soft">\s*<Icon name="info" \/>/);
  assert.match(popup, /busy=\{stopping\}\s*busyLabel=\{recordingTexts\.stopping\}/);
});

test("popup CSS: «Остановить» and «Скрыть» sit side by side, busy labels keep the width", () => {
  const css = readFileSync(new URL("../src/popup/popup.css", import.meta.url), "utf8");
  // The rule that spans a lone second button across the row must not reach the two-button row.
  const spans = css.match(/^[^{\n]*\{ grid-column: 1 \/ -1; \}/gm) ?? [];
  assert.equal(spans.length, 1);
  assert.match(spans[0], /\.pbtns:not\(\.two\) > \.btn:nth-child\(2\):last-child/);
  assert.doesNotMatch(spans[0], /\.pbtns > \.btn:nth-child/);
  assert.match(css, /\.btn-labels > \.btn-off \{ visibility: hidden; \}/);
  // .btn.danger comes after the ghost rules.
  assert.ok(css.indexOf(".btn.danger") > css.indexOf(".btn.ghost:hover:enabled"));
});

test("the main window reads the sound only while it is visible", () => {
  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /useRecordingSound\(api\.recordingSound, visible && anyRecording/);
  assert.match(page, /addEventListener\("visibilitychange"/);
  assert.match(page, /removeEventListener\("visibilitychange"/);
});
