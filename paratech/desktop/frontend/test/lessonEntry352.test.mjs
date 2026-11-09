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
  lessonProblem,
  lessonScreenKey,
  lessonScreenTexts,
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
import { closeLesson, routeToLesson, routeToLessonTab, routeToPage, routeToSubject } from "../.test-build/nav.js";
import { afterLessonDeleted, canGoBack, currentRoute, dropLesson, goBack, goForward, newHistory, pushRoute } from "../.test-build/navHistory.js";
import { mentionOpening, mentionPills } from "../.test-build/agentMentions.js";
import { pairLinks, rowActions, rowItemTab, rowLessonTab } from "../.test-build/recordingRowView.js";
import { lessonOpen, lessonLine, pairIsOver, pairRecordable, pairRecordings, postLine, recordLeadMinutes, summaryRows, texts } from "../.test-build/todayView.js";
import { LessonsSection, SummarySection } from "../.test-build/pages/TodayParts.js";
import { LessonGone } from "../.test-build/pages/LessonGone.js";
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
    assert.deepEqual(r, { page, assignment: "", subject: "", lesson: "recording:r1", tab: "transcript", subjectTab: "" });
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

test("a pair without a recording offers «Записать» from ten minutes before its start until its end; earlier and finished ones are plain rows", () => {
  assert.equal(recordLeadMinutes, 10);
  // The pair is 10:30-11:50.
  const early = clock(21, 7, 30); // three hours before
  const justOutside = clock(21, 10, 19);
  const justInside = clock(21, 10, 20);
  const soon = clock(21, 10, 25); // five minutes before
  const during = clock(21, 11, 0);
  const after = clock(21, 12, 30);
  assert.deepEqual(lessonOpen(lesson(), [], early), { kind: "none" }, "three hours before: a plain row");
  assert.deepEqual(lessonOpen(lesson(), [], justOutside), { kind: "none" });
  assert.deepEqual(lessonOpen(lesson(), [], justInside), { kind: "record" });
  assert.deepEqual(lessonOpen(lesson(), [], soon), { kind: "record" });
  assert.deepEqual(lessonOpen(lesson(), [], during), { kind: "record" });
  assert.deepEqual(lessonOpen(lesson(), [], after), { kind: "none" });
  assert.equal(pairRecordable({ start: at(21, 10, 30), end: at(21, 11, 50) }, clock(21, 11, 50)), false, "the end itself is over");
  // Without an end the pair lasts 90 minutes; an unreadable start is no window at all.
  assert.equal(pairRecordable({ start: at(21, 10, 30), end: "" }, clock(21, 11, 59)), true);
  assert.equal(pairRecordable({ start: "", end: at(21, 11, 50) }, during), false);
  // An earlier pair that already has a recording still opens it, whatever the time.
  assert.deepEqual(lessonOpen(lesson(), [rec({ id: "x" })], early), { kind: "open", recordingId: "x" });
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
  assert.match(open, /<p class="today-t"><button type="button" class="row-open" title="Открыть пару" aria-label="Открыть пару: Матанализ">Матанализ<\/button><\/p>/);
  assert.equal((open.match(/<button/g) ?? []).length, 1, "the row's one control is its title; the old «Открыть» button is gone");

  const record = html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 10, 25), onOpenLesson: noop, onRecord: noop }));
  assert.doesNotMatch(record, /row-open|row-openable/, "nothing to open");
  assert.match(record, /<button[^>]*aria-label="Записать: Матанализ"[^>]*>.*Записать<\/button>/);
  assert.doesNotMatch(html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 10, 25), onOpenLesson: noop })), /Записать/, "no handler, no button");
  // Not yet read: neither.
  assert.doesNotMatch(html(h(LessonsSection, { lessons: [lesson()], recordings: [], recordingsKnown: false, now: clock(21, 10, 25), onOpenLesson: noop, onRecord: noop })), /Записать/);

  const past = html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 15, 0), onOpenLesson: noop, onRecord: noop }));
  assert.doesNotMatch(past, /<button|row-open/, "a finished pair without a recording is a plain row");
  // Hours before the start the time column already says when: no button on every pair of the day.
  const early = html(h(LessonsSection, { lessons: [lesson()], recordings: [], now: clock(21, 7, 30), onOpenLesson: noop, onRecord: noop }));
  assert.doesNotMatch(early, /<button|row-open|Записать/, "an earlier pair is a plain row");

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

test("the row is keyboard-operable; only its real controls sit above the stretched button, a status chip does not", () => {
  const css = src("styles.css");
  assert.match(css, /\.row-open::after \{ content: ""; position: absolute; inset: 0; \}/);
  assert.match(css, /button\.row-open:not\(\.btn\) \{[^}]*cursor: pointer/);
  // The ring is drawn around the row, and the pressed «Открыть» of a recording does not shrink the stretched area.
  assert.match(css, /\.row-open:focus-visible::after \{ outline: 2px solid var\(--accent\)/);
  assert.match(css, /\.btn\.row-open:active:enabled \{ transform: none; \}/);
  assert.match(css, /\.row-openable :is\(\.today-problem, button:not\(\.row-open\)\) \{ position: relative; z-index: 1; \}/);
  assert.match(css, /\.row:has\(\.row-open\) :is\(\.row-lead-box input, \.st \.btn, \.st \.ia, \.row-below \.btn\)/);
  // A plain chip is no dead zone: no rule raises it, so a click on it reaches the stretched button (the lesson).
  const raised = css.split("\n").filter((l) => /position: relative; z-index: 1;/.test(l) && /row-open/.test(l)).join("\n");
  assert.doesNotMatch(raised, /\.chip/, "chips stay under the overlay");
  // Today: «Сегодня» keeps the problem chip a real button; a plain chip of a row is not.
  assert.match(src("pages/TodayParts.tsx"), /<button type="button" className="today-problem" onClick=\{props\.onClick\}>/);
  // Today: the line of the page, in one place.
  const page = src("pages/TodayPage.tsx");
  assert.match(page, /onOpenLesson=\{\(recordingId\) => openLesson\(recordingId\)\}\s+onRecord=\{props\.onRecord\}/);
  assert.match(page, /recordingsKnown=\{recordings !== null\}/);
});

test("the lesson line carries what the row does, from the same clock the row reads", () => {
  assert.deepEqual(lessonLine(lesson({ post: post() }), [], at(21), clock(21, 13, 0)).open, { kind: "open", recordingId: "r1" });
  assert.deepEqual(lessonLine(lesson(), [], at(21), clock(21, 10, 25)).open, { kind: "record" });
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

test("a pair row that opens its newest recording without a post is not repeated in «Итоги дня» either", () => {
  const now = clock(21, 13, 0);
  const lessons = [lesson()]; // no post: the row opens the newest saved recording of the lesson
  const recordings = [rec({ id: "new" }), rec({ id: "old" })];
  assert.deepEqual([...pairRecordings(lessons, recordings, now)], ["new"]);
  const dn = (id) => ({ recordingId: id, lesson: post().lesson, summaryReady: true, homework: 0, at: at(21, 12, 0) });
  const rows = summaryRows([], [dn("new"), dn("other")], lessons, undefined, recordings, now);
  assert.deepEqual(rows.done.map((d) => d.recordingId), ["other"], "the done line of the pair's newest recording is dropped");
  assert.equal(rows.count, 1);
  // Without the recordings list the row cannot be told: the old behaviour (nothing dropped).
  assert.equal(summaryRows([], [dn("new")], lessons, undefined, [], now).count, 1);
  // A recording still being made opens nothing, so the done line of an older one is the only place of it.
  const live = [rec({ id: "run", stage: "recording" }), rec({ id: "new" })];
  assert.deepEqual([...pairRecordings(lessons, live, now)], []);
  // The screen passes the list to the section.
  const out = html(h(SummarySection, { posts: [], done: [dn("new"), dn("other")], lessons, recordings, now, onOpenLesson: noop }));
  assert.equal((out.match(/class="today-row today-wait"/g) ?? []).length, 1);
  assert.match(src("pages/TodayPage.tsx"), /recordings=\{recordings \?\? \[\]\}\s+onOpenLesson/);
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
  // The row's button is a chevron named after the pair (TASK-368 review): «Открыть пару: <заголовок>».
  assert.equal((out.match(/aria-label="Открыть пару: /g) ?? []).length, 1);
  assert.match(out, /aria-label="Открыть пару: Пост другой"/);
  // Only that pair: nothing is left to say, the section is hidden.
  assert.equal(html(h(SummarySection, { posts: [post({ recordingId: "r1" })], done: [], lessons, onOpenLesson: noop })), "");
  // The page passes the lessons of the day.
  assert.match(src("pages/TodayPage.tsx"), /lessons=\{screen\.lessons\}/);
});

// ---- «Записи»: the row ----

test("a recording row's «Открыть» names no tab (the lesson screen chooses); «⋯» keeps each named tab; one open per row", () => {
  const row = { stage: "done", step: "", errorCode: "", lectureTextReady: false, stoppable: false, action: "", actionJobId: "", retryWork: "", canRedoSummary: false };
  const a = rowActions(row, { texts: true, redo: false });
  assert.deepEqual([a.main.key, a.main.label, a.menu.map((i) => i.key)], ["lesson", "Открыть", ["summary", "transcript"]]);
  assert.equal(rowItemTab(a.main.key), null, "the generic open forces no tab, as Today and a subject's «Пары» do");
  assert.deepEqual(a.menu.map((i) => rowItemTab(i.key)), ["summary", "transcript"]);
  const lecture = rowActions({ ...row, lectureTextReady: true }, { texts: true, redo: false });
  assert.deepEqual([lecture.main.key, lecture.main.label], ["lesson", "Открыть"]);
  assert.deepEqual(lecture.menu.map((i) => rowItemTab(i.key)), ["lecture", "summary", "transcript"]);
  const running = rowActions({ ...row, stage: "summarizing", lectureTextReady: true }, { texts: true, redo: false });
  assert.deepEqual([running.main.key, rowItemTab(running.main.key), running.main.label], ["lesson", null, "Открыть"]);
  assert.deepEqual(running.menu.map((i) => rowItemTab(i.key)), ["lecture", "summary", "transcript"]);
  // The items that open no text have no tab.
  for (const key of ["stop", "runNow", "open", "lesson", "redo"]) {
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

// TASK-353: the rows of the overview's «Пары» (10 latest recordings, with chips) went; the lessons of a subject are the
// tab «Пары», read from the knowledge base, and a row opens the lesson screen through the window (rendered and wired in
// subjectTabs353.test.mjs).
test("the lessons of a subject open the lesson screen from the tab «Пары»; the overview's own rows and their opener are gone", () => {
  const tab = src("pages/SubjectPairsTab.tsx");
  // TASK-353 fix round: the rows also get the status chip of the overview (`extra`); the opener is the same.
  assert.match(tab, /<LessonList lessons=\{data\.lessons\} onOpen=\{\(l\) => open\(l\)\} extra=\{/);
  assert.match(src("pages/SubjectsPage.tsx"), /onOpenLesson=\{\(lesson, opening\) => props\.onOpenKnowledgeLesson\?\.\(lesson, opening\)\}/);
  assert.match(src("App.tsx"), /onOpenKnowledgeLesson=\{props\.onOpenLesson\}/);
  assert.doesNotMatch(src("App.tsx"), /onOpenRecording|openRecording\b/);
  assert.doesNotMatch(src("pages/SubjectsParts.tsx"), /OverviewSections/);
});

// ---- an assignment: the links of its lesson ----

test("an assignment offers one «Открыть пару» per recording of its lesson that has a text, with no tab named", () => {
  assert.equal(openPairTexts.one, "Открыть пару");
  assert.equal(openPairTexts.many("14:05"), "Открыть пару · 14:05");
  const r = (over) => ({ id: "r", lessonId: "l1", startedAt: at(21, 10, 30), stage: "done", step: "", errorCode: "", lectureTextReady: false, ...over });
  const list = [
    r({ id: "a" }),
    r({ id: "other", lessonId: "l2" }),
    r({ id: "running", stage: "recording" }),
    r({ id: "b", lectureTextReady: true, startedAt: at(14, 9, 0) }),
    r({ id: "broken", stage: "failed", errorCode: "recordingFailed" }),
    r({ id: "c", stage: "transcribing" }),
  ];
  // The order of the list; only the lesson's own recordings that offer a text; the link carries the id and the time only.
  assert.deepEqual(pairLinks(list, "l1"), [
    { id: "a", startedAt: at(21, 10, 30) },
    { id: "b", startedAt: at(14, 9, 0) },
    { id: "c", startedAt: at(21, 10, 30) },
  ]);
  assert.deepEqual(pairLinks(list, ""), [], "no lesson, no links");
  assert.deepEqual(pairLinks([], "l1"), []);
  // The component opens by the id alone: the lesson screen applies its opening tab, as Today and a subject's «Пары» do.
  const links = src("pages/TranscriptLinks.tsx");
  assert.match(links, /onClick=\{\(\) => open\(l\.id\)\}/);
  assert.doesNotMatch(links, /rowLessonTab|\.tab\b/);
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

test("the lesson screen says its own words for a failed read: gone for good (only «Назад») or worth a repeat («Повторить» and «Назад»)", () => {
  assert.equal(lessonScreenTexts.pairGone, "Этой пары больше нет: запись или файлы удалены.");
  assert.equal(lessonScreenTexts.openFailed, "Не удалось открыть пару.");
  const gone = { kind: "gone", text: lessonScreenTexts.pairGone };
  const retry = { kind: "retry", text: lessonScreenTexts.openFailed };
  // Not found and an invalid key (also as a bare string or inside an Error) are gone for good.
  assert.deepEqual(lessonProblem(new Error(errorMessages.notFound)), gone);
  assert.deepEqual(lessonProblem(errorMessages.notFound), gone);
  assert.deepEqual(lessonProblem(new Error(errorMessages.invalidId)), gone);
  // A read failure, a busy base and anything unknown (a path in a message, a malformed reply) are worth a repeat.
  assert.deepEqual(lessonProblem(new Error(errorMessages.unavailable)), retry);
  assert.deepEqual(lessonProblem(new Error(errorMessages.busy)), retry);
  assert.deepEqual(lessonProblem(new Error("open C:\\Users\\student\\db: locked")), retry);
  assert.deepEqual(lessonProblem(undefined), retry);
  // The binding's words («запись», «обновите список») never reach this screen.
  for (const p of [gone, retry]) {
    assert.doesNotMatch(p.text, /обновите|Запись не найдена|базу знаний|Неверный/);
  }

  // The screen: the text and «Назад» for a lost pair; «Повторить» first and «Назад» for the rest.
  const calls = [];
  const screen = (problem) => html(h(LessonGone, { problem, onRetry: () => calls.push("retry"), onBack: () => calls.push("back") }));
  const lost = screen(gone);
  assert.match(lost, /Этой пары больше нет: запись или файлы удалены\./);
  assert.match(lost, />Назад</);
  assert.doesNotMatch(lost, /Повторить/, "nothing to repeat for a deleted pair");
  const failed = screen(retry);
  assert.match(failed, /Не удалось открыть пару\./);
  assert.match(failed, /Повторить<\/button>.*Назад<\/button>/);
  assert.equal((failed.match(/<button/g) ?? []).length, 2);
  assert.match(failed, /role="alert"/);
});

test("the lesson screen loads a lesson that is not in the window's memory by its key, in one place", () => {
  const page = src("pages/KnowledgeLessonPage.tsx");
  assert.equal(page.split("api.lessonByKey(").length - 1, 1);
  assert.match(page, /if \(cached !== undefined\) \{\s*return;\s*\}\s*let stopped = false;\s*api\.lessonByKey\(key\)\.then\(/);
  // A lecture of notes loaded that way is drawn by the note-only page; a recorded one by the tabs.
  assert.match(page, /if \(isNoteOnly\(lesson\)\) \{\s*return <NoteLecturePage/);
  // The refusal: the screen's own words (lessonProblem), «Повторить» that reads again, «Назад»; no recording id or key.
  assert.match(page, /setProblem\(lessonProblem\(err\)\)/);
  assert.match(page, /<LessonGone\s+problem=\{problem\}\s+onRetry=\{\(\) => \{[^}]*setAttempt\(\(n\) => n \+ 1\);[^}]*\}\}\s+onBack=\{props\.onBack\}/);
  assert.match(page, /\}, \[api, cached, key, attempt\]\);/, "the repeat runs the load again");
  // The re-read of a lesson from the window's memory has the same way out.
  assert.match(page, /setLessonError\(lessonProblem\(err\)\)/);
  assert.match(page, /\}, \[props\.api, props\.stale, id, reread\]\);/);
  assert.doesNotMatch(page, /knowledgeErrorText|\{key\}|\$\{key\}|lessonKey\}/);
});

test("deleting a recording on the lesson screen returns to the section the lesson was opened from", () => {
  const page = src("pages/KnowledgeLessonPage.tsx");
  // The head «⋯» has the deletion; the dialog is the shared one; a deletion that removed something leaves the screen.
  assert.match(page, /\{ key: "delete", label: deleteTexts\.summaryButton \},/);
  assert.match(page, /<RecordingDeleteDialog\s+api=\{deletion\}\s+ids=\{\[id\]\}/);
  assert.match(page, /if \(res\.removed > 0\) \{\s*props\.onDeleted\(\);\s*\} else \{\s*setDeleteFailed\(deleteOutcome\(res, preview\)\);\s*\}/);
  // A refusal says why in the banner and the student stays.
  assert.match(page, /\{deleteFailed && \(\s*<Banner kind="fail" role="alert">\s*\{deleteFailed\.lines\.join\(" "\)\}/);
  // The window goes back to the page under the lesson (not to «Сегодня»), forgets the lesson and takes it out of the history.
  const app = src("App.tsx");
  assert.match(app, /lessons\.current\.delete\(key\);\s*refocusSheet\.current = true;\s*navHistory\.lessonDeleted\(key\);\s*setJump\(\(j\) => j \+ 1\);/);
  assert.match(app, /onDeleted=\{lessonDeleted\}/);
  assert.doesNotMatch(app, /onDeleted=\{\(\) => openPage\("today"\)\}|setRoute\(closeLesson\);\s*setJump/);
  // The same route as «Назад»: the section under the lesson.
  for (const page of ["today", "recordings", "subjects", "homework"]) {
    assert.deepEqual(closeLesson(routeToLesson(routeToPage(page), "recording:r1")), routeToPage(page));
  }
  // The hook hands the window the history operation; the window wires nothing else to it.
  assert.match(src("useNavHistory.ts"), /afterLessonDeleted\(history\.current, lessonKey\)/);
});

test("after a deletion the history holds no step of the lesson: Alt+← goes to the page before, never back to a deleted lesson", () => {
  const key = recordingLessonKey("r1");
  const today = routeToPage("today");
  const recordings = routeToPage("recordings");

  // Today -> Записи -> the lesson (the tab was switched on the way: still one entry).
  let h = newHistory(today);
  h = pushRoute(h, recordings);
  h = pushRoute(h, routeToLesson(recordings, key));
  h = pushRoute(h, routeToLessonTab(routeToLesson(recordings, key), "summary"));
  assert.deepEqual(h.entries.map((r) => r.lesson), ["", "", key]);

  // What the window did before: the page was pushed after the lesson, so Alt+← returned to the deleted lesson.
  const old = pushRoute(h, closeLesson(currentRoute(h)));
  assert.deepEqual(old.entries.map((r) => r.lesson), ["", "", key, ""]);
  assert.equal(currentRoute(goBack(old)).lesson, key);

  // Now: the lesson leaves the history and the window shows the page under it.
  const out = afterLessonDeleted(h, key);
  assert.deepEqual(out.route, recordings);
  assert.deepEqual(out.history.entries.map((r) => r.page), ["today", "recordings"]);
  assert.equal(out.history.index, 1);
  assert.deepEqual(currentRoute(out.history), recordings);
  assert.deepEqual(currentRoute(goBack(out.history)), today, "Alt+← from the page leaves for the page before");
  assert.equal(canGoBack(goBack(out.history)), false);
  // The route state the window sets is the current entry, so the hook's own push adds nothing.
  assert.equal(pushRoute(out.history, out.route), out.history);
  assert.ok(!out.history.entries.some((r) => r.lesson === key));
  assert.equal(goForward(out.history), out.history, "nothing forward either");

  // The same lesson visited twice, with another page between: every step of it is dropped, neighbours merge.
  let twice = newHistory(recordings);
  twice = pushRoute(twice, routeToLesson(recordings, key));
  twice = pushRoute(twice, routeToPage("subjects"));
  twice = pushRoute(twice, routeToLesson(recordings, key));
  const cleaned = dropLesson(twice, key);
  assert.deepEqual(cleaned.entries.map((r) => [r.page, r.lesson]), [["recordings", ""], ["subjects", ""]]);
  assert.equal(cleaned.index, 1, "the current place stays the last page before the lesson");

  // A lesson of another key stays; an unrelated history is returned as it is.
  const other = pushRoute(h, routeToLesson(recordings, recordingLessonKey("r2")));
  assert.ok(dropLesson(other, key).entries.some((r) => r.lesson === recordingLessonKey("r2")));
  const plain = newHistory(today);
  assert.equal(dropLesson(plain, key), plain);

  // The old shape (the page pushed after the lesson) collapses to one entry of the page.
  const merged = dropLesson(old, key);
  assert.deepEqual(merged.entries.map((r) => r.page), ["today", "recordings"]);
  assert.equal(merged.index, 1);

  // The window already left the lesson (the dialog answered late): only its entries go, the place stays.
  let left = pushRoute(newHistory(today), routeToLesson(today, key));
  left = pushRoute(left, recordings);
  const late = afterLessonDeleted(left, key);
  assert.deepEqual(late.route, recordings);
  assert.deepEqual(late.history.entries.map((r) => r.page), ["today", "recordings"]);
  assert.equal(late.history.index, 1);
});

test("the screen of a lesson is mounted afresh by another opening, never by a tab switch", () => {
  const key = recordingLessonKey("r1");
  const r = routeToLesson(routeToPage("today"), key);
  // The window counts a visit on each opening of a lesson (openLessonKey) and puts it in the key with the lesson alone.
  assert.equal(lessonScreenKey(r.lesson, 1), "lesson:recording:r1:1");
  const tabbed = routeToLessonTab(r, "transcript");
  assert.equal(lessonScreenKey(tabbed.lesson, 1), lessonScreenKey(r.lesson, 1), "a tab switch keeps the screen");
  assert.equal(lessonScreenKey(routeToLessonTab(tabbed, "summary").lesson, 1), lessonScreenKey(r.lesson, 1));
  assert.notEqual(lessonScreenKey(r.lesson, 2), lessonScreenKey(r.lesson, 1), "the same lesson opened at another place");
  assert.notEqual(lessonScreenKey(recordingLessonKey("r2"), 1), lessonScreenKey(r.lesson, 1), "another lesson");
  const app = src("App.tsx");
  assert.match(app, /key=\{lessonScreenKey\(route\.lesson, lessonVisit\)\}/);
  // Only an opening counts a visit; the tab does not.
  assert.match(app, /setLessonVisit\(\(n\) => n \+ 1\);\s*setRoute\(\(r\) => routeToLesson\(/);
  assert.equal(app.split("setLessonVisit(").length - 1, 1);
  assert.match(app, /const openLessonTab = useCallback\(\(tab: string\) => setRoute\(\(r\) => routeToLessonTab\(r, tab\)\), \[\]\);/);
});

test("the transcript tab draws no head or crumbs of its own and the dead pieces of the overlays are gone", () => {
  const transcript = src("pages/TranscriptPage.tsx");
  assert.doesNotMatch(transcript, /Breadcrumbs|embedded|parent\?|onOpenSummary|<h1/);
  const view = src("summaryView.ts");
  assert.doesNotMatch(view, /noSummaryHint|headerMeta|pageTitle|summarySegments|firstSegment/);
  assert.doesNotMatch(src("knowledgeView.ts"), /recordingIdOfKey|gone:/);
});
