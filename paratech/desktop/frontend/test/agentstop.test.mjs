// BUG 2026-11-04: «Остановить» belongs to the running request only, sits at the end of that request (in the flow, not
// floating over the text), and is gone when the request ends.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { entryModel } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const reply = {
  kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
  cannotDo: null, text: "Готовый ответ.", stopReason: "", workId: "", outcome: "",
};
const entry = (turn, cards, status, withUpdate = true) => ({
  key: `k${turn}`, seq: turn, threadId: "t", turn, text: "вопрос", scope: null, callProblem: "",
  update: withUpdate ? { threadId: "t", turn, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status, stopReason: "", problem: "", cards } : null,
});
const stops = (model) => model.cards.filter((c) => c.type === "waiting");

test("a running request whose reply is already shown keeps exactly one stop, after the reply", () => {
  const model = entryModel(entry(1, [reply], "EXECUTING"));
  assert.equal(stops(model).length, 1);
  const out = renderToStaticMarkup(h(AgentRequest, { model, actions }));
  assert.ok(out.indexOf("Готовый ответ.") < out.indexOf("ag-think"), "the stop row is at the end of the request");
  assert.equal(out.split(">Остановить<").length - 1, 1);
});

test("a finished request has no stop; a request still waiting for its first update has dots and no stop", () => {
  assert.equal(stops(entryModel(entry(1, [reply], "DONE"))).length, 0);
  const queued = entryModel(entry(2, [], "", false));
  assert.equal(queued.waiting, true);
  assert.equal(stops(queued).length, 0);
});

test("the stop row is not sticky, absolute or fixed: it scrolls with the thread", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  const rule = css.match(/\.ag-think\s*\{[^}]*\}/)?.[0] ?? "";
  assert.ok(rule !== "");
  assert.doesNotMatch(rule, /position:\s*(?:sticky|absolute|fixed)/);
});
