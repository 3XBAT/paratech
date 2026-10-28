// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-272: chatCurrent binding; its update is parsed exactly like the "agent:update" event.
import { test } from "node:test";
import assert from "node:assert/strict";
import { agentApi, methods, parseCurrent, parseUpdate } from "../.test-build/bindings/agent.js";

const card = {
  kind: "reply",
  plan: null,
  questions: null,
  jobId: "",
  artifact: "",
  target: null,
  actionId: "",
  answerId: "",
  cannotDo: null,
  text: "<b>Привет</b>",
  assignments: [],
  assignment: null,
  stopReason: "",
  workId: "",
  outcome: "",
  freeWork: null,
};

const update = {
  threadId: "th1",
  turn: 2,
  subjectId: "s1",
  lessonId: "",
  assignmentId: "",
  screen: "today",
  status: "DONE",
  stopReason: "",
  problem: "",
  progress: false,
  cards: [card, { kind: "unknown_kind" }],
};

test("the update parses identically to the event parser", () => {
  const v = parseCurrent({ problem: "", update });
  assert.equal(v.problem, "");
  assert.deepEqual(v.update, parseUpdate(update));
  assert.equal(v.update.cards.length, 1);
  assert.equal(v.update.cards[0].text, "<b>Привет</b>");
});

test("an absent update is null", () => {
  assert.deepEqual(parseCurrent({ problem: "", update: null }), { problem: "", update: null });
  assert.deepEqual(parseCurrent({ problem: "" }), { problem: "", update: null });
});

test("a problem passes through without an update; unknown reads as failed", () => {
  for (const p of ["invalid", "busy", "failed", "not_found"]) {
    assert.deepEqual(parseCurrent({ problem: p, update }), { problem: p, update: null });
  }
  assert.deepEqual(parseCurrent({ problem: "weird", update: null }), { problem: "failed", update: null });
  assert.deepEqual(parseCurrent({ update: null }), { problem: "failed", update: null });
});

test("malformed or untrusted fields are rejected as the event parser does", () => {
  const bads = [{ ...update, turn: "2" }, { ...update, threadId: 1 }, { ...update, cards: "x" }, [], "x", 5];
  for (const bad of bads) {
    assert.throws(() => parseUpdate(bad));
    assert.deepEqual(parseCurrent({ problem: "", update: bad }), { problem: "failed", update: null });
  }
  for (const bad of [null, "x", 5, []]) {
    assert.deepEqual(parseCurrent(bad), { problem: "failed", update: null });
  }
});

test("chatCurrent calls the bound method with the thread id", async () => {
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, args]);
    return { problem: "", update };
  });
  const v = await api.chatCurrent("th1");
  assert.deepEqual(calls, [[methods.chatCurrent, ["th1"]]]);
  assert.equal(methods.chatCurrent.endsWith(".AgentService.ChatCurrent"), true);
  assert.deepEqual(v.update, parseUpdate(update));
  assert.deepEqual(await agentApi(async () => 42).chatCurrent("x"), { problem: "failed", update: null });
});
