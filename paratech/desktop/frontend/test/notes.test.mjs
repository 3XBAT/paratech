// The notes binding («Мой конспект», TASK-151 follow-up): method names, error texts, parsing, drop attributes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  dropAttributes, errorTexts, genericError, methods, notesApi, notesErrorText, parseAdd, parseNote,
} from "../.test-build/bindings/notes.js";

const note = (over = {}) => ({
  id: "n1", subjectId: "s1", title: "Конспект", anchor: { kind: "recording", recordingId: "r1" },
  parts: [{ materialId: "m1", name: "a.docx", type: "docx" }], removed: false, ...over,
});

test("bound method names exist in the Go NotesService", () => {
  const go = readFileSync(new URL("../../internal/input/wails/notes.go", import.meta.url), "utf8");
  const goMethods = new Set([...go.matchAll(/^func \(s \*NotesService\) ([A-Z]\w*)\(/gm)].map((m) => m[1]));
  const prefix = "paratech/desktop/internal/input/wails.NotesService.";
  for (const m of Object.values(methods)) {
    assert.ok(m.startsWith(prefix), m);
    assert.ok(goMethods.has(m.slice(prefix.length)), m);
  }
});

test("the notes error texts of Go are all known to the page", () => {
  const go = readFileSync(new URL("../../internal/input/wails/notes.go", import.meta.url), "utf8");
  const goTexts = [...go.matchAll(/= libraryError\("((?:[^"\\]|\\.)*)"\)/g)].map((m) => m[1]);
  assert.ok(goTexts.length > 10);
  for (const t of goTexts) {
    assert.ok(errorTexts.includes(t), t);
  }
});

test("a known text is shown as it is, anything else gets the generic text", () => {
  assert.equal(notesErrorText(new Error("Текст пустой.")), "Текст пустой.");
  assert.equal(notesErrorText("Этот файл уже добавлен."), "Этот файл уже добавлен.");
  assert.equal(notesErrorText(new Error("open C:\\Users\\me\\x.docx: denied")), genericError);
  assert.equal(notesErrorText(undefined), genericError);
});

test("the api calls the binding by name and parses replies", async () => {
  const calls = [];
  const api = notesApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === methods.forLesson) {
      return [note()];
    }
    if (method === methods.remove) {
      return note({ removed: true });
    }
    return null;
  });
  const target = { noteId: "", subjectId: "s1", kind: "recording", recordingId: "r1", lessonId: "", date: "" };
  const list = await api.forLesson({ recordingId: "r1", lessonId: "", subjectId: "", date: "" });
  assert.equal(list[0].parts[0].name, "a.docx");
  assert.equal((await api.remove("n1")).removed, true);
  assert.deepEqual((await api.forSubject("s1")), []);
  await assert.rejects(api.paste(target, "текст"));
  assert.equal(calls[3][0], methods.paste);
  assert.deepEqual(calls[3].slice(1), [target, "текст"]);
});

test("malformed replies are errors, a null list is empty", () => {
  assert.throws(() => parseNote({ id: 1 }));
  assert.throws(() => parseNote(null));
  assert.deepEqual(parseNote({ id: "n", subjectId: "s", title: "t", anchor: {}, parts: null }).parts, []);
});

test("an add reply keeps known file errors and replaces unknown ones", () => {
  const add = parseAdd({
    cancelled: false, created: true, note: note(), target: { kind: "recording", recordingId: "r1" },
    files: [
      { name: "a.docx", materialId: "m1", alreadyAdded: true },
      { name: "b.pdf", error: "Файл пустой." },
      { name: "c.pdf", error: "internal: /secret/path" },
    ],
  });
  assert.equal(add.files[0].alreadyAdded, true);
  assert.equal(add.files[1].error, "Файл пустой.");
  assert.equal(add.files[2].error, genericError);
  assert.equal(add.target.kind, "recording");
  assert.equal(add.error, "");
});

test("photo pages: pageUrl only from the core's notepage address, noTextPages a non-negative integer", () => {
  const n = parseNote({
    id: "n", subjectId: "s", title: "t", anchor: {}, noTextPages: 2,
    parts: [
      { materialId: "p1", type: "jpeg", textStatus: "no_text", pageUrl: "/media/notepage/p1" },
      { materialId: "p2", pageUrl: "https://evil.example/x.png" },
      { materialId: "p3", pageUrl: "/media/notepage/" },
      { materialId: "p4", pageUrl: "data:image/png;base64,AAAA" },
      { materialId: "p5" },
    ],
  });
  assert.deepEqual(n.parts.map((p) => p.pageUrl), ["/media/notepage/p1", "", "", "", ""]);
  assert.equal(n.noTextPages, 2);
  for (const bad of [-1, 1.5, "3", null, undefined]) {
    assert.equal(parseNote({ id: "n", subjectId: "s", title: "t", anchor: {}, noTextPages: bad }).noTextPages, 0);
  }
});

test("drop attributes mark the lecture and skip empty ids", () => {
  const a = dropAttributes({ noteId: "", subjectId: "s1", kind: "recording", recordingId: "r1", lessonId: "", date: "" });
  assert.deepEqual(a, {
    "data-file-drop-target": "", "data-note-kind": "recording", "data-note-subject": "s1", "data-note-recording": "r1",
  });
});
