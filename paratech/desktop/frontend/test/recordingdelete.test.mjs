// Deleting recordings (TASK-095): the binding, the confirmation, the choice of found homework, the requests and the
// result. Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorMessages, maxBatch, methods, parsePreview, parseResult, recordingDeleteApi } from "../.test-build/bindings/recordingDelete.js";
import {
  deleteConfirmation,
  deleteErrorText,
  deleteOutcome,
  deleteRequests,
  deleteTexts,
  initialDeleteChoice,
  pruneSelection,
  rowDeleteOffer,
  selectedText,
  withDraft,
  withSelected,
} from "../.test-build/recordingDeleteView.js";
import { DeleteBody } from "../.test-build/pages/RecordingDeleteDialog.js";
import { RecordingRow } from "../.test-build/pages/RecordingRow.js";

const { renderToStaticMarkup } = server;

const rec = (over = {}) => ({
  id: "r1", title: "Матанализ", startedAt: "2026-10-18T17:19:35Z", deletable: true, refusal: "", refusalMessage: "",
  sound: true, transcript: true, summary: true, lectureText: false, jobs: 0,
  deletes: ["Звук записи", "Расшифровка", "Краткое содержание"], assignments: [], ...over,
});
const asg = (id, cls, title = `Задание ${id}`) => ({ id, title, class: cls });
const preview = (recordings, missing = []) => ({ recordings, missing });

test("the binding calls the bound names and checks the replies", async () => {
  const calls = [];
  const replies = {
    [methods.preview]: preview([rec({ assignments: [asg("a1", "plain")] })], ["r9"]),
    [methods.delete]: { removed: 1, deleted: 1, pending: 0, message: "Удалено 1", failures: [], keptAssignments: [] },
  };
  const api = recordingDeleteApi(async (m, ...args) => {
    calls.push([m, ...args]);
    return replies[m];
  });
  const p = await api.preview(["r1", "r9"]);
  assert.equal(p.recordings[0].assignments[0].class, "plain");
  assert.deepEqual(p.missing, ["r9"]);
  const res = await api.delete([{ recordingId: "r1", assignmentIds: ["a1"] }]);
  assert.equal(res.message, "Удалено 1");
  assert.deepEqual(calls, [
    [methods.preview, ["r1", "r9"]],
    [methods.delete, [{ recordingId: "r1", assignmentIds: ["a1"] }]],
  ]);
  assert.equal(maxBatch, 100);
});

test("a malformed reply is an error, not a default", () => {
  assert.throws(() => parsePreview({ recordings: [rec({ refusal: "busy" })], missing: [] }));
  assert.throws(() => parsePreview({ recordings: [rec({ assignments: [asg("a1", "sent")] })], missing: [] }));
  assert.throws(() => parsePreview({ recordings: [], missing: null }));
  assert.throws(() => parseResult({ removed: -1, deleted: 0, pending: 0, message: "", failures: [], keptAssignments: [] }));
  assert.throws(() => parseResult({ removed: 0, deleted: 0, pending: 0, message: "", failures: [{}], keptAssignments: [] }));
});

test("the confirmation names the recording, lists what goes and says it is irreversible", () => {
  const m = deleteConfirmation(preview([rec()]), 1);
  assert.equal(m.title, "Удалить запись?");
  assert.equal(m.recordings[0].title, "Матанализ");
  assert.deepEqual(m.recordings[0].deletes, ["Звук записи", "Расшифровка", "Краткое содержание"]);
  assert.ok(m.canDelete);
  const html = renderToStaticMarkup(h(DeleteBody, { model: m, choice: initialDeleteChoice, disabled: false, onChoice() {} }));
  assert.ok(html.includes("без возможности восстановления"));
  assert.ok(html.includes("Краткое содержание"));
});

test("an ad-hoc recording is titled by its start, like its row", () => {
  const m = deleteConfirmation(preview([rec({ title: "" })]), 1);
  assert.match(m.recordings[0].title, /^Запись \d/);
});

test("several recordings get one confirmation; the title counts the deletable ones", () => {
  assert.equal(deleteConfirmation(null, 3).title, "Удалить 3 записи?");
  assert.equal(deleteConfirmation(null, 5).title, "Удалить 5 записей?");
  const m = deleteConfirmation(
    preview([rec(), rec({ id: "r2" }), rec({ id: "r3", deletable: false, refusal: "in_progress", refusalMessage: errorMessages.inProgress })], ["r4"]),
    4,
  );
  assert.equal(m.title, "Удалить 2 записи?");
  assert.equal(m.recordings[2].refusal, "Запись ещё идёт: сначала остановите её.");
  assert.equal(m.missing, "Уже удалены: 1.");
});

test("a recording in progress cannot be deleted: the confirmation says why and offers nothing", () => {
  const m = deleteConfirmation(
    preview([rec({ deletable: false, refusal: "in_progress", refusalMessage: errorMessages.inProgress, assignments: [asg("a1", "plain")] })]),
    1,
  );
  assert.equal(m.canDelete, false);
  assert.equal(m.plain.length, 0, "no homework of a recording that stays");
  assert.deepEqual(deleteRequests(preview([rec({ deletable: false })]), initialDeleteChoice), []);
  const html = renderToStaticMarkup(h(DeleteBody, { model: m, choice: initialDeleteChoice, disabled: false, onChoice() {} }));
  assert.ok(html.includes("Запись ещё идёт: сначала остановите её."));
  assert.ok(!html.includes("без возможности восстановления"));
});

test("found homework: plain ones checked by default, drafts apart and unchecked, emails stay", () => {
  const p = preview([rec({ assignments: [asg("a1", "plain"), asg("a2", "plain"), asg("a3", "draft"), asg("a4", "emails")] })]);
  const m = deleteConfirmation(p, 1);
  assert.equal(m.plainLabel, "Удалить и найденные задания (2)");
  assert.deepEqual(m.drafts.map((a) => a.id), ["a3"]);
  assert.deepEqual(m.kept.map((a) => a.id), ["a4"]);

  assert.equal(initialDeleteChoice.plain, true);
  assert.deepEqual(deleteRequests(p, initialDeleteChoice), [{ recordingId: "r1", assignmentIds: ["a1", "a2"] }]);
  const withA3 = withDraft(initialDeleteChoice, "a3", true);
  assert.deepEqual(deleteRequests(p, withA3), [{ recordingId: "r1", assignmentIds: ["a1", "a2", "a3"] }]);
  assert.deepEqual(deleteRequests(p, { ...withA3, plain: false }), [{ recordingId: "r1", assignmentIds: ["a3"] }]);
  assert.deepEqual(deleteRequests(p, withDraft(withA3, "a3", false)), [{ recordingId: "r1", assignmentIds: ["a1", "a2"] }]);
  // An id with emails is never sent, even if the choice somehow holds it.
  assert.ok(!deleteRequests(p, withDraft(initialDeleteChoice, "a4", true))[0].assignmentIds.includes("a4"));

  const html = renderToStaticMarkup(h(DeleteBody, { model: m, choice: initialDeleteChoice, disabled: false, onChoice() {} }));
  assert.ok(html.includes("Удалить и найденные задания (2)"));
  assert.ok(html.includes("останутся в «Домашних заданиях»"));
  // The plain box is on, the draft box is off.
  const boxes = html.match(/<input[^>]*>/g);
  assert.equal(boxes.length, 2);
  assert.ok(boxes[0].includes("checked"));
  assert.ok(!boxes[1].includes("checked"));
});

test("the requests of several recordings carry each one's homework", () => {
  const p = preview([rec({ assignments: [asg("a1", "plain")] }), rec({ id: "r2", assignments: [asg("b1", "draft")] })]);
  assert.deepEqual(deleteRequests(p, withDraft(initialDeleteChoice, "b1", true)), [
    { recordingId: "r1", assignmentIds: ["a1"] },
    { recordingId: "r2", assignmentIds: ["b1"] },
  ]);
  const m = deleteConfirmation(p, 2);
  assert.equal(m.drafts[0].recording, "Матанализ");
});

test("titles are plain text: model output is never markup", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const p = preview([rec({ title: evil, assignments: [asg("a1", "plain", evil), asg("a2", "draft", evil), asg("a3", "emails", evil)] })]);
  const html = renderToStaticMarkup(
    h(DeleteBody, { model: deleteConfirmation(p, 1), choice: initialDeleteChoice, disabled: false, onChoice() {} }),
  );
  assert.ok(!html.includes("<img"));
  assert.ok(!html.includes("<a "));
  assert.ok(html.includes("&lt;img"));
});

test("the result: «Удалено N», what stays and the recordings that failed", () => {
  const p = preview([rec(), rec({ id: "r2", title: "Физика" })]);
  const ok = { removed: 2, deleted: 2, pending: 0, message: "Удалено 2", failures: [], keptAssignments: [] };
  assert.deepEqual(deleteOutcome(ok, p), { kind: "ok", lines: ["Удалено 2"] });
  const pending = { ...ok, deleted: 1, pending: 1, message: "Удалено 2. Часть файлов удалится при следующем запуске." };
  assert.equal(deleteOutcome(pending, p).kind, "wait");
  const part = { ...ok, removed: 1, deleted: 1, message: "Удалено 1", failures: [{ recordingId: "r2", message: errorMessages.unavailable }], keptAssignments: ["a9"] };
  assert.deepEqual(deleteOutcome(part, p), {
    kind: "wait",
    lines: ["Удалено 1", "Найденные задания остались в «Домашних заданиях»: 1.", "Физика: Не удалось удалить запись. Попробуйте ещё раз."],
  });
  const none = { ...ok, removed: 0, deleted: 0, message: "Удалено 0", failures: [{ recordingId: "r1", message: errorMessages.inProgress }] };
  assert.deepEqual(deleteOutcome(none, p), { kind: "fail", lines: ["Матанализ: Запись ещё идёт: сначала остановите её."] });
});

test("errors: the core's texts as they are, anything else generic", () => {
  for (const m of Object.values(errorMessages)) {
    assert.equal(deleteErrorText(new Error(m)), m);
  }
  assert.equal(deleteErrorText(new Error("sqlite: database is locked")), errorMessages.unavailable);
  assert.equal(deleteErrorText(errorMessages.tooMany), errorMessages.tooMany);
});

const row = (over = {}) => ({
  id: "r1", lessonId: "l1", title: "Матанализ", subject: "", kind: "", teacher: "",
  status: "COMPLETED", startedAt: "2026-10-18T17:19:35Z", stoppedAt: "",
  durationSeconds: 0, hasDuration: false, problem: "", stoppable: false, stage: "done", step: "", progressPercent: 0,
  partsDone: 0, parts: 0, queueReason: "", errorCode: "", jobError: "", action: "", actionJobId: "", retryWork: "",
  canRedoSummary: false, lectureTextReady: false, remainingSeconds: 0, hasRemaining: false, advice: null, ...over,
});

test("a row in progress offers no deletion; an interrupted one does", () => {
  assert.deepEqual(rowDeleteOffer(row()), { disabled: false, hint: "" });
  assert.deepEqual(rowDeleteOffer(row({ stage: "recording", status: "RECORDING" })), { disabled: true, hint: errorMessages.inProgress });
  assert.equal(rowDeleteOffer(row({ stage: "saving", status: "FINALIZING" })).disabled, true);
  assert.equal(rowDeleteOffer(row({ stage: "saving", status: "RECOVERY_REQUIRED" })).disabled, false);
});

test("the selection drops rows that are gone or in progress", () => {
  let sel = withSelected(new Set(), "r1", true);
  sel = withSelected(sel, "r2", true);
  sel = withSelected(sel, "r3", true);
  const pruned = pruneSelection(sel, [row(), row({ id: "r2", stage: "recording", status: "RECORDING" })]);
  assert.deepEqual([...pruned], ["r1"]);
  assert.equal(pruneSelection(pruned, [row()]), pruned, "unchanged selection is the same set");
  assert.deepEqual([...withSelected(pruned, "r1", false)], []);
  assert.equal(selectedText(3), "Выбрано 3");
});

function renderRow(item, props = {}) {
  return renderToStaticMarkup(
    h(RecordingRow, {
      item, now: Date.parse("2026-10-18T18:00:00Z"), stopping: false, starting: false,
      onStop() {}, onRunNow() {}, onOpen() {}, ...props,
    }),
  );
}

test("the row has a «⋯» menu and a checkbox only when the page deletes", () => {
  const plain = renderRow(row());
  assert.ok(!plain.includes("⋯"));
  assert.ok(!plain.includes("checkbox"));
  const html = renderRow(row(), { onDelete() {}, onSelect() {}, selected: true });
  assert.ok(html.includes('aria-label="Ещё действия: Матанализ"'));
  assert.ok(html.includes('aria-label="Выбрать: Матанализ"'));
  assert.match(html, /<input[^>]*type="checkbox"[^>]*checked/);
  const running = renderRow(row({ stage: "recording", status: "RECORDING", stoppable: true }), { onDelete() {}, onSelect() {} });
  assert.match(running, /<input[^>]*type="checkbox"[^>]*disabled/);
  assert.ok(running.includes(`title="${errorMessages.inProgress}"`));
});

test("texts", () => {
  assert.equal(deleteTexts.menu, "Удалить");
  assert.equal(deleteTexts.selectedDelete, "Удалить выбранные");
  assert.ok(deleteTexts.irreversible.includes("без возможности восстановления"));
});
