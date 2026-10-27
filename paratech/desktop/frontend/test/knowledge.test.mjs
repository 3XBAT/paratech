// «База знаний» (TASK-091): the bindings, the view helpers, the parts rendered as text only and source guards. Runs on
// the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorMessages, knowledgeBaseApi, methods } from "../.test-build/bindings/knowledgeBase.js";
import {
  askSearch,
  checkQuery,
  clockText,
  correctionDraft,
  dayText,
  errorText,
  exportResultLine,
  groupHits,
  hitCountText,
  hitFocus,
  hitPlace,
  idleFlight,
  kindOptions,
  lessonHeading,
  lessonLabel,
  lessonMeta,
  lessonName,
  lessonTabs,
  noFocus,
  paragraphOf,
  readyMarks,
  searchDone,
  searchTexts,
  shownBlock,
  shownPoint,
  subjectMeta,
  subjectName,
  subjectPlaceholder,
  summaryFocus,
} from "../.test-build/knowledgeView.js";
import { HitList, LessonInfoForm, LessonList, ReadyMarks, Snippet, SubjectList } from "../.test-build/pages/KnowledgeParts.js";

const { renderToStaticMarkup } = server;

// Noon UTC is the same calendar day in every zone from UTC-11 to UTC+11, so the dates do not depend on the zone.
const noon = "2026-10-19T12:00:00Z";
// A local time, for the clock: 14:05 in the zone the test runs in.
const local1405 = new Date(2026, 9, 19, 14, 5).toISOString();

// Mirrors KnowledgeLesson of internal/input/wails/knowledgebase.go.
const lesson = (over = {}) => ({
  key: "recording:r1",
  noteOnly: false,
  undated: false,
  anchor: "recording",
  scheduleLessonId: "",
  date: "",
  notes: [],
  recordingId: "r1",
  startedAt: noon,
  durationSeconds: 5400,
  title: "Пределы",
  subject: "Матанализ",
  subjectKey: "матанализ",
  kind: "lecture",
  subjectEdited: false,
  kindEdited: false,
  transcriptReady: true,
  summaryReady: true,
  lectureTextReady: false,
  librarySubjectId: "",
  librarySubjectState: "none",
  ...over,
});
const subject = (over = {}) => ({
  key: "матанализ",
  name: "Матанализ",
  lessons: 5,
  latest: noon,
  noSubject: false,
  subjectId: "",
  ...over,
});
const hit = (over = {}) => ({
  recordingId: "r1",
  section: "transcript",
  index: 7,
  startMs: 725000,
  time: "12:05",
  before: "…предел ",
  match: "функции",
  after: " в точке…",
  noteId: "",
  materialId: "",
  locatorKind: "",
  locatorN: 0,
  label: "",
  lesson: lesson(),
  ...over,
});
// TASK-155: a lecture known only from the student's notes, of a day (as the binding sends it).
const note = (over = {}) => ({ id: "n1", title: "Ряды.docx", subjectId: "phys", createdAt: noon, ...over });
const noteLecture = (over = {}) =>
  lesson({
    key: "day:phys:2026-09-12",
    noteOnly: true,
    anchor: "day",
    date: "2026-09-12",
    notes: [note()],
    recordingId: "",
    startedAt: "2026-09-11T21:00:00Z",
    durationSeconds: 0,
    transcriptReady: false,
    summaryReady: false,
    subjectKey: "ID:phys",
    librarySubjectId: "phys",
    librarySubjectState: "matched",
    ...over,
  });
const noteHit = (over = {}) =>
  hit({
    recordingId: "",
    section: "ownNote",
    index: 2,
    startMs: 0,
    time: "",
    noteId: "n1",
    materialId: "m1",
    locatorKind: "page",
    locatorN: 3,
    label: "ваш конспект · стр. 3",
    lesson: noteLecture(),
    ...over,
  });

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return typeof reply === "function" ? reply(method) : reply;
  };
  return { calls, call };
}

test("the API calls the bound names with the page's arguments and checks the replies", async () => {
  const f = fakeCall((m) => {
    switch (m) {
      case methods.subjects:
        return [subject(), subject({ key: "", name: "Без предмета", noSubject: true })];
      case methods.lessons:
        return [lesson()];
      case methods.search:
        return { hits: [hit()], truncated: true };
      case methods.exportSubject:
        return { status: "saved", message: "Предмет сохранён.", name: "Матанализ.md" };
      default:
        return lesson();
    }
  });
  const api = knowledgeBaseApi(f.call);
  assert.equal((await api.subjects()).length, 2);
  assert.deepEqual(await api.lessons("матанализ"), [lesson()]);
  assert.deepEqual(await api.lesson("r1"), lesson());
  assert.deepEqual(await api.search("функции"), { hits: [hit()], truncated: true });
  assert.equal((await api.exportSubject("")).name, "Матанализ.md");
  await api.setLessonInfo("r1", "Физика", "seminar");
  assert.deepEqual(f.calls, [
    [methods.subjects],
    [methods.lessons, "матанализ"],
    [methods.lesson, "r1"],
    [methods.search, "функции"],
    [methods.exportSubject, ""],
    [methods.setLessonInfo, "r1", "Физика", "seminar"],
  ]);
});

test("a malformed reply is an error, not a default", async () => {
  const bad = [
    [() => knowledgeBaseApi(fakeCall(null).call).subjects(), /not a list/],
    [() => knowledgeBaseApi(fakeCall([{ ...subject(), lessons: -1 }]).call).subjects(), /lessons/],
    [() => knowledgeBaseApi(fakeCall([{ ...lesson(), summaryReady: "yes" }]).call).lessons(""), /summaryReady/],
    [() => knowledgeBaseApi(fakeCall({ hits: [hit({ section: "html" })], truncated: false }).call).search("ab"), /section/],
    [() => knowledgeBaseApi(fakeCall({ hits: [hit({ lesson: null })], truncated: false }).call).search("ab"), /lesson/],
    [() => knowledgeBaseApi(fakeCall({ hits: null, truncated: false }).call).search("ab"), /hits/],
    [() => knowledgeBaseApi(fakeCall({ status: "done", message: "", name: "" }).call).exportSubject(""), /status/],
    [() => knowledgeBaseApi(fakeCall(lesson({ librarySubjectState: "guess" })).call).lesson("r1"), /librarySubjectState/],
    [() => knowledgeBaseApi(fakeCall(lesson({ librarySubjectId: undefined })).call).lesson("r1"), /librarySubjectId/],
    [() => knowledgeBaseApi(fakeCall(lesson({ librarySubjectState: "matched" })).call).lesson("r1"), /does not fit/],
    [() => knowledgeBaseApi(fakeCall(lesson({ librarySubjectId: "phys", librarySubjectState: "ambiguous" })).call).lesson("r1"), /does not fit/],
  ];
  for (const [run, msg] of bad) {
    await assert.rejects(run, msg);
  }
});

test("the library subject of a lesson comes from the core with its state", async () => {
  for (const [id, state] of [["phys", "matched"], ["", "none"], ["", "ambiguous"], ["", "no-subject"]]) {
    const got = await knowledgeBaseApi(fakeCall(lesson({ librarySubjectId: id, librarySubjectState: state })).call).lesson("r1");
    assert.equal(got.librarySubjectId, id);
    assert.equal(got.librarySubjectState, state);
  }
});

test("TASK-155: lectures lists recorded and note-only lectures as the core orders them", async () => {
  const recorded = lesson({ scheduleLessonId: "l1", notes: [note({ id: "n0" })] });
  const ofLesson = noteLecture({ key: "lesson:l7", anchor: "lesson", scheduleLessonId: "l7", date: "2026-09-05", notes: [note({ id: "n2" })] });
  const undated = noteLecture({ key: "note:n3", anchor: "undated", undated: true, date: "", startedAt: "", notes: [note({ id: "n3" })] });
  const f = fakeCall([recorded, noteLecture(), ofLesson, undated]);
  const got = await knowledgeBaseApi(f.call).lectures("ID:phys");
  assert.deepEqual(f.calls, [[methods.lectures, "ID:phys"]]);
  assert.deepEqual(got, [recorded, noteLecture(), ofLesson, undated]);
  assert.deepEqual(
    got.map((l) => [l.key, l.anchor, l.noteOnly, l.date, l.notes.map((n) => n.id).join()]),
    [
      ["recording:r1", "recording", false, "", "n0"],
      ["day:phys:2026-09-12", "day", true, "2026-09-12", "n1"],
      ["lesson:l7", "lesson", true, "2026-09-05", "n2"],
      ["note:n3", "undated", true, "", "n3"],
    ],
  );
  // A subject known only from notes: no recorded lesson, its library subject.
  const s = await knowledgeBaseApi(fakeCall([subject({ key: "ID:chem", lessons: 0, latest: "", subjectId: "chem" })]).call).subjects();
  assert.deepEqual([s[0].lessons, s[0].latest, s[0].subjectId], [0, "", "chem"]);
});

test("TASK-155: a lesson whose anchor does not fit its ids is refused", async () => {
  const bad = [
    noteLecture({ recordingId: "r1" }), // note-only with a recording
    lesson({ noteOnly: true }), // a recording marked note-only
    lesson({ anchor: "day" }), // a recording with another anchor
    noteLecture({ anchor: "recording" }),
    noteLecture({ date: "" }), // a day lecture without its date
    noteLecture({ date: "12.09.2026" }),
    noteLecture({ anchor: "lesson" }), // a lesson lecture without its lesson
    noteLecture({ scheduleLessonId: "l7" }), // a day lecture with a lesson
    noteLecture({ undated: true }), // undated but with a date
    noteLecture({ key: "note:n3", anchor: "undated", undated: true, date: "" }), // undated with a start
    noteLecture({ notes: [] }), // a note-only lecture without notes
    lesson({ date: "2026-09-12" }), // a recording with a date
  ];
  for (const l of bad) {
    await assert.rejects(() => knowledgeBaseApi(fakeCall(l).call).lesson("r1"), /does not fit/, JSON.stringify(l));
  }
  const malformed = [
    [lesson({ anchor: "folder" }), /anchor/],
    [lesson({ notes: null }), /notes/],
    [lesson({ notes: [note({ id: "" })] }), /id/],
    [lesson({ notes: [{ ...note(), title: 1 }] }), /title/],
    [lesson({ key: undefined }), /key/],
  ];
  for (const [l, msg] of malformed) {
    await assert.rejects(() => knowledgeBaseApi(fakeCall(l).call).lesson("r1"), msg);
  }
  await assert.rejects(() => knowledgeBaseApi(fakeCall([subject({ subjectId: null })]).call).subjects(), /subjectId/);
});

test("TASK-155: a hit in the student's notes carries its place and the core's label", async () => {
  const kinds = [
    ["page", 3, "ваш конспект · стр. 3"],
    ["slide", 2, "ваш конспект · слайд 2"],
    ["section", 1, "ваш конспект · раздел „Ряды“"],
    ["fragment", 4, "ваш конспект · фрагмент 4"],
  ];
  for (const [locatorKind, locatorN, label] of kinds) {
    const h = noteHit({ locatorKind, locatorN, label });
    const got = await knowledgeBaseApi(fakeCall({ hits: [hit(), h], truncated: false }).call).search("ряды");
    assert.deepEqual(got.hits, [hit(), h]);
    assert.equal(got.hits[1].label, label);
  }
  const bad = [
    noteHit({ noteId: "" }),
    noteHit({ materialId: "" }),
    noteHit({ locatorKind: "" }),
    noteHit({ locatorN: 0 }),
    noteHit({ label: "" }),
    hit({ noteId: "n1" }), // a recording section with a note place
    hit({ label: "ваш конспект" }),
    hit({ locatorKind: "page", locatorN: 1 }),
  ];
  for (const b of bad) {
    await assert.rejects(() => knowledgeBaseApi(fakeCall({ hits: [b], truncated: false }).call).search("ab"), /does not fit/);
  }
  await assert.rejects(
    () => knowledgeBaseApi(fakeCall({ hits: [noteHit({ locatorKind: "line" })], truncated: false }).call).search("ab"),
    /locatorKind/,
  );
  await assert.rejects(
    () => knowledgeBaseApi(fakeCall({ hits: [noteHit({ locatorN: -1 })], truncated: false }).call).search("ab"),
    /locatorN/,
  );
});

test("a lesson is «19.10 · лекция · Название»; an ad-hoc one is «Запись 14:05»", () => {
  assert.equal(dayText(noon), "19.10");
  assert.equal(dayText(""), "");
  assert.equal(dayText("nope"), "");
  assert.equal(clockText(local1405), "14:05");
  assert.equal(lessonLabel(lesson()), "19.10 · лекция · Пределы");
  assert.equal(lessonLabel(lesson({ kind: "" })), "19.10 · Пределы");
  assert.equal(lessonLabel(lesson({ kind: "webinar" })), "19.10 · Пределы");
  assert.equal(lessonName(lesson({ title: " ", startedAt: local1405 })), "Запись 14:05");
  assert.equal(lessonName(lesson({ title: "", startedAt: "" })), "Запись");
  assert.equal(lessonHeading(lesson({ title: "", startedAt: local1405 })), "Запись 19.10 14:05");
  assert.equal(lessonHeading(lesson()), "Пределы");
  assert.equal(lessonMeta(lesson({ startedAt: local1405 })), "Матанализ · лекция · 19.10 в 14:05");
  assert.equal(subjectName(lesson({ subject: "" })), "Без предмета");
});

test("readiness marks and subject counts", () => {
  assert.deepEqual(
    readyMarks(lesson()).map((m) => [m.label, m.ready]),
    [
      ["Кратко", true],
      ["Текст лекции", false],
      ["Расшифровка", true],
    ],
  );
  assert.equal(subjectMeta(subject()), "5 занятий · последнее 19.10");
  assert.equal(subjectMeta(subject({ lessons: 1 })), "1 занятие · последнее 19.10");
  assert.equal(subjectMeta(subject({ lessons: 3, latest: "" })), "3 занятия");
  assert.deepEqual(
    lessonTabs.map((t) => t.label),
    ["Кратко", "Текст лекции", "Расшифровка", "Задания"],
  );
});

test("a hit opens the lesson at its place", () => {
  assert.deepEqual(hitFocus(hit()), { tab: "transcript", index: 7 });
  assert.deepEqual(hitFocus(hit({ section: "lectureText", index: 2 })), { tab: "lecture", index: 2 });
  // TASK-101: the summary index is the core's (sections, then key points); summaryFocus maps it with the view.
  assert.deepEqual(hitFocus(hit({ section: "summary", index: 0 })), { tab: "summary", index: 0 });
  assert.deepEqual(hitFocus(hit({ section: "summary", index: 3 })), { tab: "summary", index: 3 });
  assert.deepEqual(hitFocus(hit({ section: "title", index: 0 })), noFocus);
  assert.equal(hitPlace(hit()), "Расшифровка · 12:05");
  assert.equal(hitPlace(hit({ section: "summary", time: "" })), "Кратко");
});

test("hits are grouped by lesson in the core's order", () => {
  const groups = groupHits([
    hit(),
    hit({ recordingId: "r2", lesson: lesson({ recordingId: "r2" }) }),
    hit({ section: "summary", index: 0 }),
  ]);
  assert.deepEqual(
    groups.map((g) => [g.lesson.recordingId, g.hits.length]),
    [
      ["r1", 2],
      ["r2", 1],
    ],
  );
  assert.equal(hitCountText(3, 2, false), "Найдено 3 места в 2 занятиях.");
  assert.equal(hitCountText(1, 1, false), "Найдено 1 место в 1 занятии.");
  assert.equal(hitCountText(50, 20, true), "Показаны первые 50 мест: уточните запрос.");
});

test("the query is checked as the core reads it, 2 to 100 characters", () => {
  assert.deepEqual(checkQuery("   "), { kind: "empty" });
  assert.deepEqual(checkQuery(" ф "), { kind: "short" });
  assert.deepEqual(checkQuery("  предел   функции "), { kind: "ok", query: "предел функции" });
  assert.deepEqual(checkQuery("ё".repeat(100)), { kind: "ok", query: "ё".repeat(100) });
  assert.deepEqual(checkQuery("ё".repeat(101)), { kind: "long" });
  assert.equal(searchTexts.long, errorMessages.query);
});

test("one search call at a time: only the latest query waits, cleared or repeated ones are not sent again", () => {
  let a = askSearch(idleFlight, "предел");
  assert.equal(a.send, "предел");
  // Typing on while it runs: nothing is sent, only the latest query is kept.
  a = askSearch(a.flight, "предел ф");
  assert.equal(a.send, null);
  a = askSearch(a.flight, "предел функции");
  assert.equal(a.send, null);
  let d = searchDone(a.flight);
  assert.deepEqual([d.send, d.show], ["предел функции", false]);
  d = searchDone(d.flight);
  assert.deepEqual([d.send, d.show], [null, true]);
  assert.deepEqual(d.flight, idleFlight);
  // Back to the query that runs: its answer is shown, no second call.
  a = askSearch(askSearch(askSearch(idleFlight, "ряд").flight, "ряды").flight, "ряд");
  d = searchDone(a.flight);
  assert.deepEqual([d.send, d.show, d.flight], [null, true, idleFlight]);
  // The field cleared while it runs: the answer is dropped.
  d = searchDone(askSearch(askSearch(idleFlight, "ряд").flight, "").flight);
  assert.deepEqual([d.send, d.show, d.flight], [null, false, idleFlight]);
  assert.deepEqual(askSearch(idleFlight, ""), { flight: idleFlight, send: null });
});

test("the export outcome: saved names the file, cancelled waits, failed fails", () => {
  assert.deepEqual(exportResultLine({ status: "saved", message: "Предмет сохранён.", name: "Физика.md" }), {
    kind: "ok",
    text: "Сохранено: Физика.md",
  });
  assert.deepEqual(exportResultLine({ status: "cancelled", message: "Сохранение отменено.", name: "" }), {
    kind: "wait",
    text: "Сохранение отменено.",
  });
  assert.equal(exportResultLine({ status: "failed", message: "Не удалось.", name: "" }).kind, "fail");
});

test("the correction form starts with the user's own values only", () => {
  assert.deepEqual(correctionDraft(lesson()), { subject: "", kind: "" });
  assert.deepEqual(correctionDraft(lesson({ subjectEdited: true, kindEdited: true, kind: "lab" })), {
    subject: "Матанализ",
    kind: "lab",
  });
  assert.deepEqual(correctionDraft(lesson({ kindEdited: true, kind: "other" })).kind, "");
  assert.equal(subjectPlaceholder(lesson()), "Матанализ");
  assert.equal(subjectPlaceholder(lesson({ subject: "" })), "Без предмета");
  assert.deepEqual(
    kindOptions(lesson()).map((o) => o.label),
    ["Как в записи (лекция)", "лекция", "семинар", "практика", "лабораторная"],
  );
  assert.equal(kindOptions(lesson({ kindEdited: true }))[0].label, "Как в записи");
});

test("the place of a hit maps onto what the viewers show", () => {
  const lt = (blocks) => ({ lectureText: { ready: true, blocks } });
  const block = (heading, paragraphs = []) => ({ heading, startSeconds: 0, time: "", paragraphs });
  const v = lt([block("Введение"), block(" ", [" "]), block("", ["Текст"])]);
  assert.equal(shownBlock(v, 0), 0);
  assert.equal(shownBlock(v, 1), -1); // an empty block is not shown
  assert.equal(shownBlock(v, 2), 1);
  assert.equal(shownBlock(v, 3), -1);
  assert.equal(shownBlock({ lectureText: { ready: false, blocks: v.lectureText.blocks } }, 0), -1);
  assert.equal(shownPoint(["a", " ", "c"], 2), 1);
  assert.equal(shownPoint(["a", " ", "c"], 1), -1);
  assert.equal(shownPoint(["a"], -1), -1);
  // A summary hit: section i of n, else key point index-n (TASK-101).
  const sec = (heading, text = "") => ({ heading, text, points: [] });
  const sm = { ready: true, sections: [sec("Реки", "a"), sec(" ", " "), sec("Озёра")], keyPoints: ["x", " ", "z"] };
  assert.deepEqual(summaryFocus(sm, 0), { section: 0, point: -1 });
  assert.deepEqual(summaryFocus(sm, 1), { section: -1, point: -1 }); // an empty section is not shown
  assert.deepEqual(summaryFocus(sm, 2), { section: 2, point: -1 });
  assert.deepEqual(summaryFocus(sm, 3), { section: -1, point: 0 });
  assert.deepEqual(summaryFocus(sm, 4), { section: -1, point: -1 });
  assert.deepEqual(summaryFocus(sm, 5), { section: -1, point: 1 });
  assert.deepEqual(summaryFocus(sm, 6), { section: -1, point: -1 });
  assert.deepEqual(summaryFocus(sm, -1), { section: -1, point: -1 });
  assert.deepEqual(summaryFocus({ ...sm, ready: false }, 0), { section: -1, point: -1 });
  // A summary made before sections: 0 is its text, j+1 key point j, as before.
  const old = { ready: true, sections: [sec("", "Текст.")], keyPoints: ["x", "y"] };
  assert.deepEqual(summaryFocus(old, 0), { section: 0, point: -1 });
  assert.deepEqual(summaryFocus(old, 2), { section: -1, point: 1 });
  const seg = (index) => ({ index, startMs: 0, endMs: 0, start: "", text: "" });
  const paras = [
    { first: 0, start: "", startMs: 0, segments: [seg(0), seg(1)] },
    { first: 2, start: "", startMs: 0, segments: [seg(2), seg(3), seg(4)] },
  ];
  assert.equal(paragraphOf(paras, 3), 1);
  assert.equal(paragraphOf(paras, 0), 0);
  assert.equal(paragraphOf(paras, 9), -1); // not loaded yet
  assert.equal(paragraphOf(paras, -1), -1);
});

test("errors: the core's fixed texts as they are, anything else generic", () => {
  assert.equal(errorText(new Error(errorMessages.notFinished)), errorMessages.notFinished);
  assert.equal(errorText(errorMessages.query), errorMessages.query);
  assert.equal(errorText(new Error("open C:\\Users\\x\\db: denied")), errorMessages.unavailable);
});

test("lists, hits and the form render text only; the match is a <mark> around text", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const html = renderToStaticMarkup(
    h(HitList, {
      result: { hits: [hit({ before: evil, lesson: lesson({ title: evil }) })], truncated: true },
      onOpen: () => undefined,
    }),
  );
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(html.includes("<mark>функции</mark>"));
  assert.ok(html.includes("Расшифровка · 12:05"));
  assert.ok(html.includes("Показаны первые 1 место: уточните запрос."));
  assert.equal(renderToStaticMarkup(h(Snippet, { hit: { before: "a", match: "b", after: "c" } })), '<span class="kb-snippet">a<mark>b</mark>c</span>');

  const subjects = renderToStaticMarkup(
    h(SubjectList, { subjects: [subject(), subject({ key: "", name: "Без предмета", lessons: 2, noSubject: true })], onOpen: () => undefined }),
  );
  assert.ok(subjects.indexOf("Матанализ") < subjects.indexOf("Без предмета"));
  assert.ok(subjects.includes("2 занятия"));

  const plain = renderToStaticMarkup(h(LessonList, { lessons: [lesson()], onOpen: () => undefined }));
  assert.ok(plain.includes("19.10 · лекция · Пределы"));
  assert.ok(!plain.includes("Указать предмет"));
  const none = renderToStaticMarkup(h(LessonList, { lessons: [lesson()], onOpen: () => undefined, onEdit: () => undefined }));
  assert.ok(none.includes("Указать предмет"));

  const marks = renderToStaticMarkup(h(ReadyMarks, { lesson: lesson() }));
  assert.ok(marks.includes("Текст лекции<span class=\"vh\">: нет</span>"));
  assert.ok(marks.includes("Кратко<span class=\"vh\">: готово</span>"));

  const form = renderToStaticMarkup(
    h(LessonInfoForm, {
      lesson: lesson({ subject: "Физика", subjectEdited: true }),
      save: async () => lesson(),
      onSaved: () => undefined,
      onClose: () => undefined,
    }),
  );
  assert.ok(form.includes('value="Физика"'));
  assert.ok(form.includes("Как в записи (лекция)"));
});

// Source guards: read the TypeScript sources, not the build.
const src = (rel) =>
  readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");

test("the knowledge base renders text only, talks only to the core and searches after a pause", () => {
  for (const rel of [
    "bindings/knowledgeBase.ts",
    "knowledgeView.ts",
    "pages/KnowledgeParts.tsx",
    "pages/KnowledgeBasePage.tsx",
    "pages/KnowledgeLessonPage.tsx",
    "pages/TranscriptPage.tsx",
  ]) {
    const text = src(rel);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "href=", "window.open", "localStorage", "fetch(",
      "navigator.clipboard", "marked"]) {
      assert.ok(!text.includes(banned), `${rel} uses ${banned}`);
    }
  }
  const page = src("pages/KnowledgeBasePage.tsx");
  // Not per keystroke: the typing effect waits for the pause; Enter searches at once.
  assert.match(page, /setTimeout\(\(\) => void runSearch\(query, false\), searchPauseMs\)/);
  assert.match(page, /e\.key === "Enter"[\s\S]*runSearch\(query, true\)/);
  // One call at a time: the page goes through the flight helpers.
  assert.match(page, /askSearch\(flight\.current, c\.query\)/);
  assert.match(page, /searchDone\(flight\.current\)/);
  assert.equal(page.match(/api\.search\(/g)?.length, 1);
  const lessonPage = src("pages/KnowledgeLessonPage.tsx");
  assert.match(lessonPage, /summaryFocus\(view\.summary, focus\.index\)/);
  assert.match(lessonPage, /<SummaryCard view=\{view\} focusSection=\{focusSection\} focusPoint=\{focusPoint\} \/>/);
  assert.match(src("App.tsx"), /case "knowledge":\s*return <KnowledgeBasePage/);
});
