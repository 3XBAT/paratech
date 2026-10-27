// «Добавить задание» (TASK-219): field checks, the requestId session, the outcome texts, the SSR of the form and
// the button placements. Runs on the modules compiled by `npm run test`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  addOutcome,
  addRequest,
  addSession,
  addTexts,
  emptyAddForm,
  validateAddForm,
} from "../.test-build/homeworkView.js";
import { AddAssignmentDialog } from "../.test-build/pages/HomeworkDialogs.js";
import { BurningSection } from "../.test-build/pages/TodayParts.js";

const { renderToStaticMarkup } = server;

const item = (over = {}) => ({
  id: "ui-1",
  lessonId: "",
  title: "Задача",
  description: "",
  due: "2026-10-28T06:00:00Z",
  userEdited: true,
  version: 1,
  updatedAt: "2026-10-27T10:00:00Z",
  subject: "",
  subjectSource: "",
  subjectFromLesson: false,
  kind: "",
  teacher: "",
  teacherSource: "",
  lessonDate: "",
  correctedSubject: "",
  correctedTeacher: "",
  ...over,
});

const ok = { problem: "", field: "", assignment: item() };
const bad = (problem, field = "") => ({ problem, field, assignment: null });
const valid = { title: "Прочитать главу", subjectId: "", date: "2026-10-28", time: "", note: "" };

function counter() {
  let n = 0;
  return () => `id-${++n}`;
}

test("a valid form has no errors; time is optional", () => {
  assert.deepEqual(validateAddForm(valid), {});
  assert.deepEqual(validateAddForm({ ...valid, time: "09:00" }), {});
});

test("title is required, one line, at most 200 characters", () => {
  assert.ok(validateAddForm({ ...valid, title: "   " }).title);
  assert.ok(validateAddForm({ ...valid, title: "а\nб" }).title);
  assert.equal(validateAddForm({ ...valid, title: "а".repeat(200) }).title, undefined);
  assert.ok(validateAddForm({ ...valid, title: "а".repeat(201) }).title);
});

test("note is at most 2000 characters; date is required and real; time is HH:MM", () => {
  assert.equal(validateAddForm({ ...valid, note: "x".repeat(2000) }).note, undefined);
  assert.ok(validateAddForm({ ...valid, note: "x".repeat(2001) }).note);
  assert.ok(validateAddForm({ ...valid, date: "" }).due);
  assert.ok(validateAddForm({ ...valid, date: "2026-02-30" }).due);
  assert.ok(validateAddForm({ ...valid, time: "9:00" }).time);
  assert.ok(validateAddForm({ ...valid, time: "24:00" }).time);
});

test("the request carries the token, the trimmed title and no lesson", () => {
  assert.deepEqual(addRequest("t1", { ...valid, title: "  Задача  ", subjectId: "s1", time: "09:00", note: "н" }), {
    requestId: "t1",
    title: "Задача",
    subjectId: "s1",
    lessonId: "",
    date: "2026-10-28",
    time: "09:00",
    note: "н",
  });
});

test("a double submit makes one call and a retry of the same form passes the same requestId", async () => {
  const calls = [];
  let release;
  const api = {
    createAssignment: (req) => {
      calls.push(req);
      return new Promise((resolve) => {
        release = () => resolve(bad("failed"));
      });
    },
  };
  const s = addSession(counter());
  const first = s.submit(api, valid);
  const second = await s.submit(api, valid);
  assert.equal(second, null);
  release();
  const out = await first;
  assert.equal(calls.length, 1);
  assert.ok(out.general);

  const again = s.submit(api, valid);
  release();
  await again;
  assert.equal(calls.length, 2);
  assert.equal(calls[1].requestId, calls[0].requestId);
});

test("an edit after a refusal gets a new requestId; an edit before any refusal keeps it", async () => {
  const calls = [];
  const replies = [bad("failed"), ok];
  const api = {
    createAssignment: async (req) => {
      calls.push(req.requestId);
      return replies.shift();
    },
  };
  const s = addSession(counter());
  s.edited();
  assert.equal(s.requestId, "id-1");
  await s.submit(api, valid);
  s.edited();
  assert.equal(s.requestId, "id-2");
  const out = await s.submit(api, { ...valid, title: "Другое" });
  assert.deepEqual(calls, ["id-1", "id-2"]);
  assert.equal(out.created.id, "ui-1");
});

test("a thrown call keeps the token so that a repeat stores one assignment", async () => {
  const seen = [];
  const api = {
    createAssignment: async (req) => {
      seen.push(req.requestId);
      if (seen.length === 1) {
        throw new Error("network");
      }
      return ok;
    },
  };
  const s = addSession(counter());
  await assert.rejects(s.submit(api, valid));
  s.edited();
  await s.submit(api, valid);
  assert.deepEqual(seen, ["id-1", "id-1"]);
});

test("a refused requestId is replaced at once", async () => {
  const s = addSession(counter());
  await s.submit({ createAssignment: async () => bad("invalid", "requestId") }, valid);
  assert.equal(s.requestId, "id-2");
});

test("outcomes: a field error sits at its field, the rest is a general text", () => {
  for (const [field, key] of [
    ["title", "title"],
    ["note", "note"],
    ["due", "due"],
    ["time", "time"],
  ]) {
    const o = addOutcome(bad("invalid", field));
    assert.ok(o.fieldErrors[key]);
    assert.equal(o.general, "");
    assert.equal(o.created, null);
  }
  for (const r of [
    bad("invalid", "requestId"),
    bad("invalid"),
    bad("subject_not_found"),
    bad("lesson_not_found"),
    bad("failed"),
  ]) {
    const o = addOutcome(r);
    assert.deepEqual(o.fieldErrors, {});
    assert.ok(o.general);
    assert.doesNotMatch(o.general, /subject_not_found|lesson_not_found|failed|invalid|requestId/);
  }
  assert.equal(addOutcome(ok).created.id, "ui-1");
});

test("the form shows its fields, subjects as text nodes and the two buttons", () => {
  const subjects = [{ id: "s1", name: "Физика <b>1</b>", aliases: [], semester: "", semesterLabel: "", folders: [] }];
  const html = renderToStaticMarkup(
    h(AddAssignmentDialog, {
      api: { createAssignment: async () => ok },
      library: { subjects: async () => subjects },
      initialSubjects: subjects,
      onCreated: () => undefined,
      onClose: () => undefined,
    }),
  );
  for (const t of [addTexts.what, addTexts.subject, addTexts.due, addTexts.time, addTexts.note, addTexts.noSubject]) {
    assert.ok(html.includes(t), t);
  }
  assert.match(html, />Добавить</);
  assert.match(html, />Отмена</);
  assert.ok(html.includes("Физика &lt;b&gt;1&lt;/b&gt;"));
  assert.ok(!html.includes("<b>1</b>"));
  assert.match(html, /type="date"/);
  assert.match(html, /type="time"/);
  assert.equal(emptyAddForm.subjectId, "");
});

test("«Горит на 7 дней» offers the button only with a handler", () => {
  const without = renderToStaticMarkup(h(BurningSection, { burning: [], onOpenAssignment: () => undefined }));
  assert.ok(!without.includes(addTexts.button));
  const withAdd = renderToStaticMarkup(
    h(BurningSection, { burning: [], onOpenAssignment: () => undefined, onAdd: () => undefined }),
  );
  assert.ok(withAdd.includes(addTexts.button));
});

test("the pages open the form and refresh after it", () => {
  const today = readFileSync(new URL("../src/pages/TodayPage.tsx", import.meta.url), "utf8");
  assert.match(today, /onCreated=\{\(\) => \{[\s\S]*?load\(\)/);
  const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
  assert.match(page, /addTexts\.button/);
  assert.match(page, /props\.onOpen\(a\.id\)/);
});
