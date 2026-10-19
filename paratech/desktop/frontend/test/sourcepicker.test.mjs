// The source picker shared by the popup, «Диагностика» and «Записи и задачи», and «Начать запись» without the popup
// (TASK-081). Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { SourceNotes, SourcePicker } from "../.test-build/sourcePicker/SourcePicker.js";
import {
  adviceTexts,
  failedSources,
  pickerModel,
  problemTexts,
  savedSelection,
  sourceMarks,
  wholeComputerName,
} from "../.test-build/sourcePicker/view.js";
import { manualStartTexts, recordingRuns, startButton, startOutcome } from "../.test-build/manualStartView.js";
import { pickerHint, texts } from "../.test-build/diagnosticsView.js";

const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

// Mirror SourcesView of internal/input/wails/prompt.go.
const icon = "data:image/png;base64,iVBORw0KGgo=";
const chrome = { app: "chrome.exe", title: "Лекция <b> — YouTube", playing: true, muted: false, browser: true, icon };
const zoom = { app: "zoom.exe", title: "", playing: false, muted: true, browser: false, icon: "" };
const reply = (over = {}) => ({ apps: [chrome, zoom], selected: "", remembered: "", listError: false, ...over });

const noop = () => undefined;
const render = (props) =>
  server.renderToStaticMarkup(
    h(SourcePicker, { disabled: false, open: false, onToggle: noop, onClose: noop, onChoose: noop, ...props }),
  );

test("«Диагностика» shows the remembered source, the one the sound check listens to", () => {
  // Remembered and running: the app itself, not the core's preselection.
  assert.deepEqual(savedSelection(reply({ remembered: "zoom.exe", selected: "zoom.exe" }), null), {
    app: "zoom.exe", explicit: false,
  });
  // Remembered but not running: still chosen and marked closed, since the check reports it as not running.
  const closed = pickerModel(reply({ apps: [chrome], remembered: "zoom.exe" }),
    savedSelection(reply({ apps: [chrome], remembered: "zoom.exe" }), null));
  assert.equal(closed.chosen.app, "zoom.exe");
  assert.equal(closed.chosen.gone, true);
  // A choice being saved shows at once; nothing loaded yet is the whole computer.
  assert.deepEqual(savedSelection(reply({ remembered: "zoom.exe" }), "chrome.exe"), { app: "chrome.exe", explicit: true });
  assert.deepEqual(savedSelection(reply({ remembered: "zoom.exe" }), ""), { app: "", explicit: true });
  assert.deepEqual(savedSelection(null, null), { app: "", explicit: false });
});

test("a failed list offers only the whole computer and keeps the remembered source", () => {
  assert.deepEqual(failedSources(null), { apps: [], selected: "", remembered: "", listError: true });
  const kept = failedSources(reply({ remembered: "zoom.exe" }));
  assert.deepEqual(kept, { apps: [], selected: "", remembered: "zoom.exe", listError: true });
  const m = pickerModel(kept, savedSelection(kept, null));
  assert.deepEqual(m.options.map((o) => o.app), [""]);
  assert.equal(m.options[0].name, wholeComputerName);
  // Without a list nothing is known about the app: no «закрыто» mark, the list warning instead.
  assert.equal(m.chosen.gone, false);
  assert.ok(m.warnings.includes(adviceTexts.listError));
});

test("the picker renders untrusted names as text, icons and marks, and the list only when open", () => {
  const picker = pickerModel(reply(), { app: "chrome.exe", explicit: true });
  const closed = render({ picker });
  assert.ok(closed.includes("Лекция &lt;b&gt; — YouTube"), "the title is text");
  assert.ok(!closed.includes("<b>"));
  assert.ok(closed.includes(`src="${icon}"`));
  assert.ok(closed.includes(sourceMarks.playing));
  assert.ok(closed.includes('aria-haspopup="listbox"'));
  assert.ok(closed.includes('aria-expanded="false"'));
  assert.ok(!closed.includes('role="listbox"'));

  const open = render({ picker, open: true, labelId: "lbl" });
  assert.ok(open.includes('role="listbox" aria-labelledby="lbl"'));
  assert.equal(open.split('role="option"').length - 1, 3);
  assert.ok(open.includes(wholeComputerName));
  // zoom.exe has no title and no icon: its name and first letter, and the muted mark.
  assert.ok(open.includes(">Z</span>"));
  assert.ok(open.includes(sourceMarks.muted));
  assert.ok(open.includes('class="source current"'));

  // A disabled picker never opens and says why: the hover text and a hidden description.
  const locked = render({ picker, open: true, disabled: true, disabledReason: texts.sourceLocked });
  assert.ok(!locked.includes('role="listbox"'));
  assert.ok(locked.includes(`title="${texts.sourceLocked}"`));
  assert.match(locked, /aria-describedby="([^"]+)"[\s\S]*<span id="\1" class="vh">/);
  // Without a reason the hover text is the source.
  assert.ok(!render({ picker, disabled: true }).includes("vh"));
  assert.ok(closed.includes('title="Лекция &lt;b&gt; — YouTube (chrome.exe)"'));
});

test("the notes are the warnings of the chosen source and, if asked, the advice", () => {
  const picker = pickerModel(reply(), { app: "chrome.exe", explicit: true });
  const notes = server.renderToStaticMarkup(h(SourceNotes, { picker }));
  assert.ok(notes.includes(adviceTexts.browser));
  assert.ok(!notes.includes(adviceTexts.always));
  assert.ok(server.renderToStaticMarkup(h(SourceNotes, { picker, advice: true })).includes(adviceTexts.always));
});

test("one picker: the popup, «Диагностика» and «Записи и задачи» use the shared component", () => {
  const popup = src("../src/popup/Popup.tsx");
  assert.match(popup, /<SourcePicker\s/);
  assert.ok(!popup.includes('className="source'), "the popup draws its own picker");
  assert.ok(!src("../src/popup/view.ts").includes("export function pickerModel"), "picker logic left in the popup");
  for (const page of ["../src/pages/DiagnosticsPage.tsx", "../src/pages/RecordingsPage.tsx"]) {
    const s = src(page);
    assert.match(s, /<SourcePicker\s/, page);
    assert.match(s, /pickerModel\(sources, selection\)/, page);
    assert.ok(!s.includes("function SourceRow"), page);
  }
  for (const file of ["../src/sourcePicker/SourcePicker.tsx", "../src/sourcePicker/view.ts", "../src/manualStartView.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "href="]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  // Both windows load the picker styles; the popup still loads nothing else of the main window.
  assert.match(src("../src/popup/main.tsx"), /import "\.\.\/sourcePicker\/sourcePicker\.css";/);
  assert.match(src("../src/main.tsx"), /import "\.\/sourcePicker\/sourcePicker\.css";/);
  assert.ok(!src("../src/popup/main.tsx").includes("styles.css"));
  assert.ok(!src("../src/popup/popup.css").includes(".source"), "picker rules left in popup.css");
});

test("«Диагностика»: a choice is remembered by the core and cannot change during a check", () => {
  const page = src("../src/pages/DiagnosticsPage.tsx");
  assert.match(page, /await prompts\.selectSource\(app\)/);
  assert.match(page, /savedSelection\(sources, saving\)/);
  // The picker is locked while a check runs (with the reason), and the check waits for a choice being saved.
  assert.match(page, /disabled=\{sources === null \|\| saving !== null \|\| checking \|\| calling\}/);
  assert.match(page, /disabledReason=\{checking \? texts\.sourceLocked : undefined\}/);
  assert.match(page, /if \(saving !== null \|\| checkRuns\.current \|\| app === selection\.app\)/);
  assert.match(page, /<Button disabled=\{check\.disabled \|\| saving !== null\}/);
  assert.match(texts.sourceLocked, /во время проверки/);
});

test("«Диагностика»: a closed app sends the user to the picker right there", () => {
  const done = {
    state: "done", level: 0, peak: 0, heard: false, elapsedMs: 0, remainingMs: 0, durationMs: 15000,
    verdict: "not_running", message: "Выбранное приложение не запущено.", hints: [], source: "zoom.exe",
    wholeComputer: false,
  };
  assert.equal(pickerHint(done), texts.pickAnother);
  assert.match(texts.pickAnother, /в списке выше/);
  assert.match(texts.pickAnother, /Весь звук компьютера/);
  for (const verdict of ["ok", "silence", "no_device", "unsupported", "busy", "failed"]) {
    assert.equal(pickerHint({ ...done, verdict }), "", verdict);
  }
  assert.equal(pickerHint({ ...done, state: "running", verdict: "" }), "");
});

const rec = (stage) => ({ stage });

test("«Начать запись» is unavailable while a recording runs, and says why", () => {
  assert.equal(recordingRuns(null), false);
  assert.equal(recordingRuns([]), false);
  assert.equal(recordingRuns([rec("done"), rec("saving"), rec("queued")]), false);
  assert.equal(recordingRuns([rec("done"), rec("recording")]), true);

  assert.deepEqual(startButton([rec("done")], false), { disabled: false, reason: "" });
  assert.deepEqual(startButton([rec("done")], true), { disabled: true, reason: "" });
  assert.deepEqual(startButton([rec("recording")], false), { disabled: true, reason: manualStartTexts.busy });
  // The list not loaded yet: the core refuses a second recording itself.
  assert.deepEqual(startButton(null, false), { disabled: false, reason: "" });

  const page = src("../src/pages/RecordingsPage.tsx");
  assert.match(page, /const startState = startButton\(recordings, starting\);/);
  assert.match(page, /title=\{startState\.reason \|\| undefined\}\s*aria-describedby=\{startState\.reason \? reasonId : undefined\}/);
  assert.match(page, /<span id=\{reasonId\} className="vh">\s*\{startState\.reason\}/);
});

test("a started recording closes the panel; a refused one keeps it open with the popup's problem text", () => {
  assert.deepEqual(startOutcome({ started: true, problem: "", recordingId: "rec-1", linked: true }), { kind: "started" });
  assert.deepEqual(startOutcome({ started: true, problem: "", recordingId: "rec-1", linked: false }), { kind: "started" });
  for (const p of ["unavailable", "in_progress", "recovering", "source_gone", "failed"]) {
    assert.deepEqual(startOutcome({ started: false, problem: p, recordingId: "", linked: false }),
      { kind: "refused", problem: problemTexts[p] });
  }
  assert.deepEqual(startOutcome({ started: false, problem: "new_code", recordingId: "", linked: false }),
    { kind: "refused", problem: problemTexts.failed });

  const page = src("../src/pages/RecordingsPage.tsx");
  assert.match(page, /startOutcome\(await prompts\.startRecording\(picker\.chosen\.app\)\)/);
  assert.match(page, /if \(out\.kind === "started"\) \{\s*setStartOpen\(false\);\s*\} else \{\s*setStartProblem\(out\.problem\);/);
  // The list is read again after every start, so the new row shows «Идёт запись» and «Остановить».
  assert.match(page, /setStarting\(false\);\s*void refresh\(\);/);
  assert.equal(manualStartTexts.open, "Начать запись");
});
