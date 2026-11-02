// deleteAssignment, restoreAssignment and editAssignment bindings (TASK-231): the request goes out as given and the
// reply is read into known codes only; nothing throws.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  homeworkApi,
  methods,
  parseChangeAssignmentResult,
  parseDeleteImpact,
  parseEditAssignmentResult,
} from "../.test-build/bindings/homework.js";

// Mirrors AssignmentItem of internal/input/wails/homework.go.
const item = (over = {}) => ({
  id: "a1",
  lessonId: "l1",
  title: "Задачи 1–5",
  description: "",
  due: "",
  userEdited: false,
  version: 5,
  updatedAt: "2026-10-27T10:00:00Z",
  subject: "Матанализ",
  subjectSource: "schedule",
  subjectFromLesson: true,
  kind: "seminar",
  teacher: "",
  teacherSource: "",
  lessonDate: "",
  correctedSubject: "",
  correctedTeacher: "",
  ...over,
});

const recorder = (reply) => {
  const calls = [];
  const api = homeworkApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return typeof reply === "function" ? reply() : reply;
  });
  return { api, calls };
};

test("method names are the bound ones", () => {
  const p = "paratech/desktop/internal/input/wails.HomeworkService.";
  assert.equal(methods.deleteAssignment, p + "DeleteAssignment");
  assert.equal(methods.restoreAssignment, p + "RestoreAssignment");
  assert.equal(methods.editAssignment, p + "EditAssignment");
});

test("deleteAssignment and restoreAssignment send the id and the shown version", async () => {
  const { api, calls } = recorder({ problem: "", assignment: item({ version: 5 }) });
  const del = await api.deleteAssignment("a1", 4);
  assert.equal(del.problem, "");
  assert.equal(del.assignment.version, 5);
  assert.equal(del.assignment.subjectFromLesson, true);
  await api.restoreAssignment("a1", del.assignment.version);
  assert.deepEqual(calls, [
    [methods.deleteAssignment, "a1", 4],
    [methods.restoreAssignment, "a1", 5],
  ]);
});

test("stopped is read only as a literal true on a done delete", async () => {
  const stopped = parseChangeAssignmentResult({ problem: "", assignment: item(), stopped: true });
  assert.equal(stopped.stopped, true);
  assert.equal(stopped.assignment.version, 5);
  for (const odd of [false, undefined, null, "true", 1, {}]) {
    assert.equal(parseChangeAssignmentResult({ problem: "", assignment: item(), stopped: odd }).stopped, false);
  }
  const { api } = recorder({ problem: "", assignment: item(), stopped: true });
  assert.equal((await api.deleteAssignment("a1", 5)).stopped, true);
});

test("deleteImpact asks by id and reads working only as a literal true", async () => {
  assert.equal(methods.deleteImpact, "paratech/desktop/internal/input/wails.HomeworkService.DeleteImpact");
  const { api, calls } = recorder({ working: true });
  assert.deepEqual(await api.deleteImpact("a1"), { working: true });
  assert.deepEqual(calls, [[methods.deleteImpact, "a1"]]);
  for (const raw of [{ working: false }, {}, { working: "true" }, { working: 1 }, null, undefined, "x", 7]) {
    assert.deepEqual(parseDeleteImpact(raw), { working: false }, JSON.stringify(raw));
  }
});

test("a rejected deleteImpact is not working, never a thrown core text", async () => {
  const rejecting = homeworkApi(async () => {
    throw new Error(String.raw`open C:\Users\student\db: locked`);
  });
  assert.deepEqual(await rejecting.deleteImpact("a1"), { working: false });
});

test("editAssignment sends the form as given", async () => {
  const req = { id: "a1", version: 5, title: "Задачи 1–6", note: "", subjectId: "", date: "2026-10-30", time: "" };
  const { api, calls } = recorder({ problem: "", field: "", assignment: item({ title: "Задачи 1–6", version: 6 }) });
  const res = await api.editAssignment(req);
  assert.deepEqual(calls, [[methods.editAssignment, req]]);
  assert.equal(res.problem, "");
  assert.equal(res.field, "");
  assert.equal(res.assignment.version, 6);
});

test("every change problem comes without an assignment", () => {
  for (const problem of ["in_use", "changed", "not_found", "failed"]) {
    const want = { problem, assignment: null, stopped: false };
    assert.deepEqual(parseChangeAssignmentResult({ problem, assignment: null }), want);
    assert.deepEqual(parseChangeAssignmentResult({ problem, assignment: item() }), want);
    // A refusal never claims that a work was stopped.
    assert.deepEqual(parseChangeAssignmentResult({ problem, assignment: null, stopped: true }), want);
  }
});

test("every edit problem comes without an assignment; the field only for invalid", () => {
  for (const field of ["title", "note", "due", "time", ""]) {
    assert.deepEqual(parseEditAssignmentResult({ problem: "invalid", field, assignment: null }), {
      problem: "invalid",
      field,
      assignment: null,
    });
  }
  assert.deepEqual(parseEditAssignmentResult({ problem: "invalid", field: "color", assignment: null }), {
    problem: "invalid",
    field: "",
    assignment: null,
  });
  for (const problem of ["subject_not_found", "changed", "deleted", "not_found", "failed"]) {
    assert.deepEqual(parseEditAssignmentResult({ problem, field: "title", assignment: null }), {
      problem,
      field: "",
      assignment: null,
    });
  }
});

test("an unknown, missing or malformed reply is failed, never a thrown core text", async () => {
  const failedChange = { problem: "failed", assignment: null, stopped: false };
  const failedEdit = { problem: "failed", field: "", assignment: null };
  for (const raw of [
    null,
    undefined,
    "database is locked",
    42,
    {},
    { problem: "boom", assignment: null },
    { problem: 1, assignment: null },
    { problem: "" },
    { problem: "", assignment: null },
    { problem: "", assignment: { id: "a1" } },
    { problem: "", assignment: item({ version: "5" }) },
    { problem: "", assignment: item({ subjectFromLesson: undefined }) },
  ]) {
    assert.deepEqual(parseChangeAssignmentResult(raw), failedChange, JSON.stringify(raw));
    assert.deepEqual(parseEditAssignmentResult(raw), failedEdit, JSON.stringify(raw));
  }

  const rejecting = homeworkApi(async () => {
    throw new Error(String.raw`open C:\Users\student\db: locked`);
  });
  assert.deepEqual(await rejecting.deleteAssignment("a1", 1), failedChange);
  assert.deepEqual(await rejecting.restoreAssignment("a1", 1), failedChange);
  assert.deepEqual(
    await rejecting.editAssignment({ id: "a1", version: 1, title: "T", note: "", subjectId: "", date: "", time: "" }),
    failedEdit,
  );
});
