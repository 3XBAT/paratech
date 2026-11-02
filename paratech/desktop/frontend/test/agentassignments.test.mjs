// The assignment list of the reply card (TASK-176): parsing, rows, overdue, plain-text titles, empty list, click wiring.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { parseCard } from "../.test-build/bindings/agent.js";
import { assignmentRow, entryModel } from "../.test-build/agentView.js";
import { AgentRequest, AssignmentList } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const now = new Date(2026, 9, 26, 12, 0, 0);
const iso = (day) => `2026-10-${String(day).padStart(2, "0")}`;
const base = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined, now };

const raw = (assignments) => ({
  kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
  cannotDo: null, text: "Ближайшие задания", stopReason: "", assignments,
});

function render(assignments, actions = {}) {
  const card = parseCard(raw(assignments));
  const u = { threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "", problem: "", cards: [card] };
  const model = entryModel({ key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "", update: u });
  return renderToStaticMarkup(h(AgentRequest, { model, actions: { ...base, ...actions } }));
}

// time "" is a line of an older core: the rows then read the day only.
const line = (over) => ({ id: "a1", title: "Задачи 1-7", subjectId: "s1", due: iso(30), time: "", ...over });

test("the card parser keeps the assignment lines", () => {
  const c = parseCard(raw([line()]));
  assert.deepEqual(c.assignments, [line()]);
});

test("rows say today, tomorrow, a date, overdue; unknown subject is omitted", () => {
  const subjects = { s1: "Матанализ" };
  assert.equal(assignmentRow(line({ due: iso(26) }), subjects, now).due, "сегодня");
  assert.equal(assignmentRow(line({ due: iso(27) }), subjects, now).due, "завтра");
  assert.match(assignmentRow(line({ due: iso(30) }), subjects, now).due, /^до /);
  const late = assignmentRow(line({ due: iso(24) }), subjects, now);
  assert.equal(late.due, "просрочено");
  assert.equal(late.overdue, true);
  assert.equal(assignmentRow(line({ due: "" }), subjects, now).due, "");
  assert.equal(assignmentRow(line(), subjects, now).subject, "Матанализ");
  assert.equal(assignmentRow(line({ subjectId: "zzz" }), subjects, now).subject, "");
  assert.equal(assignmentRow(line({ subjectId: "constructor" }), subjects, now).subject, "");
});

test("a date-only due is a local day, not shifted by the zone", () => {
  const early = new Date(2026, 9, 26, 0, 30);
  assert.equal(assignmentRow(line({ due: "2026-10-26" }), {}, early).due, "сегодня");
  assert.equal(assignmentRow(line({ due: "2026-10-27" }), {}, early).due, "завтра");
  assert.equal(assignmentRow(line({ due: "2026-10-30" }), {}, early).due, "до пт, 30 окт.");
  const late = new Date(2026, 9, 26, 23, 30);
  assert.equal(assignmentRow(line({ due: "2026-10-26" }), {}, late).due, "сегодня");
});

test("rows with a time: today before it, past it, tomorrow, a later day (TASK-343)", () => {
  const row = (due, time, at = now) => assignmentRow(line({ due, time }), {}, at);
  // now is Mon 26 Oct 12:00.
  assert.deepEqual([row(iso(26), "20:00").due, row(iso(26), "20:00").overdue], ["сегодня до 20:00", false]);
  assert.deepEqual([row(iso(26), "09:00").due, row(iso(26), "09:00").overdue], ["просрочено", true]);
  assert.equal(row(iso(27), "18:30").due, "завтра до 18:30");
  assert.equal(row(iso(24), "18:30").due, "просрочено");
  assert.equal(row(iso(30), "18:30").due, "до пт, 30 окт., 18:30");
  assert.equal(row(iso(30), "18:30").overdue, false);
});

test("rows with a time: the moment is read against now, also at the midnight edges", () => {
  const row = (due, time, at) => assignmentRow(line({ due, time }), {}, at);
  // 20:00 deadline: one minute before it is still today, at 20:00 and later it has passed (the old text said «сегодня»).
  assert.equal(row(iso(26), "20:00", new Date(2026, 9, 26, 19, 59)).due, "сегодня до 20:00");
  assert.equal(row(iso(26), "20:00", new Date(2026, 9, 26, 20, 0)).due, "просрочено");
  assert.equal(row(iso(26), "20:00", new Date(2026, 9, 26, 23, 59)).due, "просрочено");
  // Just after midnight the deadline of yesterday evening is overdue, today's evening is today.
  const night = new Date(2026, 9, 27, 0, 1);
  assert.equal(row(iso(26), "20:00", night).due, "просрочено");
  assert.equal(row(iso(27), "20:00", night).due, "сегодня до 20:00");
  assert.equal(row(iso(28), "00:00", night).due, "завтра до 00:00");
  // A 00:00 deadline of today has passed at once.
  assert.equal(row(iso(27), "00:00", night).due, "просрочено");
  // A broken time falls back to the day.
  assert.equal(row(iso(27), "25:00", night).due, "сегодня");
});

test("a row without a time keeps the day-only wording", () => {
  assert.equal(assignmentRow(line({ due: iso(26), time: "" }), {}, now).due, "сегодня");
  assert.equal(assignmentRow(line({ due: iso(30), time: "" }), {}, now).due, "до пт, 30 окт.");
});

test("the list renders title, subject and due; overdue gets the urgent class", () => {
  const out = render([line({ due: iso(24) }), line({ id: "a2", title: "Эссе", due: iso(27) })], { subjects: { s1: "Матанализ" } });
  assert.match(out, /Задачи 1-7/);
  assert.match(out, /Матанализ/);
  assert.match(out, /ag-asg-late[^>]*>просрочено</);
  assert.match(out, />завтра</);
  assert.equal((out.match(/<button/g) ?? []).length, 2);
});

test("a title with markup stays text", () => {
  const out = render([line({ title: "<img src=x onerror=1>" })]);
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=1&gt;"));
});

test("an empty list renders nothing", () => {
  assert.equal(render([]).includes("ag-asg"), false);
  assert.equal(renderToStaticMarkup(h(AssignmentList, { items: [], actions: base })), "");
});

test("a row is a button that opens its assignment", () => {
  const el = AssignmentList({ items: [line()], actions: { ...base, openAssignment: (id) => opened.push(id) } });
  const opened = [];
  const btn = el.props.children[0].props.children.props;
  assert.equal(typeof btn.onClick, "function");
  btn.onClick();
  assert.deepEqual(opened, ["a1"]);
});
