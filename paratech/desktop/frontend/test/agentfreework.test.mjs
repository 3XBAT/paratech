// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-188: the free-work card «Такого задания нет — могу сделать как свободную работу» in the agent bindings.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  agentApi,
  cardKinds,
  freeWorkErrors,
  freeWorkErrorText,
  methods,
  parseCard,
  parseFreeWorkState,
  parseUpdate,
} from "../.test-build/bindings/agent.js";

const ref = { threadId: "th1", turn: 3, callId: "toolu_01" };

const base = {
  plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null,
  text: "", assignments: null, stopReason: "", workId: "", outcome: "", freeWork: null,
};

test("a free_work card carries the call it names, never a topic", () => {
  assert.ok(cardKinds.includes("free_work"));
  const c = parseCard({ ...base, kind: "free_work", freeWork: ref });
  assert.equal(c.kind, "free_work");
  assert.deepEqual(c.freeWork, ref);
  // Other cards and an older core: no ref.
  assert.equal(parseCard({ ...base, kind: "work", workId: "w1", outcome: "questions" }).freeWork, null);
  const { freeWork: _, ...older } = base;
  assert.equal(parseCard({ ...older, kind: "reply", text: "x" }).freeWork, null);
  // A malformed ref drops the card, not the update.
  const u = parseUpdate({
    threadId: "th1", turn: 3, subjectId: "", lessonId: "", assignmentId: "", screen: "", status: "DONE", stopReason: "",
    problem: "", progress: false,
    cards: [{ ...base, kind: "free_work", freeWork: { threadId: "th1", turn: "3", callId: "c" } }, { ...base, kind: "free_work", freeWork: ref }],
  });
  assert.equal(u.cards.length, 1);
  assert.deepEqual(u.cards[0].freeWork, ref);
});

test("freeWork and freeWorkCard call the bound Go methods with the ref only", async () => {
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { state: "done", workId: "w1" };
  });
  assert.deepEqual(await api.freeWork({ ...ref, topic: "не уходит в ядро" }, true, true), { state: "done", workId: "w1" });
  assert.deepEqual(await api.freeWork(ref, false, false), { state: "done", workId: "w1" });
  assert.deepEqual(await api.freeWorkCard(ref), { state: "done", workId: "w1" });
  assert.deepEqual(calls, [
    ["paratech/desktop/internal/input/wails.AgentService.FreeWork", ref, true, true],
    ["paratech/desktop/internal/input/wails.AgentService.FreeWork", ref, false, false],
    ["paratech/desktop/internal/input/wails.AgentService.FreeWorkCard", ref],
  ]);
  assert.equal(methods.freeWork, calls[0][0]);
  assert.equal(methods.freeWorkCard, calls[2][0]);
});

test("the free-work state is parsed strictly", () => {
  for (const s of ["waiting", "done", "undone", "failed"]) {
    assert.deepEqual(parseFreeWorkState({ state: s, workId: "" }), { state: s, workId: "" });
  }
  for (const bad of [null, {}, { state: "declined", workId: "" }, { state: "done" }, { state: "done", workId: 1 }]) {
    assert.throws(() => parseFreeWorkState(bad), /free work state/);
  }
});

test("errors: the core's known texts as they are, anything else the generic one", async () => {
  for (const msg of Object.values(freeWorkErrors)) {
    assert.equal(freeWorkErrorText(new Error(msg)), msg);
  }
  assert.equal(freeWorkErrorText(new Error("open C:\\Users\\student\\db")), freeWorkErrors.failed);
  assert.equal(freeWorkErrorText(null), freeWorkErrors.failed);
  const api = agentApi(async () => {
    throw new Error(freeWorkErrors.notFound);
  });
  await assert.rejects(api.freeWork(ref, true, false), new RegExp(freeWorkErrors.notFound));
});
