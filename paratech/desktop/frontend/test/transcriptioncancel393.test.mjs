// «Отменить расшифровку» and «Расшифровать снова» on the recording row (TASK-393). Runs on the modules compiled by
// `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { CancelTranscriptionDialog, RecordingRow } from "../.test-build/pages/RecordingRow.js";
import {
  actionLabels,
  cancelledTexts,
  chainStatusView,
  errorTexts,
  isFinishedRow,
  pageGroups,
  recordingRowView,
  rowAction,
  rowActions,
} from "../.test-build/recordingRowView.js";
import { problemOf } from "../.test-build/todayView.js";
import {
  cancelAskOpen,
  cancelAskSkipped,
  cancelBody,
  cancelDecision,
  cancelNote,
  cancelSkipKey,
  cancelTexts,
  confirmCancel,
  Flights,
  flightKey,
  focusMove,
  keptAfterCancel,
  keptAfterRetranscribe,
  noteTexts,
  noteVisible,
  retranscribeNote,
  retranscribeTexts,
  rowOffersCancel,
  thrownNotes,
} from "../.test-build/transcriptionCancelView.js";

const { renderToStaticMarkup } = server;

const started = "2026-10-18T17:19:35Z";
const row = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", subject: "", kind: "", teacher: "",
  status: "COMPLETED", startedAt: started, stoppedAt: "",
  durationSeconds: 0, hasDuration: false, problem: "", stoppable: false, stage: "done", step: "", progressPercent: 0,
  partsDone: 0, parts: 0, queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "", retryWork: "",
  canRedoSummary: false, lectureTextReady: false, canCancel: false,
  remainingSeconds: 0, hasRemaining: false, advice: null, origin: "recorded", ...over,
});

// What the core sends (TASK-392 / TASK-409).
const running = (over = {}) =>
  row({ stage: "transcribing", step: "transcription", progressPercent: 42, canCancel: true, remainingSeconds: 600, hasRemaining: true, ...over });
const cancelled = (over = {}) =>
  row({ stage: "failed", step: "transcription", errorCode: "transcriptionCancelled", action: "retranscribe", ...over });
const failedTranscription = (over = {}) =>
  row({ stage: "failed", step: "transcription", errorCode: "transcriptionFailed", action: "retranscribe", ...over });

function render(item, props = {}) {
  return renderToStaticMarkup(
    h(RecordingRow, {
      item, now: Date.parse(started), stopping: false, starting: false,
      onStop: () => undefined, onRunNow: () => undefined, onOpen: () => undefined, ...props,
    }),
  );
}

// An in-memory localStorage; `fail` makes every call throw (a blocked storage).
function fakeStore(initial = {}, fail = false) {
  const data = { ...initial };
  return {
    data,
    getItem(k) {
      if (fail) throw new Error("blocked");
      return Object.hasOwn(data, k) ? data[k] : null;
    },
    setItem(k, v) {
      if (fail) throw new Error("blocked");
      data[k] = v;
    },
  };
}

test("a cancelled transcription is a quiet status with «Расшифровать снова» as the main word button", () => {
  assert.deepEqual(cancelledTexts, { label: "Расшифровка отменена", hint: "Запись сохранена." });
  const v = recordingRowView(cancelled(), 0);
  assert.deepEqual([v.status.label, v.status.kind, v.status.icon, v.status.hint], ["Расшифровка отменена", "neutral", "stop", "Запись сохранена."]);
  assert.deepEqual(rowAction(cancelled()), { kind: "retranscribe", label: "Расшифровать снова" });
  assert.equal(actionLabels.retranscribe, "Расшифровать снова");
  assert.ok(!Object.hasOwn(errorTexts, "transcriptionCancelled"), "no error text: it is not an error");

  // The main button leads even when the page offers the texts: the way back is a visible word button.
  const a = rowActions(cancelled(), { texts: true, redo: true });
  assert.deepEqual([a.main?.key, a.main?.label, a.menu.map((i) => i.key)], ["retranscribe", "Расшифровать снова", ["transcript"]]);
  const bare = rowActions(cancelled(), { texts: false, redo: false });
  assert.deepEqual([bare.main?.key, bare.menu], ["retranscribe", []]);

  const html = render(cancelled(), { onOpenLesson: () => undefined, onRetranscribe: () => undefined });
  assert.match(html, /<span class="chip s-neutral"[^>]*>.*Расшифровка отменена/);
  assert.ok(!html.includes("s-fail") && !html.includes("Ошибка"), "not the error colour");
  assert.ok(html.includes("Запись сохранена."));
  assert.match(html, /<div class="acts"><button[^>]*class="btn"[^>]*>.*Расшифровать снова<\/span>.*<\/button><div class="mmenu">/);
});

test("a failed transcription keeps its text and also offers «Расшифровать снова» (TASK-409 gap)", () => {
  const v = recordingRowView(failedTranscription(), 0);
  assert.deepEqual([v.status.label, v.status.kind, v.status.hint], ["Ошибка", "fail", "Не удалось расшифровать запись."]);
  assert.deepEqual(rowAction(failedTranscription()), { kind: "retranscribe", label: "Расшифровать снова" });
  const a = rowActions(failedTranscription(), { texts: true, redo: false });
  assert.equal(a.main?.key, "retranscribe");
  assert.ok(render(failedTranscription(), { onOpenLesson: () => undefined }).includes("Расшифровать снова"));
  // The old action still works.
  const old = rowActions(failedTranscription({ action: "openModels" }), { texts: false, redo: false });
  assert.equal(old.main?.key, "open");
});

// Replaces the old rule «every row with retranscribe stays active» (isFinishedRow): only a row the student cancelled in this
// session (keepActive) stays in «Идут»; every old failed transcription folds as before.
test("a row cancelled in this session stays in «Идут» while it offers «Расшифровать снова»; old failures fold", () => {
  assert.equal(isFinishedRow(cancelled()), true, "the rule of a finished row is unchanged");
  assert.equal(isFinishedRow(failedTranscription()), true);

  const rows = [cancelled({ id: "c" }), failedTranscription({ id: "f" }), row({ id: "d" })];
  const plain = pageGroups(rows, []);
  assert.deepEqual(plain.recordings.active.map((r) => r.id), []);
  assert.deepEqual(plain.recordings.finished.map((r) => r.id).sort(), ["c", "d", "f"]);
  const empty = pageGroups(rows, [], new Set());
  assert.deepEqual(empty.recordings.active.map((r) => r.id), []);

  const g = pageGroups(rows, [], new Set(["c"]));
  assert.deepEqual(g.recordings.active.map((r) => r.id), ["c"]);
  assert.deepEqual(g.recordings.finished.map((r) => r.id).sort(), ["d", "f"], "a plain old failure still folds");

  // The kept id counts only while the action is «Расшифровать снова»: once the chain moved on, the usual rules apply.
  const moved = pageGroups([row({ id: "c" }), cancelled({ id: "x", action: "openModels" })], [], new Set(["c", "x"]));
  assert.deepEqual(moved.recordings.active.map((r) => r.id), []);
  const restarted = pageGroups([running({ id: "c" })], [], new Set(["c"]));
  assert.deepEqual(restarted.recordings.active.map((r) => r.id), ["c"], "in progress is active anyway");
});

test("the kept set: a cancel (any answer but failed) adds the row, a started retranscription drops it", () => {
  const none = new Set();
  for (const result of ["cancelled", "not_running", "ending"]) {
    assert.deepEqual([...keptAfterCancel(none, "r1", result)], ["r1"], result);
  }
  assert.equal(keptAfterCancel(none, "r1", "failed"), none, "a failed cancel changes nothing");
  assert.deepEqual([...none], [], "the argument is not mutated");

  const kept = new Set(["r1", "r2"]);
  assert.deepEqual([...keptAfterRetranscribe(kept, "r1", "started")], ["r2"]);
  for (const result of ["no_model", "failed", "not_allowed"]) {
    assert.equal(keptAfterRetranscribe(kept, "r1", result), kept, result);
  }
  assert.equal(keptAfterRetranscribe(kept, "other", "started"), kept);
  assert.deepEqual([...kept], ["r1", "r2"], "the argument is not mutated");

  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /pageGroups\(recordings \?\? \[\], jobs \?\? \[\], kept\)/);
});

test("the focus after a cancel: to «Расшифровать снова» once it is there, back to the icon after a failure, never pulled", () => {
  const lost = { lost: true, busy: false };
  // The call has run and the refresh brought the cancelled row.
  assert.equal(focusMove("again", cancelled(), lost), "retranscribe");
  // The row is not refreshed yet (or «ending»): it still shows the progress; wait for the next refresh.
  assert.equal(focusMove("again", running(), lost), "wait");
  // The call still runs: wait.
  assert.equal(focusMove("again", cancelled(), { lost: true, busy: true }), "wait");
  assert.equal(focusMove("cancel", running(), { lost: true, busy: true }), "wait");
  // The student has moved the focus: leave it.
  assert.equal(focusMove("again", cancelled(), { lost: false, busy: false }), "drop");
  assert.equal(focusMove("cancel", running(), { lost: false, busy: false }), "drop");
  // A failed cancel: the row still offers the icon.
  assert.equal(focusMove("cancel", running(), lost), "cancel");
  // A failed cancel, but the recognition ended meanwhile.
  assert.equal(focusMove("cancel", cancelled(), lost), "retranscribe");
  assert.equal(focusMove("cancel", row(), lost), "drop");
  // The row is gone / has nothing to focus.
  assert.equal(focusMove("again", undefined, lost), "drop");
  assert.equal(focusMove("again", row(), lost), "drop");

  // The controls carry the data the page finds them by.
  assert.match(render(running(), { onCancel: () => undefined }), /<button[^>]*data-cancel-for="r1"[^>]*aria-label="Отменить расшифровку"|<button[^>]*aria-label="Отменить расшифровку"[^>]*data-cancel-for="r1"/);
  assert.match(render(cancelled(), { onRetranscribe: () => undefined }), /<button[^>]*data-retranscribe-for="r1"/);
});

test("a rejected cancel or retranscribe becomes the row's note and frees the flight", () => {
  assert.deepEqual(thrownNotes.cancel, { kind: "cancel", text: "Не удалось отменить расшифровку. Можно попробовать ещё раз.", openModels: false });
  assert.deepEqual(thrownNotes.retranscribe, { kind: "retranscribe", text: "Не удалось запустить расшифровку. Можно попробовать ещё раз.", openModels: false });
  assert.deepEqual(thrownNotes.cancel, cancelNote("failed"), "the same words as the core's «failed»");
  assert.deepEqual(thrownNotes.retranscribe, retranscribeNote("failed"));
  assert.equal(noteVisible(thrownNotes.cancel, running()), true);
  assert.equal(noteVisible(thrownNotes.retranscribe, cancelled()), true);

  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  const guarded = page.match(/async function guarded\([^]*?\n  \}\n/)?.[0] ?? "";
  assert.match(guarded, /catch \{[^]*?setNote\(id, thrownNotes\[kind\]\);/);
  assert.match(guarded, /finally \{\s*flights\.current\.end\(key\);/);
  assert.ok(!guarded.includes("setActionError"), "not the page-level action error");
});

test("«Сегодня» shows a cancelled recognition as its quiet status, not as a recording problem", () => {
  const p = problemOf(cancelled());
  assert.deepEqual(p, { label: "Расшифровка отменена", kind: "neutral", icon: "stop" });
  assert.equal(problemOf(failedTranscription()).label, "Проблема с записью");
});

test("the cancel icon shows with canCancel in transcribing and queued, next to the progress", () => {
  assert.equal(cancelTexts.action, "Отменить расшифровку");
  const html = render(running(), { onCancel: () => undefined });
  const button = html.match(/<button[^>]*aria-label="Отменить расшифровку"[^>]*>/)?.[0] ?? "";
  assert.ok(button.includes('type="button"') && button.includes("ia-danger") && button.includes("ia-sm"), button);
  assert.ok(!button.includes("disabled"), button);
  assert.ok(html.includes('<span class="ia-tip" aria-hidden="true">Отменить расшифровку</span>'), "the tooltip");
  assert.ok(html.includes("42 %"), "the progress stays");
  assert.match(html, /≈ \d+ мин осталось/, "the remaining time stays");
  assert.ok(html.indexOf("осталось") < html.indexOf("Отменить расшифровку"), "the icon after the remaining time");
  assert.ok(button.includes('class="ia '), "an icon button, not a word button");
  assert.ok(!/<button[^>]*class="btn/.test(html.slice(0, html.indexOf('<div class="acts">'))), "no word button in the status cell");

  const queued = render(running({ stage: "queued", queueReason: "inLine" }), { onCancel: () => undefined });
  assert.ok(queued.includes('aria-label="Отменить расшифровку"'));

  // Not offered: no flag, other stages, no handler.
  assert.ok(!render(running({ canCancel: false }), { onCancel: () => undefined }).includes("Отменить расшифровку"));
  assert.ok(!render(row({ stage: "summarizing", canCancel: true }), { onCancel: () => undefined }).includes("Отменить расшифровку"));
  assert.ok(!render(cancelled({ canCancel: true }), { onCancel: () => undefined }).includes("Отменить расшифровку"));
  assert.ok(!render(running()).includes("Отменить расшифровку"));
  assert.equal(rowOffersCancel(running()), true);
  assert.equal(rowOffersCancel(running({ stage: "recording" })), false);

  // The call running: the icon is off and says so.
  const busy = render(running(), { onCancel: () => undefined, cancelling: true }).match(/<button[^>]*aria-label="Отменить расшифровку"[^>]*>/)?.[0] ?? "";
  assert.ok(busy.includes("disabled") && busy.includes('aria-busy="true"'), busy);

  // The actions cell keeps its grid: the main button (if any) and the «⋯» cell, nothing else.
  const acts = html.slice(html.indexOf('<div class="acts">'));
  assert.ok(!acts.includes("Отменить расшифровку"), "the icon is in the status cell");
});

test("the first cancel asks; «Больше не спрашивать» is stored only with «Да, отменить»", () => {
  assert.equal(cancelSkipKey, "paratech.confirm.cancelTranscription.skip");
  const store = fakeStore();
  assert.equal(cancelDecision(store), "ask", "the first time");

  confirmCancel(store, false);
  assert.deepEqual(store.data, {}, "confirmed with the box unchecked stores nothing");
  assert.equal(cancelDecision(store), "ask");

  // «Не отменять» and Esc call nothing in the view: the page only closes the dialog, so nothing is stored.
  assert.equal(cancelAskSkipped(store), false);

  confirmCancel(store, true);
  assert.deepEqual(store.data, { [cancelSkipKey]: "1" });
  assert.equal(cancelDecision(store), "run", "later cancels run without the dialog");
  assert.equal(cancelDecision(fakeStore({ [cancelSkipKey]: "0" })), "ask", "only the stored mark skips");
});

test("a missing or failing localStorage never throws and always asks", () => {
  assert.equal(cancelDecision(null), "ask");
  assert.doesNotThrow(() => confirmCancel(null, true));
  const blocked = fakeStore({}, true);
  assert.equal(cancelDecision(blocked), "ask");
  assert.doesNotThrow(() => confirmCancel(blocked, true));
});

test("the dialog: texts, focus on the safe button, the checkbox, Esc = «Не отменять»", () => {
  assert.deepEqual(
    [cancelTexts.title, cancelTexts.confirm, cancelTexts.keep, cancelTexts.skip],
    ["Отменить расшифровку?", "Да, отменить", "Не отменять", "Больше не спрашивать"],
  );
  assert.equal(
    cancelBody("Матанализ"),
    "Расшифровка «Матанализ» остановится, готовая часть не сохранится. Запись останется — расшифровать её можно снова, с начала.",
  );
  assert.ok(cancelBody("").startsWith("Расшифровка остановится,"), "a row without a title");
  const noop = () => undefined;
  const dialog = (over = {}) =>
    renderToStaticMarkup(h(CancelTranscriptionDialog, { open: false, recordingTitle: "Матанализ", skip: false, onSkip: noop, onConfirm: noop, onCancel: noop, ...over }));
  const html = dialog();
  assert.ok(html.includes("<h2") && html.includes("Отменить расшифровку?"));
  assert.ok(html.includes("Расшифровка «Матанализ» остановится, готовая часть не сохранится."), "names the row and the cost");
  assert.match(html, /<label class="del-check"><input type="checkbox"\/><span>Больше не спрашивать<\/span><\/label>/);
  // The destructive confirm is the danger button, like the delete dialog.
  assert.match(html, /<button type="button" class="btn danger">Да, отменить<\/button><button type="button" class="btn">Не отменять<\/button>/);
  assert.ok(dialog({ skip: true }).includes('checked=""'));
  // The title is untrusted text: a text node, escaped, never markup.
  const hostile = dialog({ recordingTitle: "<img src=x onerror=alert(1)>" });
  assert.ok(!hostile.includes("<img") && hostile.includes("&lt;img src=x onerror=alert(1)&gt;"), hostile);

  // Wiring on the page: Esc and «Не отменять» only close; only the confirmation calls the core, once per kind.
  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /onCancel=\{\(\) => setCancelAsk\(null\)\}/);
  assert.match(page, /onConfirm=\{confirmCancelAsk\}/);
  assert.equal(page.split("api.cancelTranscription(").length - 1, 1, "one call site");
  assert.equal(page.split("api.retranscribe(").length - 1, 1, "one call site");
  assert.match(page, /function confirmCancelAsk\(\) \{[^]*?confirmCancel\(browserStore\(\), cancelSkip\);[^]*?runCancel\(id\);/);
  const src = readFileSync(new URL("../src/pages/RecordingRow.tsx", import.meta.url), "utf8");
  assert.ok(!src.includes("dangerouslySetInnerHTML"));
});

test("the dialog closes by itself when the row stops offering the cancel", () => {
  assert.equal(cancelAskOpen(null, [running()]), false);
  assert.equal(cancelAskOpen("r1", [running()]), true);
  assert.equal(cancelAskOpen("r1", [running({ canCancel: false })]), false);
  assert.equal(cancelAskOpen("r1", [cancelled()]), false);
  assert.equal(cancelAskOpen("r1", [running({ id: "other" })]), false, "the recording is gone");
  assert.equal(cancelAskOpen("r1", []), false);
});

test("a second click while a call is in flight starts no second call, per recording and kind", () => {
  const f = new Flights();
  assert.equal(f.begin(flightKey("cancel", "r1")), true);
  assert.equal(f.begin(flightKey("cancel", "r1")), false, "double click on cancel");
  assert.equal(f.begin(flightKey("cancel", "r2")), true, "another recording");
  assert.equal(f.begin(flightKey("retranscribe", "r1")), true, "another kind");
  assert.equal(f.begin(flightKey("retranscribe", "r1")), false, "double click on retranscribe");
  f.end(flightKey("cancel", "r1"));
  assert.equal(f.begin(flightKey("cancel", "r1")), true, "free again after the call");
  assert.notEqual(flightKey("cancel", "r1"), flightKey("retranscribe", "r1"));

  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /const flights = useRef\(new Flights\(\)\);/);
  assert.match(page, /if \(!flights\.current\.begin\(key\)\) \{\s*return;\s*\}/);
  assert.match(page, /finally \{\s*flights\.current\.end\(key\);/);
});

test("each cancel result maps to its note; the row refreshes either way", () => {
  assert.equal(cancelNote("cancelled"), null);
  assert.equal(cancelNote("not_running"), null);
  assert.deepEqual(cancelNote("ending"), { kind: "cancel", text: noteTexts.cancelEnding, openModels: false });
  assert.deepEqual(cancelNote("failed"), { kind: "cancel", text: "Не удалось отменить расшифровку. Можно попробовать ещё раз.", openModels: false });
  assert.equal(noteTexts.cancelEnding, "Расшифровка уже завершается. Если она не закончится, отмену можно повторить.");
  assert.ok(!/отмените/.test(noteTexts.cancelEnding), "impersonal voice");
  // The page refreshes after every call, thrown or not.
  const page = readFileSync(new URL("../src/pages/RecordingsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /const note = await call\(\);[^]*?finally \{[^]*?\n    if \(mounted\.current\) \{\s*await refresh\(\);/);
});

test("each retranscribe result maps to its note; no_model offers «Открыть модели речи»", () => {
  assert.equal(retranscribeNote("started"), null, "the row then shows «В очереди»");
  assert.equal(retranscribeNote("not_allowed"), null, "quiet: the state changed");
  assert.deepEqual(retranscribeNote("no_model"), { kind: "retranscribe", text: "Модель расшифровки не установлена.", openModels: true });
  assert.deepEqual(retranscribeNote("failed"), { kind: "retranscribe", text: "Не удалось запустить расшифровку. Можно попробовать ещё раз.", openModels: false });

  const opened = [];
  const note = retranscribeNote("no_model");
  const html = render(cancelled(), { note, onOpen: (p) => opened.push(p), onRetranscribe: () => undefined });
  assert.match(html, /<span class="small muted row-note" role="status">Модель расшифровки не установлена\.<\/span>/);
  assert.match(html, /<span class="st-fixes"><button[^>]*class="btn sm"[^>]*>.*Открыть модели речи<\/button><\/span>/);
  assert.ok(html.includes("Расшифровать снова"), "the main button stays for another try");
  const failed = render(cancelled(), { note: retranscribeNote("failed") });
  assert.ok(failed.includes("Не удалось запустить расшифровку.") && !failed.includes("Открыть модели речи<"));
  assert.equal(actionLabels.openModels, "Открыть модели речи", "the same words as the core's openModels action");
  assert.ok(!("openModels" in retranscribeTexts), "no second wording of the button");
  const page = readFileSync(new URL("../src/pages/RecordingRow.tsx", import.meta.url), "utf8");
  assert.match(page, /props\.onOpen\("models"\)/);
});

test("a note shows only while it still matches the row", () => {
  const cancelNoteValue = cancelNote("ending");
  const againNote = retranscribeNote("failed");
  assert.equal(noteVisible(cancelNoteValue, running()), true);
  assert.equal(noteVisible(cancelNoteValue, cancelled()), false, "the cancel went through");
  assert.equal(noteVisible(cancelNoteValue, row({ stage: "done" })), false);
  assert.equal(noteVisible(againNote, cancelled()), true);
  assert.equal(noteVisible(againNote, failedTranscription()), true);
  assert.equal(noteVisible(againNote, running()), false, "started: the row moved on");
  assert.equal(noteVisible(null, cancelled()), false);
  assert.equal(noteVisible(undefined, cancelled()), false);

  assert.ok(render(running(), { note: cancelNoteValue, onCancel: () => undefined }).includes(noteTexts.cancelEnding));
  assert.ok(!render(running({ canCancel: false }), { note: cancelNoteValue, onCancel: () => undefined }).includes(noteTexts.cancelEnding));
  assert.ok(!render(running(), { note: againNote, onCancel: () => undefined }).includes("Не удалось запустить"));
});

test("«Расшифровать снова» is busy while its call runs and rows without the action do not show it", () => {
  const busy = render(cancelled(), { retranscribing: true, onRetranscribe: () => undefined });
  assert.match(busy, /<button[^>]*disabled=""[^>]*>/);
  assert.ok(busy.includes("Запуск…"));
  assert.ok(!render(row()).includes("Расшифровать снова"));
  assert.ok(!render(running()).includes("Расшифровать снова"));
  assert.equal(chainStatusView(running(), 0).label, "Расшифровка");
});
