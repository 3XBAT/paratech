// TASK-227: the page of a subject (overview, «Ближайшее», lessons, «новое») and lastProblem / sentAt of a work.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { libraryApi, methods, parseOverview, parseLesson, parseSlot, parseDeadline } from "../.test-build/bindings/library.js";
import {
  daysChip,
  durationText,
  freshMarks,
  lessonChip,
  lessonMeta,
  lessonTitle,
  scheduleLine,
  SubjectOverviewFlow,
  texts,
  weekdayShort,
} from "../.test-build/libraryView.js";
import { OverviewSections, SubjectDetail, SubjectTree } from "../.test-build/pages/SubjectsParts.js";
import { parseWork } from "../.test-build/bindings/turnkey.js";
import { triageReason, turnkeyTexts } from "../.test-build/turnkeyView.js";
import { sendWorkTexts, sentNote, initialSendWork } from "../.test-build/sendWorkView.js";
import { formatTimeMinutes } from "../.test-build/activityView.js";
import { WorkBody } from "../.test-build/agent/WorkCard.js";
import { WorkSendView } from "../.test-build/agent/WorkSend.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const evil = "<b>x</b>";
const evilEscaped = "&lt;b&gt;x&lt;/b&gt;";

const lesson = (over = {}) => ({
  recordingId: "r1", title: "Матрицы", kind: "lecture", startedAt: "2026-10-20T12:00:00Z", durationSeconds: 5412,
  status: "summaryReady", summaryNew: false, ...over,
});

const overview = (over = {}) => ({
  subjectId: "s1", name: "Линейная алгебра", openedAt: "2026-10-20T08:00:00Z",
  deadlines: [{ assignmentId: "a1", title: "Задачи 3, 5, 8", due: "2026-10-27T12:00:00Z", daysLeft: 6 }],
  schedule: [{ kind: "lecture", weekday: 2, start: "10:30", end: "12:00" }, { kind: "seminar", weekday: 1, start: "12:10", end: "13:40" }],
  lessons: [lesson()],
  newMaterials: [{ materialId: "m1", folderId: "f1" }],
  ...over,
});

// ---- bindings ----

test("the overview binding is called by name and parsed strictly", async () => {
  const calls = [];
  const api = libraryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return method === methods.subjectOverview ? overview({ deadlines: null, lessons: null }) : null;
  });
  const o = await api.subjectOverview("s1");
  assert.deepEqual([o.deadlines, o.lessons], [[], []]);
  await api.markSubjectOpened("s1");
  assert.deepEqual(calls, [[methods.subjectOverview, "s1"], [methods.markSubjectOpened, "s1"]]);
});

test("an unknown lesson status, a bad weekday or a malformed deadline is rejected", () => {
  assert.throws(() => parseLesson({ ...lesson(), status: "weird" }));
  assert.throws(() => parseLesson({ ...lesson(), summaryNew: "yes" }));
  assert.throws(() => parseLesson({ ...lesson(), durationSeconds: -1 }));
  assert.throws(() => parseSlot({ kind: "lecture", weekday: 8, start: "10:30", end: "12:00" }));
  assert.throws(() => parseDeadline({ assignmentId: "a", title: "t", due: "x", daysLeft: 1.5 }));
  assert.throws(() => parseOverview(null));
  assert.throws(() => parseOverview({ ...overview(), lessons: [{ ...lesson(), status: "x" }] }));
  assert.equal(parseOverview(overview()).lessons[0].status, "summaryReady");
});

// ---- view logic ----

test("the schedule line, weekdays, day chips and lesson texts", () => {
  assert.equal(scheduleLine(overview().schedule), "Лекции — вт, 10:30 · Семинары — пн, 12:10");
  assert.equal(scheduleLine([]), "");
  assert.equal(weekdayShort(7), "вс");
  assert.deepEqual(daysChip(0), { kind: "wait", label: "сегодня" });
  assert.equal(daysChip(1).label, "завтра");
  assert.equal(daysChip(2).label, "2 дня");
  assert.equal(daysChip(6).label, "6 дней");
  assert.equal(daysChip(21).label, "21 день");
  assert.equal(daysChip(12).label, "12 дней");
  assert.equal(daysChip(-1).label, "просрочено");
  assert.equal(durationText(5412), "1:30:12");
  assert.equal(durationText(125), "2:05");
  assert.equal(durationText(0), "");
});

test("a lesson without a title is named by its date; the status chips are short Russian words", () => {
  assert.equal(lessonTitle(lesson({ title: "Смена базиса" })), "Смена базиса");
  assert.match(lessonTitle(lesson({ title: "" })), /^Занятие \d\d\.\d\d$/);
  assert.match(lessonMeta(lesson()), /\d\d\.\d\d · 1:30:12$/);
  assert.equal(lessonMeta(lesson({ durationSeconds: 0 })).includes("·"), false);
  assert.deepEqual(
    ["recorded", "transcribed", "summaryReady", "failed"].map((s) => lessonChip(s).label),
    ["Записано", "Расшифровано", "Конспект готов", "Ошибка"],
  );
});

test("fresh marks collect folders and materials of the visit", () => {
  const f = freshMarks(overview({ newMaterials: [{ materialId: "m1", folderId: "f1" }, { materialId: "m2", folderId: "f1" }] }));
  assert.deepEqual([...f.folders], ["f1"]);
  assert.deepEqual([...f.materials], ["m1", "m2"]);
  assert.equal(freshMarks(null).folders.size, 0);
});

// ---- the flow ----

function flowApi(over = {}) {
  const calls = [];
  return {
    calls,
    subjectOverview: async (id) => {
      calls.push(["overview", id]);
      if (over.fail) throw new Error("boom");
      return overview({ subjectId: id });
    },
    markSubjectOpened: async (id) => {
      calls.push(["mark", id]);
      if (over.markFail) throw new Error("mark");
    },
  };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

test("the overview is loaded, then the opening is marked exactly once", async () => {
  const api = flowApi();
  const states = [];
  const flow = new SubjectOverviewFlow(api, (s) => states.push(s));
  flow.open("s1");
  await settle();
  assert.deepEqual(api.calls, [["overview", "s1"], ["mark", "s1"]]);
  assert.deepEqual(states.map((s) => s.status), ["loading", "ready"]);
  // Nothing re-marks without a new opening.
  await settle();
  assert.equal(api.calls.filter((c) => c[0] === "mark").length, 1);
});

test("a failed overview shows a Russian line and marks nothing; a failed mark is silent", async () => {
  const bad = flowApi({ fail: true });
  const states = [];
  new SubjectOverviewFlow(bad, (s) => states.push(s)).open("s1");
  await settle();
  assert.deepEqual(bad.calls, [["overview", "s1"]]);
  assert.equal(states.at(-1).status, "failed");
  assert.equal(states.at(-1).error, texts.overviewFailed);

  const markBad = flowApi({ markFail: true });
  const s2 = [];
  new SubjectOverviewFlow(markBad, (s) => s2.push(s)).open("s1");
  await settle();
  assert.equal(s2.at(-1).status, "ready");
});

test("a closed or superseded opening neither shows nor marks", async () => {
  const api = flowApi();
  const states = [];
  const flow = new SubjectOverviewFlow(api, (s) => states.push(s));
  flow.open("s1");
  flow.close();
  await settle();
  assert.deepEqual(api.calls, [["overview", "s1"]]);
  assert.deepEqual(states.map((s) => s.status), ["loading"]);

  flow.open("s1");
  flow.open("s2");
  await settle();
  assert.deepEqual(api.calls.filter((c) => c[0] === "mark"), [["mark", "s2"]]);
});

// ---- rendering ----

test("«Ближайшее» and the lessons render with chips and «новое»; titles are text nodes", () => {
  const o = overview({
    deadlines: [{ assignmentId: "a1", title: evil, due: "2026-10-27T12:00:00Z", daysLeft: 0 }],
    lessons: [lesson({ title: evil, summaryNew: true }), lesson({ recordingId: "r2", title: "", status: "failed" })],
  });
  const out = html(h(OverviewSections, { overview: o }));
  assert.ok(out.includes(texts.upcoming));
  assert.ok(out.includes(evilEscaped));
  assert.ok(!out.includes("<b>x</b>"));
  assert.ok(out.includes("сегодня"));
  assert.ok(out.includes("Конспект готов"));
  assert.ok(out.includes("Ошибка"));
  assert.equal(out.split(">новое<").length - 1, 1);
});

test("empty sections are omitted", () => {
  assert.equal(html(h(OverviewSections, { overview: overview({ deadlines: [], lessons: [] }) })), "");
});

test("folders and files of the visit carry «новое»; without marks the old page is unchanged", () => {
  const subject = { id: "s1", name: "Алгебра", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [{ id: "f1", name: "Лекции", system: "lectures" }, { id: "f2", name: "Семинары", system: "seminars" }] };
  const materials = [{ id: "m1", name: evil, type: "pdf", sizeBytes: 10, subjectId: "s1", folderId: "f1", inbox: false, removed: false, textStatus: "", textNote: "", createdAt: "" }];
  const props = { subject, materials, folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop };
  const plain = html(h(SubjectDetail, props));
  assert.ok(!plain.includes(">новое<"));
  const marked = html(h(SubjectDetail, { ...props, fresh: freshMarks(overview()) }));
  // The folder heading and the file row.
  assert.equal(marked.split(">новое<").length - 1, 2);
  assert.ok(marked.includes(evilEscaped));
  const tree = html(h(SubjectTree, { subjects: [subject], currentId: "s1", counts: new Map(), freshFolders: new Set(["f1"]), busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop }));
  assert.equal(tree.split(">новое<").length - 1, 1);
});

// ---- the work: lastProblem and sentAt ----

const baseWork = { id: "w1", status: "triage", version: 1, problem: "", message: "" };

test("parseWork reads lastProblem, lastMessage and sentAt strictly; an older reply gives the defaults", () => {
  const w = parseWork({ ...baseWork, lastProblem: "offline", lastMessage: "Нет связи.", sentAt: "2026-10-20T10:00:00Z" });
  assert.deepEqual([w.lastProblem, w.lastMessage, w.sentAt], ["offline", "Нет связи.", "2026-10-20T10:00:00Z"]);
  const old = parseWork(baseWork);
  assert.deepEqual([old.lastProblem, old.lastMessage, old.sentAt], ["", "", ""]);
  for (const p of ["offline", "quota", "refused", "no_text", "source_gone"]) {
    assert.equal(parseWork({ ...baseWork, lastProblem: p }).lastProblem, p);
  }
  assert.throws(() => parseWork({ ...baseWork, lastProblem: "weird" }));
  assert.throws(() => parseWork({ ...baseWork, lastProblem: 3 }));
  assert.throws(() => parseWork({ ...baseWork, sentAt: "yesterday" }));
  assert.throws(() => parseWork({ ...baseWork, lastMessage: 5 }));
});

const work = (over) => parseWork({ ...baseWork, ...over });
const renderBody = (w) => html(h(WorkBody, { work: w, api: {}, onWork: noop, library: {} }));

test("a triage without a call problem shows the stored lastMessage; the call's own message wins", () => {
  const stored = work({ lastProblem: "quota", lastMessage: "Лимит запросов исчерпан: разбор не завершён." });
  assert.equal(triageReason(stored), "Лимит запросов исчерпан: разбор не завершён.");
  const out = renderBody(stored);
  assert.ok(out.includes("Лимит запросов исчерпан"));
  assert.ok(out.includes(turnkeyTexts.retry));
  const fresh = work({ problem: "offline", message: "Нет связи.", lastProblem: "quota", lastMessage: "Лимит." });
  assert.equal(triageReason(fresh), "Нет связи.");
  assert.equal(triageReason(work({ lastProblem: "refused", lastMessage: "" })), turnkeyTexts.triageFailed);
  assert.equal(triageReason(work({})), "");
});

test("a failed work shows its stored reason", () => {
  const out = renderBody(work({ status: "failed", lastProblem: "refused", lastMessage: "Ответ не прошёл проверку." }));
  assert.ok(out.includes("Ответ не прошёл проверку."));
});

test("the lastMessage is shown as text", () => {
  const out = renderBody(work({ lastProblem: "offline", lastMessage: evil }));
  assert.ok(out.includes(evilEscaped));
});

test("sentAt drives «Отправить ещё раз»; the session flag only bridges the gap", () => {
  const at = "2026-10-20T10:00:00Z";
  const sent = html(h(WorkSendView, { state: initialSendWork, sendable: true, sentAt: at, onSend: noop }));
  assert.ok(sent.includes(`Отправлено ${formatTimeMinutes(at)}`));
  assert.ok(!/Отправлено \S+,? \d\d:\d\d:\d\d/.test(sent));
  assert.match(sentNote(at, false).text, /^Отправлено \d\d\.\d\d\.\d{4}, \d\d:\d\d\. /);
  assert.ok(sent.includes(sendWorkTexts.again));
  assert.ok(!sent.includes(`>${sendWorkTexts.button}<`));

  const never = html(h(WorkSendView, { state: initialSendWork, sendable: true, sentAt: "", onSend: noop }));
  assert.ok(never.includes(`>${sendWorkTexts.button}<`));
  assert.ok(!never.includes("Отправлено"));

  const justSent = html(h(WorkSendView, { state: { ...initialSendWork, sentBefore: true }, sendable: true, sentAt: "", onSend: noop }));
  assert.ok(justSent.includes(sendWorkTexts.alreadySent));
  assert.deepEqual(sentNote("", false), { sent: false, text: "" });
  assert.equal(sentNote(at, false).sent, true);
});
