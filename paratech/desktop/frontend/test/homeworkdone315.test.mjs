// «Выполнено» bindings (TASK-315): the filter of the list, markDone and restoreDone, the summary the page shows and
// the done count of a «Сегодня» post. The reply is read into known codes only; nothing throws.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  doneMessages,
  doneSummary,
  errorMessages,
  homeworkApi,
  methods,
  parseAssignment,
  parseDoneResult,
} from "../.test-build/bindings/homework.js";
import { parsePost } from "../.test-build/bindings/today.js";

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
  done: false,
  doneAt: "",
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

const unavailable = { problem: "unavailable", outcomes: [], prior: [], priorDone: false };

test("method names are the bound ones", () => {
  const p = "paratech/desktop/internal/input/wails.HomeworkService.";
  assert.equal(methods.markDone, p + "MarkDone");
  assert.equal(methods.restoreDone, p + "RestoreDone");
  assert.equal(errorMessages.listState, "Неизвестный фильтр списка заданий: обновите страницу.");
});

test("an assignment carries the done mark; an older reply without it is open", () => {
  const done = parseAssignment(item({ done: true, doneAt: "2026-10-28T09:00:00Z" }));
  assert.equal(done.done, true);
  assert.equal(done.doneAt, "2026-10-28T09:00:00Z");
  const { done: _d, doneAt: _a, ...older } = item();
  const open = parseAssignment(older);
  assert.equal(open.done, false);
  assert.equal(open.doneAt, "");
  assert.throws(() => parseAssignment(item({ done: "yes" })));
  assert.throws(() => parseAssignment(item({ doneAt: 0 })));
});

test("listAssignments always sends the filter, open by default", async () => {
  const { api, calls } = recorder([item({ done: true, doneAt: "2026-10-28T09:00:00Z" })]);
  await api.listAssignments();
  await api.listAssignments("done");
  await api.listAssignments("all");
  const listed = await api.listAssignments("open");
  assert.equal(listed[0].done, true);
  assert.deepEqual(calls, [
    [methods.listAssignments, "open"],
    [methods.listAssignments, "done"],
    [methods.listAssignments, "all"],
    [methods.listAssignments, "open"],
  ]);
});

const marked = {
  problem: "",
  outcomes: [
    { id: "a1", code: "changed", done: true, version: 8 },
    { id: "a2", code: "unchanged", done: true, version: 3 },
    { id: "a3", code: "not_found", done: false, version: 0 },
    { id: "a4", code: "deleted", done: false, version: 0 },
    { id: "a5", code: "conflict", done: false, version: 0 },
    { id: "a6", code: "failed", done: false, version: 0 },
  ],
  prior: [{ id: "a1", version: 8 }],
  priorDone: false,
};

test("markDone sends the ids and the state; restoreDone sends back exactly the returned prior", async () => {
  const { api, calls } = recorder(marked);
  const res = await api.markDone(["a1", "a2", "a3", "a4", "a5", "a6"], true);
  assert.deepEqual(res, marked);
  await api.restoreDone(res.priorDone, res.prior);
  // Nothing else crosses: an extra field of a prior entry is not sent.
  await api.restoreDone(true, [{ id: "a1", version: 9, done: false, title: "x" }]);
  assert.deepEqual(calls, [
    [methods.markDone, ["a1", "a2", "a3", "a4", "a5", "a6"], true],
    [methods.restoreDone, false, [{ id: "a1", version: 8 }]],
    [methods.restoreDone, true, [{ id: "a1", version: 9 }]],
  ]);
});

test("the problems come without outcomes", () => {
  for (const problem of ["invalid", "unavailable"]) {
    assert.deepEqual(parseDoneResult({ ...marked, problem }), { problem, outcomes: [], prior: [], priorDone: false });
  }
});

test("an unknown code is failed and never passed on; a malformed stored state too", () => {
  const res = parseDoneResult({
    problem: "",
    outcomes: [
      { id: "a1", code: "archived", done: true, version: 2 },
      { id: "a2", code: "changed", done: "true", version: 2 },
      { id: "a3", code: "unchanged", done: true, version: -1 },
      { id: "a4", code: 7 },
    ],
    prior: [],
    priorDone: false,
  });
  assert.deepEqual(
    res.outcomes.map((o) => [o.id, o.code, o.done, o.version]),
    [
      ["a1", "failed", false, 0],
      ["a2", "failed", false, 0],
      ["a3", "failed", false, 0],
      ["a4", "failed", false, 0],
    ],
  );
});

test("a prior that is malformed or names no changed outcome offers no undo", () => {
  for (const prior of [
    [{ id: "a1", version: "8" }],
    [{ id: "a1", version: -1 }],
    [{ id: "a2", version: 3 }],
    [{ id: "a1", version: 8 }, null],
    [{ version: 8 }],
  ]) {
    const res = parseDoneResult({ ...marked, prior });
    assert.deepEqual(res.prior, [], JSON.stringify(prior));
    assert.equal(res.outcomes.length, 6);
    assert.equal(doneSummary(res).undo, false);
  }
});

test("a malformed reply or a rejected call is unavailable, never a thrown core text", async () => {
  for (const raw of [
    null,
    undefined,
    "database is locked",
    42,
    {},
    { problem: "boom" },
    { problem: 1, outcomes: [], prior: [], priorDone: false },
    { problem: "" },
    { problem: "", outcomes: null, prior: [], priorDone: false },
    { problem: "", outcomes: [], prior: null, priorDone: false },
    { problem: "", outcomes: [], prior: [], priorDone: "false" },
    { problem: "", outcomes: [null], prior: [], priorDone: false },
    { problem: "", outcomes: [{ code: "changed" }], prior: [], priorDone: false },
  ]) {
    assert.deepEqual(parseDoneResult(raw), unavailable, JSON.stringify(raw));
  }

  const rejecting = homeworkApi(async () => {
    throw new Error(String.raw`open C:\Users\student\db: locked`);
  });
  assert.deepEqual(await rejecting.markDone(["a1"], true), unavailable);
  assert.deepEqual(await rejecting.restoreDone(false, [{ id: "a1", version: 8 }]), unavailable);
});

test("the summary counts, shows each problem text once and offers undo only with a prior", () => {
  assert.deepEqual(doneSummary(parseDoneResult(marked)), {
    changed: 1,
    unchanged: 1,
    messages: [doneMessages.not_found, doneMessages.conflict, doneMessages.failed],
    undo: true,
  });
  assert.equal(doneMessages.not_found, doneMessages.deleted);
  assert.match(doneMessages.conflict, /повторной обработки/);

  // A repeated click changes nothing: no undo, no error.
  const repeated = parseDoneResult({
    problem: "",
    outcomes: [{ id: "a1", code: "unchanged", done: true, version: 8 }],
    prior: [],
    priorDone: false,
  });
  assert.deepEqual(doneSummary(repeated), { changed: 0, unchanged: 1, messages: [], undo: false });

  // An undo that met a reprocessed assignment: the conflict is explained, nothing to undo again.
  const conflict = parseDoneResult({
    problem: "",
    outcomes: [{ id: "a1", code: "conflict", done: false, version: 0 }],
    prior: [],
    priorDone: true,
  });
  assert.deepEqual(doneSummary(conflict), { changed: 0, unchanged: 0, messages: [doneMessages.conflict], undo: false });

  for (const problem of ["invalid", "unavailable"]) {
    assert.deepEqual(doneSummary(parseDoneResult({ problem })), {
      changed: 0,
      unchanged: 0,
      messages: [doneMessages[problem]],
      undo: false,
    });
  }
});

// Mirrors TodayPost of internal/input/wails/today.go.
const post = (over = {}) => ({
  recordingId: "r1",
  transcriptId: "t1",
  lesson: {
    lessonId: "l1",
    title: "Матанализ",
    subject: "Матанализ",
    subjectId: "s1",
    subjectName: "Матанализ",
    kind: "lecture",
    start: "",
  },
  headline: "Конспект готов",
  summaryReady: true,
  homework: 3,
  homeworkDone: 2,
  earliestDue: "",
  calendar: [],
  shownAt: "2026-10-28T09:00:00Z",
  ...over,
});

test("a «Сегодня» post carries the done count; an older post without it has none", () => {
  assert.equal(parsePost(post()).homeworkDone, 2);
  const { homeworkDone: _h, ...older } = post();
  assert.equal(parsePost(older).homeworkDone, 0);
  assert.throws(() => parsePost(post({ homeworkDone: "2" })));
});
