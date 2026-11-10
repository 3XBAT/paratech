// TASK-429: the recorded lessons of a subject in its system folders, as the core sends them.

import { test } from "node:test";
import assert from "node:assert/strict";
import { errorMessages, knowledgeBaseApi, methods, parseFolderLesson } from "../.test-build/bindings/knowledgeBase.js";

// Mirrors KnowledgeFolderLesson of internal/input/wails/knowledgebase.go.
const lesson = (over = {}) => ({
  key: "recording:r1",
  noteOnly: false,
  undated: false,
  anchor: "recording",
  scheduleLessonId: "l1",
  date: "",
  notes: [],
  recordingId: "r1",
  startedAt: "2026-10-19T12:00:00Z",
  durationSeconds: 5400,
  title: "Пределы",
  subject: "Матанализ",
  subjectKey: "ID:math",
  kind: "lecture",
  subjectEdited: false,
  kindEdited: false,
  transcriptReady: true,
  summaryReady: true,
  lectureTextReady: false,
  librarySubjectId: "math",
  librarySubjectState: "matched",
  ...over,
});
const row = (over = {}) => ({ folderId: "math-lectures", system: "lectures", lesson: lesson(), ...over });

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { calls, call };
}

test("folderLessons calls the bound name with the subject id and keeps the core's rows and order", async () => {
  const rows = [
    row({ folderId: "math-seminars", system: "seminars", lesson: lesson({ key: "recording:r2", recordingId: "r2", kind: "seminar" }) }),
    row(),
    row({ folderId: "math-labs", system: "labs", lesson: lesson({ key: "recording:r3", recordingId: "r3", kind: "lab" }) }),
  ];
  const f = fakeCall(rows);
  assert.deepEqual(await knowledgeBaseApi(f.call).folderLessons("math"), rows);
  assert.deepEqual(f.calls, [[methods.folderLessons, "math"]]);
  assert.equal(methods.folderLessons, "paratech/desktop/internal/input/wails.KnowledgeBaseService.FolderLessons");
  assert.deepEqual(await knowledgeBaseApi(fakeCall([]).call).folderLessons("math"), []);
});

test("a row with another system, an empty folder or a lesson that is not a recording is refused", async () => {
  const bad = [
    [row({ system: "materials" }), /system/],
    [row({ system: "homework" }), /system/],
    [row({ system: "" }), /system/],
    [row({ folderId: "" }), /folderId is empty/],
    [row({ folderId: null }), /folderId/],
    [
      row({
        lesson: lesson({
          key: "day:math:2026-09-12",
          noteOnly: true,
          anchor: "day",
          date: "2026-09-12",
          scheduleLessonId: "",
          recordingId: "",
          notes: [{ id: "n1", title: "Ряды.docx", subjectId: "math", createdAt: "2026-10-19T12:00:00Z" }],
        }),
      }),
      /not a recording/,
    ],
    [row({ lesson: null }), /lesson/],
    [row({ lesson: lesson({ anchor: "folder" }) }), /anchor/],
  ];
  for (const [r, msg] of bad) {
    assert.throws(() => parseFolderLesson(r), msg, JSON.stringify(r));
    await assert.rejects(() => knowledgeBaseApi(fakeCall([r]).call).folderLessons("math"), msg);
  }
  await assert.rejects(() => knowledgeBaseApi(fakeCall(null).call).folderLessons("math"), /not a list/);
});

test("the invalid subject text is the binding's", () => {
  assert.equal(errorMessages.invalidSubjectId, "Неверный идентификатор предмета.");
});
