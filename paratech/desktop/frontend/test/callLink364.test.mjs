// The link to the online class kept on a subject (TASK-364): the binding calls, the problem codes, the defensive parsing
// and the lesson fields of «Сегодня».
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  callLinkOpenProblems,
  callLinkProblems,
  libraryApi,
  methods,
  parseCallLink,
  parseCallLinkOpen,
  parseCallLinkResult,
  parseSubject,
} from "../.test-build/bindings/library.js";
import { parseLesson } from "../.test-build/bindings/today.js";

const link = { link: "https://meet.example.org/j/1?pwd=FakePwd42", host: "meet.example.org" };

function subject(over = {}) {
  return {
    id: "s1", name: "Физика", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026", folders: [], version: 3,
    teachers: { lecture: null, practice: null, lab: null }, callLink: link, ...over,
  };
}

test("the method names and problem codes match the Go binding", () => {
  const go = readFileSync(new URL("../../internal/input/wails/library_calllink.go", import.meta.url), "utf8");
  const value = (name) => go.match(new RegExp(`${name}\\s*=\\s*"([^"]*)"`))[1];
  assert.deepEqual([...callLinkProblems].sort(), ["", value("callLinkInvalid"), value("callLinkNotFound"), value("callLinkFailed")].sort());
  assert.deepEqual([...callLinkOpenProblems].sort(), ["", value("callLinkNone"), value("callLinkNotFound"), value("callLinkFailed")].sort());
  assert.equal(methods.setSubjectCallLink, "paratech/desktop/internal/input/wails.LibraryService.SetSubjectCallLink");
  assert.equal(methods.clearSubjectCallLink, "paratech/desktop/internal/input/wails.LibraryService.ClearSubjectCallLink");
  assert.equal(methods.openSubjectCallLink, "paratech/desktop/internal/input/wails.LibraryService.OpenSubjectCallLink");
  for (const field of ["callLink", "link", "host"]) {
    assert.match(go + readFileSync(new URL("../../internal/input/wails/library.go", import.meta.url), "utf8"), new RegExp(`json:"${field}"`), field);
  }
});

test("a subject carries its link; none or a malformed one is null", () => {
  assert.deepEqual(parseSubject(subject()).callLink, link);
  assert.equal(parseSubject(subject({ callLink: null })).callLink, null);
  const old = subject();
  delete old.callLink;
  assert.equal(parseSubject(old).callLink, null);
  for (const raw of [undefined, null, "x", [], {}, { link: "https://a.example.org" }, { host: "a.example.org" }, { link: 1, host: "a" }]) {
    assert.equal(parseCallLink(raw), null, JSON.stringify(raw));
  }
});

test("set, clear and open pass only the subject id (and the typed text to set) and read the replies", async () => {
  const calls = [];
  const api = libraryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === methods.openSubjectCallLink) {
      return { problem: "" };
    }
    return { problem: "", subject: subject({ version: 4, callLink: method === methods.clearSubjectCallLink ? null : link }) };
  });
  const set = await api.setSubjectCallLink("s1", " https://meet.example.org/j/1?pwd=FakePwd42 ");
  assert.equal(set.problem, "");
  assert.deepEqual(set.subject.callLink, link);
  const cleared = await api.clearSubjectCallLink("s1");
  assert.equal(cleared.subject.callLink, null);
  assert.deepEqual(await api.openSubjectCallLink("s1"), { problem: "" });
  assert.deepEqual(calls, [
    [methods.setSubjectCallLink, "s1", " https://meet.example.org/j/1?pwd=FakePwd42 "],
    [methods.clearSubjectCallLink, "s1"],
    [methods.openSubjectCallLink, "s1"],
  ]);
});

test("problem codes are read as they are; an unknown, malformed or failed reply is «failed»", async () => {
  for (const problem of ["invalid", "not_found", "failed"]) {
    assert.deepEqual(parseCallLinkResult({ problem, subject: subject() }), { problem, subject: null }, problem);
  }
  for (const problem of ["no_link", "not_found", "failed", ""]) {
    assert.deepEqual(parseCallLinkOpen({ problem }), { problem }, problem);
  }
  for (const raw of [null, undefined, "", [], {}, { problem: "toString" }, { problem: "weird" }, { problem: 0 }, { problem: "" }, { problem: "", subject: { id: 1 } }]) {
    assert.deepEqual(parseCallLinkResult(raw), { problem: "failed", subject: null }, JSON.stringify(raw));
  }
  for (const raw of [null, undefined, "", [], {}, { problem: "invalid" }, { problem: 1 }]) {
    assert.deepEqual(parseCallLinkOpen(raw), { problem: "failed" }, JSON.stringify(raw));
  }
  const api = libraryApi(async () => {
    throw new Error("boom");
  });
  assert.deepEqual(await api.setSubjectCallLink("s1", "x"), { problem: "failed", subject: null });
  assert.deepEqual(await api.clearSubjectCallLink("s1"), { problem: "failed", subject: null });
  assert.deepEqual(await api.openSubjectCallLink("s1"), { problem: "failed" });
});

test("a lesson of «Сегодня» carries its library subject and the host of its link; an older shape has none", () => {
  const base = { id: "l1", title: "Физика", subject: "Физика", kind: "lecture", teacher: "", start: "", end: "", post: null };
  const full = parseLesson({ ...base, subjectId: "s1", subjectName: "Физика", hasCallLink: true, callLinkHost: "meet.example.org" });
  assert.deepEqual(
    [full.subjectId, full.subjectName, full.hasCallLink, full.callLinkHost],
    ["s1", "Физика", true, "meet.example.org"],
  );
  const old = parseLesson(base);
  assert.deepEqual([old.subjectId, old.subjectName, old.hasCallLink, old.callLinkHost], ["", "", false, ""]);
  assert.throws(() => parseLesson({ ...base, hasCallLink: "yes" }));
  assert.throws(() => parseLesson({ ...base, subjectId: 1 }));

  const go = readFileSync(new URL("../../internal/input/wails/today.go", import.meta.url), "utf8");
  for (const field of ["subjectId", "subjectName", "hasCallLink", "callLinkHost"]) {
    assert.match(go, new RegExp(`json:"${field}"`), field);
  }
});
