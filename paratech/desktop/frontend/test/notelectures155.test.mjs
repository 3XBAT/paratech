// TASK-155 (ADR-0027): lectures known only from the student's notes, «Добавить конспект прошедшей пары», the labels of
// hits and citations. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  checkPastDate,
  citationOpening,
  citationPlace,
  dateCheckText,
  dateFullText,
  dateText,
  groupHits,
  hitFocus,
  hitOpening,
  hitPlace,
  isNoteOnly,
  lectureLabel,
  lecturesMeta,
  lessonsWithoutNotes,
  localDay,
  noFocus,
  noteLectureHeading,
  noteLectureMeta,
  noteLectureTexts,
  orderLectures,
  pastLesson,
  subjectMeta,
} from "../.test-build/knowledgeView.js";
import { isLecture, lectureKey, lectureOf, lectureTarget, readLectureNotes, undatedNoteId } from "../.test-build/ownNotesView.js";
import { AddPastNote, HitList, LessonList, SubjectList } from "../.test-build/pages/KnowledgeParts.js";
import { texts as ticketTexts, ticketRow } from "../.test-build/ticketsView.js";

const { renderToStaticMarkup } = server;

const note = (over = {}) => ({ id: "n1", title: "Ряды.docx", subjectId: "phys", createdAt: "2026-09-12T10:00:00Z", ...over });
const lesson = (over = {}) => ({
  key: "recording:r1",
  noteOnly: false,
  undated: false,
  anchor: "recording",
  scheduleLessonId: "",
  date: "",
  notes: [],
  recordingId: "r1",
  startedAt: "2026-10-19T12:00:00Z",
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
const dayLecture = (over = {}) =>
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
    librarySubjectId: "phys",
    librarySubjectState: "matched",
    ...over,
  });
const undatedLecture = (over = {}) =>
  dayLecture({ key: "note:n3", anchor: "undated", undated: true, date: "", startedAt: "", notes: [note({ id: "n3" })], ...over });
const lessonLecture = (over = {}) =>
  dayLecture({ key: "lesson:l7", anchor: "lesson", scheduleLessonId: "l7", date: "2026-09-05", ...over });
const hit = (over = {}) => ({
  recordingId: "r1",
  section: "transcript",
  index: 7,
  startMs: 725000,
  time: "12:05",
  before: "до ",
  match: "ряды",
  after: " после",
  noteId: "",
  materialId: "",
  locatorKind: "",
  locatorN: 0,
  label: "",
  lesson: lesson(),
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
    lesson: dayLecture(),
    ...over,
  });
const subject = (over = {}) => ({ key: "ID:phys", name: "Физика", lessons: 0, latest: "", noSubject: false, subjectId: "phys", ...over });

// ---- Labels ----

test("an own-note hit shows the core's label as plain text, other sections keep their names", () => {
  assert.equal(hitPlace(noteHit()), "ваш конспект · стр. 3");
  assert.equal(hitPlace(noteHit({ label: "ваш конспект · раздел „Ряды“" })), "ваш конспект · раздел „Ряды“");
  assert.equal(hitPlace(hit()), "Расшифровка · 12:05");
  const evil = "<img src=x onerror=alert(1)>";
  const html = renderToStaticMarkup(h(HitList, { result: { hits: [noteHit({ label: `ваш конспект · ${evil}` })], truncated: false }, onOpen: () => undefined }));
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("ваш конспект · &lt;img src=x onerror=alert(1)&gt;"));
});

test("hits of two note-only lectures are not merged, the core's order is kept", () => {
  const other = dayLecture({ key: "day:phys:2026-09-05", date: "2026-09-05" });
  const groups = groupHits([noteHit(), noteHit({ lesson: other }), noteHit({ index: 3 })]);
  assert.deepEqual(groups.map((g) => [g.lesson.key, g.hits.length]), [["day:phys:2026-09-12", 2], ["day:phys:2026-09-05", 1]]);
  // The order inside a group is the binding's: the page never re-sorts.
  assert.deepEqual(groups[0].hits.map((x) => x.index), [2, 3]);
});

test("a hit in the notes opens the notes, never a recording tab or a recording-only binding", () => {
  const o = hitOpening(noteHit());
  assert.equal(o.lesson.key, "day:phys:2026-09-12");
  assert.deepEqual(o.focus, noFocus);
  assert.equal(o.materialId, "m1");
  assert.ok(isNoteOnly(o.lesson));
  assert.deepEqual(hitFocus(noteHit()), noFocus);
  // A transcript hit keeps its tab and marks no file.
  assert.deepEqual(hitOpening(hit()).focus, { tab: "transcript", index: 7 });
  assert.equal(hitOpening(hit()).materialId, "");
  // A note hit of a recorded lecture opens that recording's page (its block shows the note).
  assert.ok(!isNoteOnly(hitOpening(noteHit({ recordingId: "r1", lesson: lesson({ notes: [note()] }) })).lesson));
  // The opening is by ids: the lecture the block gets holds no path.
  assert.deepEqual(lectureOf(o.lesson), { kind: "day", date: "2026-09-12", subjectId: "phys" });
});

test("lines of lectures: a recording as before, a note-only lecture by day or «без даты» with the first note's title", () => {
  assert.equal(lectureLabel(lesson()), "19.10 · лекция · Пределы");
  assert.equal(lectureLabel(dayLecture()), "12.09 · Ряды.docx");
  assert.equal(lectureLabel(dayLecture({ notes: [note(), note({ id: "n2" }), note({ id: "n9" })] })), "12.09 · Ряды.docx и ещё 2");
  assert.equal(lectureLabel(undatedLecture()), "без даты · Ряды.docx");
  assert.equal(lectureLabel(lessonLecture()), "05.09 · Ряды.docx");
  assert.equal(lectureLabel(dayLecture({ notes: [note({ title: " " })] })), "12.09 · Без названия");
  assert.equal(dateText("2026-09-12"), "12.09");
  assert.equal(dateFullText("2026-09-12"), "12.09.2026");
  assert.equal(dateText("12.09.2026"), "");
  assert.equal(noteLectureHeading(dayLecture()), "Пара 12.09.2026");
  assert.equal(noteLectureHeading(undatedLecture()), "Пара без даты");
  assert.equal(noteLectureMeta(dayLecture({ subject: "Физика" })), "Физика · ваш конспект");
});

test("the list shows the chip «ваш конспект» on note-only lectures and the readiness marks on recordings", () => {
  const html = renderToStaticMarkup(h(LessonList, { lessons: [lesson(), dayLecture(), undatedLecture()], onOpen: () => undefined, onEdit: () => undefined }));
  assert.equal(html.split("ваш конспект").length - 1, 2);
  assert.ok(html.includes("Расшифровка"));
  // A note-only lecture has no recording whose subject could be corrected.
  assert.equal(html.split("Указать предмет").length - 1, 1);
  const evil = "<script>x</script>.docx";
  const e = renderToStaticMarkup(h(LessonList, { lessons: [dayLecture({ notes: [note({ title: evil })] })], onOpen: () => undefined }));
  assert.ok(!e.includes("<script>"));
  assert.ok(e.includes("&lt;script&gt;"));
});

test("a subject known only from notes is listed with the chip text instead of «0 занятий»", () => {
  assert.equal(subjectMeta(subject()), "ваш конспект");
  const html = renderToStaticMarkup(h(SubjectList, { subjects: [subject()], onOpen: () => undefined }));
  assert.ok(html.includes("Физика"));
  assert.ok(!html.includes("0 занятий"));
});

// ---- Ordering ----

test("lectures are by date newest first, «без даты» last, equal dates keep the core's order", () => {
  const a = lesson({ key: "a", startedAt: "2026-10-19T12:00:00Z" });
  const b = dayLecture({ key: "b", startedAt: "2026-09-11T21:00:00Z" });
  const c = lessonLecture({ key: "c", startedAt: "2026-09-05T08:00:00Z" });
  const u1 = undatedLecture({ key: "u1" });
  const u2 = undatedLecture({ key: "u2" });
  const same1 = lesson({ key: "s1", startedAt: "2026-10-01T10:00:00Z" });
  const same2 = dayLecture({ key: "s2", startedAt: "2026-10-01T10:00:00Z" });
  const bad = lesson({ key: "bad", startedAt: "not a date" });
  const out = orderLectures([u1, c, bad, same1, b, u2, same2, a]);
  assert.deepEqual(out.map((l) => l.key), ["a", "s1", "s2", "b", "c", "u1", "bad", "u2"]);
  assert.deepEqual(orderLectures([]), []);
  // The input is not changed.
  assert.equal(out.length, 8);
});

test("the heading counts recorded lessons and takes the newest start from them", () => {
  assert.deepEqual(lecturesMeta([dayLecture(), lesson({ startedAt: "2026-10-19T12:00:00Z" }), lesson({ key: "x" })]), {
    lessons: 2,
    latest: "2026-10-19T12:00:00Z",
  });
  assert.deepEqual(lecturesMeta([dayLecture()]), { lessons: 0, latest: "" });
});

// ---- The add flow ----

test("recorded lessons without notes are offered; ones with notes and note-only ones are not", () => {
  const withNotes = lesson({ key: "recording:r2", recordingId: "r2", notes: [note()] });
  const free = lesson({ key: "recording:r3", recordingId: "r3" });
  assert.deepEqual(lessonsWithoutNotes([lesson(), withNotes, dayLecture(), free]).map((l) => l.recordingId), ["r1", "r3"]);
});

test("the choice builds the lecture and the target by ids: recording, day, undated", () => {
  const s = subject();
  const rec = pastLesson(s, { kind: "recording", lesson: lesson({ recordingId: "r9", key: "recording:r9" }) });
  assert.deepEqual(lectureOf(rec), { kind: "recording", recordingId: "r9" });
  assert.deepEqual(lectureTarget(lectureOf(rec), "phys"), {
    noteId: "", subjectId: "phys", kind: "recording", recordingId: "r9", lessonId: "", date: "",
  });

  const day = pastLesson(s, { kind: "day", date: "2026-09-12" });
  assert.ok(isNoteOnly(day));
  assert.equal(day.librarySubjectId, "phys");
  assert.equal(day.librarySubjectState, "matched");
  assert.deepEqual(lectureOf(day), { kind: "day", date: "2026-09-12", subjectId: "phys" });
  assert.deepEqual(lectureTarget(lectureOf(day), "phys"), {
    noteId: "", subjectId: "phys", kind: "day", recordingId: "", lessonId: "", date: "2026-09-12",
  });

  const un = pastLesson(s, { kind: "undated" });
  assert.equal(un.undated, true);
  assert.deepEqual(lectureOf(un), { kind: "undated", subjectId: "phys", noteId: "" });
  assert.deepEqual(lectureTarget(lectureOf(un), "phys"), {
    noteId: "", subjectId: "phys", kind: "undated", recordingId: "", lessonId: "", date: "",
  });
  assert.notEqual(day.key, un.key);
  assert.equal(noteLectureHeading(un), "Пара без даты");
});

test("an existing lecture is opened by its anchor: lesson, day, undated", async () => {
  assert.deepEqual(lectureOf(lessonLecture()), { kind: "lesson", lessonId: "l7", subjectId: "phys" });
  assert.deepEqual(lectureTarget(lectureOf(lessonLecture()), "phys"), {
    noteId: "", subjectId: "phys", kind: "lesson", recordingId: "", lessonId: "l7", date: "",
  });
  const un = lectureOf(undatedLecture());
  assert.deepEqual(un, { kind: "undated", subjectId: "phys", noteId: "n3" });
  assert.equal(undatedNoteId(un, []), "n3");
  assert.equal(undatedNoteId(un, [{ id: "n3", removed: true }, { id: "n4", removed: false }]), "n4");
  assert.equal(undatedNoteId({ kind: "day", date: "2026-09-12", subjectId: "phys" }, [{ id: "n1", removed: false }]), "");
  assert.equal(lectureTarget(un, "phys", undatedNoteId(un, [])).noteId, "n3");
  // The subject of a note-only lecture without a matched library subject is the one its note names.
  assert.equal(lectureOf(dayLecture({ librarySubjectId: "", librarySubjectState: "no-subject" })).subjectId, "phys");

  const calls = [];
  const api = {
    forLesson: async (q) => (calls.push(["forLesson", q]), [{ id: "x", subjectId: "phys" }]),
    forSubject: async (id) => (calls.push(["forSubject", id]), [{ id: "n3" }, { id: "n4" }]),
  };
  const none = { recordingId: "", lessonId: "", subjectId: "", date: "" };
  await readLectureNotes(api, { kind: "recording", recordingId: "r1" });
  await readLectureNotes(api, { kind: "lesson", lessonId: "l7", subjectId: "phys" });
  await readLectureNotes(api, { kind: "day", date: "2026-09-12", subjectId: "phys" });
  assert.deepEqual((await readLectureNotes(api, un)).map((n) => n.id), ["n3"]);
  assert.deepEqual(await readLectureNotes(api, { kind: "undated", subjectId: "phys", noteId: "" }), []);
  assert.deepEqual(calls, [
    ["forLesson", { ...none, recordingId: "r1" }],
    ["forLesson", { ...none, lessonId: "l7" }],
    ["forLesson", { ...none, subjectId: "phys", date: "2026-09-12" }],
    ["forSubject", "phys"],
  ]);
});

test("a drop event belongs to the lecture it names", () => {
  const t = (over) => ({ noteId: "", subjectId: "phys", kind: "day", recordingId: "", lessonId: "", date: "2026-09-12", ...over });
  assert.ok(isLecture({ kind: "recording", recordingId: "r1" }, t({ kind: "recording", recordingId: "r1" })));
  assert.ok(!isLecture({ kind: "recording", recordingId: "r1" }, t({ kind: "recording", recordingId: "r2" })));
  assert.ok(isLecture({ kind: "day", date: "2026-09-12", subjectId: "phys" }, t()));
  assert.ok(!isLecture({ kind: "day", date: "2026-09-13", subjectId: "phys" }, t()));
  assert.ok(isLecture({ kind: "lesson", lessonId: "l7", subjectId: "phys" }, t({ kind: "lesson", lessonId: "l7" })));
  assert.ok(isLecture({ kind: "undated", subjectId: "phys", noteId: "n3" }, t({ kind: "undated", noteId: "n3" })));
  assert.ok(!isLecture({ kind: "undated", subjectId: "phys", noteId: "n3" }, t({ kind: "undated", noteId: "n4" })));
  assert.notEqual(lectureKey({ kind: "day", date: "2026-09-12", subjectId: "phys" }), lectureKey({ kind: "undated", subjectId: "phys", noteId: "" }));
});

test("the date of a past pair is a real day, not after today", () => {
  const today = "2026-10-27";
  assert.deepEqual(checkPastDate("2026-10-27", today), { kind: "ok", date: "2026-10-27" });
  assert.deepEqual(checkPastDate("2026-09-12", today), { kind: "ok", date: "2026-09-12" });
  assert.equal(checkPastDate("2026-10-28", today).kind, "future");
  assert.equal(checkPastDate("", today).kind, "empty");
  assert.equal(checkPastDate("2026-02-30", today).kind, "invalid");
  assert.equal(checkPastDate("12.09.2026", today).kind, "invalid");
  assert.equal(dateCheckText({ kind: "future" }), "Дата не может быть в будущем.");
  assert.equal(dateCheckText({ kind: "ok", date: today }), "");
  assert.equal(localDay(new Date(2026, 0, 5, 23, 59)), "2026-01-05");
});

test("«Добавить конспект прошедшей пары» is off without a library subject and says why", () => {
  const off = renderToStaticMarkup(h(AddPastNote, { subject: subject({ subjectId: "" }), lectures: [], onChoose: () => undefined }));
  assert.ok(off.includes(noteLectureTexts.addPast));
  assert.match(off, /<button[^>]*disabled/);
  assert.ok(off.includes("Предмет занятия не найден в библиотеке"));
  const noSubj = renderToStaticMarkup(h(AddPastNote, { subject: subject({ subjectId: "", noSubject: true }), lectures: [], onChoose: () => undefined }));
  assert.ok(noSubj.includes("не найден предмет в библиотеке"));
  const on = renderToStaticMarkup(h(AddPastNote, { subject: subject(), lectures: [lesson()], onChoose: () => undefined, today: "2026-10-27" }));
  assert.doesNotMatch(on, /<button[^>]*disabled/);
});

// ---- Citations ----

const source = (over = {}) => ({
  section: "ownNote",
  recordingId: "",
  index: 2,
  time: "",
  noteId: "n1",
  materialId: "m1",
  locatorKind: "page",
  locatorN: 3,
  label: "ваш конспект · стр. 3",
  removed: false,
  ...over,
});

test("a citation of the notes reads as the binding's label and opens by note and file ids", () => {
  assert.equal(citationPlace(source()), "ваш конспект · стр. 3");
  assert.deepEqual(citationOpening(source()), { kind: "note", noteId: "n1", materialId: "m1", locatorKind: "page", locatorN: 3 });
  assert.equal(citationPlace(source({ section: "transcript", recordingId: "r1", noteId: "", materialId: "", locatorKind: "", locatorN: 0, label: "", time: "12:05" })), "Расшифровка · 12:05");
  assert.deepEqual(citationOpening(source({ section: "summary", recordingId: "r1", index: 1 })), {
    kind: "recording", recordingId: "r1", focus: { tab: "summary", index: 1 },
  });
});

test("a removed source shows the binding's text and does not open", () => {
  const gone = source({ removed: true, label: "источник удалён" });
  assert.equal(citationPlace(gone), "источник удалён");
  assert.equal(citationOpening(gone), null);
  // Only when the binding sends no label does the one fallback text stand in.
  assert.equal(citationPlace(source({ removed: true, label: "" })), noteLectureTexts.sourceRemoved);
  assert.equal(citationOpening(source({ noteId: "" })), null);
  assert.equal(citationOpening(source({ section: "transcript", recordingId: "" })), null);
});

// ---- Tickets and source guards ----

test("the not-covered hint of a ticket is shown as it is", () => {
  const set = { status: "done", tickets: [] };
  const row = ticketRow({ number: 1, title: "T", answered: false, coverage: "not_covered", answerId: "" }, set);
  assert.equal(row.chip.hint, ticketTexts.notCoveredHint);
  const src = readFileSync(new URL("../src/pages/TicketsParts.tsx", import.meta.url), "utf8");
  assert.ok(src.includes("title={r.chip.hint || undefined}"));
});

test("the note-only page reads nothing by a recording id and renders no HTML", () => {
  const src = readFileSync(new URL("../src/pages/KnowledgeLessonPage.tsx", import.meta.url), "utf8");
  const start = src.indexOf("export function NoteLecturePage");
  const end = src.indexOf("function LectureTab");
  const body = src.slice(start, end);
  assert.ok(start > 0 && end > start);
  for (const banned of ["summary.view", "recordingId", "api.lesson", "TranscriptPage", "useOpenSummary", "dangerouslySetInnerHTML"]) {
    assert.ok(!body.includes(banned), banned);
  }
  const page = readFileSync(new URL("../src/pages/KnowledgeBasePage.tsx", import.meta.url), "utf8");
  assert.match(page, /isNoteOnly\(open\.lesson\)[\s\S]*?<NoteLecturePage/);
  assert.match(page, /api\.lectures\(subject\.key\)/);
  for (const f of ["knowledgeView.ts", "pages/KnowledgeParts.tsx", "pages/KnowledgeBasePage.tsx"]) {
    const s = readFileSync(new URL(`../src/${f}`, import.meta.url), "utf8");
    assert.ok(!s.includes("dangerouslySetInnerHTML") && !s.includes("innerHTML"), f);
  }
});
