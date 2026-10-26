// The action line of create_assignment (TASK-220): wording, «Открыть», «Отменить», «Отменено», plain-text title, labels.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { parseCard } from "../.test-build/bindings/agent.js";
import { addedAssignmentText, entryModel, stepText } from "../.test-build/agentView.js";
import { lineModel } from "../.test-build/journalView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const now = new Date(2026, 9, 26, 12, 0, 0);
const base = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined, now, subjects: { s1: "Матанализ" } };

const added = (over = {}) => ({ id: "a1", title: "Задачи 1-7", subjectId: "s1", due: "2026-10-30", time: "23:59", ...over });

const raw = (assignment) => ({
  kind: "action_line", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "act1", answerId: "",
  cannotDo: null, text: "", stopReason: "", assignments: [], assignment,
});

function model(assignment) {
  const u = { threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "", problem: "", cards: [parseCard(raw(assignment))] };
  return entryModel({ key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "", update: u });
}

const render = (assignment, actions = {}) => renderToStaticMarkup(h(AgentRequest, { model: model(assignment), actions: { ...base, ...actions } }));

test("the text has title, subject and due with date and time", () => {
  assert.equal(addedAssignmentText(added(), base.subjects, now), "Задание добавлено: Задачи 1-7 · Матанализ · срок пт, 30 окт., 23:59");
  assert.equal(addedAssignmentText(added({ due: "2026-10-27", time: "18:00" }), base.subjects, now), "Задание добавлено: Задачи 1-7 · Матанализ · срок завтра, 18:00");
});

test("absent subject, unknown subject and absent due are left out", () => {
  assert.equal(addedAssignmentText(added({ subjectId: "" }), base.subjects, now), "Задание добавлено: Задачи 1-7 · срок пт, 30 окт., 23:59");
  assert.equal(addedAssignmentText(added({ subjectId: "zzz" }), base.subjects, now), "Задание добавлено: Задачи 1-7 · срок пт, 30 окт., 23:59");
  assert.equal(addedAssignmentText(added({ due: "", time: "" }), base.subjects, now), "Задание добавлено: Задачи 1-7 · Матанализ");
});

test("the panel line shows the text, «Открыть» and «Отменить»", () => {
  const out = render(added(), { openAssignment: noop });
  assert.ok(out.includes("Задание добавлено: Задачи 1-7 · Матанализ · срок пт, 30 окт., 23:59"));
  assert.match(out, />Открыть</);
  assert.match(out, />Отменить</);
});

test("«Открыть» opens the assignment, «Отменить» undoes the action", () => {
  const opened = [];
  const undone = [];
  const el = AgentRequest({ model: model(added()), actions: { ...base, openAssignment: (id) => opened.push(id), undo: (id) => undone.push(id) } });
  const buttons = [];
  const walk = (n) => {
    if (n === null || typeof n !== "object") return;
    if (Array.isArray(n)) return n.forEach(walk);
    if (n.type && typeof n.type === "function" && n.props && typeof n.props.onClick === "function") buttons.push(n.props);
    walk(n.props?.children);
    if (typeof n.type === "function" && n.type.name === "Card") walk(n.type(n.props));
  };
  walk(el);
  const open = buttons.find((b) => b.children === "Открыть");
  const undo = buttons.find((b) => b.children === "Отменить");
  assert.ok(open && undo);
  open.onClick();
  undo.onClick();
  assert.deepEqual(opened, ["a1"]);
  assert.deepEqual(undone, ["act1"]);
});

test("after undo the line says «Отменено» with no title and no link", () => {
  const out = render(added(), { undoNote: () => ({ busy: false, done: true, text: "" }) });
  assert.ok(out.includes("Отменено"));
  assert.ok(!out.includes("Задачи 1-7"));
  assert.doesNotMatch(out, />Открыть</);
});

test("a deleted assignment (null) has no link", () => {
  const out = render(null);
  assert.ok(out.includes("Действие выполнено"));
  assert.doesNotMatch(out, />Открыть</);
  assert.match(out, />Отменить</);
});

test("a hostile title is text", () => {
  const out = render(added({ title: "<img src=x onerror=alert(1)>" }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

test("tool labels of the plan and of the journal", () => {
  assert.equal(stepText({ tool: "create_assignment", args: [], deferred: [] }), "Добавление задания");
  const l = { kind: "action", at: "", updatedAt: "", subjectId: "", actionId: "a1", threadId: "t", turn: 1, tool: "create_assignment", jobId: "", jobType: "", status: "DONE", undoable: true, cannotUndo: false, noUndo: "" };
  assert.equal(lineModel(l, 0, new Map()).title, "Добавлено задание");
});
