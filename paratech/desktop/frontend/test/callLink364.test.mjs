// The link to the online class kept on a subject (TASK-364): the binding calls, the problem codes, the defensive parsing
// and the lesson fields of «Сегодня».
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  callKinds,
  callLinkOpenProblems,
  lessonCallLink,
  callLinkProblems,
  createWithCallLinkProblems,
  libraryApi,
  methods,
  parseCallLink,
  parseCallLinkOpen,
  parseCallLinkResult,
  parseCreateWithCallLink,
  parseSubject,
} from "../.test-build/bindings/library.js";
import { parseLesson, subjectStates } from "../.test-build/bindings/today.js";

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

test("set and clear pass the kind, open the lesson kind; only the id and the typed text cross; the replies are read", async () => {
  const calls = [];
  const api = libraryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === methods.openSubjectCallLink) {
      return { problem: "" };
    }
    return { problem: "", subject: subject({ version: 4, callLink: method === methods.clearSubjectCallLink ? null : link }) };
  });
  const set = await api.setSubjectCallLink("s1", "general", " https://meet.example.org/j/1?pwd=FakePwd42 ");
  assert.equal(set.problem, "");
  assert.deepEqual(set.subject.callLink, link);
  const cleared = await api.clearSubjectCallLink("s1", "lecture");
  assert.equal(cleared.subject.callLink, null);
  assert.deepEqual(await api.openSubjectCallLink("s1", "practice"), { problem: "" });
  assert.deepEqual(await api.openSubjectCallLink("s1", ""), { problem: "" });
  assert.deepEqual(calls, [
    [methods.setSubjectCallLink, "s1", "general", " https://meet.example.org/j/1?pwd=FakePwd42 "],
    [methods.clearSubjectCallLink, "s1", "lecture"],
    [methods.openSubjectCallLink, "s1", "practice"],
    [methods.openSubjectCallLink, "s1", ""],
  ]);
});

test("a subject carries its links by kind: set ones only, unknown keys ignored, a malformed entry dropped (TASK-432)", () => {
  const other = { link: "https://meet.example.org/j/2", host: "meet.example.org" };
  assert.deepEqual(parseSubject(subject({ callLinks: {} })).callLinks, {});
  assert.deepEqual(parseSubject(subject({ callLinks: { general: link, lecture: other } })).callLinks, { general: link, lecture: other });
  assert.deepEqual(
    parseSubject(subject({ callLinks: { seminar: other, lab: link, practice: link, extra: 1 } })).callLinks,
    { seminar: other, lab: link },
    "an unknown key is ignored",
  );
  assert.deepEqual(
    parseSubject(subject({ callLinks: { general: link, lecture: { link: "https://a.example.org" }, seminar: "x", lab: null } })).callLinks,
    { general: link },
    "a malformed entry is dropped like a malformed single link",
  );
  // A reply without the field (older core) or with a wrong type is an empty object; callLink stays what it was.
  const old = subject();
  assert.deepEqual(parseSubject(old).callLinks, {});
  assert.deepEqual(parseSubject(old).callLink, link);
  for (const raw of [null, "x", [], 5]) {
    assert.deepEqual(parseSubject(subject({ callLinks: raw })).callLinks, {}, JSON.stringify(raw));
  }
});

test("the link a lesson opens: its kind, else the general one; practice takes the seminar link (TASK-432)", () => {
  const lecture = { link: "https://meet.example.org/lec", host: "meet.example.org" };
  const seminar = { link: "https://meet.example.org/sem", host: "meet.example.org" };
  const s = parseSubject(subject({ callLinks: { general: link, lecture, seminar } }));
  assert.deepEqual(lessonCallLink(s, "lecture"), { kind: "lecture", link: lecture });
  assert.deepEqual(lessonCallLink(s, "seminar"), { kind: "seminar", link: seminar });
  assert.deepEqual(lessonCallLink(s, "practice"), { kind: "seminar", link: seminar });
  assert.deepEqual(lessonCallLink(s, "lab"), { kind: "general", link }, "no lab link: the general one");
  assert.deepEqual(lessonCallLink(s, "unknown"), { kind: "general", link });
  assert.deepEqual(lessonCallLink(s, ""), { kind: "general", link });
  const only = parseSubject(subject({ callLink: null, callLinks: { lecture } }));
  assert.equal(lessonCallLink(only, "seminar"), null, "a seminar never takes the lecture link");
  assert.equal(lessonCallLink(only, ""), null);
  // A subject with only the single general field (no callLinks) still answers with it.
  assert.deepEqual(lessonCallLink({ callLink: link }, "lecture"), { kind: "general", link });
  assert.deepEqual(callKinds, ["general", "lecture", "seminar", "lab"]);
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
  assert.deepEqual(await api.setSubjectCallLink("s1", "general", "x"), { problem: "failed", subject: null });
  assert.deepEqual(await api.clearSubjectCallLink("s1", "general"), { problem: "failed", subject: null });
  assert.deepEqual(await api.openSubjectCallLink("s1", ""), { problem: "failed" });
});

test("createSubjectWithCallLink: the name and the link cross in one call; the problems match the Go binding", async () => {
  const go = readFileSync(new URL("../../internal/input/wails/library_calllink.go", import.meta.url), "utf8");
  const discardGo = readFileSync(new URL("../../internal/input/wails/library_discard.go", import.meta.url), "utf8");
  const value = (src, name) => src.match(new RegExp(`${name}\\s*=\\s*"([^"]*)"`))[1];
  assert.deepEqual(
    [...createWithCallLinkProblems].sort(),
    ["", value(go, "callLinkInvalid"), value(go, "callLinkInvalidName"), value(go, "callLinkNameTaken"),
      value(discardGo, "RemovedNameTaken"), value(go, "callLinkFailed")].sort(),
  );
  assert.equal(methods.createSubjectWithCallLink, "paratech/desktop/internal/input/wails.LibraryService.CreateSubjectWithCallLink");
  for (const field of ["problem", "subject", "removedSubjectId", "removedVersion"]) {
    assert.match(go, new RegExp(`json:"${field}"`), field);
  }

  const calls = [];
  const api = libraryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { problem: "", subject: subject({ version: 1 }), removedSubjectId: "", removedVersion: 0 };
  });
  const created = await api.createSubjectWithCallLink("Физика", " https://meet.example.org/j/1?pwd=FakePwd42 ");
  assert.equal(created.problem, "");
  assert.deepEqual(created.subject.callLink, link);
  assert.equal(created.removed, null);
  assert.deepEqual(calls, [[methods.createSubjectWithCallLink, "Физика", " https://meet.example.org/j/1?pwd=FakePwd42 "]]);
});

test("createSubjectWithCallLink replies: problems as they are, «Вернуть» data, anything malformed is «failed»", async () => {
  const failed = { problem: "failed", subject: null, removed: null };
  for (const problem of ["invalid", "invalid_name", "name_taken", "failed"]) {
    assert.deepEqual(parseCreateWithCallLink({ problem, subject: subject() }), { problem, subject: null, removed: null }, problem);
  }
  assert.deepEqual(
    parseCreateWithCallLink({ problem: "removed_name_taken", subject: null, removedSubjectId: "s9", removedVersion: 4 }),
    { problem: "removed_name_taken", subject: null, removed: { subjectId: "s9", version: 4 } },
  );
  for (const raw of [
    null, undefined, "", [], {}, { problem: "toString" }, { problem: "weird" }, { problem: 0 }, { problem: "" },
    { problem: "", subject: { id: 1 } }, { problem: "removed_name_taken" },
    { problem: "removed_name_taken", removedSubjectId: "", removedVersion: 4 },
    { problem: "removed_name_taken", removedSubjectId: "s9", removedVersion: 0 },
    { problem: "removed_name_taken", removedSubjectId: "s9", removedVersion: -1 },
  ]) {
    assert.deepEqual(parseCreateWithCallLink(raw), failed, JSON.stringify(raw));
  }
  const api = libraryApi(async () => {
    throw new Error("boom");
  });
  assert.deepEqual(await api.createSubjectWithCallLink("Физика", "x"), failed);
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
  for (const field of ["subjectState", "subjectId", "subjectName", "hasCallLink", "callLinkHost"]) {
    assert.match(go, new RegExp(`json:"${field}"`), field);
  }
});

test("a lesson of «Сегодня» tells which link it opens (TASK-432); missing, empty or unknown is none", () => {
  const base = { id: "l1", title: "Физика", subject: "Физика", kind: "lecture", teacher: "", start: "", end: "", post: null, subjectId: "s1" };
  for (const k of ["general", "lecture", "seminar", "lab"]) {
    assert.equal(parseLesson({ ...base, hasCallLink: true, callLinkHost: "h", callLinkKind: k }).callLinkKind, k, k);
  }
  for (const k of [undefined, "", "practice", "weird", "toString", 1, null]) {
    assert.equal(parseLesson({ ...base, callLinkKind: k }).callLinkKind, "", String(k));
  }
});

test("a lesson of «Сегодня» tells the state of its subject; an unknown or inconsistent one offers nothing", () => {
  const types = readFileSync(new URL("../../internal/cases/todayfeed/types.go", import.meta.url), "utf8");
  const goStates = [...types.matchAll(/^\s+Subject\w+\s+SubjectState = "([^"]*)"/gm)].map((m) => m[1]);
  assert.deepEqual([...subjectStates].sort(), [...goStates].sort());

  const base = { id: "l1", title: "Физика", subject: "Физика", kind: "lecture", teacher: "", start: "", end: "", post: null };
  for (const st of ["none", "unknown", "ambiguous", "unavailable"]) {
    assert.equal(parseLesson({ ...base, subjectState: st, subjectId: "" }).subjectState, st, st);
  }
  assert.equal(parseLesson({ ...base, subjectState: "known", subjectId: "s1" }).subjectState, "known");
  // "known" with no subject would offer a control with nowhere to keep the link.
  assert.equal(parseLesson({ ...base, subjectState: "known", subjectId: "" }).subjectState, "unavailable");
  for (const st of ["weird", "toString", 1, null]) {
    assert.equal(parseLesson({ ...base, subjectState: st, subjectId: "" }).subjectState, "unavailable", String(st));
  }
  // An older core without the field: told from the subject alone.
  assert.equal(parseLesson({ ...base, subjectId: "s1" }).subjectState, "known");
  assert.equal(parseLesson(base).subjectState, "unavailable");
});
