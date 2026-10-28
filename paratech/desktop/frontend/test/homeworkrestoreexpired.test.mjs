// restoreAssignment after the restore window (TASK-274, ADR-0029 addendum 2): the reply "expired" is read as is, without
// an assignment; the page text is TASK-275.
import { test } from "node:test";
import assert from "node:assert/strict";
import { changeProblems, homeworkApi, methods, parseChangeAssignmentResult } from "../.test-build/bindings/homework.js";

test("expired is a known change problem", () => {
  assert.ok(changeProblems.includes("expired"));
  assert.deepEqual(parseChangeAssignmentResult({ problem: "expired", assignment: null }), {
    problem: "expired",
    assignment: null,
    stopped: false,
  });
  assert.deepEqual(parseChangeAssignmentResult({ problem: "expired", assignment: { id: "a1" } }), {
    problem: "expired",
    assignment: null,
    stopped: false,
  });
});

test("restoreAssignment answers expired", async () => {
  const calls = [];
  const api = homeworkApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { problem: "expired", assignment: null };
  });
  const res = await api.restoreAssignment("a1", 5);
  assert.deepEqual(res, { problem: "expired", assignment: null, stopped: false });
  assert.deepEqual(calls, [[methods.restoreAssignment, "a1", 5]]);
});

test("a code that only looks like expired is failed", () => {
  for (const problem of ["Expired", "expired ", "restore_expired"]) {
    assert.equal(parseChangeAssignmentResult({ problem, assignment: null }).problem, "failed");
  }
});
