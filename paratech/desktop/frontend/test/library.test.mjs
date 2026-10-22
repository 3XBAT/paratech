// «Предметы» (TASK-124): the library binding, the semester grouping, the subject page and the plain-text rule.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { errorTexts, genericError, libraryApi, libraryErrorText, methods, parseImport, parseSubject } from "../.test-build/bindings/library.js";
import { folderCountText, folderTitle, groupBySemester, importNote, orderedFolders, sizeText } from "../.test-build/libraryView.js";
import { InboxList, SubjectDetail, SubjectList } from "../.test-build/pages/SubjectsParts.js";

const noop = () => undefined;

function folder(id, system, name = id) {
  return { id, name, system };
}

function subject(over = {}) {
  return {
    id: "s1", name: "Матанализ", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026",
    folders: [folder("f6", "tickets"), folder("f1", "lectures"), folder("f9", "", "Шпаргалки"), folder("f4", "materials"), folder("f2", "seminars"), folder("f3", "labs"), folder("f5", "homework")],
    ...over,
  };
}

function material(over = {}) {
  return {
    id: "m1", name: "лекция.pdf", type: "pdf", sizeBytes: 2048, subjectId: "s1", folderId: "f1", inbox: false, removed: false,
    textStatus: "pdf_not_extracted", textNote: "текст PDF не извлечён", createdAt: "2026-10-22T10:00:00Z", ...over,
  };
}

test("bound method names and error texts match the Go LibraryService", () => {
  const go = readFileSync(new URL("../../internal/input/wails/library.go", import.meta.url), "utf8");
  const goMethods = new Set([...go.matchAll(/^func \(s \*LibraryService\) ([A-Z]\w*)\(/gm)].map((m) => m[1]));
  const prefix = "paratech/desktop/internal/input/wails.LibraryService.";
  for (const m of Object.values(methods)) {
    assert.ok(goMethods.has(m.slice(prefix.length)), m);
  }
  const goTexts = [...go.matchAll(/= libraryError\("((?:[^"\\]|\\.)*)"\)/g)].map((m) => m[1].replace(/\\\\/g, "\\").replace(/\\"/g, '"'));
  assert.deepEqual([...errorTexts].sort(), goTexts.sort());
});

test("a core refusal text is shown as it is; any other error gets the generic text", () => {
  assert.equal(libraryErrorText(new Error("Предмет с таким названием уже есть.")), "Предмет с таким названием уже есть.");
  assert.equal(libraryErrorText("Файл пустой."), "Файл пустой.");
  assert.equal(libraryErrorText(new Error("open C:\\Users\\me\\secret.docx: access denied")), genericError);
  assert.equal(libraryErrorText(undefined), genericError);
});

test("the api calls the binding by name and parses the replies defensively", async () => {
  const calls = [];
  const api = libraryApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === methods.subjects) {
      return [{ id: "s1", name: "Физика", aliases: null, semester: "2026-autumn", semesterLabel: "Осень 2026", folders: null }];
    }
    return null;
  });
  const subjects = await api.subjects();
  assert.deepEqual(subjects[0].aliases, []);
  assert.deepEqual(subjects[0].folders, []);
  assert.equal(api.importDroppedFile, undefined);
  assert.equal(parseImport({ cancelled: true, material: null }).material, null);
  assert.throws(() => parseSubject({ id: 1 }));
  // The fake answers null: a malformed reply is an error, not a default subject.
  await assert.rejects(api.createSubject("Химия", ""));
  assert.deepEqual(calls.at(-1), [methods.createSubject, "Химия", [], ""]);
});

test("system folders come first in their fixed order with Russian names, own folders after", () => {
  const folders = orderedFolders(subject().folders);
  assert.deepEqual(
    folders.map(folderTitle),
    ["Лекции", "Семинары", "Лабораторные", "Материалы", "ДЗ", "Билеты", "Шпаргалки"],
  );
});

test("subjects are grouped by semester, the latest first", () => {
  const groups = groupBySemester([
    subject({ id: "a", semester: "2026-spring", semesterLabel: "Весна 2026" }),
    subject({ id: "b" }),
    subject({ id: "c", semester: "2025-autumn", semesterLabel: "Осень 2025" }),
    subject({ id: "d" }),
  ]);
  assert.deepEqual(groups.map((g) => g.label), ["Осень 2026", "Весна 2026", "Осень 2025"]);
  assert.deepEqual(groups[0].subjects.map((s) => s.id), ["b", "d"]);
});

test("sizes and counts are worded in Russian", () => {
  assert.equal(sizeText(12), "12 Б");
  assert.equal(sizeText(2048), "2 КБ");
  assert.equal(sizeText(1.5 * 1024 * 1024), "1,5 МБ");
  assert.deepEqual([1, 2, 5, 11, 21].map(folderCountText), ["1 файл", "2 файла", "5 файлов", "11 файлов", "21 файл"]);
});

test("the list shows semesters and subjects with «Открыть»; an empty library offers «Предмет»", () => {
  const out = renderToStaticMarkup(h(SubjectList, { subjects: [subject()], onOpen: noop, onAdd: noop }));
  assert.match(out, /Осень 2026/);
  assert.match(out, /Матанализ/);
  assert.match(out, /Лекции · Семинары · Лабораторные · Материалы · ДЗ · Билеты · Шпаргалки/);
  assert.match(out, />Открыть</);
  const empty = renderToStaticMarkup(h(SubjectList, { subjects: [], onOpen: noop, onAdd: noop }));
  assert.match(empty, /Предметов пока нет/);
  assert.match(empty, />Предмет</);
});

test("the subject page lists the folders with their files and offers «Папка»", () => {
  const out = renderToStaticMarkup(
    h(SubjectDetail, {
      subject: subject(), materials: [material(), material({ id: "m2", name: "удалён.pdf", removed: true }), material({ id: "m3", name: "входящий.txt", folderId: "", inbox: true })],
      folderForm: false, busy: false, error: "", onAddFolder: noop, onCreateFolder: noop, onCancelFolder: noop,
    }),
  );
  assert.match(out, /Лекции[\s\S]*1 файл/);
  assert.match(out, /лекция\.pdf/);
  assert.match(out, /PDF · 2 КБ · текст PDF не извлечён/);
  assert.doesNotMatch(out, /удалён\.pdf/);
  assert.doesNotMatch(out, /входящий\.txt/);
  assert.match(out, /В этой папке пока нет файлов/);
  assert.match(out, />Папка</);
  const form = renderToStaticMarkup(
    h(SubjectDetail, {
      subject: subject(), materials: [], folderForm: true, busy: false, error: "Папка с таким именем уже есть.",
      onAddFolder: noop, onCreateFolder: noop, onCancelFolder: noop,
    }),
  );
  assert.match(form, /Название папки/);
  assert.match(form, /Папка с таким именем уже есть\./);
});

test("names of subjects, folders and files are text nodes, never markup", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const s = subject({ name: evil, aliases: [evil], folders: [folder("f1", "", evil)] });
  const out =
    renderToStaticMarkup(h(SubjectList, { subjects: [s], onOpen: noop, onAdd: noop })) +
    renderToStaticMarkup(
      h(SubjectDetail, {
        subject: s, materials: [material({ name: evil, folderId: "f1" })], folderForm: false, busy: false, error: "",
        onAddFolder: noop, onCreateFolder: noop, onCancelFolder: noop,
      }),
    ) +
    renderToStaticMarkup(h(InboxList, { materials: [material({ name: evil })] }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

test("the library code builds no markup from text", () => {
  for (const f of ["../src/pages/SubjectsParts.tsx", "../src/pages/SubjectsPage.tsx", "../src/pages/DropZone.tsx", "../src/pages/JournalParts.tsx"]) {
    assert.doesNotMatch(readFileSync(new URL(f, import.meta.url), "utf8"), /dangerouslySetInnerHTML|innerHTML|insertAdjacentHTML/, f);
  }
});

test("the line after «Добавить файл»: added, already there, nothing when cancelled", () => {
  assert.equal(importNote({ cancelled: true, existing: false }), "");
  assert.equal(importNote({ cancelled: false, existing: true }), "Файл уже есть в списке.");
  assert.equal(importNote({ cancelled: false, existing: false }), "Файл добавлен.");
});

test("SubjectsPage returns to the list on a new visit", () => {
  const src = readFileSync(new URL("../src/pages/SubjectsPage.tsx", import.meta.url), "utf8");
  assert.match(src, /props\.visit !== firstVisit\.current/);
  assert.match(readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8"), /<SubjectsPage[^>]*visit=\{props\.visit\}/);
});
