// TASK-318: the long plan card of the agent panel: collapse after 8 steps, one «Да» for N steps, step progress.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { parseCard } from "../.test-build/bindings/agent.js";
import { entryModel } from "../.test-build/agentView.js";
import {
  approveLabel,
  failedStepIndex,
  moreStepsLabel,
  planProgress,
  planWindow,
  progressLabel,
  stepMarks,
  stepsWord,
} from "../.test-build/planView.js";
import { AgentRequest, PlanSteps } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const step = (i, over = {}) => ({
  tool: "create_subject",
  args: [{ name: "name", type: "label", idKind: "", value: `Предмет ${i}`, values: [] }],
  deferred: [],
  ...over,
});

const rawCard = (plan) => ({
  kind: "plan", plan, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null,
  text: "", assignments: [], assignment: null, stopReason: "", workId: "", outcome: "", freeWork: null,
});

/** A plan of n steps; `states` (optional) gives the state of every step. */
function plan(n, states, summary = "Создать предметы") {
  return {
    summary,
    steps: Array.from({ length: n }, (_, i) => step(i + 1, states && states[i] !== undefined ? { state: states[i] } : {})),
    notes: [],
  };
}

function modelOf(p, status = "AWAITING_PLAN") {
  const u = {
    threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status,
    stopReason: "", problem: "", progress: false, cards: [rawCard(p)],
  };
  return entryModel({ key: "k", seq: 1, threadId: "t1", turn: 1, text: "", scope: null, callProblem: "", update: u });
}

const html = (p, status) => renderToStaticMarkup(h(AgentRequest, { model: modelOf(p, status), actions }));
const count = (s, re) => (s.match(re) ?? []).length;
const lis = (s) => count(s, /<li[ >]/g);

// ---- View model ----

test("planWindow: collapse boundaries 0, 1, 3, 8, 9 and 30", () => {
  for (const n of [0, 1, 3, 8]) {
    const w = planWindow(n, false, -1);
    assert.deepEqual([w.shown, w.hidden, w.canToggle, w.scroll], [n, 0, false, false], `n=${n}`);
  }
  const nine = planWindow(9, false, -1);
  assert.deepEqual([nine.shown, nine.hidden, nine.canToggle, nine.expanded, nine.scroll], [8, 1, true, false, false]);
  const thirty = planWindow(30, false, -1);
  assert.deepEqual([thirty.shown, thirty.hidden, thirty.canToggle], [8, 22, true]);
  const open = planWindow(30, true, -1);
  assert.deepEqual([open.shown, open.hidden, open.canToggle, open.expanded, open.scroll], [30, 0, true, true, true]);
  // A short plan never has a toggle or a scroll area, whatever "expanded" says.
  assert.equal(planWindow(8, true, -1).canToggle, false);
  assert.equal(planWindow(8, true, -1).scroll, false);
});

test("planWindow: a failed step beyond the first 8 opens the list and takes the toggle away", () => {
  const w = planWindow(30, false, 20);
  assert.deepEqual([w.shown, w.hidden, w.canToggle, w.expanded, w.scroll], [30, 0, false, true, true]);
  // A failure among the first 8 changes nothing.
  assert.deepEqual([planWindow(30, false, 7).shown, planWindow(30, false, 7).canToggle], [8, true]);
  // The step right after the limit (index 8) is already hidden.
  assert.equal(planWindow(9, false, 8).shown, 9);
});

test("step counter and «Ещё N шагов» use the Russian plural", () => {
  const words = { 1: "1 шаг", 2: "2 шага", 4: "4 шага", 5: "5 шагов", 11: "11 шагов", 12: "12 шагов", 14: "14 шагов", 21: "21 шаг", 22: "22 шага", 111: "111 шагов", 200: "200 шагов" };
  for (const [n, want] of Object.entries(words)) {
    assert.equal(stepsWord(Number(n)), want);
    assert.equal(moreStepsLabel(Number(n)), `Ещё ${want}`);
  }
});

test("approve label names the number of steps; a plan with no steps keeps «Да»", () => {
  assert.equal(approveLabel(1), "Да, выполнить 1 шаг");
  assert.equal(approveLabel(2), "Да, выполнить 2 шага");
  assert.equal(approveLabel(5), "Да, выполнить 5 шагов");
  assert.equal(approveLabel(30), "Да, выполнить 30 шагов");
  assert.equal(approveLabel(0), "Да");

  const m = modelOf(plan(5)).cards[0];
  assert.deepEqual(m.buttons.map((b) => [b.id, b.label]), [["approve", "Да, выполнить 5 шагов"], ["revise", "Изменить"], ["cancel", "Отмена"]]);
  assert.equal(modelOf(plan(0)).cards[0].buttons[0].label, "Да");
  // Only while the plan awaits an answer.
  assert.deepEqual(modelOf(plan(5), "EXECUTING").cards[0].buttons, []);
});

test("planProgress: k of N while executing, only when the core reports states", () => {
  const none = [undefined, undefined, undefined];
  const pending = ["", "", ""];
  assert.equal(planProgress(none, "EXECUTING"), null);
  assert.equal(planProgress([], "EXECUTING"), null);
  assert.deepEqual(planProgress(pending, "EXECUTING"), { current: 1, total: 3 });
  assert.deepEqual(planProgress(["done", "", ""], "EXECUTING"), { current: 2, total: 3 });
  assert.deepEqual(planProgress(["done", "done", ""], "EXECUTING"), { current: 3, total: 3 });
  // Never above N.
  assert.deepEqual(planProgress(["done", "done", "done"], "EXECUTING"), { current: 3, total: 3 });
  // A failed step does not count as done.
  assert.deepEqual(planProgress(["done", "failed", ""], "EXECUTING"), { current: 2, total: 3 });
  // Not executing: no indicator.
  for (const s of ["AWAITING_PLAN", "DONE", "STOPPED", "FAILED", "PLANNING"]) {
    assert.equal(planProgress(["done", "", ""], s), null, s);
  }
  assert.equal(progressLabel({ current: 3, total: 8 }), "Шаг 3 из 8");
});

test("stepMarks: none without states; the first unfinished step runs while executing", () => {
  assert.deepEqual(stepMarks([undefined, undefined], "EXECUTING"), []);
  assert.deepEqual(stepMarks(["done", "", ""], "EXECUTING"), ["done", "current", "pending"]);
  assert.deepEqual(stepMarks(["done", "", ""], "DONE"), ["done", "pending", "pending"]);
  assert.deepEqual(stepMarks(["done", "done"], "DONE"), ["done", "done"]);
  assert.deepEqual(stepMarks(["done", "failed", ""], "EXECUTING"), ["done", "failed", "pending"]);
  assert.equal(failedStepIndex(["done", "failed", "pending"]), 1);
  assert.equal(failedStepIndex(["done", "pending"]), -1);
});

test("parseCard reads the optional step state leniently", () => {
  const parse = (state) => parseCard(rawCard({ summary: "s", steps: [{ tool: "create_subject", args: [], deferred: [], ...state }], notes: [] })).plan.steps[0];
  assert.equal("state" in parse({}), false, "absent stays absent");
  assert.equal("state" in parse({ state: null }), false, "null stays absent");
  assert.equal(parse({ state: "" }).state, "");
  assert.equal(parse({ state: "done" }).state, "done");
  assert.equal(parse({ state: "failed" }).state, "failed");
  assert.equal(parse({ state: "running" }).state, "", "an unknown string reads as pending");
  assert.equal(parse({ state: 7 }).state, "", "a non-string reads as pending and does not throw");
  assert.equal(parse({ state: { a: 1 } }).state, "");
});

// ---- Rendering ----

test("a plan of 3 and of 8 steps shows every step and no «Ещё», no scroll area", () => {
  for (const n of [3, 8]) {
    const out = html(plan(n));
    assert.equal(lis(out), n, `n=${n}`);
    assert.ok(!out.includes("Ещё"), `n=${n}`);
    assert.ok(!out.includes("ag-plan-scroll") && !out.includes("ag-plan-more") && !out.includes("aria-controls"), `n=${n}`);
    assert.ok(!out.includes("Шаги плана"), `n=${n}`);
    assert.ok(out.includes(`Да, выполнить ${stepsWord(n)}`) && out.includes("Изменить") && out.includes("Отмена"));
  }
});

test("a plan of 30 steps is collapsed to 8 with «Ещё 22 шага»; the one «Да» names 30 steps", () => {
  const out = html(plan(30));
  assert.equal(lis(out), 8);
  assert.ok(out.includes("Предмет 8") && !out.includes("Предмет 9"));
  assert.match(out, /<button[^>]*type="button"[^>]*aria-expanded="false"[^>]*>Ещё 22 шага<\/button>/);
  assert.ok(!out.includes("Свернуть") && !out.includes("ag-plan-scroll"));
  assert.ok(out.includes(">Да, выполнить 30 шагов<"));
  assert.equal(count(out, /Да, выполнить/g), 1, "one «Да» for the whole plan");
});

test("PlanSteps expanded: 30 steps, scroll area with a label, «Свернуть», aria-expanded and aria-controls", () => {
  const steps = Array.from({ length: 30 }, (_, i) => `Шаг номер ${i + 1}`);
  const open = renderToStaticMarkup(h(PlanSteps, { steps, expanded: true, onToggle: noop }));
  assert.equal(lis(open), 30);
  assert.ok(open.includes("Шаг номер 30"));
  assert.match(open, /<button[^>]*aria-expanded="true"[^>]*>Свернуть<\/button>/);
  assert.ok(!open.includes("Ещё"));
  assert.match(open, /class="ag-plan-scroll"/);
  assert.match(open, /tabindex="0"/);
  assert.match(open, /aria-label="Шаги плана"/);

  const listId = /<ol id="([^"]+)"/.exec(open)?.[1];
  assert.ok(listId, "the list has an id");
  assert.ok(open.includes(`aria-controls="${listId}"`), "the button controls the list");

  const closed = renderToStaticMarkup(h(PlanSteps, { steps, expanded: false, onToggle: noop }));
  assert.equal(lis(closed), 8);
  assert.match(closed, /aria-expanded="false"/);
  assert.ok(closed.includes("Ещё 22 шага"));
  const closedId = /<ol id="([^"]+)"/.exec(closed)?.[1];
  assert.ok(closed.includes(`aria-controls="${closedId}"`));
  assert.ok(!closed.includes("tabindex"), "no scroll area while collapsed");

  // Up to 8 steps: the markup of a plain list.
  const short = renderToStaticMarkup(h(PlanSteps, { steps: steps.slice(0, 8), expanded: true, onToggle: noop }));
  assert.ok(!short.includes("<button") && !short.includes("tabindex") && !short.includes("<div"));
  assert.equal(renderToStaticMarkup(h(PlanSteps, { steps: [], expanded: false, onToggle: noop })), "");
});

test("a failed step beyond the first 8 shows the whole list and no toggle", () => {
  const states = Array.from({ length: 30 }, (_, i) => (i < 19 ? "done" : i === 19 ? "failed" : ""));
  const out = html(plan(30, states), "STOPPED");
  assert.equal(lis(out), 30);
  assert.ok(out.includes("не удался"));
  assert.ok(!out.includes("ag-plan-more") && !out.includes("Ещё") && !out.includes("Свернуть"));
  assert.match(out, /aria-label="Шаги плана"/);
  // A failure among the first 8 leaves the list collapsed.
  const early = html(plan(30, ["done", "failed"]), "STOPPED");
  assert.equal(lis(early), 8);
  assert.ok(early.includes("не удался") && early.includes("Ещё 22 шага"));
});

test("executing plan: «Шаг 3 из 8» without a live region, done / current / failed markers", () => {
  const out = html(plan(8, ["done", "done", "", "", "", "", "", ""]), "EXECUTING");
  assert.ok(out.includes("Шаг 3 из 8"));
  assert.match(out, /<span class="ag-plan-prog small muted">Шаг 3 из 8<\/span>/);
  assert.ok(!/role="status"[^>]*>Шаг 3/.test(out) && !out.includes("aria-live"), "no live region on the indicator");
  assert.equal(count(out, /выполнен</g), 2, "two done steps, hidden text");
  assert.equal(count(out, /#i-ok/g), 2);
  assert.equal(count(out, /class="ag-dot ag-plan-dot"/g), 1, "one running step");
  assert.ok(out.includes("выполняется"));
  assert.ok(!out.includes("не удался"));
  assert.ok(!out.includes("Да, выполнить") && !out.includes("Изменить"), "no confirm buttons while executing");
  assert.ok(out.includes("Понял так"));

  const failed = html(plan(8, ["done", "failed", "", "", "", "", "", ""]), "EXECUTING");
  assert.ok(failed.includes("#i-x") && failed.includes(">не удался<"));
  assert.equal(count(failed, /ag-plan-dot/g), 0, "no running dot beside a failed step");

  // Indicator never above N.
  const last = html(plan(3, ["done", "done", "done"]), "EXECUTING");
  assert.ok(last.includes("Шаг 3 из 3"));
});

test("done plan: every step marked done, no indicator and no buttons", () => {
  const out = html(plan(3, ["done", "done", "done"]), "DONE");
  assert.equal(count(out, /#i-ok/g), 3);
  assert.ok(!out.includes("Шаг 3 из") && !out.includes("<button"));
});

test("no states from the core: a plain list, no indicator, no markers", () => {
  for (const status of ["AWAITING_PLAN", "EXECUTING", "DONE"]) {
    const out = html(plan(5), status);
    assert.ok(!out.includes("ag-plan-prog") && !out.includes("#i-ok") && !out.includes("#i-x") && !out.includes("ag-plan-dot"), status);
    assert.ok(!out.includes("ag-plan-head") && !out.includes("ag-plan-steps"), status);
    assert.equal(lis(out), 5);
  }
});

// ---- Plain text only ----

test("a step label and the summary with markup stay escaped text", () => {
  const evil = "<img src=x onerror=1>";
  const p = plan(30, undefined, evil);
  p.steps[0] = step(1, { args: [{ name: "name", type: "label", idKind: "", value: evil, values: [] }] });
  p.steps[20] = step(21, { args: [{ name: "name", type: "label", idKind: "", value: evil, values: [] }] });
  const collapsed = html(p);
  assert.ok(!collapsed.includes("<img"), "no element from model text");
  assert.ok(collapsed.includes("&lt;img src=x onerror=1&gt;"));
  const open = renderToStaticMarkup(h(PlanSteps, { steps: modelOf(p).cards[0].steps, marks: [], expanded: true, onToggle: noop }));
  assert.ok(!open.includes("<img") && open.includes("&lt;img src=x onerror=1&gt;"));
});

test("the plan code never sets HTML from text", () => {
  for (const f of ["../src/agent/AgentCards.tsx", "../src/planView.ts", "../src/agentView.ts"]) {
    const src = readFileSync(new URL(f, import.meta.url), "utf8");
    assert.ok(!src.includes("dangerouslySetInnerHTML"), f);
    assert.ok(!src.includes("innerHTML"), f);
  }
});
