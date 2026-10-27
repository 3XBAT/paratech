// The lesson of an assignment (TASK-092): the label helper, the bindings, the lists and the correction form. Runs on
// the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { homeworkApi, methods } from "../.test-build/bindings/homework.js";
import { summaryApi } from "../.test-build/bindings/summary.js";
import { lessonDateText, lessonLine, lessonSource, lessonText } from "../.test-build/lessonLabel.js";
import { LessonCorrection, maxNameLength } from "../.test-build/pages/LessonCorrection.js";
import { HomeworkCard } from "../.test-build/pages/SummaryParts.js";

const { renderToStaticMarkup } = server;

// Noon UTC is the same calendar day in every zone from UTC-11 to UTC+11, so the test does not depend on the zone.
const noon = "2026-10-19T12:00:00Z";

const lesson = (over = {}) => ({
  subject: "Физика",
  subjectSource: "schedule",
  kind: "lecture",
  teacher: "Иванов И. И.",
  teacherSource: "schedule",
  lessonDate: noon,
  ...over,
});

// Mirrors AssignmentItem of internal/input/wails/homework.go.
const item = (over = {}) => ({
  id: "a1",
  lessonId: "l1",
  title: "Задачи 1–5",
  description: "",
  due: "",
  userEdited: false,
  version: 1,
  updatedAt: "2026-10-19T12:00:00Z",
  ...lesson(),
  subjectFromLesson: true,
  correctedSubject: "",
  correctedTeacher: "",
  ...over,
});

test("the line is «Предмет · тип · преподаватель», empty parts skipped, nothing when all are empty", () => {
  assert.equal(lessonLine(lesson()), "Физика · лекция · Иванов И. И.");
  assert.equal(lessonLine(lesson({ kind: "" })), "Физика · Иванов И. И.");
  assert.equal(lessonLine(lesson({ subject: "", subjectSource: "" })), "лекция · Иванов И. И.");
  assert.equal(lessonLine(lesson({ teacher: " ", teacherSource: "" })), "Физика · лекция");
  assert.equal(lessonLine(lesson({ subject: "", subjectSource: "", kind: "", teacher: "", teacherSource: "" })), "");
  assert.equal(lessonText(lesson({ subject: "", kind: "", teacher: "", lessonDate: "" })), "");
});

test("kinds are in Russian; an unknown code shows no kind", () => {
  const kinds = { lecture: "лекция", seminar: "семинар", practice: "практика", lab: "лабораторная" };
  for (const [code, label] of Object.entries(kinds)) {
    assert.equal(lessonLine(lesson({ subject: "", teacher: "", kind: code })), label);
  }
  assert.equal(lessonLine(lesson({ kind: "unknown" })), "Физика · Иванов И. И.");
  assert.equal(lessonLine(lesson({ kind: "webinar" })), "Физика · Иванов И. И.");
});

test("only a hint from the recording is marked «по записи»", () => {
  assert.equal(lessonLine(lesson({ subjectSource: "transcript" })), "Физика (по записи) · лекция · Иванов И. И.");
  assert.equal(lessonLine(lesson({ teacherSource: "transcript" })), "Физика · лекция · Иванов И. И. (по записи)");
  assert.equal(lessonLine(lesson({ subjectSource: "user", teacherSource: "user" })), "Физика · лекция · Иванов И. И.");
  assert.equal(lessonLine(lesson({ subject: "", subjectSource: "transcript" })), "лекция · Иванов И. И.");
});

test("the lesson date is «занятие ДД.ММ» in local time, nothing when unknown", () => {
  assert.equal(lessonDateText(noon), "занятие 19.10");
  assert.equal(lessonDateText("2026-03-05T12:00:00Z"), "занятие 05.03");
  assert.equal(lessonDateText(""), "");
  assert.equal(lessonDateText("later"), "");
  assert.equal(lessonText(lesson()), "Физика · лекция · Иванов И. И. · занятие 19.10");
  assert.equal(lessonText(lesson({ subject: "", kind: "", teacher: "" })), "занятие 19.10");
  assert.equal(lessonText(lesson({ lessonDate: "" })), "Физика · лекция · Иванов И. И.");
});

test("a source code the page does not know is shown as no source", () => {
  for (const s of ["schedule", "transcript", "user", ""]) {
    assert.equal(lessonSource(s), s);
  }
  assert.equal(lessonSource("model"), "");
});

test("correctLesson calls the bound method and returns the assignment as shown", async () => {
  const calls = [];
  const reply = item({ subject: "Механика", subjectSource: "user", correctedSubject: "Механика" });
  const api = homeworkApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  });
  const a = await api.correctLesson("a1", "Механика", "");
  assert.deepEqual(calls, [[methods.correctLesson, "a1", "Механика", ""]]);
  assert.deepEqual(a, reply);
  assert.equal(methods.correctLesson, "paratech/desktop/internal/input/wails.HomeworkService.CorrectLesson");

  const odd = await homeworkApi(async () => item({ teacherSource: "model" })).correctLesson("a1", "", "");
  assert.equal(odd.teacherSource, "");
  for (const key of ["subject", "kind", "teacher", "lessonDate", "correctedSubject", "correctedTeacher", "subjectSource"]) {
    const bad = item();
    delete bad[key];
    await assert.rejects(homeworkApi(async () => bad).correctLesson("a1", "", ""), undefined, key);
  }
  const listed = await homeworkApi(async () => [item({ subjectSource: "transcript" })]).listAssignments();
  assert.equal(lessonText(listed[0]), "Физика (по записи) · лекция · Иванов И. И. · занятие 19.10");
});

const summaryView = (assignment) => ({
  recordingId: "r1",
  lessonId: "l1",
  title: "Физика",
  startedAt: noon,
  durationSeconds: 60,
  hasDuration: true,
  transcribed: true,
  noSpeech: false,
  summary: { state: "done", waitReason: "", message: "", jobError: "", ready: false, text: "", sections: [], keyPoints: [], updatedAt: "" },
  homework: { state: "done", waitReason: "", message: "", jobError: "", assignments: [assignment] },
  lectureText: {
    state: "none", waitReason: "", message: "", jobError: "", ready: false, blocks: [], updatedAt: "",
    partsDone: 0, parts: 0, canStart: false, retryWork: "",
  },
});

const summaryAssignment = (over = {}) => ({
  id: "a1",
  title: "Задачи 1–5",
  description: "",
  due: "",
  userEdited: false,
  ...lesson(),
  ...over,
});

test("the summary's assignments carry the lesson and show it, a hint marked «по записи»", async () => {
  const v = await summaryApi(async () =>
    summaryView(summaryAssignment({ teacher: "<b>Петров</b>", teacherSource: "transcript" })),
  ).view("r1");
  const a = v.homework.assignments[0];
  assert.equal(a.teacherSource, "transcript");
  const html = renderToStaticMarkup(h(HomeworkCard, { view: v, onOpenAssignment: () => undefined }));
  assert.ok(html.includes("Физика · лекция · &lt;b&gt;Петров&lt;/b&gt; (по записи) · занятие 19.10"), html);
  assert.ok(!html.includes("<b>"));

  const bare = summaryAssignment({ subject: "", subjectSource: "", kind: "", teacher: "", teacherSource: "", lessonDate: "" });
  const none = renderToStaticMarkup(h(HomeworkCard, { view: summaryView(bare), onOpenAssignment: () => undefined }));
  assert.ok(!none.includes("sm-lesson"), "nothing shown when the lesson is unknown");

  const missing = summaryAssignment();
  delete missing.kind;
  await assert.rejects(summaryApi(async () => summaryView(missing)).view("r1"));
});

test("the correction form shows the student's correction, the shown values as placeholders, and calls nothing by itself", () => {
  const calls = [];
  const a = item({
    subject: "<i>Физика</i>",
    subjectSource: "transcript",
    teacher: "Иванов И. И.",
    correctedTeacher: "Петров П. П.",
    teacherSource: "user",
  });
  const html = renderToStaticMarkup(
    h(LessonCorrection, {
      assignment: a,
      correct: async (...args) => {
        calls.push(args);
        return a;
      },
      onCorrected: () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.match(html, /Поправить предмет и преподавателя/);
  assert.ok(html.includes('placeholder="&lt;i&gt;Физика&lt;/i&gt;"'), html);
  assert.ok(html.includes('value="Петров П. П."'));
  assert.equal(html.split(`maxLength="${maxNameLength}"`).length - 1, 2);
  assert.equal(maxNameLength, 200);
  assert.ok(!html.includes("<i>"));
  assert.match(html, />Сохранить</);
  assert.deepEqual(calls, []);
});

const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");

test("the list, the assignment screen and the summary show the lesson; the form saves through correctLesson", () => {
  const page = src("pages/HomeworkPage.tsx");
  assert.equal(page.split("lessonText(a)").length - 1, 2, "the list row and the assignment screen");
  assert.match(page, /correct=\{api\.correctLesson\}/);
  assert.match(page, /assignment: next/, "the shown assignment is replaced by the core's reply");
  assert.match(src("pages/SummaryParts.tsx"), /lessonText\(a\)/);
  const form = src("pages/LessonCorrection.tsx");
  assert.equal(form.split("props.correct(").length - 1, 1);
  assert.ok(!form.includes("dangerouslySetInnerHTML"));
});
