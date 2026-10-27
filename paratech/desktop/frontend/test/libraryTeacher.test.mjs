// The teacher of a subject (TASK-236): the binding calls, the problem codes and the defensive parsing.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { libraryApi, methods, parseSubject, parseTeacher, parseTeacherResult, teacherProblems } from "../.test-build/bindings/library.js";

const teacher = { name: "Петров Пётр Петрович", email: "petrov@example.invalid" };

function subject(over = {}) {
  return { id: "s1", name: "Физика", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [], version: 3, teacher, ...over };
}

test("the problem codes and fields match the Go binding", () => {
  const go = readFileSync(new URL("../../internal/input/wails/library_teacher.go", import.meta.url), "utf8");
  const value = (name) => go.match(new RegExp(`${name}\\s*=\\s*"([^"]*)"`))[1];
  assert.deepEqual(
    [...teacherProblems].sort(),
    ["", value("teacherInvalid"), value("teacherChanged"), value("teacherNotFound"), value("teacherFailed")].sort(),
  );
  assert.equal(value("fieldTeacherName"), "name");
  assert.equal(value("fieldTeacherEmail"), "email");
  assert.equal(methods.setSubjectTeacher, "paratech/desktop/internal/input/wails.LibraryService.SetSubjectTeacher");
  assert.equal(methods.clearSubjectTeacher, "paratech/desktop/internal/input/wails.LibraryService.ClearSubjectTeacher");
});

test("a subject carries its version and teacher; none or a malformed one is null", () => {
  const s = parseSubject(subject());
  assert.equal(s.version, 3);
  assert.deepEqual(s.teacher, teacher);
  assert.equal(parseSubject(subject({ teacher: null })).teacher, null);
  const old = subject();
  delete old.teacher;
  delete old.version;
  assert.equal(parseSubject(old).teacher, null);
  assert.equal(parseSubject(old).version, 0);
  for (const v of [-1, 1.5, "3", Number.NaN, null]) {
    assert.equal(parseSubject(subject({ version: v })).version, 0, String(v));
  }
  for (const raw of [undefined, "x", [], {}, { name: "Петров" }, { email: "a@b.c" }, { name: 1, email: "a@b.c" }, { name: "", email: "" }]) {
    assert.equal(parseTeacher(raw), null, JSON.stringify(raw));
  }
});

test("setSubjectTeacher and clearSubjectTeacher pass the id, version and texts and read the subject", async () => {
  const calls = [];
  const api = libraryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { problem: "", field: "", subject: subject({ version: 4, teacher: method === methods.clearSubjectTeacher ? null : teacher }) };
  });
  const set = await api.setSubjectTeacher("s1", 3, teacher.name, teacher.email);
  assert.equal(set.problem, "");
  assert.equal(set.subject.version, 4);
  assert.deepEqual(set.subject.teacher, teacher);
  const cleared = await api.clearSubjectTeacher("s1", 4);
  assert.equal(cleared.problem, "");
  assert.equal(cleared.subject.teacher, null);
  assert.deepEqual(calls, [
    [methods.setSubjectTeacher, "s1", 3, teacher.name, teacher.email],
    [methods.clearSubjectTeacher, "s1", 4],
  ]);
});

test("each problem code is read as it is, a field only for invalid, never a subject", () => {
  assert.deepEqual(parseTeacherResult({ problem: "invalid", field: "name", subject: null }), { problem: "invalid", field: "name", subject: null });
  assert.deepEqual(parseTeacherResult({ problem: "invalid", field: "email" }), { problem: "invalid", field: "email", subject: null });
  assert.deepEqual(parseTeacherResult({ problem: "invalid", field: "title" }), { problem: "invalid", field: "", subject: null });
  for (const problem of ["changed", "not_found", "failed"]) {
    assert.deepEqual(parseTeacherResult({ problem, field: "name", subject: subject() }), { problem, field: "", subject: null }, problem);
  }
});

test("an unknown, malformed or failed reply is «failed»", async () => {
  const failed = { problem: "failed", field: "", subject: null };
  for (const raw of [null, undefined, "", [], {}, { problem: "toString" }, { problem: "weird" }, { problem: 0 }, { problem: "" }, { problem: "", subject: { id: 1 } }]) {
    assert.deepEqual(parseTeacherResult(raw), failed, JSON.stringify(raw));
  }
  const api = libraryApi(async () => {
    throw new Error("boom");
  });
  assert.deepEqual(await api.setSubjectTeacher("s1", 1, teacher.name, teacher.email), failed);
  assert.deepEqual(await api.clearSubjectTeacher("s1", 1), failed);
});
