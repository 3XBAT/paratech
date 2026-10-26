// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-189: the free-work card in the panel and its switch in «Настройки».
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { FreeWorkFlow, freeWorkModel, freeWorkTexts, initialFreeWorkFlow } from "../.test-build/freeWorkView.js";
import { FreeWorkCardView } from "../.test-build/agent/FreeWorkCard.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { entryModel } from "../.test-build/agentView.js";
import { freeWorkErrors } from "../.test-build/bindings/agent.js";
import { methods, settingsApi } from "../.test-build/bindings/settings.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const ref = { threadId: "th1", turn: 3, callId: "toolu_01" };
const waiting = { state: "waiting", workId: "" };
const done = { state: "done", workId: "w1" };

// A fake core: freeWork answers after `gate` is released; calls are recorded.
function fakeApi(o = {}) {
  const calls = [];
  let release = noop;
  const gate = new Promise((r) => (release = r));
  return {
    calls,
    release: () => release(),
    freeWorkCard: async () => {
      calls.push(["card"]);
      const next = o.cards?.shift() ?? o.card ?? waiting;
      if (next instanceof Error) throw next;
      return next;
    },
    freeWork: async (r, accept, dontShow) => {
      calls.push(["freeWork", r, accept, dontShow]);
      if (o.gated) await gate;
      const next = o.answers?.shift() ?? o.answer ?? done;
      if (next instanceof Error) throw next;
      return next;
    },
  };
}

async function started(api) {
  const states = [];
  const flow = new FreeWorkFlow(api, ref, (s) => states.push(s));
  await flow.load();
  return { flow, states };
}

test("the waiting card shows the text, the checkbox and the two buttons", () => {
  const out = html(h(FreeWorkCardView, { state: { ...initialFreeWorkFlow, phase: "waiting" }, onDontShow: noop, onYes: noop, onNo: noop, onReload: noop }));
  assert.match(out, /Такого задания нет — могу сделать как свободную работу \(с уточняющими вопросами\)/);
  assert.match(out, /Сделать/);
  assert.match(out, /Не надо/);
  assert.match(out, /Больше не показывать/);
  assert.match(out, /type="checkbox"/);
  assert.doesNotMatch(out, /disabled/);
});

test("while a call runs the buttons and the checkbox are disabled", () => {
  const out = html(h(FreeWorkCardView, { state: { ...initialFreeWorkFlow, phase: "waiting", busy: true }, onDontShow: noop, onYes: noop, onNo: noop, onReload: noop }));
  assert.equal(out.match(/disabled/g).length, 3);
});

test("a finished card shows a line, not buttons", () => {
  const declined = html(h(FreeWorkCardView, { state: { ...initialFreeWorkFlow, phase: "declined" }, onDontShow: noop, onYes: noop, onNo: noop, onReload: noop }));
  assert.match(declined, /Не делаем/);
  assert.doesNotMatch(declined, /<button|checkbox/);
  assert.equal(freeWorkModel({ ...initialFreeWorkFlow, phase: "undone" }).line, freeWorkTexts.undone);
});

test("a done card without a work reader renders nothing; with the work id it is the work card", () => {
  const state = { ...initialFreeWorkFlow, phase: "done", workId: "w1" };
  assert.equal(freeWorkModel(state).kind, "work");
  assert.equal(html(h(FreeWorkCardView, { state, onDontShow: noop, onYes: noop, onNo: noop, onReload: noop })), "");
});

test("a double click makes one call", async () => {
  const api = fakeApi({ gated: true });
  const { flow } = await started(api);
  const first = flow.answer(true);
  const second = flow.answer(true);
  api.release();
  await Promise.all([first, second]);
  assert.equal(api.calls.filter((c) => c[0] === "freeWork").length, 1);
  assert.equal(flow.state.phase, "done");
  assert.equal(flow.state.workId, "w1");
});

test("the checkbox value is passed as dontShowAgain, for either answer", async () => {
  const a = fakeApi();
  const one = await started(a);
  one.flow.setDontShow(true);
  await one.flow.answer(true);
  assert.deepEqual(a.calls.find((c) => c[0] === "freeWork"), ["freeWork", ref, true, true]);

  const b = fakeApi();
  const two = await started(b);
  await two.flow.answer(false);
  assert.deepEqual(b.calls.find((c) => c[0] === "freeWork"), ["freeWork", ref, false, false]);
});

test("«Не надо» ends with «Не делаем»", async () => {
  const api = fakeApi({ answer: waiting });
  const { flow } = await started(api);
  await flow.answer(false);
  assert.equal(flow.state.phase, "declined");
  assert.equal(freeWorkModel(flow.state).line, "Не делаем");
  // Nothing more can be pressed.
  await flow.answer(true);
  assert.equal(api.calls.filter((c) => c[0] === "freeWork").length, 1);
});

test("a reopened old thread draws the outcome without a call to freeWork", async () => {
  const api = fakeApi({ card: done });
  const { flow } = await started(api);
  assert.equal(flow.state.phase, "done");
  assert.equal(flow.state.workId, "w1");
  assert.deepEqual(api.calls, [["card"]]);

  const undone = await started(fakeApi({ card: { state: "undone", workId: "w1" } }));
  assert.equal(undone.flow.state.phase, "undone");

  // «Нет» stores nothing: a declined card reads as waiting again.
  const again = await started(fakeApi({ card: waiting }));
  assert.equal(again.flow.state.phase, "waiting");
});

test("a failed second press re-reads the card and shows the work when it is done", async () => {
  const api = fakeApi({ answer: { state: "failed", workId: "" }, cards: [waiting, done] });
  const { flow } = await started(api);
  await flow.answer(true);
  assert.equal(flow.state.phase, "done");
  assert.equal(flow.state.error, "");
  assert.equal(api.calls.filter((c) => c[0] === "card").length, 2);
});

test("a refused call that did not start the work shows the core's text and brings the buttons back", async () => {
  const api = fakeApi({ answer: new Error(freeWorkErrors.unavailable), card: waiting });
  const { flow } = await started(api);
  await flow.answer(true);
  assert.equal(flow.state.phase, "waiting");
  assert.equal(flow.state.busy, false);
  assert.equal(flow.state.error, freeWorkErrors.unavailable);

  const failed = fakeApi({ answer: { state: "failed", workId: "" }, card: waiting });
  const f = await started(failed);
  await f.flow.answer(true);
  assert.equal(f.flow.state.error, freeWorkErrors.failed);
  assert.equal(f.flow.state.busy, false);
});

test("an unreadable card offers to reload instead of buttons", async () => {
  const api = fakeApi({ cards: [new Error("boom C:\\secret"), waiting] });
  const { flow } = await started(api);
  assert.equal(flow.state.phase, "unknown");
  assert.equal(flow.state.error, freeWorkErrors.failed);
  const out = html(h(FreeWorkCardView, { state: flow.state, onDontShow: noop, onYes: noop, onNo: noop, onReload: noop }));
  assert.match(out, /Обновить/);
  assert.doesNotMatch(out, /Сделать/);
  await flow.load();
  assert.equal(flow.state.phase, "waiting");
});

// ---- In the panel ----

const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const card = (freeWork) => ({
  kind: "free_work", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null,
  text: "", assignments: [], stopReason: "", workId: "", outcome: "", freeWork,
});
const update = (cards) => ({
  threadId: "th1", turn: 3, subjectId: "", lessonId: "", assignmentId: "", screen: "", status: "AWAITING_STUDENT", stopReason: "", problem: "", progress: false, cards,
});
const modelOf = (u) => entryModel({ key: "k", seq: 1, threadId: "th1", turn: 3, text: "", scope: null, callProblem: "", update: u });

test("the panel makes a free_work card of the update and skips one without a call", () => {
  const m = modelOf(update([card(ref), card(null)]));
  assert.deepEqual(m.cards.map((c) => c.type), ["free_work"]);
  assert.deepEqual(m.cards[0].ref, ref);
  // Without the core calls the card is not drawn; with them it starts by reading the state.
  assert.doesNotMatch(html(h(AgentRequest, { model: m, actions })), /ag-fw/);
  const out = html(h(AgentRequest, { model: m, actions: { ...actions, freeWork: fakeApi() } }));
  assert.match(out, /Свободная работа/);
  assert.match(out, /Загрузка…/);
});

// ---- «Настройки» ----

test("the settings switch calls SetFreeWorkNotice and takes the stored value", async () => {
  const stored = { recordingPromptsEnabled: true, processOnlyOnAcPower: false, recognitionLoad: "gentle", loadEstimates: null, loadEstimatesModel: "", freeWorkNotice: true };
  const calls = [];
  const api = settingsApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return stored;
  });
  assert.equal((await api.setFreeWorkNotice(true)).freeWorkNotice, true);
  assert.deepEqual(calls, [[methods.setFreeWorkNotice, true]]);
});
