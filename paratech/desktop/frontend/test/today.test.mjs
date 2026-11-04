// «Сегодня» (TASK-126): the binding parsing, the view logic and the plain-text rule.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { errorTexts, genericError, methods, parseScreen, todayApi, todayErrorText } from "../.test-build/bindings/today.js";
import { draftEvent, draftText, requestDraft } from "../.test-build/agentDraft.js";
import {
  dayTitle,
  daysLeftText,
  deadlineChip,
  deadlineLine,
  doneLine,
  dueText,
  homeworkText,
  isOverdue,
  lessonLine,
  liveStatus,
  loadView,
  looseProblems,
  nearestDeadlineText,
  newestOnly,
  problemFor,
  stagesChanged,
  postLine,
  shortDate,
  stagesKey,
  statusParts,
  summaryRows,
  texts,
  timeText,
  titleOf,
  waitingLine,
} from "../.test-build/todayView.js";
import * as todayView from "../.test-build/todayView.js";
import { assignmentRow } from "../.test-build/agentView.js";
import { AskSection, BurningSection, LessonsSection, LoadError, StatusBar, SummarySection, WaitingSection } from "../.test-build/pages/TodayParts.js";

const noop = () => undefined;
// Local-time moments, so the expected texts do not depend on the machine's zone.
const at = (d, hh = 0, mm = 0) => new Date(2026, 9, d, hh, mm).toISOString();

function post(over = {}) {
  return {
    recordingId: "r1", transcriptId: "t1",
    lesson: { lessonId: "l1", title: "Матанализ", subject: "Матанализ", subjectId: "s1", subjectName: "Матанализ", kind: "lecture", start: at(21, 10, 30) },
    headline: "Конспект готов", summaryReady: true, homework: 1, earliestDue: at(24, 23, 59), calendar: [], shownAt: at(21, 11, 52), ...over,
  };
}

function lesson(over = {}) {
  return { id: "l1", title: "Матанализ", subject: "Матанализ", kind: "lecture", teacher: "Петров А. В.", start: at(21, 10, 30), end: at(21, 11, 50), post: null, ...over };
}

function screen(over = {}) {
  return { day: at(21), lessons: [], posts: [], waiting: [], burning: [], done: [], chips: [], ...over };
}

function rec(over = {}) {
  return { id: "rec1", lessonId: "l1", stage: "done", progressPercent: 0, ...over };
}

test("the binding names and the fixed error texts", () => {
  assert.equal(methods.today, "paratech/desktop/internal/input/wails.TodayService.Today");
  assert.equal(methods.subjectPosts, "paratech/desktop/internal/input/wails.TodayService.SubjectPosts");
  for (const t of errorTexts) {
    assert.equal(todayErrorText(new Error(t)), t);
    assert.equal(todayErrorText(t), t);
  }
  assert.equal(todayErrorText(new Error("open C:\\secret\\file: access denied")), genericError);
  assert.equal(todayErrorText(undefined), genericError);
});

test("parseScreen reads a full reply and takes null lists as empty", () => {
  const s = parseScreen({
    day: at(21), lessons: [lesson({ post: post() })], posts: [post()], waiting: null, burning: [{ assignmentId: "a1", title: "Реферат", subject: "История", due: at(23), daysLeft: 2, urgent: true }],
    done: [{ recordingId: "r1", lesson: post().lesson, summaryReady: true, homework: 1, at: at(21, 11, 52) }], chips: [{ kind: "todaySubject", text: "Что горит?" }],
  });
  assert.equal(s.lessons[0].post.recordingId, "r1");
  assert.deepEqual(s.waiting, []);
  assert.equal(s.burning[0].urgent, true);
  assert.equal(s.chips[0].text, "Что горит?");
  assert.equal(parseScreen({ day: at(21), lessons: [lesson()] }).lessons[0].post, null);
});

test("parseScreen refuses unknown input", () => {
  for (const bad of [null, undefined, "x", 5, [], { lessons: [] }, { day: 5 }]) {
    assert.throws(() => parseScreen(bad));
  }
  assert.throws(() => parseScreen(screen({ lessons: "x" })));
  assert.throws(() => parseScreen(screen({ lessons: [{ id: "l1" }] })));
  assert.throws(() => parseScreen(screen({ burning: [{ assignmentId: "a", title: "t", due: at(1), daysLeft: "2" }] })));
  assert.throws(() => parseScreen(screen({ posts: [{ ...post(), homework: "1" }] })));
  assert.throws(() => parseScreen(screen({ waiting: [null] })));
  // Fields Go always sends are strict: a missing string or a non-boolean flag is refused.
  assert.throws(() => parseScreen(screen({ lessons: [{ ...lesson(), teacher: undefined }] })));
  assert.throws(() => parseScreen(screen({ posts: [{ ...post(), summaryReady: "yes" }] })));
  assert.throws(() => parseScreen(screen({ posts: [{ ...post(), summaryReady: undefined }] })));
  assert.throws(() => parseScreen(screen({ burning: [{ assignmentId: "a", title: "t", subject: "", due: at(1), daysLeft: 2, urgent: 1 }] })));
  assert.throws(() => parseScreen(screen({ chips: [{ kind: "x" }] })));
});

test("todayApi calls the bound methods and parses the reply", async () => {
  const calls = [];
  const api = todayApi(async (m, ...args) => {
    calls.push([m, ...args]);
    return m === methods.today ? screen() : [post()];
  });
  assert.equal((await api.today()).lessons.length, 0);
  assert.equal((await api.subjectPosts("s1"))[0].recordingId, "r1");
  assert.deepEqual(calls, [[methods.today], [methods.subjectPosts, "s1"]]);
  await assert.rejects(todayApi(async () => 42).today());
});

test("times, dates and due texts", () => {
  assert.equal(timeText(at(21, 9, 5)), "09:05");
  assert.equal(timeText(""), "");
  assert.equal(timeText("not a date"), "");
  assert.equal(shortDate(at(21)), "21.10");
  assert.equal(dayTitle(at(21)), "Среда, 21 октября");
  assert.equal(dueText(at(24, 23, 59)), "до сб, 24 окт., 23:59");
  assert.equal(dueText(at(24, 20, 0)), "до сб, 24 окт., 20:00");
  assert.equal(dueText(""), "");
  assert.equal(dueText("not a date"), "");
});

test("deadline text carries the time: today, tomorrow, later, passed (TASK-343)", () => {
  const now = new Date(2026, 9, 21, 12, 0); // Wed 21 Oct 12:00, local
  assert.equal(dueText(at(21, 20, 0), now), "сегодня до 20:00");
  assert.equal(dueText(at(22, 18, 30), now), "завтра до 18:30");
  assert.equal(dueText(at(24, 18, 30), now), "до сб, 24 окт., 18:30");
  // A passed moment is never «до …»: «просрочено» with the full date and time.
  assert.equal(dueText(at(21, 9, 0), now), "просрочено, ср, 21 окт., 09:00");
  assert.equal(dueText(at(21, 12, 0), now), "просрочено, ср, 21 окт., 12:00");
  assert.equal(dueText(at(20, 20, 0), now), "просрочено, вт, 20 окт., 20:00");
  // Midnight edges: 23:59 of today is still today, 00:00 of tomorrow is tomorrow, a minute before midnight "now" too.
  assert.equal(dueText(at(21, 23, 59), new Date(2026, 9, 21, 23, 58)), "сегодня до 23:59");
  assert.equal(dueText(at(22, 0, 0), new Date(2026, 9, 21, 23, 58)), "завтра до 00:00");
  assert.equal(dueText(at(22, 0, 0), new Date(2026, 9, 22, 0, 1)), "просрочено, чт, 22 окт., 00:00");
  // Without `now` the wording stays «до …» (the calendar confirm card strips the «до »).
  assert.equal(dueText(at(21, 9, 0)), "до ср, 21 окт., 09:00");
});

test("a date-only text is read as a local day, whatever the zone (TASK-343)", () => {
  // The date-only form of the agent rows is parsed by calendar parts, never as UTC midnight.
  const early = new Date(2026, 9, 26, 0, 30);
  const row = (due, time) => assignmentRow({ id: "a", title: "t", subjectId: "", due, time }, {}, early);
  assert.equal(row("2026-10-26", "").due, "сегодня");
  assert.equal(row("2026-10-26", "20:00").due, "сегодня до 20:00");
  assert.equal(row("2026-10-27", "20:00").due, "завтра до 20:00");
});

test("days left: today, tomorrow, plural forms, overdue", () => {
  assert.equal(daysLeftText(0), "сегодня");
  assert.equal(daysLeftText(1), "завтра");
  assert.equal(daysLeftText(2), "через 2 дня");
  assert.equal(daysLeftText(5), "через 5 дней");
  assert.equal(daysLeftText(21), "через 21 день");
  assert.equal(daysLeftText(-1), "просрочено");
});

test("homework text and titles", () => {
  assert.equal(homeworkText(0, at(24)), "");
  assert.equal(homeworkText(1, at(24, 20, 0)), "1 задание · до сб, 24 окт., 20:00");
  assert.equal(homeworkText(1, at(24, 20, 0), 0, new Date(2026, 9, 24, 9, 0)), "1 задание · сегодня до 20:00");
  assert.equal(homeworkText(1, at(24, 20, 0), 0, new Date(2026, 9, 24, 21, 0)), "1 задание · просрочено, сб, 24 окт., 20:00");
  assert.equal(homeworkText(2, at(25, 18, 30), 0, new Date(2026, 9, 24, 9, 0)), "2 задания · завтра до 18:30");
  assert.equal(homeworkText(1, at(24, 20, 0), 1, new Date(2026, 9, 24, 9, 0)), "1 задание · выполнено");
  assert.equal(homeworkText(3, ""), "3 задания");
  assert.equal(homeworkText(5, ""), "5 заданий");
  assert.equal(titleOf("  "), "Без названия");
  assert.equal(titleOf("Реферат"), "Реферат");
});

test("live status comes from the recordings of the lesson", () => {
  assert.equal(liveStatus("l1", []), null);
  assert.equal(liveStatus("", [rec({ lessonId: "" , stage: "recording" })]), null);
  assert.equal(liveStatus("l1", [rec()]), null);
  assert.deepEqual(liveStatus("l1", [rec({ stage: "transcribing", progressPercent: 63.6 })]), { kind: "run", text: "Расшифровка · 64 %" });
  assert.deepEqual(liveStatus("l1", [rec({ stage: "transcribing", progressPercent: 140 })]), { kind: "run", text: "Расшифровка · 100 %" });
  assert.deepEqual(liveStatus("l1", [rec({ stage: "transcribing" }), rec({ id: "rec2", stage: "recording" })]), { kind: "rec", text: "Идёт запись" });
  assert.equal(liveStatus("l2", [rec({ stage: "recording" })]), null);
  assert.notEqual(stagesKey([rec()]), stagesKey([rec({ stage: "recording" })]));
});

test("lesson, waiting, deadline, done and post lines", () => {
  const l = lessonLine(lesson({ post: post() }), []);
  assert.deepEqual([l.start, l.end, l.title, l.meta], ["10:30", "11:50", "Матанализ", "Лекция · Петров А. В."]);
  assert.deepEqual(l.post, { text: "Конспект готов", homework: "1 задание · до сб, 24 окт., 23:59", canOpen: true, recordingId: "r1" });
  assert.equal(lessonLine(lesson({ post: post({ summaryReady: false }) }), []).post.canOpen, false);
  assert.equal(lessonLine(lesson({ kind: "", teacher: "" }), []).meta, "");

  const w = waitingLine({ proposalId: "p1", kind: "send_homework_email", assignmentId: "a1", title: "Реферат", subject: "История", due: "", recipient: "teacher@example.test", createdAt: "" });
  assert.equal(w.label, "Письмо преподавателю");
  assert.equal(w.subject, "История");
  assert.equal(w.meta, "→ teacher@example.test");
  assert.equal(waitingLine({ proposalId: "p2", kind: "create_deadline_event", assignmentId: "a1", title: "ДЗ", subject: "", due: at(24, 20, 0), recipient: "", createdAt: "" }).meta, "до сб, 24 окт., 20:00");
  const wd = { proposalId: "p2", kind: "create_deadline_event", assignmentId: "a1", title: "ДЗ", subject: "", due: at(21, 20, 0), recipient: "", createdAt: "" };
  assert.equal(waitingLine(wd, new Date(2026, 9, 21, 9, 0)).meta, "сегодня до 20:00");
  assert.equal(waitingLine({ ...wd, recipient: "t@example.test" }, new Date(2026, 9, 21, 9, 0)).meta, "сегодня до 20:00 · → t@example.test");
  assert.equal(waitingLine({ ...wd, due: at(22, 18, 30) }, new Date(2026, 9, 21, 9, 0)).meta, "завтра до 18:30");
  assert.equal(waitingLine({ proposalId: "p3", kind: "unknown", assignmentId: "", title: "", subject: "", due: "", recipient: "", createdAt: "" }).label, "Действие");

  const d = deadlineLine({ assignmentId: "a1", title: "Реферат", subject: "История", due: at(23, 20, 0), daysLeft: 2, urgent: true }, new Date(2026, 9, 21, 12, 0));
  // TASK-345: the chip carries the weekday and the date itself, the row has no date column.
  assert.deepEqual([d.chip, d.urgent, d.overdue, "weekday" in d, "date" in d], ["до пт, 23 окт., 20:00", true, false, false, false]);

  const done = doneLine({ recordingId: "r1", lesson: post().lesson, summaryReady: true, homework: 1, at: at(21, 11, 52) });
  assert.equal(done.title, "Конспект · Матанализ");
  assert.equal(done.meta, "Лекция · 21.10 · 11:52 · нашлось 1 задание");
  assert.equal(done.canOpen, true);
  assert.equal(done.icon, "ok");
  const notReady = doneLine({ recordingId: "r1", lesson: post().lesson, summaryReady: false, homework: 0, at: at(21, 11, 52) });
  assert.equal(notReady.title, "Конспект готовится · Матанализ");
  assert.equal(notReady.icon, "clock");
  assert.equal(notReady.canOpen, false);

  const p = postLine(post());
  assert.equal(p.headline, "Конспект готов");
  assert.equal(p.meta, "Матанализ · 11:52");
});

test("the «Сроки на неделе» chip is the dueText phrase (TASK-343, TASK-345)", () => {
  const chip = (day, hh, mm, daysLeft, now) => deadlineChip({ due: at(day, hh, mm), daysLeft }, now);
  const noon = new Date(2026, 9, 21, 12, 0);
  assert.equal(chip(21, 20, 0, 0, noon), "сегодня до 20:00");
  assert.equal(chip(22, 18, 30, 1, noon), "завтра до 18:30");
  assert.equal(chip(23, 20, 0, 2, noon), "до пт, 23 окт., 20:00");
  assert.equal(chip(26, 18, 30, 5, noon), "до пн, 26 окт., 18:30");
  // The chip is exactly dueText: one wording for «Сегодня», the panel and «Задания».
  for (const [day, hh, mm] of [[21, 20, 0], [22, 18, 30], [24, 18, 30], [21, 9, 0], [20, 20, 0]]) {
    assert.equal(chip(day, hh, mm, 3, noon), dueText(at(day, hh, mm), noon));
  }
  // The days come from the local clock: a stale snapshot never says «завтра» for today, or «сегодня» for tomorrow.
  assert.equal(chip(21, 20, 0, 1, noon), "сегодня до 20:00");
  assert.equal(chip(22, 18, 30, 0, noon), "завтра до 18:30");
  assert.equal(chip(24, 18, 30, 1, noon), "до сб, 24 окт., 18:30");
  // After 20:00 the core may still count the day as today: the passed moment wins, with its date.
  assert.equal(chip(21, 20, 0, 0, new Date(2026, 9, 21, 20, 0)), "просрочено, ср, 21 окт., 20:00");
  assert.equal(chip(21, 20, 0, 0, new Date(2026, 9, 21, 23, 59)), "просрочено, ср, 21 окт., 20:00");
  assert.equal(chip(21, 20, 0, 0, new Date(2026, 9, 21, 19, 59)), "сегодня до 20:00");
  assert.equal(chip(21, 9, 0, -1, noon), "просрочено, ср, 21 окт., 09:00");
  assert.equal(chip(20, 20, 0, -1, noon), "просрочено, вт, 20 окт., 20:00");
  // Midnight: 00:00 tomorrow is «завтра до 00:00», and at 00:01 on that day it has passed.
  assert.equal(chip(22, 0, 0, 1, new Date(2026, 9, 21, 23, 58)), "завтра до 00:00");
  assert.equal(chip(22, 0, 0, 0, new Date(2026, 9, 22, 0, 1)), "просрочено, чт, 22 окт., 00:00");
  // An unreadable due keeps the day count of the core.
  assert.equal(deadlineChip({ due: "", daysLeft: 0 }, noon), "сегодня");
  assert.equal(deadlineChip({ due: "", daysLeft: 1 }, noon), "завтра");
  assert.equal(deadlineChip({ due: "not a date", daysLeft: 3 }, noon), "через 3 дня");
  assert.equal(deadlineChip({ due: "", daysLeft: -1 }, noon), "просрочено");
  assert.equal(deadlineLine({ assignmentId: "a", title: "ЛР", subject: "", due: at(21, 20, 0), daysLeft: 0, urgent: true }, noon).chip, "сегодня до 20:00");
});

test("the nearest deadline of the week is worded as the list (dueText) (TASK-343, TASK-345)", () => {
  const d = (day, hh, mm) => ({ due: at(day, hh, mm) });
  assert.equal(nearestDeadlineText([]), "");
  // Without `now` there is no «сегодня»/«завтра»: the plain «до …» phrase.
  assert.equal(nearestDeadlineText([d(23, 20, 0), d(25, 9, 5)]), "Ближайший срок — до пт, 23 окт., 20:00");
  assert.equal(nearestDeadlineText([d(23, 20, 0), d(23, 18, 30)]), "Ближайший срок — до пт, 23 окт., 18:30");
  // With `now` a deadline that has passed is skipped; when all have passed there is no text.
  const evening = new Date(2026, 9, 23, 19, 0);
  assert.equal(nearestDeadlineText([d(23, 18, 30), d(23, 20, 0), d(25, 9, 5)], evening), "Ближайший срок — сегодня до 20:00");
  assert.equal(nearestDeadlineText([d(23, 18, 30)], evening), "");
  const noon = new Date(2026, 9, 21, 12, 0);
  assert.equal(nearestDeadlineText([d(22, 18, 30)], noon), "Ближайший срок — завтра до 18:30");
  assert.equal(nearestDeadlineText([d(24, 18, 30)], noon), "Ближайший срок — до сб, 24 окт., 18:30");
  // The same words as the row chip of the list.
  assert.equal(nearestDeadlineText([d(21, 20, 0)], noon), `Ближайший срок — ${deadlineChip({ due: at(21, 20, 0), daysLeft: 0 }, noon)}`);
});

test("an overdue deadline is told apart from «сегодня» (TASK-345)", () => {
  const noon = new Date(2026, 9, 21, 12, 0);
  const dl = (day, hh, mm, daysLeft = 0, urgent = true) => ({ assignmentId: "a", title: "ЛР", subject: "", due: at(day, hh, mm), daysLeft, urgent });
  assert.equal(deadlineLine(dl(21, 20, 0), noon).overdue, false);
  assert.equal(deadlineLine(dl(21, 9, 0), noon).overdue, true);
  assert.equal(deadlineLine(dl(20, 9, 0, -1), noon).overdue, true);
  assert.equal(deadlineLine(dl(21, 12, 0), noon).overdue, true, "the very moment has passed");
  assert.equal(isOverdue({ due: "", daysLeft: -1 }, noon), true);
  assert.equal(isOverdue({ due: "", daysLeft: 0 }, noon), false);
});

test("what the page shows about the read of the day (TASK-345)", () => {
  assert.equal(loadView(false, false, false), "loading", "the first read");
  assert.equal(loadView(false, true, false), "error", "failed, no day yet: the red alert");
  assert.equal(loadView(true, true, false), "stale", "failed re-read of a shown day: the quiet line");
  assert.equal(loadView(true, false, false), "none");
  assert.equal(loadView(true, false, true), "loading", "retry in flight over a shown day");
  assert.equal(loadView(false, false, true), "loading", "retry in flight, no day");
});

test("the hint chip text goes to the panel through an event", () => {
  const target = new EventTarget();
  const got = [];
  target.addEventListener(draftEvent, (e) => got.push(draftText(e)));
  requestDraft("Что горит на неделе?", target);
  assert.deepEqual(got, ["Что горит на неделе?"]);
  assert.equal(draftText(new Event(draftEvent)), "");
  assert.equal(draftText(new CustomEvent(draftEvent, { detail: 5 })), "");
});

const html = (el) => renderToStaticMarkup(el);
const evil = "<img src=x onerror=alert(1)>";

test("untrusted text stays text in every section", () => {
  const p = post({ headline: evil, lesson: { ...post().lesson, title: evil, subjectName: evil, subject: evil } });
  const out = [
    html(h(LessonsSection, { lessons: [lesson({ title: evil, teacher: evil, post: p })], recordings: [], onOpenLesson: noop })),
    html(h(SummarySection, { posts: [p], done: [], onOpenLesson: noop })),
    html(h(SummarySection, { posts: [], done: [{ recordingId: "r1", lesson: p.lesson, summaryReady: true, homework: 0, at: at(21) }], onOpenLesson: noop })),
    html(h(WaitingSection, { waiting: [{ proposalId: "p", kind: "send_homework_email", assignmentId: "", title: evil, subject: evil, due: "", recipient: evil, createdAt: "" }] })),
    html(h(BurningSection, { burning: [{ assignmentId: "a", title: evil, subject: evil, due: at(23), daysLeft: 1, urgent: false }], onOpenAssignment: noop })),
    html(h(AskSection, { chips: [{ kind: "x", text: evil }], onChip: noop })),
  ];
  for (const s of out) {
    assert.ok(!s.includes("<img"), s);
    assert.ok(s.includes("&lt;img"), s);
  }
});

test("empty sections of an empty day: the heading and one short line; the others are hidden (TASK-345)", () => {
  const lessons = html(h(LessonsSection, { lessons: [], recordings: [], onOpenLesson: noop }));
  const waiting = html(h(WaitingSection, { waiting: [] }));
  const burning = html(h(BurningSection, { burning: [], onOpenAssignment: noop }));
  assert.match(lessons, /Пары дня[^]*На сегодня пар нет\./);
  assert.match(waiting, /Нужно ваше решение[^]*Ничего не ждёт решения\./);
  assert.match(burning, /Сроки на неделе[^]*Сроков на неделе нет\./);
  for (const s of [lessons, waiting, burning]) {
    assert.equal(s.match(/<p /g).length, 1, "one line: " + s);
    assert.equal(s.match(/<h2/g).length, 1);
    assert.doesNotMatch(s, /пока нет|пока ничего/);
  }
  assert.equal(html(h(SummarySection, { posts: [], done: [], onOpenLesson: noop })), "");
  assert.equal(html(h(AskSection, { chips: [], onChip: noop })), "");
  assert.match(html(h(AskSection, { chips: [{ kind: "x", text: "Что горит?" }], onChip: noop })), /Спросить paratech[^]*Что горит\?/);
});

test("«Итоги дня»: one list, the posts first, then the done ones; a done lesson a post already tells about is not repeated (TASK-345)", () => {
  const dn = (id, title) => ({ recordingId: id, lesson: { ...post().lesson, title, subjectName: title, subject: title }, summaryReady: true, homework: 0, at: at(21, 12, 0) });
  const rows = summaryRows([post({ recordingId: "r1" }), post({ recordingId: "r2" })], [dn("r2", "Дубль"), dn("r3", "Физика")]);
  assert.deepEqual(rows.posts.map((p) => p.recordingId), ["r1", "r2"]);
  assert.deepEqual(rows.done.map((d) => d.recordingId), ["r3"]);
  assert.equal(rows.count, 3);
  assert.equal(summaryRows([], []).count, 0);

  const out = html(h(SummarySection, { posts: [post({ recordingId: "r1", headline: "Пост один" })], done: [dn("r1", "Повтор"), dn("r3", "Физика")], onOpenLesson: noop }));
  assert.equal((out.match(/<h2/g) ?? []).length, 1, "no inner sub-headings");
  assert.match(out, /Итоги дня[^]*<span class="num today-cnt"> · 2<\/span>/);
  assert.equal((out.match(/role="list"/g) ?? []).length, 1, "one card list");
  assert.ok(out.indexOf("Пост один") < out.indexOf("Конспект · Физика"), "posts before the done rows");
  assert.ok(!out.includes("Повтор"), "the done row of a told recording is not repeated");
  assert.equal((out.match(/aria-label="Открыть пару: /g) ?? []).length, 2, "every ready row opens its lesson");
  assert.match(out, /aria-label="Открыть пару: Пост один"/);
  assert.match(out, /aria-label="Открыть пару: Конспект · Физика"/);
  assert.doesNotMatch(out, /Сообщения paratech|Готово за день/);
});

test("a load error: the fixed text in an alert and «Повторить»; no «Обновить» anywhere (TASK-345)", () => {
  let retried = 0;
  const el = LoadError({ onRetry: () => retried++ });
  const out = html(el);
  assert.match(out, /role="alert"[^]*Не удалось загрузить день\./);
  assert.match(out, /<button[^>]*>[^]*Повторить[^]*<\/button>/);
  assert.equal((out.match(/<button/g) ?? []).length, 1);
  // The button calls the reload as it is.
  const button = el.props.children[1];
  button.props.onClick();
  assert.equal(retried, 1);
  assert.equal(texts.loadFailed, "Не удалось загрузить день.");
  assert.ok(!("refresh" in texts));
  const page = readFileSync(new URL("../src/pages/TodayPage.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(page, /texts\.refresh/);
  assert.match(page, /<SpeechComponentNotice primary=\{false\} \/>[\s\S]*view === "error" && <LoadError onRetry=\{retry\} \/>/, "the notice sits above the error, its button is secondary");
  assert.doesNotMatch(page, /\ball\b|setAll/, "the «all» mode is gone since TASK-348: «Записи» is a section");
  assert.match(page, /className="muted small"[^>]*>\s*\{texts\.loading\}/, "loading is a small inline line, not a banner");
});

test("«Повторить» reacts at once: the error line gives way to «Загрузка…», the focus goes to the section (TASK-345)", () => {
  const page = readFileSync(new URL("../src/pages/TodayPage.tsx", import.meta.url), "utf8");
  assert.match(page, /const retry = useCallback\(\(\) => \{\s*rootRef\.current\?\.focus\(\);\s*setFailed\(false\);\s*setRetrying\(true\);\s*load\(\);/);
  assert.ok(page.includes('view === "loading" && ('), "the small inline line also over a shown day");
  // A repeated failure shows the line again; every answer ends the retry.
  assert.match(page, /setFailed\(true\);\s*setRetrying\(false\);/);
  assert.match(page, /setFailed\(false\);\s*setRetrying\(false\);/);
  // Only the button moves the focus; the reads on entry, on window focus and on a stage change do not.
  assert.equal((page.match(/rootRef\.current\?\.focus\(\)/g) ?? []).length, 1);
  assert.match(page, /<LoadError onRetry=\{retry\} quiet \/>/);
});

test("a failed re-read of a shown day is a quiet line, not a red alert (TASK-345)", () => {
  const quiet = html(h(LoadError, { onRetry: noop, quiet: true }));
  assert.match(quiet, /Не удалось обновить день\./);
  assert.match(quiet, /Повторить/);
  assert.doesNotMatch(quiet, /role="alert"|s-fail|Не удалось загрузить день/);
  const loud = html(h(LoadError, { onRetry: noop }));
  assert.match(loud, /role="alert"[^]*Не удалось загрузить день\./);
  assert.equal(texts.refreshFailed, "Не удалось обновить день.");
});

test("«Итоги дня» is last in one column and under the lessons in two (TASK-345)", () => {
  const page = readFileSync(new URL("../src/pages/TodayPage.tsx", import.meta.url), "utf8");
  const lessons = page.indexOf("<LessonsSection");
  const decide = page.indexOf('className="today-decide"');
  const summary = page.indexOf("<SummarySection");
  assert.ok(lessons < decide && decide < summary, "DOM order: lessons, decisions, summary");
  const css = readFileSync(new URL("../src/pages/today.css", import.meta.url), "utf8");
  assert.match(css, /\.today-cols \{ grid-template-areas: "lessons" "decide" "summary"; \}/);
  assert.match(css, /grid-template-areas: "lessons decide" "summary decide"; grid-template-rows: auto 1fr;/);
  assert.match(css, /\.today-summary \{ grid-area: summary; \}/);
  assert.match(css, /\.today-cols > div > \.today-sec:first-child \{ margin-top: 0; \}/);
  assert.ok(html(h(SummarySection, { posts: [post()], done: [], onOpenLesson: noop })).includes("today-summary"));
});

test("a deadline row: the chip sits under the title with the subject, the row is a plain column (TASK-345)", () => {
  const out = html(h(BurningSection, { now: new Date(2026, 9, 21, 12, 0), burning: [{ assignmentId: "a", title: "Реферат", subject: "История", due: at(23, 20, 0), daysLeft: 2, urgent: false }], onOpenAssignment: noop }));
  assert.match(out, /today-t">Реферат<\/div><div class="today-dlmeta"><div class="small muted today-subject" title="История">История<\/div><span class="chip s-neutral"/);
  const button = out.match(/<button[^>]*today-dl[^>]*>([^]*?)<\/button>/)[1];
  assert.ok(button.includes("chip") && button.indexOf("today-main") < button.indexOf("chip"), "the chip is inside the main column");
  const css = readFileSync(new URL("../src/pages/today.css", import.meta.url), "utf8");
  assert.match(css, /\.today-dl \{ grid-template-columns: minmax\(0, 1fr\); /);
  assert.match(css, /\.today-dlmeta \{ display: flex; flex-wrap: wrap;/);
});

test("the status bar shows the queue and the nearest deadline only, and is absent when both are empty (TASK-345)", () => {
  assert.deepEqual(statusParts("", ""), []);
  assert.deepEqual(statusParts("В очереди: 2", ""), ["В очереди: 2"]);
  assert.deepEqual(statusParts("В очереди: 2", "Ближайший срок — пт"), ["В очереди: 2", "Ближайший срок — пт"]);
  assert.equal(html(h(StatusBar, { queue: "", nearest: "" })), "");
  const bar = html(h(StatusBar, { queue: "В очереди: 2", nearest: "Ближайший срок — пт, 23 окт., 20:00" }));
  assert.match(bar, /class="statusbar"/);
  assert.match(bar, /В очереди: 2/);
  assert.match(bar, /Ближайший срок/);
  assert.doesNotMatch(bar, /Звук обрабатывается|<svg/);
  assert.ok(!("statusTexts" in todayView));
});

test("an urgent deadline chip has the clock icon and the word, a plain one has none (TASK-345)", () => {
  const now = new Date(2026, 9, 21, 12, 0);
  const row = (due, urgent) => html(h(BurningSection, { now, burning: [{ assignmentId: "a", title: "Реферат", subject: "", due: at(...due), daysLeft: 0, urgent }], onOpenAssignment: noop }));
  const today = row([21, 20, 0], true);
  // TASK-367 review: urgent is amber (wait) with the clock, not the quiet in-progress grey (s-run).
  assert.match(today, /s-wait[^>]*><svg[^]*?<\/svg><span class="chip-text"><span class="num">сегодня до 20:00<\/span>/);
  assert.match(row([22, 18, 30], true), /s-wait[^>]*><svg[^]*?<\/svg><span class="chip-text"><span class="num">завтра до 18:30/);
  assert.doesNotMatch(today, /s-run/);
  // TASK-345 review: a passed moment is a failure chip with the warning icon, not the «сегодня» one.
  const gone = row([20, 9, 0], true);
  assert.match(gone, /s-fail[^>]*><svg[^]*?<\/svg><span class="chip-text"><span class="num">просрочено, вт, 20 окт., 09:00/);
  assert.doesNotMatch(gone, /s-run|s-wait/);
  assert.match(row([21, 9, 0], false), /s-fail/, "overdue wins over a calm row");
  assert.doesNotMatch(today, /s-fail/);
  const calm = row([26, 18, 30], false);
  assert.match(calm, /s-neutral[^>]*><span class="chip-text"><span class="num">до пн, 26 окт., 18:30/);
  assert.doesNotMatch(calm, /<svg/);
  assert.doesNotMatch(today, /today-day/, "the chip carries the date: no separate column");
});

test("rows: live chip, urgent chip, the whole row opens the lesson (TASK-352), no confirm buttons", () => {
  const live = html(h(LessonsSection, { lessons: [lesson()], recordings: [rec({ stage: "recording" })], onOpenLesson: noop }));
  assert.match(live, /Идёт запись/);
  assert.match(html(h(LessonsSection, { lessons: [lesson()], recordings: [rec({ stage: "transcribing", progressPercent: 40 })], onOpenLesson: noop })), /Расшифровка · 40 %/);

  // The row itself is the open (a title button stretched over it); no separate «Открыть» button is left.
  const ready = html(h(LessonsSection, { lessons: [lesson({ post: post() })], recordings: [], onOpenLesson: noop }));
  assert.match(ready, /Конспект готов/);
  assert.match(ready, /<button type="button" class="row-open" title="Открыть пару" aria-label="Открыть пару: Матанализ">Матанализ<\/button>/);
  assert.doesNotMatch(ready, />Открыть</);
  // A pending summary opens too: the lesson screen shows where it stands and holds the transcript.
  const pending = html(h(LessonsSection, { lessons: [lesson({ post: post({ summaryReady: false }) })], recordings: [], onOpenLesson: noop }));
  assert.match(pending, /class="row-open"/);

  const burning = html(h(BurningSection, { now: new Date(2026, 9, 21, 12, 0), burning: [{ assignmentId: "a", title: "Реферат", subject: "", due: at(23), daysLeft: 2, urgent: true }, { assignmentId: "b", title: "ЛР", subject: "", due: at(26), daysLeft: 5, urgent: false }], onOpenAssignment: noop }));
  assert.match(burning, /s-wait[^>]*>.*до пт, 23 окт\., 00:00/);
  assert.match(burning, /s-neutral[^>]*>.*до пн, 26 окт\., 00:00/);

  const waiting = html(h(WaitingSection, { waiting: [{ proposalId: "p", kind: "create_deadline_event", assignmentId: "a", title: "ДЗ", subject: "", due: at(24), recipient: "", createdAt: "" }] }));
  assert.ok(!waiting.includes("<button"));
});

test("a failed recording of the day shows a problem chip that opens «Записи»", () => {
  const failed = (over = {}) => rec({ stage: "failed", errorCode: "transcriptionFailed", status: "COMPLETED", startedAt: at(21, 10, 30), ...over });
  assert.equal(problemFor("l1", [rec()], at(21)), null);
  assert.deepEqual(problemFor("l1", [failed()], at(21)), { label: "Проблема с записью", kind: "fail", icon: "warn" });
  assert.equal(problemFor("l1", [failed({ errorCode: "silentRecording" })], at(21)).label, "Запись без звука");
  assert.equal(problemFor("l1", [failed({ startedAt: at(1) })], at(21)), null);
  assert.equal(problemFor("l2", [failed()], at(21)), null);
  assert.equal(lessonLine(lesson(), [failed()], at(21)).problem.label, "Проблема с записью");
  assert.equal(looseProblems(["l1"], [failed()], at(21)).length, 0);
  assert.equal(looseProblems(["l1"], [failed({ lessonId: "" })], at(21)).length, 1);

  let opened = 0;
  const out = html(h(LessonsSection, { lessons: [lesson()], recordings: [failed()], day: at(21), onOpenLesson: noop, onOpenRecordings: () => opened++ }));
  assert.match(out, /Проблема с записью/);
  assert.match(out, /<button[^>]*today-problem/);
  const loose = html(h(LessonsSection, { lessons: [], recordings: [failed({ lessonId: "" })], day: at(21), onOpenLesson: noop, onOpenRecordings: noop }));
  assert.match(loose, /Проблема с записью/);
});

test("only the newest reply of the screen is applied", () => {
  const r = newestOnly();
  const a = r.next();
  const b = r.next();
  assert.equal(r.isNewest(a), false);
  assert.equal(r.isNewest(b), true);
});

test("the first reply of the recordings list is not a change", () => {
  assert.equal(stagesChanged(null, "r:done"), false);
  assert.equal(stagesChanged("", ""), false);
  assert.equal(stagesChanged("", "r:recording"), true);
  assert.equal(stagesChanged("r:recording", "r:saving"), true);
  assert.equal(stagesChanged("r:done", "r:done"), false);
});

test("a waiting row opens its assignment, with no confirm buttons", () => {
  const w = { proposalId: "p", kind: "create_deadline_event", assignmentId: "a1", title: "ДЗ", subject: "", due: at(24), recipient: "", createdAt: "" };
  const withNav = html(h(WaitingSection, { waiting: [w], onOpenAssignment: noop }));
  assert.equal((withNav.match(/<button/g) ?? []).length, 1);
  assert.ok(!/<button[^>]*>[^<]*(Да|Подтвердить)/.test(withNav));
  assert.ok(!html(h(WaitingSection, { waiting: [{ ...w, assignmentId: "" }], onOpenAssignment: noop })).includes("<button"));
});
