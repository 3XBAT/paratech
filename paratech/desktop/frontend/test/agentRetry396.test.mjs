// TASK-396: the `retry` field of agent:update (attempt number of the automatic retry) is parsed tolerantly.
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseUpdate } from "../.test-build/bindings/agent.js";

const wire = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: true, cards: [], fill: null, ...over,
});

test("parseUpdate reads retry 1..3 as the attempt number", () => {
  for (const n of [1, 2, 3]) {
    assert.equal(parseUpdate(wire({ retry: n })).retry, n);
  }
  assert.equal(parseUpdate(wire({ retry: 0 })).retry, 0);
});

test("parseUpdate: a missing retry (older core) reads 0 and the other fields are unchanged", () => {
  const u = parseUpdate(wire({}));
  assert.equal(u.retry, 0);
  assert.equal(u.progress, true);
  assert.equal(u.status, "EXECUTING");
});

test("parseUpdate: a garbage retry reads 0", () => {
  for (const bad of ["2", "", -1, 1.5, NaN, Infinity, null, true, [], {}]) {
    assert.equal(parseUpdate(wire({ retry: bad })).retry, 0, JSON.stringify(bad));
  }
});
