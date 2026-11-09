// TASK-398: the queue of waiting questions in the bindings: Ask answers { problem, queued, ticket }, Withdraw, and the
// `ticket` / `queued` fields of agent:update. No panel view yet (TASK-399).
import { test } from "node:test";
import assert from "node:assert/strict";
import { agentApi, maxTicketLength, methods, parseAskResult, parseUpdate, parseWithdrawResult, withdrawResults } from "../.test-build/bindings/agent.js";

const wire = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: true, cards: [], fill: null, ...over,
});

const none = { problem: "", queued: false, ticket: "" };

test("parseAskResult: started, queued with a ticket, a refusal", () => {
  assert.deepEqual(parseAskResult({ problem: "", queued: false, ticket: "" }), none);
  assert.deepEqual(parseAskResult({ problem: "", queued: true, ticket: "q7" }), { problem: "", queued: true, ticket: "q7" });
  assert.deepEqual(parseAskResult({ problem: "offline", queued: false, ticket: "" }), { problem: "offline", queued: false, ticket: "" });
  assert.deepEqual(parseAskResult({ problem: "busy", queued: false, ticket: "" }), { problem: "busy", queued: false, ticket: "" });
});

test("parseAskResult: a queued answer without a valid ticket is a broken core and reads failed", () => {
  const failed = { problem: "failed", queued: false, ticket: "" };
  assert.deepEqual(parseAskResult({ problem: "", queued: true, ticket: "" }), failed);
  assert.deepEqual(parseAskResult({ problem: "", queued: true }), failed);
  assert.deepEqual(parseAskResult({ problem: "", queued: true, ticket: 7 }), failed);
  assert.deepEqual(parseAskResult({ problem: "", queued: true, ticket: "x".repeat(maxTicketLength + 1) }), failed);
  assert.deepEqual(parseAskResult({ problem: "", queued: true, ticket: "x".repeat(maxTicketLength) }), { problem: "", queued: true, ticket: "x".repeat(maxTicketLength) });
  // A refusal never queues.
  assert.deepEqual(parseAskResult({ problem: "offline", queued: true, ticket: "q7" }), failed);
});

test("parseAskResult: an older core answers {problem} only; an unknown problem reads failed", () => {
  assert.deepEqual(parseAskResult({ problem: "" }), none);
  assert.deepEqual(parseAskResult({ problem: "offline" }), { problem: "offline", queued: false, ticket: "" });
  assert.deepEqual(parseAskResult({ problem: "something new" }), { problem: "failed", queued: false, ticket: "" });
  // A ticket without the queued flag is ignored; "queued" must be exactly true.
  assert.deepEqual(parseAskResult({ problem: "", ticket: "q7" }), none);
  assert.deepEqual(parseAskResult({ problem: "", queued: "true", ticket: "q7" }), none);
});

test("parseAskResult: a malformed answer is an error (the panel reads a rejected ask as failed)", () => {
  assert.throws(() => parseAskResult(null));
  assert.throws(() => parseAskResult(42));
  assert.throws(() => parseAskResult({ queued: true, ticket: "q7" }));
});

test("agentApi.ask sends the input and returns the parsed result", async () => {
  const calls = [];
  const answers = [{ problem: "", queued: true, ticket: "q7" }, { problem: "" }, { problem: "offline", queued: false, ticket: "" }];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return answers.shift();
  });
  const input = { text: "x", subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId: "th1" };
  assert.deepEqual(await api.ask(input), { problem: "", queued: true, ticket: "q7" });
  assert.deepEqual(await api.ask(input), none);
  assert.deepEqual(await api.ask(input), { problem: "offline", queued: false, ticket: "" });
  assert.deepEqual(calls[0], [methods.ask, input]);
  assert.equal(methods.ask, "paratech/desktop/internal/input/wails.AgentService.Ask");
});

test("withdraw: the bound name, two positional arguments and every result", async () => {
  assert.equal(methods.withdraw, "paratech/desktop/internal/input/wails.AgentService.Withdraw");
  const calls = [];
  for (const result of withdrawResults) {
    const api = agentApi(async (method, ...args) => {
      calls.push([method, ...args]);
      return { result };
    });
    assert.equal(await api.withdraw("th1", "q7"), result);
  }
  assert.deepEqual(calls[0], [methods.withdraw, "th1", "q7"]);
  assert.deepEqual([...withdrawResults], ["withdrawn", "started", "not_found", "invalid", "failed"]);
});

test("withdraw: an unknown or missing result reads failed, a malformed answer is an error", async () => {
  assert.equal(parseWithdrawResult({ result: "gone" }), "failed");
  assert.equal(parseWithdrawResult({ result: "" }), "failed");
  assert.equal(parseWithdrawResult({ result: 3 }), "failed");
  assert.equal(parseWithdrawResult({}), "failed");
  assert.throws(() => parseWithdrawResult(null));
  const api = agentApi(async () => ({ result: "withdrawn" }));
  assert.equal(await api.withdraw("th1", "q7"), "withdrawn");
});

test("parseUpdate reads ticket and queued; an older core reads \"\" and false", () => {
  const queued = parseUpdate(wire({ status: "", progress: false, ticket: "q7", queued: true }));
  assert.equal(queued.ticket, "q7");
  assert.equal(queued.queued, true);
  // The first updates of a request that started from the queue carry the ticket, not queued.
  const started = parseUpdate(wire({ ticket: "q7", queued: false }));
  assert.equal(started.ticket, "q7");
  assert.equal(started.queued, false);
  const old = parseUpdate(wire({}));
  assert.equal(old.ticket, "");
  assert.equal(old.queued, false);
  assert.equal(old.status, "EXECUTING");
});

test("parseUpdate: a garbage ticket reads \"\", a too long one too; queued must be exactly true", () => {
  assert.equal(parseUpdate(wire({ ticket: "x".repeat(maxTicketLength) })).ticket, "x".repeat(maxTicketLength));
  assert.equal(parseUpdate(wire({ ticket: "x".repeat(maxTicketLength + 1) })).ticket, "");
  for (const bad of [7, null, true, [], {}]) {
    assert.equal(parseUpdate(wire({ ticket: bad })).ticket, "", JSON.stringify(bad));
  }
  for (const bad of ["true", 1, null, []]) {
    assert.equal(parseUpdate(wire({ queued: bad })).queued, false, JSON.stringify(bad));
  }
});
