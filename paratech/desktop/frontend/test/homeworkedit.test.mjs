// «Удалить», «Вернуть» and «Изменить» (TASK-232): refusal texts, the single-call guard, the edit form prefill and
// its field errors. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  changeOutcome,
  changeProblemText,
  changeTexts,
  dueFields,
  editForm,
  editOutcome,
  editRequest,
  flightGuard,
  missingSubjectName,
  subjectMissingText,
  noSubjectLabel,
  removeLabel,
  validateEditForm,
} from "../.test-build/homeworkView.js";
import { EditAssignmentDialog, RemovedLine } from "../.test-build/pages/HomeworkDialogs.js";

const { renderToStaticMarkup } = server;

const local = (y, mo, d, hh, mm) => new Date(y, mo - 1, d, hh, mm).toISOString();

const item = (over = {}) => ({
  id: "ui-1",
  lessonId: "",
  title: "Задача",
  description: "Заметка",
  due: local(2026, 10, 28, 9, 0),
  userEdited: true,
  version: 3,
  updatedAt: "2026-10-27T10:00:00Z",
  subject: "Физика",
  subjectSource: "",
  subjectFromLesson: false,
  kind: "",
  teacher: "",
  teacherSource: "",
  lessonDate: "",
  correctedSubject: "",
  correctedTeacher: "",
  ...over,
});

const subj = (id, name) => ({ id, name, aliases: [], semester: "", semesterLabel: "", folders: [] });
const subjects = [subj("s1", "Математика"), subj("s2", "Физика")];

const changeBad = (problem) => ({ problem, assignment: null, stopped: false });
const editBad = (problem, field = "") => ({ problem, field, assignment: null });

test("delete and restore refusals have their own Russian text", () => {
  assert.equal(changeProblemText("in_use"), "Письмо преподавателю ещё отправляется — удалить можно после");
  assert.match(changeProblemText("changed"), /изменилось.*повторите/);
  assert.equal(changeProblemText("not_found"), "Задание не найдено");
  assert.match(changeProblemText("failed"), /Не удалось выполнить действие/);
  assert.equal(changeProblemText("deleted"), "Задание уже удалено");
  assert.equal(changeProblemText("something_new"), changeProblemText("failed"));
});

test("a change outcome carries the new assignment or the text; stale data is reloaded", () => {
  const done = changeOutcome({ problem: "", assignment: item({ version: 4 }), stopped: false });
  assert.equal(done.assignment.version, 4);
  assert.equal(done.text, "");
  for (const p of ["changed", "not_found"]) {
    const o = changeOutcome(changeBad(p));
    assert.equal(o.assignment, null);
    assert.equal(o.reload, true);
    assert.equal(o.text, changeProblemText(p));
  }
  for (const p of ["in_use", "failed"]) {
    assert.equal(changeOutcome(changeBad(p)).reload, false);
  }
});

test("a double click reaches the binding once; the key is free again afterwards", async () => {
  const g = flightGuard();
  let calls = 0;
  let release;
  const call = () => {
    calls++;
    return new Promise((resolve) => {
      release = () => resolve("done");
    });
  };
  const first = g.run("a1", call);
  const second = await g.run("a1", call);
  assert.equal(second, null);
  assert.equal(g.has("a1"), true);
  // Another assignment is not blocked.
  const other = g.run("a2", async () => "x");
  assert.equal(await other, "x");
  release();
  assert.equal(await first, "done");
  assert.equal(calls, 1);
  assert.equal(g.has("a1"), false);
  const again = g.run("a1", call);
  release();
  await again;
  assert.equal(calls, 2);
});

test("a thrown call frees the key", async () => {
  const g = flightGuard();
  await assert.rejects(g.run("a1", async () => Promise.reject(new Error("x"))));
  assert.equal(g.has("a1"), false);
});

test("«Задание удалено» comes with «Вернуть» as a status line", () => {
  const html = renderToStaticMarkup(h(RemovedLine, { busy: false, onRestore: () => undefined }));
  assert.match(html, /role="status"[^>]*>Задание удалено</);
  assert.match(html, />Вернуть</);
  assert.ok(!html.includes("disabled"));
  const busy = renderToStaticMarkup(h(RemovedLine, { busy: true, onRestore: () => undefined }));
  assert.match(busy, /disabled/);
});

test("the delete button names the assignment as text", () => {
  assert.equal(removeLabel("Задача"), "Удалить задание: Задача");
  assert.equal(changeTexts.remove, "Удалить");
});

test("due date and time are prefilled in local time; the end of the day is no time", () => {
  assert.deepEqual(dueFields(local(2026, 10, 28, 9, 5)), { date: "2026-10-28", time: "09:05" });
  assert.deepEqual(dueFields(local(2026, 10, 28, 23, 59)), { date: "2026-10-28", time: "" });
  assert.deepEqual(dueFields(""), { date: "", time: "" });
  assert.deepEqual(dueFields("not a date"), { date: "", time: "" });
});

test("the subject is prefilled by name among the library subjects", () => {
  assert.equal(editForm(item({ subject: "Физика" }), subjects).subjectId, "s2");
  assert.equal(editForm(item({ subject: "Химия" }), subjects).subjectId, "");
  assert.equal(editForm(item({ subject: "" }), subjects).subjectId, "");
  const f = editForm(item(), subjects);
  assert.equal(f.title, "Задача");
  assert.equal(f.note, "Заметка");
  assert.equal(f.date, "2026-10-28");
  assert.equal(f.time, "09:00");
});

test("a subject of the lesson is not turned into a choice; its empty option says so", () => {
  const fromLesson = item({ lessonId: "l1", subjectFromLesson: true, subject: "Физика" });
  assert.equal(editForm(fromLesson, subjects).subjectId, "");
  assert.equal(noSubjectLabel(fromLesson), "Предмет занятия");
  assert.equal(noSubjectLabel(item()), "Без предмета");
});

test("an assignment without a deadline can be edited; other fields are still checked", () => {
  const f = { title: "Задача", subjectId: "", date: "", time: "", note: "" };
  assert.deepEqual(validateEditForm(f), {});
  assert.ok(validateEditForm({ ...f, title: "" }).title);
  assert.ok(validateEditForm({ ...f, date: "2026-02-30" }).due);
});

test("a time without a date asks for the date at the date field", () => {
  const f = { title: "Задача", subjectId: "", date: "", time: "09:00", note: "" };
  assert.equal(validateEditForm(f).due, "Укажите дату.");
  assert.equal(validateEditForm({ ...f, time: "99:00" }).due, undefined);
  assert.ok(validateEditForm({ ...f, time: "99:00" }).time);
});

test("a stored subject with no library match is reported; the first of equal names is taken", () => {
  assert.equal(missingSubjectName(item({ subject: "Химия" }), subjects), "Химия");
  assert.equal(missingSubjectName(item({ subject: "Физика" }), subjects), "");
  assert.equal(missingSubjectName(item({ subject: "" }), subjects), "");
  assert.equal(missingSubjectName(item({ subject: "Химия", subjectFromLesson: true }), subjects), "");
  assert.equal(subjectMissingText("Химия"), "Предмет «Химия» не найден в библиотеке: выберите предмет или «Без предмета»");
  assert.equal(editForm(item(), [subj("a", "Физика"), subj("b", "Физика")]).subjectId, "a");
});

test("an unmatched subject blocks saving until the select is touched, and is shown as text", () => {
  const html = renderToStaticMarkup(
    h(EditAssignmentDialog, {
      api: { editAssignment: async () => editBad("failed") },
      library: { subjects: async () => subjects },
      initialSubjects: subjects,
      assignment: item({ subject: "Химия <b>1</b>" }),
      onSaved: () => undefined,
      onStale: async () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.ok(html.includes("Предмет «Химия &lt;b&gt;1&lt;/b&gt;» не найден в библиотеке"));
  assert.ok(!html.includes("<b>1</b>"));
  assert.match(html, />Выберите предмет</);
  assert.match(html, /<button[^>]*disabled=""[^>]*>Сохранить</);
});

test("the edit request carries the shown version and the trimmed title", () => {
  assert.deepEqual(
    editRequest(item(), { title: "  Новое ", subjectId: "s1", date: "2026-10-29", time: "09:00", note: "н" }),
    { id: "ui-1", version: 3, title: "Новое", note: "н", subjectId: "s1", date: "2026-10-29", time: "09:00" },
  );
});

test("edit outcomes: a field error sits at its field, the rest is a general text", () => {
  for (const [field, key] of [
    ["title", "title"],
    ["note", "note"],
    ["due", "due"],
    ["time", "time"],
  ]) {
    const o = editOutcome(editBad("invalid", field));
    assert.ok(o.fieldErrors[key]);
    assert.equal(o.general, "");
    assert.equal(o.saved, null);
  }
  assert.match(editOutcome(editBad("invalid", "note")).fieldErrors.note, /управляющие символы/);
  assert.equal(editOutcome(editBad("subject_not_found")).fieldErrors.subject, "Такого предмета нет в библиотеке");
  assert.ok(editOutcome(editBad("invalid")).general);
  assert.equal(editOutcome(editBad("deleted")).general, "Задание уже удалено");
  assert.equal(editOutcome(editBad("not_found")).general, "Задание не найдено");
  assert.equal(editOutcome(editBad("failed")).general, changeProblemText("failed"));
  for (const p of ["changed", "deleted", "not_found"]) {
    assert.equal(editOutcome(editBad(p)).reload, true);
  }
  assert.equal(editOutcome(editBad("failed")).reload, false);
  assert.match(editOutcome(editBad("changed")).general, /повторите/);
  assert.equal(editOutcome({ problem: "", field: "", assignment: item() }).saved.id, "ui-1");
});

test("the edit form shows the stored values and the lesson label", () => {
  const html = renderToStaticMarkup(
    h(EditAssignmentDialog, {
      api: { editAssignment: async () => editBad("failed") },
      library: { subjects: async () => subjects },
      initialSubjects: [...subjects, subj("s3", "Химия <b>1</b>")],
      assignment: item({ title: "Решить <i>задачу</i>", description: "Из <b>лекции</b>" }),
      onSaved: () => undefined,
      onStale: async () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.match(html, />Изменить задание</);
  assert.match(html, />Сохранить</);
  assert.match(html, />Отмена</);
  assert.match(html, /value="Решить &lt;i&gt;задачу&lt;\/i&gt;"/);
  assert.ok(html.includes("Из &lt;b&gt;лекции&lt;/b&gt;"));
  assert.ok(!html.includes("<b>лекции</b>"));
  assert.ok(html.includes("Химия &lt;b&gt;1&lt;/b&gt;"));
  assert.match(html, /<option value="s2" selected="">Физика<\/option>/);
  assert.match(html, /value="2026-10-28"/);
  assert.match(html, /value="09:00"/);
  assert.match(html, />Без предмета</);

  const lesson = renderToStaticMarkup(
    h(EditAssignmentDialog, {
      api: { editAssignment: async () => editBad("failed") },
      library: { subjects: async () => subjects },
      initialSubjects: subjects,
      assignment: item({ lessonId: "l1", subjectFromLesson: true }),
      onSaved: () => undefined,
      onStale: async () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.match(lesson, />Предмет занятия</);
  assert.ok(!lesson.includes(">Без предмета<"));
});

test("the dialog can be saved only after the subjects are known", () => {
  const html = renderToStaticMarkup(
    h(EditAssignmentDialog, {
      api: { editAssignment: async () => editBad("failed") },
      library: { subjects: async () => subjects },
      assignment: item(),
      onSaved: () => undefined,
      onStale: async () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.ok(html.includes(changeTexts.subjectsLoading));
  assert.match(html, /<button[^>]*disabled=""[^>]*>Сохранить</);
});

test("the pages wire the buttons to the bindings (a delete asks only while its work runs, TASK-338)", () => {
  const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
  assert.match(page, /api\.deleteAssignment\(a\.id, a\.version\)/);
  assert.match(page, /api\.deleteAssignment\(id, version\)/);
  assert.match(page, /api\.restoreAssignment\(/);
  assert.match(page, /changeTexts\.edit/);
  assert.match(page, /<EditAssignmentDialog/);
  assert.doesNotMatch(page, /confirm\(/);
});
