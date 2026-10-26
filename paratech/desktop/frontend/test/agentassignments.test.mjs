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
const iso = (day) => new Date(2026, 9, day, 18, 0, 0).toISOString();
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

const line = (over) => ({ id: "a1", title: "Задачи 1-7", subjectId: "s1", due: iso(30), ...over });

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
