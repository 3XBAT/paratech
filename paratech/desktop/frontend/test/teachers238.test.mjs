// TASK-238: the block «Преподаватели» on the page of a subject: rows, the small form, «Как у лектора / семинариста»,
// the flow of save and clear (versions, refusals, re-reads, double click), and the one-shot request «Указать почту».
// Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  TeacherFlow,
  canCopy,
  copyInto,
  copySources,
  formFor,
  idleTeacher,
  keepTeacherFocus,
  noTeachers,
  rowLabels,
  teacherFocusStep,
  teacherRows,
  teacherTexts,
  triggerId,
} from "../.test-build/teachersView.js";
import { TeachersBlock } from "../.test-build/pages/TeachersBlock.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const evil = "<img src=x onerror=alert(1)>";

const lecturer = { name: "Иванов И. И.", email: "ivanov@example.test" };
const seminar = { name: "Петров П. П.", email: "petrov@example.test" };
const none = { lecture: null, practice: null, lab: null };

const subject = (over = {}) => ({
  id: "s1", name: "Физика", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [], version: 3,
  teachers: { ...none }, ...over,
});
const withTeachers = (teachers, over = {}) => subject({ teachers: { ...none, ...teachers }, ...over });

const ok = (s) => ({ problem: "", field: "", subject: s });
const refused = (problem, field = "") => ({ problem, field, subject: null });

/** A page in miniature: the list of one subject, the fake calls and the states the flow reports. */
function setup(initial = subject(), results = []) {
  const page = { subject: initial, calls: [], replaced: 0, rereads: 0, rereadOk: true, states: [] };
  const queue = [...results];
  const reply = async (kind, args) => {
    page.calls.push([kind, ...args]);
    return queue.length > 0 ? queue.shift() : ok(page.subject);
  };
  const api = {
    setSubjectTeacher: (...a) => reply("set", a),
    clearSubjectTeacher: (...a) => reply("clear", a),
  };
  const host = {
    subject: () => page.subject,
    replace: (s) => {
      page.subject = s;
      page.replaced++;
    },
    reread: async () => {
      page.rereads++;
      return page.rereadOk;
    },
  };
  page.flow = new TeacherFlow(api, host, (s) => page.states.push(s));
  return page;
}

const view = (teachers, state = idleTeacher) =>
  html(h(TeachersBlock, { teachers, state, on: { open: noop, type: noop, copy: noop, save: noop, cancel: noop, clear: noop } }));

// ---- Rows ----

test("three rows in a fixed order; an empty one says «Не указан» and offers «Указать»", () => {
  const rows = teacherRows(none);
  assert.deepEqual(rows.map((r) => r.role), ["lecture", "practice", "lab"]);
  assert.deepEqual(rows.map((r) => r.label), ["Лектор", "Семинары и практика", "Лабораторные"]);
  assert.ok(rows.every((r) => !r.filled && r.action === "Указать"));
  const out = view(none);
  assert.equal(out.split("Не указан").length - 1, 3);
  assert.equal((out.match(/>Указать</g) ?? []).length, 3);
  assert.doesNotMatch(out, /Изменить/);
  assert.match(out, new RegExp(teacherTexts.hint), "an empty block says what the addresses are for");
  assert.match(out, /<h2[^>]*>Преподаватели<\/h2>/);
});

test("a filled row shows the name and the address and offers «Изменить»; the hint goes away", () => {
  const teachers = { lecture: lecturer, practice: null, lab: null };
  const rows = teacherRows(teachers);
  assert.deepEqual([rows[0].filled, rows[0].name, rows[0].email, rows[0].action], [true, lecturer.name, lecturer.email, "Изменить"]);
  assert.equal(rows[0].actionLabel, "Изменить: лектор", "the accessible name says which row");
  assert.equal(rows[1].actionLabel, "Указать: семинары и практика");
  assert.equal(noTeachers(teachers), false);
  const out = view(teachers);
  assert.ok(out.includes(lecturer.name) && out.includes(lecturer.email));
  assert.equal(out.split("Не указан").length - 1, 2);
  assert.doesNotMatch(out, new RegExp(teacherTexts.hint));
  assert.ok(out.includes(`id="${triggerId("lecture")}"`), "the button the focus returns to");
});

test("names and addresses stay text: bdi text nodes, no link, no markup", () => {
  const out = view({ lecture: { name: evil, email: `${evil}@example.test` }, practice: null, lab: null });
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img"));
  assert.doesNotMatch(out, /<a[ >]|href=|mailto:|dangerouslySetInnerHTML/);
  assert.match(out, /<bdi>&lt;img/);
  for (const file of ["pages/TeachersBlock.tsx", "pages/RecipientParts.tsx", "teachersView.ts", "recipientView.ts"]) {
    const src = readFileSync(new URL(`../src/${file}`, import.meta.url), "utf8").replace(/\/\/.*$/gm, "");
    assert.doesNotMatch(src, /dangerouslySetInnerHTML|innerHTML|mailto:|href=/, file);
  }
});

// ---- Form: open, type, cancel ----

test("«Указать» opens an empty form, «Изменить» a filled one; one form at a time; «Отмена» closes it", () => {
  const page = setup(withTeachers({ lecture: lecturer }));
  page.flow.open("practice");
  assert.deepEqual(page.flow.state.form, { role: "practice", name: "", email: "" });
  page.flow.open("lecture");
  assert.deepEqual(page.flow.state.form, { role: "lecture", name: lecturer.name, email: lecturer.email }, "the other form is replaced");
  page.flow.type({ name: "Другой" });
  assert.equal(page.flow.state.form.name, "Другой");
  page.flow.cancel();
  assert.equal(page.flow.state.form, null);
  assert.equal(page.flow.state.focusRole, "lecture", "the focus goes back to the button that opened it");
  page.flow.open("lecture");
  assert.equal(page.flow.state.focusRole, "", "an open form owns the focus");
  assert.deepEqual(page.calls, []);
});

test("the form: labelled fields, «Сохранить» and «Отмена»; «Убрать» only for a filled row", () => {
  const teachers = { ...none, lecture: lecturer };
  const open = (role) => view(teachers, { ...idleTeacher, form: formFor(role, teachers) });
  const filled = open("lecture");
  assert.match(filled, /<form[^>]*aria-label="Лектор"/);
  assert.match(filled, /<label[^>]*>Имя<\/label>/);
  assert.match(filled, /<label[^>]*>Почта<\/label>/);
  assert.ok(filled.includes(`value="${lecturer.name}"`) && filled.includes(`value="${lecturer.email}"`));
  assert.match(filled, /type="submit"[^>]*><span class="btn-labels"><span>Сохранить</);
  assert.match(filled, />Отмена</);
  assert.match(filled, />Убрать</);
  const empty = open("practice");
  assert.match(empty, />Сохранить</);
  assert.doesNotMatch(empty, />Убрать</, "nothing to remove from an empty row");
  assert.doesNotMatch(empty, /Как у/, "only the labs copy");
  assert.equal((empty.match(/<form/g) ?? []).length, 1);
  const src = readFileSync(new URL("../src/pages/TeachersBlock.tsx", import.meta.url), "utf8");
  assert.match(src, /autoFocus/, "the focus goes into the opened form");
  assert.match(src, /e\.key === "Escape"/, "Escape cancels");
  assert.match(src, /onSubmit=\{submit\}/, "Enter saves");
  assert.match(src, /document\.getElementById\(triggerId\(back\)\)\?\.focus\(\)/, "the focus returns to the trigger");
});

test("while a call runs the form is off and says so", () => {
  const teachers = { ...none, practice: seminar };
  const out = view(teachers, { ...idleTeacher, form: formFor("practice", teachers), busy: "save" });
  assert.match(out, /Сохранение…/);
  assert.equal((out.match(/<button[^>]*disabled/g) ?? []).length >= 3, true, "save, cancel and remove are off");
  assert.match(out, /<input[^>]*disabled/);
});

// ---- Labs: «Как у лектора» / «Как у семинариста» ----

test("the lab form copies the name and the address of that row once, into the fields", () => {
  const teachers = { lecture: lecturer, practice: seminar, lab: null };
  assert.deepEqual(copySources("lab"), ["lecture", "practice"]);
  assert.deepEqual(copySources("lecture").concat(copySources("practice")), []);
  assert.equal(canCopy("lecture", teachers), true);
  const form = formFor("lab", teachers);
  assert.deepEqual(copyInto(form, "practice", teachers), { role: "lab", name: seminar.name, email: seminar.email });
  assert.deepEqual(copyInto(form, "lecture", { ...teachers, lecture: null }), form, "an empty source copies nothing");

  const page = setup(withTeachers(teachers));
  page.flow.open("lab");
  page.flow.copy("practice");
  assert.deepEqual(page.flow.state.form, { role: "lab", name: seminar.name, email: seminar.email });
  assert.deepEqual(page.calls, [], "copying saves nothing");
  // A copy, not a link: the seminar teacher changing later does not touch the form.
  page.subject = withTeachers({ ...teachers, practice: { name: "Другой", email: "other@example.test" } });
  assert.equal(page.flow.state.form.name, seminar.name);
  page.flow.copy("lecture");
  assert.equal(page.flow.state.form.name, lecturer.name, "the second button replaces the fields again");
});

test("the lab form shows both buttons; a button is off, with the reason, while its row is empty", () => {
  const both = { lecture: lecturer, practice: seminar, lab: null };
  const on = view(both, { ...idleTeacher, form: formFor("lab", both) });
  assert.match(on, />Как у лектора</);
  assert.match(on, />Как у семинариста</);
  assert.doesNotMatch(on, /лектор не указан|семинарист не указан/, "no reason while the button works");
  assert.doesNotMatch(on, /<button[^>]*disabled=""[^>]*>Как у/);

  const onlyLecture = { lecture: lecturer, practice: null, lab: null };
  const off = view(onlyLecture, { ...idleTeacher, form: formFor("lab", onlyLecture) });
  assert.match(off, /<button[^>]*disabled=""[^>]*>Как у семинариста</);
  assert.match(off, /семинарист не указан/);
  assert.doesNotMatch(off, /<button[^>]*disabled=""[^>]*>Как у лектора</);

  const nothing = view(none, { ...idleTeacher, form: formFor("lab", none) });
  assert.match(nothing, /<button[^>]*disabled=""[^>]*>Как у лектора</);
  assert.match(nothing, /<button[^>]*disabled=""[^>]*>Как у семинариста</);

  const page = setup(withTeachers(onlyLecture));
  page.flow.open("lab");
  page.flow.copy("practice");
  assert.deepEqual(page.flow.state.form, { role: "lab", name: "", email: "" }, "a disabled source copies nothing");
  const other = setup(withTeachers(both));
  other.flow.open("practice");
  other.flow.copy("lecture");
  assert.equal(other.flow.state.form.name, seminar.name, "only the lab form copies");
});

test("«Сохранить» after a copy is the normal save of the lab row", async () => {
  const teachers = { lecture: lecturer, practice: seminar, lab: null };
  const page = setup(withTeachers(teachers), []);
  page.flow.open("lab");
  page.flow.copy("practice");
  await page.flow.save();
  assert.deepEqual(page.calls, [["set", "s1", 3, "lab", seminar.name, seminar.email]]);
});

// ---- Save and clear ----

test("save sends the subject id, the version shown, the role and the trimmed texts, then closes the form", async () => {
  const saved = withTeachers({ practice: seminar }, { version: 4 });
  const page = setup(subject(), [ok(saved)]);
  page.flow.open("practice");
  page.flow.type({ name: `  ${seminar.name} ` });
  page.flow.type({ email: ` ${seminar.email}  ` });
  await page.flow.save();
  assert.deepEqual(page.calls, [["set", "s1", 3, "practice", seminar.name, seminar.email]]);
  assert.equal(page.replaced, 1);
  assert.equal(page.subject.version, 4);
  const s = page.flow.state;
  assert.deepEqual([s.form, s.busy, s.note, s.focusRole], [null, "", teacherTexts.saved, "practice"]);
  assert.deepEqual(s.errors, { name: "", email: "", general: "" });
});

test("an empty name or address is told at its field and calls nothing", async () => {
  const page = setup();
  page.flow.open("lecture");
  await page.flow.save();
  assert.deepEqual(page.flow.state.errors, { name: teacherTexts.nameEmpty, email: teacherTexts.emailEmpty, general: "" });
  page.flow.type({ name: "Иванов" });
  assert.equal(page.flow.state.errors.name, "", "typing clears that field's error");
  assert.equal(page.flow.state.errors.email, teacherTexts.emailEmpty);
  page.flow.type({ email: "   " });
  await page.flow.save();
  assert.equal(page.flow.state.errors.email, teacherTexts.emailEmpty);
  assert.deepEqual(page.calls, []);
  assert.notEqual(page.flow.state.form, null, "the form stays open");
});

test("«invalid» is shown at the field the core named and keeps the form and what was typed", async () => {
  const page = setup(subject(), [refused("invalid", "name"), refused("invalid", "email"), refused("invalid", "role")]);
  page.flow.open("lecture");
  page.flow.type({ name: "Иванов", email: "ivanov" });
  await page.flow.save();
  assert.deepEqual(page.flow.state.errors, { name: teacherTexts.nameInvalid, email: "", general: "" });
  await page.flow.save();
  assert.deepEqual(page.flow.state.errors, { name: "", email: teacherTexts.emailInvalid, general: "" });
  await page.flow.save();
  assert.deepEqual(page.flow.state.errors, { name: "", email: "", general: teacherTexts.saveFailed });
  assert.deepEqual(page.flow.state.form, { role: "lecture", name: "Иванов", email: "ivanov" });
  assert.equal(page.flow.state.busy, "");
  assert.equal(page.rereads, 0, "a mistake in a field needs no re-read");

  const out = view(none, { ...idleTeacher, form: page.flow.state.form, errors: { name: teacherTexts.nameInvalid, email: teacherTexts.emailInvalid, general: "" } });
  assert.equal((out.match(/role="alert"/g) ?? []).length, 2, "an error is an alert next to its field");
  assert.ok(out.includes(teacherTexts.nameInvalid) && out.includes(teacherTexts.emailInvalid));
  assert.match(out, /aria-invalid="true"/);
});

test("«changed» re-reads the list, says so, and the next save goes over the new version with the typed text", async () => {
  const page = setup(withTeachers({ lecture: lecturer }), [refused("changed")]);
  page.rereadOk = true;
  page.flow.open("practice");
  page.flow.type({ name: seminar.name, email: seminar.email });
  await page.flow.save();
  assert.equal(page.rereads, 1);
  assert.equal(page.flow.state.errors.general, teacherTexts.changed);
  assert.match(teacherTexts.changed, /Данные предмета изменились, список обновлён/);
  assert.deepEqual(page.flow.state.form, { role: "practice", name: seminar.name, email: seminar.email });
  page.subject = withTeachers({ lecture: lecturer }, { version: 9 }); // the re-read list
  await page.flow.save();
  assert.deepEqual(page.calls[1], ["set", "s1", 9, "practice", seminar.name, seminar.email]);
  assert.equal(page.flow.state.form, null);
});

test("«failed» re-reads and says the data is fresh; a failed re-read tells to reopen the page", async () => {
  const page = setup(subject(), [refused("failed"), refused("failed")]);
  page.flow.open("lecture");
  page.flow.type({ name: "Иванов", email: "ivanov@example.test" });
  await page.flow.save();
  assert.equal(page.rereads, 1);
  assert.equal(page.flow.state.errors.general, teacherTexts.saveFailed);
  assert.notEqual(page.flow.state.form, null);
  page.rereadOk = false;
  await page.flow.save();
  assert.equal(page.flow.state.errors.general, teacherTexts.rereadFailed);
  assert.equal(page.flow.state.busy, "", "the buttons come back: there is a next step");
});

test("«not_found» closes the form, re-reads and says so", async () => {
  const page = setup(subject(), [refused("not_found")]);
  page.flow.open("lecture");
  page.flow.type({ name: "Иванов", email: "ivanov@example.test" });
  await page.flow.save();
  assert.deepEqual([page.flow.state.form, page.flow.state.errors.general, page.rereads], [null, teacherTexts.notFound, 1]);
});

test("a throwing call is «failed» too", async () => {
  const page = setup();
  page.flow = new TeacherFlow(
    { setSubjectTeacher: async () => { throw new Error("boom"); }, clearSubjectTeacher: async () => { throw new Error("boom"); } },
    { subject: () => page.subject, replace: noop, reread: async () => true },
    (s) => page.states.push(s),
  );
  page.flow.open("lecture");
  page.flow.type({ name: "Иванов", email: "ivanov@example.test" });
  await page.flow.save();
  assert.equal(page.flow.state.errors.general, teacherTexts.saveFailed);
});

test("a double click makes one call; the buttons are off meanwhile", async () => {
  const page = setup();
  let release = noop;
  const hold = new Promise((r) => (release = r));
  const calls = [];
  page.flow = new TeacherFlow(
    {
      setSubjectTeacher: async (...a) => {
        calls.push(a);
        await hold;
        return ok(withTeachers({ lecture: lecturer }, { version: 4 }));
      },
      clearSubjectTeacher: async () => refused("failed"),
    },
    { subject: () => page.subject, replace: (s) => (page.subject = s), reread: async () => true },
    (s) => page.states.push(s),
  );
  page.flow.open("lecture");
  page.flow.type({ name: lecturer.name, email: lecturer.email });
  const first = page.flow.save();
  const second = page.flow.save();
  assert.equal(page.flow.state.busy, "save");
  page.flow.cancel();
  page.flow.open("practice");
  assert.equal(page.flow.state.form.role, "lecture", "nothing changes while a call runs");
  release();
  await Promise.all([first, second]);
  assert.equal(calls.length, 1);
  assert.equal(page.flow.state.busy, "");
  assert.equal(page.flow.state.form, null);
});

test("«Убрать» calls clear with the role and the version, only for a filled row", async () => {
  const page = setup(withTeachers({ lecture: lecturer }), [ok(subject({ version: 4 }))]);
  page.flow.open("practice");
  await page.flow.clear();
  assert.deepEqual(page.calls, [], "nothing to remove");
  page.flow.open("lecture");
  await page.flow.clear();
  assert.deepEqual(page.calls, [["clear", "s1", 3, "lecture"]]);
  assert.deepEqual([page.flow.state.form, page.flow.state.note, page.flow.state.focusRole], [null, teacherTexts.cleared, "lecture"]);
  assert.equal(page.subject.version, 4);
});

test("a failed clear says so and re-reads", async () => {
  const page = setup(withTeachers({ lecture: lecturer }), [refused("changed"), refused("failed")]);
  page.flow.open("lecture");
  await page.flow.clear();
  assert.equal(page.flow.state.errors.general, teacherTexts.changed);
  await page.flow.clear();
  assert.equal(page.flow.state.errors.general, teacherTexts.clearFailed);
  assert.equal(page.rereads, 2);
  assert.notEqual(page.flow.state.form, null);
});

test("another subject: reset drops the form, and a late reply no longer touches it", async () => {
  const page = setup();
  let release = noop;
  const hold = new Promise((r) => (release = r));
  page.flow = new TeacherFlow(
    { setSubjectTeacher: async () => (await hold, ok(withTeachers({ lecture: lecturer }, { version: 4 }))), clearSubjectTeacher: async () => refused("failed") },
    { subject: () => page.subject, replace: (s) => (page.subject = s), reread: async () => true },
    (s) => page.states.push(s),
  );
  page.flow.open("lecture");
  page.flow.type({ name: "Иванов", email: "ivanov@example.test" });
  const running = page.flow.save();
  page.flow.reset();
  assert.deepEqual(page.flow.state, idleTeacher);
  release();
  await running;
  assert.equal(page.flow.state.form, null);
  assert.equal(page.flow.state.note, "");
  assert.equal(page.subject.version, 4, "the list still learns the saved subject");
});

test("a disposed flow reports nothing", async () => {
  const page = setup();
  page.flow.open("lecture");
  const before = page.states.length;
  page.flow.dispose();
  page.flow.cancel();
  assert.equal(page.states.length, before);
  page.flow.attach();
  page.flow.open("lecture");
  assert.equal(page.states.length, before + 1);
});

// ---- «Указать почту»: the one-shot request ----

test("a request waits for the list, is dropped for a missing subject and is taken once for the right one", () => {
  const focus = { subjectId: "s1", role: "practice" };
  assert.deepEqual(teacherFocusStep(null, "s1", true, true), { role: null, done: false });
  assert.deepEqual(teacherFocusStep(focus, "", true, false), { role: null, done: false }, "another page of the same section");
  assert.deepEqual(teacherFocusStep(focus, "s2", true, true), { role: null, done: false }, "another subject");
  assert.deepEqual(teacherFocusStep(focus, "s1", false, false), { role: null, done: false }, "the list is still being read");
  assert.deepEqual(teacherFocusStep(focus, "s1", true, false), { role: null, done: true }, "the subject is gone");
  assert.deepEqual(teacherFocusStep(focus, "s1", true, true), { role: "practice", done: true });
  assert.deepEqual(teacherFocusStep({ subjectId: "s1", role: "" }, "s1", true, true), { role: null, done: true }, "the page only");
});

test("the window keeps a request only while the route points at its subject", () => {
  const focus = { subjectId: "s1", role: "lecture" };
  assert.equal(keepTeacherFocus(focus, { page: "subjects", subject: "s1" }), focus);
  assert.equal(keepTeacherFocus(focus, { page: "subjects", subject: "" }), null);
  assert.equal(keepTeacherFocus(focus, { page: "subjects", subject: "s2" }), null);
  assert.equal(keepTeacherFocus(focus, { page: "homework", subject: "s1" }), null);
  assert.equal(keepTeacherFocus(null, { page: "subjects", subject: "s1" }), null);
});

test("the subject page owns the block near the top, the teachers come from the list, and the request is wired once", () => {
  const read = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
  const page = read("pages/SubjectsPage.tsx");
  assert.ok(page.indexOf("<TeachersBlock") > page.indexOf("<h1>{subject.name}</h1>"), "under the title");
  assert.ok(page.indexOf("<TeachersBlock") < page.indexOf("<OverviewSections"), "before «Ближайшее» and «Занятия»");
  assert.match(page, /teachers=\{subject\.teachers\}/, "from Subjects(), not from the overview");
  assert.match(page, /api\.subjects\(\)\.then/, "re-read of the list");
  assert.ok(page.indexOf("flow.reset()") < page.indexOf("teacherFocusStep("), "the reset of the previous subject comes first");
  const app = read("App.tsx");
  assert.match(app, /RecipientNavContext\.Provider/);
  assert.match(app, /keepTeacherFocus\(f,/);
  assert.match(app, /routeToSubject\(r, subjectId\)/, "«Указать почту» opens the subject (an entry of the history, so «Назад» works)");
  assert.equal(rowLabels.lab, "Лабораторные");
});
