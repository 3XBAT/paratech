// TASK-289: «Удалить предмет» with its warning and «Вернуть» (ADR-0027). The Russian plurals and the warning text, the
// dialog and the restore line as models, the mapping of every problem to a text and a button, the binding parsers added
// for the removal, «Предмет с таким названием удалён» and the skipped rows of the schedule review. The page flows have
// source-pattern tests (there is no DOM harness).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { parseCreateFromSchedule, parseDiscard, parseRestore, removedNameOf } from "../.test-build/bindings/library.js";
import { plural } from "../.test-build/plural.js";
import {
  countsPhrase,
  dialogView,
  discardProblemView,
  filesText,
  idleRestore,
  knownCounts,
  notesText,
  removalTexts,
  removeBody,
  removedLineText,
  removedRowText,
  removeQuestion,
  removeWarning,
  restoredRowText,
  restoreLineView,
  restoreNeedsReload,
  restoreState,
  runDiscard,
  runRestore,
} from "../.test-build/subjectRemovalView.js";
import { initialState, reduce, splitSkipped, subjectsChanged } from "../.test-build/scheduleSubjectsView.js";
import { ReviewView } from "../.test-build/pages/ScheduleSubjectsReview.js";
import { RemoveSubjectDialog, RemovedNameLine, RestoreRow } from "../.test-build/pages/SubjectRemoval.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");

// ---- the words ----

test("«конспект» and «файл» agree with the number: 0, 1, 2, 4, 5, 11, 12, 21, 22, 25, 101, 111", () => {
  const ns = [0, 1, 2, 4, 5, 11, 12, 21, 22, 25, 101, 111];
  assert.deepEqual(ns.map(notesText), [
    "0 конспектов", "1 конспект", "2 конспекта", "4 конспекта", "5 конспектов", "11 конспектов", "12 конспектов",
    "21 конспект", "22 конспекта", "25 конспектов", "101 конспект", "111 конспектов",
  ]);
  assert.deepEqual(ns.map(filesText), [
    "0 файлов", "1 файл", "2 файла", "4 файла", "5 файлов", "11 файлов", "12 файлов",
    "21 файл", "22 файла", "25 файлов", "101 файл", "111 файлов",
  ]);
  // The one function behind both is the shared plural().
  assert.equal(notesText(3), `3 ${plural(3, "конспект", "конспекта", "конспектов")}`);
});

test("the phrase leaves a part that is 0 out", () => {
  assert.equal(countsPhrase({ notes: 2, files: 1 }), "2 конспекта и 1 файл");
  assert.equal(countsPhrase({ notes: 0, files: 3 }), "3 файла");
  assert.equal(countsPhrase({ notes: 5, files: 0 }), "5 конспектов");
  assert.equal(countsPhrase({ notes: 0, files: 0 }), "");
});

test("the warning: every combination of notes and files", () => {
  const w = "Вернуть можно в течение 3 дней.";
  assert.equal(removeQuestion("Физика"), "Удалить предмет «Физика»?");
  assert.equal(removeWarning("Физика", { notes: 2, files: 1 }), `Удалить предмет «Физика»? Вместе с ним удалятся 2 конспекта и 1 файл. ${w}`);
  assert.equal(removeBody({ notes: 1, files: 1 }), `Вместе с ним удалятся 1 конспект и 1 файл. ${w}`);
  assert.equal(removeBody({ notes: 5, files: 11 }), `Вместе с ним удалятся 5 конспектов и 11 файлов. ${w}`);
  // One part: the verb agrees with the number («удалится 1 конспект», «удалятся 2 файла»).
  assert.equal(removeBody({ notes: 1, files: 0 }), `Вместе с ним удалится 1 конспект. ${w}`);
  assert.equal(removeBody({ notes: 0, files: 21 }), `Вместе с ним удалится 21 файл. ${w}`);
  assert.equal(removeBody({ notes: 0, files: 2 }), `Вместе с ним удалятся 2 файла. ${w}`);
  assert.equal(removeBody({ notes: 11, files: 0 }), `Вместе с ним удалятся 11 конспектов. ${w}`);
  // Nothing goes with it: only the question and the term.
  assert.equal(removeWarning("Физика", { notes: 0, files: 0 }), `Удалить предмет «Физика»? ${w}`);
  // The page does not know the counts: no numbers, but both kinds of things are named.
  assert.equal(removeBody(null), `Вместе с ним удалятся его конспекты и файлы. ${w}`);
  // Names are plain text, passed through as they are.
  assert.equal(removeQuestion("<b>Х</b> & «Y»"), "Удалить предмет «<b>Х</b> & «Y»»?");
});

const note = (id, parts, removed = false) => ({ id, removed, parts: parts.map((materialId) => ({ materialId })) });
const file = (id, removed = false) => ({ id, removed });

test("the counts come from what the page has: active notes, and files that are no part of one", () => {
  const notes = [note("n1", ["m1", "m2"]), note("n2", ["m3"]), note("gone", ["m4"], true)];
  const files = [file("m1"), file("m2"), file("m3"), file("m4"), file("m5"), file("m6", true)];
  // m1-m3 belong to active notes, m6 is deleted; m4 belonged to a removed note, so it is an ordinary file again.
  assert.deepEqual(knownCounts(notes, files), { notes: 2, files: 2 });
  assert.deepEqual(knownCounts([], [file("a")]), { notes: 0, files: 1 });
  assert.equal(knownCounts(null, files), null, "the notes could not be read");
  assert.equal(knownCounts(notes, null), null, "the files are not known");
});

// ---- the dialog ----

const ask = (over = {}) => dialogView({ name: "Физика", counts: { notes: 2, files: 1 }, busy: false, problem: "", ...over });

test("the dialog asks with «Отмена» and the red «Удалить»", () => {
  const v = ask();
  assert.equal(v.title, "Удалить предмет «Физика»?");
  assert.equal(v.body, "Вместе с ним удалятся 2 конспекта и 1 файл. Вернуть можно в течение 3 дней.");
  assert.deepEqual(v.cancel, { label: "Отмена", disabled: false });
  assert.deepEqual(v.confirm, { label: "Удалить", disabled: false, action: "delete", danger: true });
  assert.equal(v.status, "");
  assert.equal(v.problem, "");
  assert.equal(v.busy, false);
});

test("while the notes are counted: a small inline status, «Удалить» waits", () => {
  const v = ask({ counts: "loading" });
  assert.equal(v.status, "Подсчёт конспектов и файлов…");
  assert.equal(v.confirm.disabled, true);
  assert.equal(v.cancel.disabled, false, "a way out stays");
  assert.equal(v.body, "Вернуть можно в течение 3 дней.");
  // Unknown counts (the notes did not load): the warning still names both and «Удалить» works.
  const u = ask({ counts: null });
  assert.equal(u.confirm.disabled, false);
  assert.match(u.body, /^Вместе с ним удалятся его конспекты и файлы\./);
});

test("while the call runs: «Удаление…», both buttons off, no second submit", () => {
  const v = ask({ busy: true });
  assert.equal(v.status, "Удаление…");
  assert.equal(v.confirm.label, "Удаление…");
  assert.equal(v.confirm.disabled, true);
  assert.equal(v.cancel.disabled, true);
  assert.equal(v.busy, true);
});

test("every refusal says why and has a button", () => {
  const changed = ask({ problem: "changed" });
  assert.equal(changed.problem, "Предмет изменился, откройте его ещё раз");
  assert.deepEqual(changed.confirm, { label: "Обновить", disabled: false, action: "refresh", danger: false });
  assert.equal(changed.cancel.label, "Закрыть");

  const gone = ask({ problem: "not_found" });
  assert.equal(gone.problem, "Предмет уже удалён");
  assert.deepEqual(gone.confirm, { label: "Закрыть", disabled: false, action: "close", danger: false });
  assert.equal(gone.cancel, null, "«Закрыть» is the only button");

  const failed = ask({ problem: "failed" });
  assert.equal(failed.problem, "Не удалось удалить предмет");
  assert.deepEqual(failed.confirm, { label: "Повторить", disabled: false, action: "delete", danger: false });
  assert.equal(failed.cancel.label, "Закрыть");

  // «Повторить» runs again: busy, no red button anywhere in a refusal.
  const again = ask({ problem: "failed", busy: true });
  assert.equal(again.confirm.disabled, true);
  for (const p of ["changed", "not_found", "failed"]) {
    assert.equal(ask({ problem: p }).confirm.danger, false, p);
    assert.ok(discardProblemView(p).text.length > 0);
  }
});

test("runDiscard: done with the reply, a problem as it is, a rejected call is «failed»", async () => {
  const done = { problem: "", notes: 2, files: 1, version: 7 };
  const calls = [];
  const api = { discardSubject: async (id, version) => (calls.push([id, version]), done) };
  assert.deepEqual(await runDiscard(api, "s1", 4), { kind: "done", discard: done });
  assert.deepEqual(calls, [["s1", 4]]);
  assert.deepEqual(await runDiscard({ discardSubject: async () => ({ problem: "changed", notes: 0, files: 0, version: 0 }) }, "s1", 4), {
    kind: "problem",
    problem: "changed",
  });
  assert.deepEqual(
    await runDiscard(
      {
        discardSubject: async () => {
          throw new Error("boom");
        },
      },
      "s1",
      4,
    ),
    { kind: "problem", problem: "failed" },
  );
});

// ---- «Вернуть» ----

test("the restore line: the place's own text, then the result of the call", () => {
  const idle = restoreLineView("Предмет «Физика» удалён", "", idleRestore, true);
  assert.deepEqual(idle, { text: "Предмет «Физика» удалён", tone: "info", button: "restore", busy: false, close: true });
  const busy = restoreLineView("Предмет «Физика» удалён", "", { phase: "busy" }, true);
  assert.equal(busy.busy, true);
  assert.equal(busy.button, "restore");
  assert.equal(busy.close, true, "the line stays closable");
  const done = restoreLineView("x", "Предмет возвращён", { phase: "done" }, false);
  assert.deepEqual(done, { text: "Предмет возвращён", tone: "ok", button: null, busy: false, close: false });
});

test("every restore refusal: plain words, a button where a repeat can help", () => {
  const line = (problem, canClose = true) => restoreLineView("x", "y", { phase: "problem", problem }, canClose);
  assert.deepEqual(line("expired"), { text: "Предмет удалён окончательно, вернуть уже нельзя", tone: "info", button: null, busy: false, close: true });
  assert.equal(line("changed").text, "Предмет изменился, вернуть его не удалось. Список предметов обновлён");
  assert.equal(line("changed").button, null);
  assert.equal(line("not_found").text, "Предмет не найден: возможно, он уже удалён окончательно");
  assert.deepEqual(line("failed"), { text: "Не удалось вернуть предмет", tone: "fail", button: "retry", busy: false, close: true });
  // A place without «Закрыть» (the create form, the schedule row) has its own way out.
  for (const p of ["expired", "changed", "not_found", "failed"]) {
    assert.equal(line(p, false).close, false, p);
    assert.doesNotMatch(line(p).text, /!/);
  }
  assert.deepEqual(["expired", "changed", "not_found"].map((p) => restoreNeedsReload({ phase: "problem", problem: p })), [true, true, true]);
  assert.equal(restoreNeedsReload({ phase: "problem", problem: "failed" }), false);
  assert.equal(restoreNeedsReload({ phase: "done" }), false);
});

test("restoreState and runRestore: the reply, a problem, and a rejected call", async () => {
  const subject = { id: "s1", name: "Физика" };
  assert.deepEqual(restoreState({ problem: "", subject }), { phase: "done" });
  assert.deepEqual(restoreState({ problem: "expired", subject: null }), { phase: "problem", problem: "expired" });
  const calls = [];
  const ok = await runRestore({ restoreSubject: async (id, v) => (calls.push([id, v]), { problem: "", subject }) }, "s1", 9);
  assert.deepEqual(ok, { state: { phase: "done" }, subject });
  assert.deepEqual(calls, [["s1", 9]]);
  assert.deepEqual(await runRestore({ restoreSubject: async () => ({ problem: "expired", subject: null }) }, "s1", 9), {
    state: { phase: "problem", problem: "expired" },
    subject: null,
  });
  const thrown = await runRestore(
    {
      restoreSubject: async () => {
        throw new Error("boom");
      },
    },
    "s1",
    9,
  );
  assert.deepEqual(thrown, { state: { phase: "problem", problem: "failed" }, subject: null });
});

test("the texts speak in the impersonal voice, with the glossary words and no exclamation marks", () => {
  for (const [k, v] of Object.entries(removalTexts)) {
    assert.doesNotMatch(v, /!/, k);
    assert.doesNotMatch(v, /заняти|Задачи|Попробовать снова/, k);
  }
  assert.equal(removalTexts.retry, "Повторить");
  assert.equal(removalTexts.restoring, "Возврат…", "the busy label of the assignments' «Вернуть»");
  assert.equal(removedLineText("Физика"), "Предмет «Физика» удалён");
  assert.equal(removalTexts.nameRemoved, "Предмет с таким названием удалён");
});

// ---- the binding ----

test("parseDiscard: the counts and the version of a delete, a problem, anything else is «failed»", () => {
  assert.deepEqual(parseDiscard({ problem: "", notes: 2, files: 1, version: 5 }), { problem: "", notes: 2, files: 1, version: 5 });
  for (const p of ["changed", "not_found", "failed"]) {
    assert.deepEqual(parseDiscard({ problem: p, notes: 9, files: 9, version: 9 }), { problem: p, notes: 0, files: 0, version: 0 });
  }
  const failed = { problem: "failed", notes: 0, files: 0, version: 0 };
  for (const bad of [null, undefined, "x", [], {}, { problem: "because" }, { problem: "" }, { problem: "", notes: -1, files: 0, version: 1 }, { problem: "", notes: 1.5, files: 0, version: 1 }, { problem: "", notes: 0, files: 0, version: 0 }, { problem: "", notes: "2", files: 0, version: 1 }]) {
    assert.deepEqual(parseDiscard(bad), failed, JSON.stringify(bad));
  }
});

test("parseRestore: the subject on success, a problem without one, anything else is «failed»", () => {
  const raw = { id: "s1", name: "Физика", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [], version: 3 };
  const ok = parseRestore({ problem: "", subject: raw });
  assert.equal(ok.problem, "");
  assert.equal(ok.subject.id, "s1");
  assert.equal(ok.subject.version, 3);
  for (const p of ["expired", "changed", "not_found", "failed"]) {
    assert.deepEqual(parseRestore({ problem: p, subject: raw }), { problem: p, subject: null });
  }
  for (const bad of [null, "x", [], {}, { problem: "later" }, { problem: "", subject: null }, { problem: "", subject: { id: 1 } }]) {
    assert.deepEqual(parseRestore(bad), { problem: "failed", subject: null }, JSON.stringify(bad));
  }
});

test("removedNameOf reads the removed subject from the rejection's cause; any other refusal is null", () => {
  const taken = { problem: "removed_name_taken", subjectId: "s7", version: 4 };
  assert.deepEqual(removedNameOf({ message: "Предмет с таким названием уже есть.", cause: taken }), { subjectId: "s7", version: 4 });
  for (const bad of [
    null,
    undefined,
    "text",
    new Error("Предмет с таким названием уже есть."),
    { cause: null },
    { cause: "removed_name_taken" },
    { cause: [] },
    { cause: { ...taken, problem: "name_taken" } },
    { cause: { ...taken, subjectId: "" } },
    { cause: { ...taken, subjectId: 7 } },
    { cause: { ...taken, version: 0 } },
    { cause: { ...taken, version: -1 } },
    { cause: { ...taken, version: 1.5 } },
    { cause: { problem: "removed_name_taken", subjectId: "s7" } },
  ]) {
    assert.equal(removedNameOf(bad), null, JSON.stringify(bad));
  }
});

test("a skipped schedule row keeps the restore pair only when both parts are valid and the reason is name_taken", () => {
  const reply = (skipped) => ({ created: [], aliased: [], skipped, problem: "" });
  const rows = parseCreateFromSchedule(
    reply([
      { key: "a", subjectId: "", reason: "name_taken", removedSubjectId: "s7", removedSubjectVersion: 4 },
      { key: "b", subjectId: "", reason: "name_taken" },
      { key: "c", subjectId: "", reason: "name_taken", removedSubjectId: "s8" },
      { key: "d", subjectId: "", reason: "name_taken", removedSubjectId: "", removedSubjectVersion: 2 },
      { key: "e", subjectId: "", reason: "name_taken", removedSubjectId: "s8", removedSubjectVersion: 0 },
      { key: "f", subjectId: "s1", reason: "subject_gone", removedSubjectId: "s9", removedSubjectVersion: 2 },
    ]),
  ).skipped;
  assert.deepEqual(rows[0], { key: "a", subjectId: "", reason: "name_taken", removedSubjectId: "s7", removedSubjectVersion: 4 });
  for (const r of rows.slice(1)) {
    assert.equal("removedSubjectId" in r, false, r.key);
    assert.equal("removedSubjectVersion" in r, false, r.key);
  }
});

// ---- the schedule review ----

const group = (key, name) => ({
  key, name, aliases: [], kinds: [], lessons: 1, recordings: 0, doubtful: false, members: [], action: "create", subjectId: "", subjectName: "",
});
const proposal = { token: "tok1", groups: [group("fizika", "Физика"), group("matan", "Матан")], omitted: 0, problem: "" };
const created = (skipped) => ({ type: "created", reply: { created: [], aliased: [], skipped, problem: "" } });
const removedSkip = (key) => ({ key, subjectId: "", reason: "name_taken", removedSubjectId: "s7", removedSubjectVersion: 4 });

test("a skipped row a removed subject blocks becomes a row with «Вернуть»; the others stay lines", () => {
  const s0 = reduce(initialState(proposal), { type: "saving" });
  const s = reduce(s0, created([removedSkip("fizika"), { key: "matan", subjectId: "", reason: "name_taken" }]));
  assert.deepEqual(s.notice.rows, [{ key: "fizika", name: "Физика", subjectId: "s7", version: 4 }]);
  assert.deepEqual(s.notice.lines, ["«Матан» не создан: такое название уже занято"]);
  assert.equal(s.notice.text, "Ничего не создано");
  assert.deepEqual(s.notice.steps, ["refresh"]);
  // Only rows: there is still a notice, so the student is told.
  const only = reduce(s0, created([removedSkip("fizika")]));
  assert.deepEqual(only.notice.lines, []);
  assert.equal(only.notice.rows.length, 1);
  // Without the pair nothing changes: no `rows` key at all (the earlier notices keep their exact shape).
  const plain = reduce(s0, created([{ key: "fizika", subjectId: "", reason: "name_taken" }]));
  assert.equal("rows" in plain.notice, false);
  assert.deepEqual(splitSkipped(s0, [removedSkip("fizika")]).plain, []);
});

test("the row texts: the name first when it is known, the same words as the form", () => {
  const row = { key: "k", name: "Физика", subjectId: "s7", version: 4 };
  assert.equal(removedRowText(row), "«Физика» · Предмет с таким названием удалён");
  assert.equal(restoredRowText(row), "«Физика» · Предмет возвращён");
  assert.equal(removedRowText({ ...row, name: "" }), "Предмет с таким названием удалён");
  assert.equal(restoredRowText({ ...row, name: "" }), "Предмет возвращён");
});

test("the review shows «Вернуть» on such a row; without a restore call the row is plain text", () => {
  const s0 = reduce(initialState(proposal), { type: "saving" });
  const s = reduce(s0, created([removedSkip("fizika")]));
  const on = { check: noop, rename: noop, member: noop, renameMember: noop, create: noop, undo: noop, step: noop, close: noop };
  const withRestore = html(h(ReviewView, { state: s, subjects: [], on: { ...on, restore: async () => ({ state: { phase: "done" }, subject: null }) } }));
  assert.match(withRestore, /«Физика» · Предмет с таким названием удалён/);
  assert.match(withRestore, />Вернуть</);
  const without = html(h(ReviewView, { state: s, subjects: [], on }));
  assert.match(without, /«Физика» · Предмет с таким названием удалён/);
  assert.doesNotMatch(without, />Вернуть</);
  // The restore changes the subjects, so the page reads them again: the controller calls onChanged when it is done.
  const review = src("pages/ScheduleSubjectsReview.tsx");
  assert.match(review, /call\.state\.phase === "done"[\s\S]{0,80}onChanged\(\)/);
  assert.equal(subjectsChanged({ type: "created", reply: { created: [], aliased: [], skipped: [removedSkip("fizika")], problem: "" } }), false);
});

// ---- the components ----

test("the dialog is a labelled modal dialog with the warning, a status line and the alert for a refusal", () => {
  const view = ask();
  const out = html(h(RemoveSubjectDialog, { view, returnTo: null, onAction: noop, onDismiss: noop }));
  assert.match(out, /role="dialog"/);
  assert.match(out, /aria-modal="true"/);
  assert.match(out, /aria-labelledby="[^"]+"/);
  assert.match(out, /aria-describedby="[^"]+"/);
  assert.match(out, /Удалить предмет «Физика»\?/);
  assert.match(out, /Вместе с ним удалятся 2 конспекта и 1 файл\. Вернуть можно в течение 3 дней\./);
  assert.match(out, /class="btn danger"[^>]*>Удалить</);
  assert.match(out, />Отмена</);
  assert.match(out, /role="status"/);
  assert.doesNotMatch(out, /role="alert"/);

  const refused = html(h(RemoveSubjectDialog, { view: ask({ problem: "failed" }), returnTo: null, onAction: noop, onDismiss: noop }));
  assert.match(refused, /role="alert"[^>]*>.*Не удалось удалить предмет/);
  assert.match(refused, />Повторить</);
  assert.doesNotMatch(refused, /class="btn danger"/);

  const busy = html(h(RemoveSubjectDialog, { view: ask({ busy: true }), returnTo: null, onAction: noop, onDismiss: noop }));
  assert.match(busy, /aria-busy="true"/);
  assert.equal((busy.match(/disabled=""/g) ?? []).length, 2, "both buttons are off");
});

test("a subject name is a text node, never markup", () => {
  const name = '<img src=x onerror="1">';
  const out = html(h(RemoveSubjectDialog, { view: ask({ name }), returnTo: null, onAction: noop, onDismiss: noop }));
  assert.doesNotMatch(out, /<img/);
  assert.match(out, /&lt;img/);
});

test("the restore row: text and «Вернуть», the form's line, no markup from the name", () => {
  const row = html(h(RestoreRow, { idleText: removedLineText("<i>Физика</i>"), doneText: "", restore: async () => null, onSettled: noop, onClose: noop }));
  assert.match(row, /Предмет «&lt;i&gt;Физика&lt;\/i&gt;» удалён/);
  assert.match(row, />Вернуть</);
  assert.match(row, />Закрыть</);
  assert.match(row, /role="status"/);
  const line = html(h(RemovedNameLine, { removed: { subjectId: "s7", version: 4 }, restore: async () => null, onSettled: noop }));
  assert.match(line, /Предмет с таким названием удалён/);
  assert.match(line, />Вернуть</);
  assert.doesNotMatch(line, />Закрыть</, "the form has its own «Отмена»");
});

// ---- the sources ----

test("source: the dialog is a native modal (focus trap, inert page), Esc is «Отмена», nothing happens while busy", () => {
  const s = src("pages/SubjectRemoval.tsx");
  assert.match(s, /<dialog[\s\S]{0,200}role="dialog"[\s\S]{0,40}aria-modal="true"/);
  assert.match(s, /showModal\(\)/);
  assert.match(s, /onCancel=\{\(e\) => \{\s*e\.preventDefault\(\);\s*if \(!busy\)/);
  assert.match(s, /\(cancel\.current \?\? way\.current\)\?\.focus\(\)/, "the focus starts on «Отмена», never on the red button");
  assert.match(s, /back\.focus\(\)/, "the focus returns to the opener");
  assert.match(s, /running\.current/, "a double click starts one call");
  assert.doesNotMatch(s, /dangerouslySetInnerHTML/);
});

test("source: the page asks before it deletes and sends the shown version; the line is state of the page", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /<RemoveSubjectButton disabled=\{busy\} onClick=\{\(opener\) => removal\.open\(subject, opener\)\} \/>/);
  assert.match(page, /<RemoveSubjectDialog/);
  assert.match(page, /removal\.removed\.map/);
  assert.match(page, /removal\.settled\(r, call\)/);
  assert.match(page, /\{removalBlock\}/);
  // The subject leaves for the home subject through a click-like selection, not a redirect effect.
  assert.match(page, /props\.onSelect\(homeSubjectId\(rest, ""\)\)/);
  // The button is quiet: a ghost button with the words of the glossary, red only in the dialog.
  const parts = src("pages/SubjectRemoval.tsx");
  assert.match(parts, /<Button variant="ghost"[\s\S]{0,120}\{removalTexts\.action\}/);
  assert.equal(removalTexts.action, "Удалить предмет");
  const hook = src("pages/useSubjectRemoval.ts");
  assert.match(hook, /runDiscard\(latest\.current\.api, a\.id, a\.version\)/);
  assert.match(hook, /subject\.version/);
  assert.match(hook, /if \(a === null \|\| running\.current\)/);
  assert.match(hook, /out\.discard\.version/, "«Вернуть» uses the version the delete answered with");
  // Nothing is stored: leaving the page drops the lines.
  assert.doesNotMatch(hook, /localStorage|sessionStorage|browserStore/);
});

test("source: the create form, the inbox panel and the schedule review ask removedNameOf / restore the same way", () => {
  const page = src("pages/SubjectsPage.tsx");
  assert.match(page, /removedNameOf\(err\)/);
  assert.match(page, /<RemovedNameLine/);
  const mat = src("pages/useMaterialActions.ts");
  assert.match(mat, /removedNameOf\(err\)/);
  assert.match(mat, /restoreSubject: async \(removed\)/);
  const rows = src("pages/FileRows.tsx");
  assert.match(rows, /onRestoreRemoved=\{a\.restoreSubject\}/);
  assert.match(rows, /onRestoreRemoved=\{placing\.restoreSubject\}/);
  const review = src("pages/ScheduleSubjectsReview.tsx");
  assert.match(review, /restore: \(subjectId, version\) => runRestore\(library, subjectId, version\)/);
});

test("source: the tree rows carry the id the focus finds after «Вернуть»", () => {
  assert.match(src("pages/SubjectsParts.tsx"), /data-subject-id=\{s\.id\}/);
  assert.match(src("subjectRemovalView.ts"), /subjectRowAttr = "data-subject-id"/);
});
