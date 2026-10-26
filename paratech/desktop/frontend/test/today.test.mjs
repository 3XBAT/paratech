// «Сегодня» (TASK-126): the binding parsing, the view logic and the plain-text rule.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { errorTexts, genericError, methods, parseScreen, todayApi, todayErrorText } from "../.test-build/bindings/today.js";
import { draftEvent, draftText, requestDraft } from "../.test-build/agentDraft.js";
import {
  dayTitle,
  daysLeftText,
  deadlineLine,
  doneLine,
  dueText,
  homeworkText,
  lessonLine,
  liveStatus,
  looseProblems,
  newestOnly,
  problemFor,
  stagesChanged,
  postLine,
  shortDate,
  stagesKey,
  timeText,
  titleOf,
  waitingLine,
} from "../.test-build/todayView.js";
import { AskSection, BurningSection, DoneSection, LessonsSection, PostsSection, WaitingSection } from "../.test-build/pages/TodayParts.js";

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
  assert.equal(dueText(at(24, 23, 59)), "до сб, 24 окт.");
  assert.equal(dueText(""), "");
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
  assert.equal(homeworkText(1, at(24)), "1 задание · до сб, 24 окт.");
  assert.equal(homeworkText(3, ""), "3 задания");
  assert.equal(homeworkText(5, ""), "5 заданий");
  assert.equal(titleOf("  "), "Без названия");
  assert.equal(titleOf("Реферат"), "Реферат");
});

test("live status comes from the recordings of the lesson", () => {
  assert.equal(liveStatus("l1", []), null);
  assert.equal(liveStatus("", [rec({ lessonId: "" , stage: "recording" })]), null);
  assert.equal(liveStatus("l1", [rec()]), null);
  assert.deepEqual(liveStatus("l1", [rec({ stage: "transcribing", progressPercent: 63.6 })]), { kind: "run", text: "Распознавание · 64 %" });
  assert.deepEqual(liveStatus("l1", [rec({ stage: "transcribing", progressPercent: 140 })]), { kind: "run", text: "Распознавание · 100 %" });
  assert.deepEqual(liveStatus("l1", [rec({ stage: "transcribing" }), rec({ id: "rec2", stage: "recording" })]), { kind: "rec", text: "Идёт запись" });
  assert.equal(liveStatus("l2", [rec({ stage: "recording" })]), null);
  assert.notEqual(stagesKey([rec()]), stagesKey([rec({ stage: "recording" })]));
});

test("lesson, waiting, deadline, done and post lines", () => {
  const l = lessonLine(lesson({ post: post() }), []);
  assert.deepEqual([l.start, l.end, l.title, l.meta], ["10:30", "11:50", "Матанализ", "Лекция · Петров А. В."]);
  assert.deepEqual(l.post, { text: "Конспект готов", homework: "1 задание · до сб, 24 окт.", canOpen: true, recordingId: "r1" });
  assert.equal(lessonLine(lesson({ post: post({ summaryReady: false }) }), []).post.canOpen, false);
  assert.equal(lessonLine(lesson({ kind: "", teacher: "" }), []).meta, "");

  const w = waitingLine({ proposalId: "p1", kind: "send_homework_email", assignmentId: "a1", title: "Реферат", subject: "История", due: "", recipient: "teacher@example.test", createdAt: "" });
  assert.equal(w.label, "Письмо преподавателю");
  assert.equal(w.subject, "История");
  assert.equal(w.meta, "→ teacher@example.test");
  assert.equal(waitingLine({ proposalId: "p2", kind: "create_deadline_event", assignmentId: "a1", title: "ДЗ", subject: "", due: at(24), recipient: "", createdAt: "" }).meta, "до сб, 24 окт.");
  assert.equal(waitingLine({ proposalId: "p3", kind: "unknown", assignmentId: "", title: "", subject: "", due: "", recipient: "", createdAt: "" }).label, "Действие");

  const d = deadlineLine({ assignmentId: "a1", title: "Реферат", subject: "История", due: at(23), daysLeft: 2, urgent: true });
  assert.deepEqual([d.weekday, d.date, d.chip, d.urgent], ["пт", "23.10", "через 2 дня", true]);

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
    html(h(LessonsSection, { lessons: [lesson({ title: evil, teacher: evil, post: p })], recordings: [], onOpenSummary: noop })),
    html(h(PostsSection, { posts: [p], onOpenSummary: noop })),
    html(h(DoneSection, { done: [{ recordingId: "r1", lesson: p.lesson, summaryReady: true, homework: 0, at: at(21) }], onOpenSummary: noop })),
    html(h(WaitingSection, { waiting: [{ proposalId: "p", kind: "send_homework_email", assignmentId: "", title: evil, subject: evil, due: "", recipient: evil, createdAt: "" }] })),
    html(h(BurningSection, { burning: [{ assignmentId: "a", title: evil, subject: evil, due: at(23), daysLeft: 1, urgent: false }], onOpenAssignment: noop })),
    html(h(AskSection, { chips: [{ kind: "x", text: evil }], onChip: noop })),
  ];
  for (const s of out) {
    assert.ok(!s.includes("<img"), s);
    assert.ok(s.includes("&lt;img"), s);
  }
});

test("empty sections say so", () => {
  assert.match(html(h(LessonsSection, { lessons: [], recordings: [], onOpenSummary: noop })), /На сегодня пар нет/);
  assert.match(html(h(WaitingSection, { waiting: [] })), /Ничего не ждёт подтверждения/);
  assert.match(html(h(BurningSection, { burning: [], onOpenAssignment: noop })), /сроков на неделю нет/);
  assert.match(html(h(DoneSection, { done: [], onOpenSummary: noop })), /пока ничего не готово/);
  assert.match(html(h(PostsSection, { posts: [], onOpenSummary: noop })), /Сообщений после пар пока нет/);
  assert.match(html(h(AskSection, { chips: [], onChip: noop })), /Подсказок пока нет/);
});

test("rows: live chip, urgent chip, open only for a ready summary, no confirm buttons", () => {
  const live = html(h(LessonsSection, { lessons: [lesson()], recordings: [rec({ stage: "recording" })], onOpenSummary: noop }));
  assert.match(live, /Идёт запись/);
  assert.match(html(h(LessonsSection, { lessons: [lesson()], recordings: [rec({ stage: "transcribing", progressPercent: 40 })], onOpenSummary: noop })), /Распознавание · 40 %/);

  const ready = html(h(LessonsSection, { lessons: [lesson({ post: post() })], recordings: [], onOpenSummary: noop }));
  assert.match(ready, /Конспект готов/);
  assert.match(ready, /Открыть/);
  const pending = html(h(LessonsSection, { lessons: [lesson({ post: post({ summaryReady: false }) })], recordings: [], onOpenSummary: noop }));
  assert.ok(!pending.includes("Открыть"));

  const burning = html(h(BurningSection, { burning: [{ assignmentId: "a", title: "Реферат", subject: "", due: at(23), daysLeft: 2, urgent: true }, { assignmentId: "b", title: "ЛР", subject: "", due: at(26), daysLeft: 5, urgent: false }], onOpenAssignment: noop }));
  assert.match(burning, /s-run[^>]*>.*через 2 дня/);
  assert.match(burning, /s-neutral[^>]*>.*через 5 дней/);

  const waiting = html(h(WaitingSection, { waiting: [{ proposalId: "p", kind: "create_deadline_event", assignmentId: "a", title: "ДЗ", subject: "", due: at(24), recipient: "", createdAt: "" }] }));
  assert.ok(!waiting.includes("<button"));
});

test("a failed recording of the day shows a problem chip that opens all records", () => {
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
  const out = html(h(LessonsSection, { lessons: [lesson()], recordings: [failed()], day: at(21), onOpenSummary: noop, onOpenAll: () => opened++ }));
  assert.match(out, /Проблема с записью/);
  assert.match(out, /<button[^>]*today-problem/);
  const loose = html(h(LessonsSection, { lessons: [], recordings: [failed({ lessonId: "" })], day: at(21), onOpenSummary: noop, onOpenAll: noop }));
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
