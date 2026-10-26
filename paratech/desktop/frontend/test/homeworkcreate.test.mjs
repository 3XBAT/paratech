// createAssignment binding (TASK-218): the request goes out as typed and the reply is checked.
import { test } from "node:test";
import assert from "node:assert/strict";
import { homeworkApi, methods, parseCreateAssignmentResult } from "../.test-build/bindings/homework.js";

// Mirrors AssignmentItem of internal/input/wails/homework.go.
const item = (over = {}) => ({
  id: "ui-0123",
  lessonId: "",
  title: "Прочитать главу 3",
  description: "стр. 10",
  due: "2026-10-30T06:00:00Z",
  userEdited: true,
  updatedAt: "2026-10-27T10:00:00Z",
  subject: "Физика",
  subjectSource: "user",
  kind: "",
  teacher: "",
  teacherSource: "",
  lessonDate: "",
  correctedSubject: "Физика",
  correctedTeacher: "",
  ...over,
});

const req = {
  requestId: "3f2b-77",
  title: "Прочитать главу 3",
  subjectId: "s1",
  lessonId: "",
  date: "2026-10-30",
  time: "09:00",
  note: "стр. 10",
};

test("createAssignment sends the form as typed and parses the created assignment", async () => {
  const calls = [];
  const api = homeworkApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { problem: "", field: "", assignment: item() };
  });
  const res = await api.createAssignment(req);
  assert.deepEqual(calls, [[methods.createAssignment, req]]);
  assert.equal(res.problem, "");
  assert.equal(res.assignment.title, "Прочитать главу 3");
  assert.equal(
    methods.createAssignment,
    "paratech/desktop/internal/input/wails.HomeworkService.CreateAssignment",
  );
});

test("every problem code comes without an assignment", () => {
  for (const [problem, field] of [
    ["invalid", "title"],
    ["invalid", "note"],
    ["invalid", "due"],
    ["invalid", "time"],
    ["invalid", "requestId"],
    ["invalid", ""],
    ["subject_not_found", ""],
    ["lesson_not_found", ""],
    ["failed", ""],
  ]) {
    assert.deepEqual(parseCreateAssignmentResult({ problem, field, assignment: null }), {
      problem,
      field,
      assignment: null,
    });
  }
});

test("a malformed create reply is an error, not a default", () => {
  const ok = { problem: "", field: "", assignment: item() };
  assert.throws(() => parseCreateAssignmentResult(null), /create assignment result/);
  assert.throws(() => parseCreateAssignmentResult({ ...ok, problem: "boom" }), /problem/);
  assert.throws(() => parseCreateAssignmentResult({ ...ok, field: "color" }), /field/);
  assert.throws(() => parseCreateAssignmentResult({ ...ok, field: undefined }), /field/);
  assert.throws(() => parseCreateAssignmentResult({ ...ok, assignment: null }), /assignment/);
  assert.throws(() => parseCreateAssignmentResult({ ...ok, assignment: { id: "x" } }), /assignment/);
  assert.throws(() => parseCreateAssignmentResult({ problem: "failed", field: "", assignment: item() }), /assignment/);
  assert.throws(() => parseCreateAssignmentResult({ problem: "failed", field: "" }), /assignment/);
});
