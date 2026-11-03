// TASK-352: every «Открыть конспект / расшифровку» leads to the one lesson screen. The helpers of the entry points (tab
// and place), the pair rows of «Сегодня» (open / «Записать» / plain), a lesson shown once, the clickable rows of a
// subject, the links of an assignment, the pills and sources of the agent, the by-key load and the return after a
// deletion. Pages have no DOM harness: their wiring is checked in the source text, the rest on the compiled modules.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { errorMessages, knowledgeBaseApi, methods } from "../.test-build/bindings/knowledgeBase.js";
import {
  citationOpening,
  errorText,
  findPairLecture,
  hitFocus,
  noFocus,
  noteJumpTexts,
  openingRouteTab,
  recordingLessonKey,
  recordingLessonOpening,
  resolveOpening,
  runJump,
  jumpTicket,
} from "../.test-build/knowledgeView.js";
import * as nav from "../.test-build/nav.js";
import { closeLesson, routeToLesson, routeToPage, routeToSubject } from "../.test-build/nav.js";
import { mentionOpening, mentionPills } from "../.test-build/agentMentions.js";
import { rowActions, rowItemTab, rowLessonTab } from "../.test-build/recordingRowView.js";
import { lessonOpen, lessonLine, pairIsOver, pairRecordings, postLine, summaryRows, texts } from "../.test-build/todayView.js";
import { LessonsSection, SummarySection } from "../.test-build/pages/TodayParts.js";
import { OverviewSections } from "../.test-build/pages/SubjectsParts.js";
import { openPairTexts } from "../.test-build/pages/TranscriptLinks.js";

const src = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const at = (d, hh = 0, mm = 0) => new Date(2026, 9, d, hh, mm).toISOString();
const clock = (d, hh, mm) => new Date(2026, 9, d, hh, mm);

// ---- the helper every entry point uses ----

test("a link into a recorded lesson names the key, the tab and the place as the old links did", () => {
  assert.deepEqual(recordingLessonOpening("r1"), { key: "recording:r1", focus: noFocus, materialId: "", editing: false });
  assert.equal(recordingLessonKey("r1"), "recording:r1");
  // «Открыть текст лекции» / «Открыть конспект» / «Открыть расшифровку»: the tab alone, the top of it.
  for (const tab of ["lecture", "summary", "transcript"]) {
    const o = recordingLessonOpening("r1", tab);
    assert.deepEqual(o.focus, { tab, index: -1 });
    assert.equal(openingRouteTab(o.focus, o.materialId), tab, "the route carries the tab");
  }
  // A source of the agent: the block, the segment, the section of the summary.
  const block = recordingLessonOpening("r1", "lecture", 4);
  assert.deepEqual(block.focus, { tab: "lecture", index: 4 });
  assert.equal(openingRouteTab(block.focus, ""), "lecture");
  // Without a tab the screen chooses (its opening tab): the route has none.
  assert.equal(openingRouteTab(recordingLessonOpening("r1").focus, ""), "");
});

test("the route of a lesson from any page: it opens over that page with the tab, and closing returns to the page", () => {
  for (const page of ["today", "recordings", "subjects", "homework", "knowledge"]) {
    const o = recordingLessonOpening("r1", "transcript", 7);
    const r = routeToLesson(routeToPage(page), o.key, openingRouteTab(o.focus, o.materialId));
    assert.deepEqual(r, { page, assignment: "", subject: "", lesson: "recording:r1", tab: "transcript" });
    assert.deepEqual(closeLesson(r), routeToPage(page), `back to ${page}`);
  }
  const under = routeToSubject(routeToPage("subjects"), "s1");
  assert.deepEqual(closeLesson(routeToLesson(under, "recording:r1", "summary")), under, "the subject under the lesson stays");
});

test("the old overlay routes and their openers are gone", () => {
  for (const name of ["routeToSummary", "routeToTranscript", "closeTranscript"]) {
    assert.equal(name in nav, false, name);
  }
  for (const rel of ["openSummary.ts", "openTranscript.ts", "pages/SummaryPage.tsx"]) {
    assert.equal(existsSync(new URL(`../src/${rel}`, import.meta.url)), false, rel);
  }
  assert.equal(existsSync(new URL("../src/openLesson.ts", import.meta.url)), true);
  for (const rel of ["App.tsx", "pages/TodayPage.tsx", "pages/TodayParts.tsx", "pages/RecordingsPage.tsx", "pages/RecordingRow.tsx", "pages/TranscriptLinks.tsx"]) {
    assert.doesNotMatch(src(rel), /useOpenSummary|useOpenTranscript|OpenSummaryContext|OpenTranscriptContext|openSummary|openTranscript/, rel);
  }
});

// ---- «Сегодня»: the pair row ----

const lesson = (over = {}) => ({ id: "l1", title: "Матанализ", subject: "Матанализ", kind: "lecture", teacher: "", start: at(21, 10, 30), end: at(21, 11, 50), post: null, ...over });
const post = (over = {}) => ({
  recordingId: "r1", transcriptId: "t1",
  lesson: { lessonId: "l1", title: "Матанализ", subject: "Матанализ", subjectId: "s1", subjectName: "Матанализ", kind: "lecture", start: at(21, 10, 30) },
  headline: "Конспект готов", summaryReady: true, homework: 1, homeworkDone: 0, earliestDue: at(24, 23, 59), calendar: [], shownAt: at(21, 11, 52), ...over,
});
const rec = (over = {}) => ({ id: "rec1", lessonId: "l1", stage: "done", errorCode: "", progressPercent: 0, ...over });

test("a pair row opens the lesson of the recording its post is about, else of its newest saved recording", () => {
  const now = clock(21, 13, 0);
  assert.deepEqual(lessonOpen(lesson({ post: post() }), [], now), { kind: "open", recordingId: "r1" });
  assert.deepEqual(lessonOpen(lesson(), [rec({ id: "new" }), rec({ id: "old" })], now), { kind: "open", recordingId: "new" });
  // The summary is still being made: the row opens all the same (the lesson screen says where it stands).
  assert.deepEqual(lessonOpen(lesson(), [rec({ id: "x", stage: "summarizing" })], now), { kind: "open", recordingId: "x" });
  assert.deepEqual(lessonOpen(lesson(), [rec({ id: "x", stage: "transcribing" })], now), { kind: "open", recordingId: "x" });
  // Another pair's recording is not this pair's.
  assert.deepEqual(lessonOpen(lesson(), [rec({ lessonId: "l2" })], now), { kind: "none" });
});

test("a recording that runs or is being saved has nothing to open yet; the live chip says where it stands", () => {
  const now = clock(21, 11, 0);
  for (const stage of ["recording", "saving"]) {
    assert.deepEqual(lessonOpen(lesson({ post: post() }), [rec({ stage })], now), { kind: "none" }, stage);
    assert.deepEqual(lessonOpen(lesson(), [rec({ stage })], now), { kind: "none" }, stage);
  }
});

test("a pair before or during its time without a recording offers «Записать»; a finished one is a plain row", () => {
  const before = clock(21, 9, 0);
  const during = clock(21, 11, 0);
  const after = clock(21, 12, 30);
  assert.deepEqual(lessonOpen(lesson(), [], before), { kind: "record" });
  assert.deepEqual(lessonOpen(lesson(), [], during), { kind: "record" });
  assert.deepEqual(lessonOpen(lesson(), [], after), { kind: "none" });
  // A failed recording is no recording: the pair can be recorded again while it lasts.
  assert.deepEqual(lessonOpen(lesson(), [rec({ stage: "failed", errorCode: "recordingFailed" })], during), { kind: "record" });
  assert.deepEqual(lessonOpen(lesson(), [rec({ stage: "failed", errorCode: "recordingFailed" })], after), { kind: "none" });
  // The list of recordings has not been read yet: no offer on a guess.
  assert.deepEqual(lessonOpen(lesson(), [], during, false), { kind: "none" });
  // A pair without an id cannot be matched to anything.
  assert.deepEqual(lessonOpen(lesson({ id: "" }), [], during), { kind: "none" });
});

test("a pair without an end lasts a pair (90 minutes) for «during»; an unreadable start is not over", () => {
  assert.equal(pairIsOver({ start: at(21, 10, 30), end: "" }, clock(21, 11, 59)), false);
  assert.equal(pairIsOver({ start: at(21, 10, 30), end: "" }, clock(21, 12, 1)), true);
  assert.equal(pairIsOver({ start: at(21, 10, 30), end: at(21, 11, 50) }, clock(21, 11, 50)), true);
  assert.equal(pairIsOver({ start: "", end: "" }, clock(21, 12, 0)), false);
});

test("the pair row: the title is the open button stretched over the row; «Записать» is a separate button; no nested buttons", () => {
  const open = html(h(LessonsSection, { lessons: [lesson({ post: post() })], recordings: [], now: clock(21, 13, 0), onOpenLesson: noop, onRecord: noop }));
  assert.match(open, /class="today-row today-lesson row-openable"/);
  assert.match(open, /<p class="today-t"><button type="button" class="row-open" title="Открыть пару">Матанализ<\/button><\/p>/);
  assert.equal((open.match(/<button/g) ?? []).length, 1, "the row's one control is its title; the old «Открыть» button is gone");

  const record = html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 10, 0), onOpenLesson: noop, onRecord: noop }));
  assert.doesNotMatch(record, /row-open|row-openable/, "nothing to open");
  assert.match(record, /<button[^>]*aria-label="Записать: Матанализ"[^>]*>.*Записать<\/button>/);
  assert.doesNotMatch(html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 10, 0), onOpenLesson: noop })), /Записать/, "no handler, no button");
  // Not yet read: neither.
  assert.doesNotMatch(html(h(LessonsSection, { lessons: [lesson()], recordings: [], recordingsKnown: false, now: clock(21, 10, 0), onOpenLesson: noop, onRecord: noop })), /Записать/);

  const past = html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 15, 0), onOpenLesson: noop, onRecord: noop }));
  assert.doesNotMatch(past, /<button|row-open/, "a finished pair without a recording is a plain row");

  // The problem chip of a failed recording and the title button are siblings, never nested.
  const failed = rec({ stage: "failed", errorCode: "transcriptionFailed", status: "COMPLETED", startedAt: at(21, 10, 30) });
  const both = html(h(LessonsSection, { lessons: [lesson()], recordings: [failed], day: at(21), now: clock(21, 13, 0), onOpenLesson: noop, onOpenRecordings: noop }));
  assert.match(both, /class="row-open"/);
  assert.match(both, /<button[^>]*today-problem/);
  assert.doesNotMatch(both, /<button(?:(?!<\/button>)[^])*<button/);
  // The title is text, never markup.
  const evil = html(h(LessonsSection, { lessons: [lesson({ title: "<img src=x onerror=1>", post: post() })], recordings: [], now: clock(21, 13, 0), onOpenLesson: noop }));
  assert.match(evil, /&lt;img src=x onerror=1&gt;/);
  assert.doesNotMatch(evil, /<img/);
});

test("the row is keyboard-operable and its inner controls sit above the stretched button", () => {
  const css = src("styles.css");
  assert.match(css, /\.row-open::after \{ content: ""; position: absolute; inset: 0; \}/);
  assert.match(css, /button\.row-open:not\(\.btn\) \{[^}]*cursor: pointer/);
  // The ring is drawn around the row, and the pressed «Открыть» of a recording does not shrink the stretched area.
  assert.match(css, /\.row-open:focus-visible::after \{ outline: 2px solid var\(--accent\)/);
  assert.match(css, /\.btn\.row-open:active:enabled \{ transform: none; \}/);
  assert.match(css, /\.row-openable :is\(\.today-problem, \.today-st \.chip\) \{ position: relative; z-index: 1; \}/);
  assert.match(css, /\.row:has\(\.row-open\) :is\(\.row-lead-box, \.st \.chip, \.st \.btn, \.row-below\)/);
  // Today: the line of the page, in one place.
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /onOpenLesson=\{\(recordingId\) => openLesson\(recordingId\)\}\s+onRecord=\{props\.onRecord\}/);
  assert.match(page, /recordingsKnown=\{recordings !== null\}/);
});

test("the lesson line carries what the row does, from the same clock the row reads", () => {
  assert.deepEqual(lessonLine(lesson({ post: post() }), [], at(21), clock(21, 13, 0)).open, { kind: "open", recordingId: "r1" });
  assert.deepEqual(lessonLine(lesson(), [], at(21), clock(21, 10, 0)).open, { kind: "record" });
  assert.equal(texts.record, "Записать");
  assert.equal(texts.openLesson, "Открыть пару");
});

// ---- «Сегодня»: a lesson appears once ----

test("«Итоги дня» does not repeat a pair that has its row; a post with a question of the agent stays without «Открыть»", () => {
  const lessons = [lesson({ post: post({ recordingId: "r1" }) })];
  assert.deepEqual([...pairRecordings(lessons)], ["r1"]);
  const dn = (id) => ({ recordingId: id, lesson: post().lesson, summaryReady: true, homework: 0, at: at(21, 12, 0) });
  const posts = [post({ recordingId: "r1" }), post({ recordingId: "r2", headline: "Другая пара" })];
  const rows = summaryRows(posts, [dn("r1"), dn("r3")], lessons);
  assert.deepEqual(rows.posts.map((p) => p.recordingId), ["r2"], "the post of a pair row is dropped");
  assert.deepEqual(rows.done.map((d) => d.recordingId), ["r3"], "so is its done line");
  assert.equal(rows.count, 2);
  // Without the pairs of the day nothing changes (the old behaviour).
  assert.equal(summaryRows(posts, [dn("r1"), dn("r3")]).count, 3);

  // A question of the agent (the calendar) has no other place: the post stays, and the pair row above opens the lesson.
  const asking = summaryRows(posts, [], lessons, (p) => p.recordingId === "r1");
  assert.deepEqual(asking.posts.map((p) => p.recordingId), ["r1", "r2"]);
  assert.equal(postLine(post({ recordingId: "r1" }), undefined, asking.onDay).canOpen, false);
  assert.equal(postLine(post({ recordingId: "r2" }), undefined, asking.onDay).canOpen, true);
  assert.equal(postLine(post({ recordingId: "r1" })).canOpen, true);
});

test("the screen of the day shows one lesson once: its row opens it, «Итоги дня» keeps only the rest", () => {
  const lessons = [lesson({ post: post({ recordingId: "r1", headline: "Пост пары" }) })];
  const out = html(
    h(SummarySection, {
      posts: [post({ recordingId: "r1", headline: "Пост пары" }), post({ recordingId: "r2", headline: "Пост другой" })],
      done: [],
      lessons,
      onOpenLesson: noop,
    }),
  );
  assert.doesNotMatch(out, /Пост пары/);
  assert.match(out, /Пост другой/);
  assert.equal((out.match(/>Открыть</g) ?? []).length, 1);
  // Only that pair: nothing is left to say, the section is hidden.
  assert.equal(html(h(SummarySection, { posts: [post({ recordingId: "r1" })], done: [], lessons, onOpenLesson: noop })), "");
  // The page passes the lessons of the day.
  assert.match(src("pages/TodayPage.tsx"), /lessons=\{screen\.lessons\}/);
});

// ---- «Записи»: the row ----

test("a recording row opens its lesson on the tab of its main text; «⋯» holds the other tabs; one open per row", () => {
  const row = { stage: "done", step: "", errorCode: "", lectureTextReady: false, stoppable: false, action: "", actionJobId: "", retryWork: "", canRedoSummary: false };
  const a = rowActions(row, { texts: true, redo: false });
  assert.deepEqual([a.main.key, a.main.label, a.menu.map((i) => i.key)], ["summary", "Открыть", ["transcript"]]);
  assert.equal(rowItemTab(a.main.key), "summary");
  assert.deepEqual(a.menu.map((i) => rowItemTab(i.key)), ["transcript"]);
  const lecture = rowActions({ ...row, lectureTextReady: true }, { texts: true, redo: false });
  assert.deepEqual([lecture.main.key, lecture.main.label], ["summary", "Открыть"]);
  assert.deepEqual(lecture.menu.map((i) => rowItemTab(i.key)), ["lecture", "transcript"]);
  const running = rowActions({ ...row, stage: "summarizing", lectureTextReady: true }, { texts: true, redo: false });
  assert.deepEqual([rowItemTab(running.main.key), running.main.label], ["lecture", "Открыть"]);
  // The items that open no text have no tab.
  for (const key of ["stop", "runNow", "open", "redo"]) {
    assert.equal(rowItemTab(key), null, key);
  }
  // Stop and «Запустить сейчас» keep the main place; the texts then stay in «⋯» by name (the lesson is not ready).
  const stopping = rowActions({ ...row, stage: "recording", stoppable: true }, { texts: true, redo: false });
  assert.equal(stopping.main.key, "stop");
});

test("the tab a recording's lesson link opens: the lecture text, else the summary, else the transcript, else none", () => {
  const r = (over) => ({ stage: "done", step: "", errorCode: "", lectureTextReady: false, ...over });
  assert.equal(rowLessonTab(r({ lectureTextReady: true })), "lecture");
  assert.equal(rowLessonTab(r()), "summary");
  assert.equal(rowLessonTab(r({ stage: "lectureText", step: "lectureText" })), "summary");
  assert.equal(rowLessonTab(r({ stage: "summarizing" })), "transcript", "the summary is not there yet");
  assert.equal(rowLessonTab(r({ stage: "transcribing" })), "transcript");
  assert.equal(rowLessonTab(r({ stage: "failed", errorCode: "summaryFailed" })), "transcript");
  assert.equal(rowLessonTab(r({ stage: "failed", errorCode: "homeworkFailed" })), "summary");
  assert.equal(rowLessonTab(r({ stage: "recording" })), null);
  assert.equal(rowLessonTab(r({ stage: "failed", errorCode: "recordingFailed" })), null);
});

// ---- a subject: the rows of «Пары» ----

const overview = {
  subjectId: "s1", name: "Матанализ", openedAt: "", deadlines: [], schedule: [], newMaterials: [],
  lessons: [
    { recordingId: "r1", title: "Пределы", kind: "lecture", startedAt: at(21, 10, 30), durationSeconds: 2700, status: "summaryReady", summaryNew: true },
    { recordingId: "r2", title: "<b>Ряды</b>", kind: "lecture", startedAt: at(14, 10, 30), durationSeconds: 0, status: "recorded", summaryNew: false },
  ],
};

test("the lessons of a subject are rows that open the lesson, with the old chips inside; they stay plain without a handler", () => {
  const out = html(h(OverviewSections, { overview, onOpenLesson: noop }));
  const buttons = out.match(/<button[^>]*drow-open[^>]*>/g) ?? [];
  assert.equal(buttons.length, 2, "one whole-row button per lesson");
  assert.match(out, /<div class="drow-item" role="listitem"><button type="button" class="lrow drow drow-open" title="Открыть пару">/);
  assert.match(out, /Пределы/);
  assert.match(out, /Конспект готов/);
  assert.match(out, /новое/);
  // Spans only inside a button; the title is text.
  assert.doesNotMatch(out, /<button(?:(?!<\/button>)[^])*<(p|div)[ >]/);
  assert.match(out, /&lt;b&gt;Ряды&lt;\/b&gt;/);
  const plain = html(h(OverviewSections, { overview }));
  assert.doesNotMatch(plain, /<button/);
  assert.match(plain, /Пределы/);
  // The window passes the opener down to the page.
  assert.match(src("pages/SubjectsPage.tsx"), /<OverviewSections overview=\{overview\} onOpenAssignment=\{props\.onOpenAssignment\} onOpenLesson=\{props\.onOpenLesson\} \/>/);
  assert.match(src("App.tsx"), /onOpenLesson=\{props\.onOpenRecording\}/);
});

// ---- an assignment: the links of its lesson ----

test("an assignment offers one «Открыть пару» per recording of its lesson, on the right tab", () => {
  assert.equal(openPairTexts.one, "Открыть пару");
  assert.equal(openPairTexts.many("14:05"), "Открыть пару · 14:05");
  const links = src("pages/TranscriptLinks.tsx");
  assert.match(links, /const tab = rowLessonTab\(r\);/);
  assert.match(links, /onClick=\{\(\) => open\(l\.id, l\.tab\)\}/);
  assert.match(links, /links\.length === 1 \? openPairTexts\.one : openPairTexts\.many\(formatTime\(l\.startedAt\)\)/);
  assert.doesNotMatch(links, /Открыть конспект|Открыть расшифровку|Открыть текст лекции|Исходная расшифровка|transcriptApi|useOpenSummary/);
  assert.match(src("pages/HomeworkPage.tsx"), /<TranscriptLinks lessonId=\{a\.lessonId\} \/>/);
});

// ---- the agent: the pill «Пара» and the sources of an answer ----

test("the pill «Пара» opens the lesson of that pair; the sources of an answer open the lesson at their place", () => {
  assert.deepEqual(mentionOpening("lesson", "sched1"), { type: "pair", lessonId: "sched1" });
  const [pill] = mentionPills([{ kind: "lesson", id: "sched1", label: "Лекция 3" }]);
  assert.equal(pill.aria, "Открыть пару «Лекция 3»");
  assert.doesNotMatch(pill.title, /База знаний/);

  const app = src("App.tsx");
  // A recording's place opens its lesson at once, with the tab and the place of the source.
  assert.match(app, /if \(o\.kind === "recording"\) \{\s*openLessonKey\(recordingLessonKey\(o\.recordingId\), \{ focus: o\.focus, materialId: "", editing: false \}\);\s*return;\s*\}/);
  // A note or a pair is found by its ids first («База знаний» does it and takes the jump once).
  assert.match(app, /setSourceJump\(o\);\s*setRoute\(routeToPage\("knowledge"\)\);/);
  assert.match(app, /\} else if \(o\.type === "pair"\) \{\s*openSource\(\{ kind: "pair", lessonId: o\.lessonId \}\);/);

  // The sources of the answer card: a hit in the transcript, the lecture text, the summary, the title, the student's notes.
  const source = (section, index, extra = {}) => ({ section, recordingId: "r1", index, time: "", noteId: "", materialId: "", locatorKind: "", locatorN: 0, label: "", removed: false, ...extra });
  assert.deepEqual(citationOpening(source("transcript", 12)), { kind: "recording", recordingId: "r1", focus: { tab: "transcript", index: 12 } });
  assert.deepEqual(citationOpening(source("lectureText", 3)), { kind: "recording", recordingId: "r1", focus: { tab: "lecture", index: 3 } });
  assert.deepEqual(citationOpening(source("summary", 0)), { kind: "recording", recordingId: "r1", focus: { tab: "summary", index: 0 } });
  assert.deepEqual(hitFocus({ section: "title", index: 0 }), noFocus);
  const o = citationOpening(source("transcript", 12));
  const link = recordingLessonOpening(o.recordingId, o.focus.tab, o.focus.index);
  assert.deepEqual(link.focus, o.focus, "the helper and the window agree on the place");
});

test("a pair is found among the lectures by its schedule lesson, a recording or a lecture of notes alike; none says why", async () => {
  const lec = (over) => ({ key: "recording:r1", scheduleLessonId: "", notes: [], ...over });
  const lectures = {
    s1: [lec({ key: "recording:r1", scheduleLessonId: "sched0" }), lec({ key: "recording:r2", scheduleLessonId: "sched1" })],
    s2: [lec({ key: "lesson:sched2", scheduleLessonId: "sched2", noteOnly: true })],
  };
  const api = {
    subjects: async () => [{ key: "s1" }, { key: "s2" }],
    lectures: async (k) => lectures[k] ?? [],
    lesson: async () => {
      throw new Error("not used");
    },
  };
  assert.equal((await findPairLecture(api, "sched1")).key, "recording:r2");
  assert.equal((await findPairLecture(api, "sched2")).key, "lesson:sched2");
  assert.equal(await findPairLecture(api, "nope"), null);
  assert.equal(await findPairLecture(api, ""), null);
  const found = await resolveOpening(api, { kind: "pair", lessonId: "sched1" });
  assert.deepEqual([found.lesson.key, found.focus, found.materialId], ["recording:r2", noFocus, ""]);

  // The jump says why when the pair has nothing to open; the text is plain and names no id.
  const seen = [];
  await runJump(jumpTicket({ kind: "pair", lessonId: "nope" }), api, {
    started: () => seen.push("started"),
    opened: () => seen.push("opened"),
    failed: (t) => seen.push(t),
    settled: () => seen.push("settled"),
  });
  assert.deepEqual(seen, ["started", noteJumpTexts.pairNotFound, "settled"]);
  assert.equal(noteJumpTexts.pairNotFound, "У этой пары нет записи и конспекта: открывать нечего.");
});

// ---- the lesson screen: by key, and the deletion ----

test("lessonByKey calls the bound method with the key alone and parses any lesson, a lecture of notes included", async () => {
  assert.equal(methods.lessonByKey, "paratech/desktop/internal/input/wails.KnowledgeBaseService.LessonByKey");
  const noteOnly = {
    key: "day:s1:2026-09-12", noteOnly: true, undated: false, anchor: "day", scheduleLessonId: "", date: "2026-09-12",
    notes: [{ id: "n1", title: "Ряды.docx", subjectId: "s1", createdAt: "2026-09-12T08:00:00Z" }],
    recordingId: "", startedAt: "", durationSeconds: 0, title: "", subject: "Матанализ", subjectKey: "s1", kind: "",
    subjectEdited: false, kindEdited: false, transcriptReady: false, summaryReady: false, lectureTextReady: false,
    librarySubjectId: "s1", librarySubjectState: "matched",
  };
  const calls = [];
  const api = knowledgeBaseApi(async (m, ...args) => {
    calls.push([m, ...args]);
    return noteOnly;
  });
  const l = await api.lessonByKey("day:s1:2026-09-12");
  assert.deepEqual(calls, [[methods.lessonByKey, "day:s1:2026-09-12"]]);
  assert.equal(l.noteOnly, true, "a cold start into a lecture of notes");
  assert.equal(l.recordingId, "");
  assert.equal(l.notes[0].id, "n1");
  // A reply that is not a lesson is an error, never a default.
  await assert.rejects(knowledgeBaseApi(async () => ({ key: "x" })).lessonByKey("x"));
});

test("a refusal of the core shows its fixed text, anything else the generic one (the parity with Go is checked there)", () => {
  assert.equal(errorText(new Error(errorMessages.notFound)), errorMessages.notFound);
  assert.equal(errorText(errorMessages.invalidId), errorMessages.invalidId);
  assert.equal(errorText(new Error("open C:\\Users\\student\\db: locked")), errorMessages.unavailable);
  const go = readFileSync(new URL("../../internal/input/wails/knowledgebase.go", import.meta.url), "utf8");
  for (const text of [errorMessages.notFound, errorMessages.invalidId, errorMessages.unavailable]) {
    assert.ok(go.includes(`"${text}"`), text);
  }
});

test("the lesson screen loads a lesson that is not in the window's memory by its key, in one place", () => {
  const page = src("pages/KnowledgeLessonPage.tsx");
  assert.equal(page.split("api.lessonByKey(").length - 1, 1);
  assert.match(page, /if \(cached !== undefined\) \{\s*return;\s*\}\s*let stopped = false;\s*api\.lessonByKey\(key\)\.then\(/);
  // A lecture of notes loaded that way is drawn by the note-only page; a recorded one by the tabs.
  assert.match(page, /if \(isNoteOnly\(lesson\)\) \{\s*return <NoteLecturePage/);
  // The refusal: the text of the core and «Назад»; the screen shows no recording id or key.
  assert.match(page, /<LessonGone text=\{error\} onBack=\{props\.onBack\} \/>/);
  assert.doesNotMatch(page, /\{key\}|\$\{key\}|lessonKey\}/);
});

test("deleting a recording on the lesson screen returns to the section the lesson was opened from", () => {
  const page = src("pages/KnowledgeLessonPage.tsx");
  // The head «⋯» has the deletion; the dialog is the shared one; a deletion that removed something leaves the screen.
  assert.match(page, /\{ key: "delete", label: deleteTexts\.summaryButton \},/);
  assert.match(page, /<RecordingDeleteDialog\s+api=\{deletion\}\s+ids=\{\[id\]\}/);
  assert.match(page, /if \(res\.removed > 0\) \{\s*props\.onDeleted\(\);\s*\} else \{\s*setDeleteFailed\(deleteOutcome\(res, preview\)\);\s*\}/);
  // A refusal says why in the banner and the student stays.
  assert.match(page, /\{deleteFailed && \(\s*<Banner kind="fail" role="alert">\s*\{deleteFailed\.lines\.join\(" "\)\}/);
  // The window goes back by closeLesson (the page under the lesson), not to «Сегодня», and forgets the lesson.
  const app = src("App.tsx");
  assert.match(app, /const lessonDeleted = useCallback\(\(key: string\) => \{\s*lessons\.current\.delete\(key\);\s*setRoute\(closeLesson\);\s*setJump\(\(j\) => j \+ 1\);\s*\}, \[\]\);/);
  assert.match(app, /onDeleted=\{lessonDeleted\}/);
  assert.doesNotMatch(app, /onDeleted=\{\(\) => openPage\("today"\)\}/);
  // The same route as «Назад»: the section under the lesson.
  for (const page of ["today", "recordings", "subjects", "homework"]) {
    assert.deepEqual(closeLesson(routeToLesson(routeToPage(page), "recording:r1")), routeToPage(page));
  }
});

test("the transcript tab draws no head or crumbs of its own and the dead pieces of the overlays are gone", () => {
  const transcript = src("pages/TranscriptPage.tsx");
  assert.doesNotMatch(transcript, /Breadcrumbs|embedded|parent\?|onOpenSummary|<h1/);
  const view = src("summaryView.ts");
  assert.doesNotMatch(view, /noSummaryHint|headerMeta|pageTitle|summarySegments|firstSegment/);
  assert.doesNotMatch(src("knowledgeView.ts"), /recordingIdOfKey|gone:/);
});
