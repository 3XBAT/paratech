// «Мой конспект» on the lecture page (TASK-154): view model, duplicates, remove/undo, no HTML injection.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  addLines, checkPaste, maxPasteBytes, movedOrder, orderOf, ownNotesTexts as t, partRows, pickSubjectId, recordingTarget,
  undoCall, visibleNotes, withNote,
} from "../.test-build/ownNotesView.js";
import { OwnNotesView, AiNoteLabel } from "../.test-build/pages/OwnNotesParts.js";

const evil = '<img src=x onerror=alert(1)>';
const part = (id, over = {}) => ({ materialId: id, name: `${id}.docx`, type: "docx", textStatus: "extracted", removed: false, missing: false, ...over });
const note = (over = {}) => ({
  id: "n1", subjectId: "s1", title: "Конспект", anchor: {}, parts: [part("m1"), part("m2"), part("m3")], removed: false,
  createdAt: "", updatedAt: "", ...over,
});
const noop = () => undefined;
const actions = { onAddFiles: noop, onTogglePaste: noop, onPasteText: noop, onPasteSave: noop, onReplace: noop, onMove: noop, onRemove: noop, onUndo: noop };
const model = (over = {}) => ({
  notes: [note()], loading: false, subjectId: "s1", target: recordingTarget("r1", "s1"), busy: false, undo: null,
  status: null, lines: [], pasteOpen: false, pasteText: "", ...over,
});
const render = (m) => renderToStaticMarkup(h(OwnNotesView, { model: m, actions }));

test("part rows show name, type and state, and which moves are possible", () => {
  const rows = partRows(note({ parts: [part("a"), part("b", { type: "pdf", textStatus: "pdf_not_extracted" }), part("c", { missing: true })] }));
  assert.deepEqual(rows.map((r) => [r.canUp, r.canDown]), [[false, true], [true, true], [true, false]]);
  assert.equal(rows[0].meta, "DOCX");
  assert.equal(rows[1].meta, "PDF · текст PDF не извлечён");
  assert.equal(rows[2].meta, "DOCX · файла больше нет в библиотеке");
});

test("movedOrder swaps neighbours and refuses the edges", () => {
  assert.deepEqual(movedOrder(["a", "b", "c"], 1, -1), ["b", "a", "c"]);
  assert.deepEqual(movedOrder(["a", "b", "c"], 1, 1), ["a", "c", "b"]);
  assert.equal(movedOrder(["a", "b"], 0, -1), null);
  assert.equal(movedOrder(["a", "b"], 1, 1), null);
  assert.deepEqual(orderOf(note()), ["m1", "m2", "m3"]);
});

test("paste is limited by bytes of UTF-8 and the limit is visible", () => {
  assert.equal(checkPaste("  \n").kind, "empty");
  assert.equal(checkPaste("конспект").kind, "ok");
  assert.equal(checkPaste("я".repeat(maxPasteBytes / 2)).kind, "ok");
  assert.equal(checkPaste("я".repeat(maxPasteBytes / 2 + 1)).kind, "long");
  const html = render(model({ pasteOpen: true, pasteText: "abc" }));
  assert.ok(html.includes("Не больше 1 МБ."));
  assert.ok(html.includes("<textarea"));
});

test("a duplicate says «уже добавлено»; a refusal and a cancel read in Russian", () => {
  const lines = addLines({
    cancelled: false, error: "", files: [
      { name: "a.docx", alreadyAdded: true, error: "" },
      { name: "b.pdf", alreadyAdded: false, error: "Файл пустой." },
      { name: "c.txt", alreadyAdded: false, error: "" },
    ],
  });
  assert.deepEqual(lines, [
    { ok: true, text: "a.docx: уже добавлено" },
    { ok: false, text: "b.pdf: Файл пустой." },
    { ok: true, text: "c.txt: добавлено" },
  ]);
  assert.deepEqual(addLines({ cancelled: true, error: "", files: [] }), [{ ok: true, text: t.cancelled }]);
  assert.deepEqual(addLines({ cancelled: false, error: "Не выбрано ни одного файла.", files: [] }), [{ ok: false, text: "Не выбрано ни одного файла." }]);
  const html = render(model({ lines }));
  assert.ok(html.includes("уже добавлено"));
});

test("remove keeps the note on screen for «Отменить» only until the undo is gone", () => {
  const removed = note({ removed: true });
  assert.equal(visibleNotes([removed], "").length, 0);
  assert.equal(visibleNotes([removed], "n1").length, 1);
  const html = render(model({ notes: [removed], undo: { kind: "remove", noteId: "n1" }, status: { ok: true, text: t.removedLine } }));
  assert.ok(html.indexOf('role="status"') < html.indexOf(t.removedLine));
  assert.ok(html.includes(t.removedLine));
  assert.ok(html.includes(t.undo));
  assert.ok(!render(model({ notes: [removed] })).includes(t.undo));
  const restored = withNote([removed], note());
  assert.equal(restored.length, 1);
  assert.equal(restored[0].removed, false);
});

test("a replace offers «Отменить» in the status region", () => {
  const html = render(model({ undo: { kind: "replace", noteId: "n1", currentId: "m4", previousId: "m1" }, status: { ok: true, text: "Файл заменён." } }));
  assert.ok(html.includes('role="status"'));
  assert.ok(html.includes("Файл заменён."));
  assert.ok(html.includes(t.undo));
});

test("the two blocks are labelled and never merged", () => {
  const html = render(model());
  assert.ok(html.includes(t.ownChip));
  assert.ok(!html.includes(t.paratechChip));
  const label = renderToStaticMarkup(h(AiNoteLabel));
  assert.ok(label.includes(t.paratechChip));
  assert.ok(!label.includes(t.ownChip));
});

test("without a library subject adding is off and says why", () => {
  const html = render(model({ subjectId: "" }));
  assert.ok(html.includes(t.noSubject));
  assert.ok(!html.includes("data-file-drop-target"));
  assert.ok(render(model()).includes("data-file-drop-target"));
});

test("the subject is the one of an existing note, else the one the core matched", () => {
  assert.equal(pickSubjectId([{ subjectId: "s1" }], "s9", "matched"), "s1");
  assert.equal(pickSubjectId([{ subjectId: "s1" }], "", "ambiguous"), "s1");
  assert.equal(pickSubjectId([], "s9", "matched"), "s9");
  for (const state of ["none", "ambiguous", "no-subject"]) {
    assert.equal(pickSubjectId([], "", state), "");
    assert.equal(pickSubjectId([], "s9", state), "");
  }
});

test("names, titles and refusals render as text: no element is created from them", () => {
  const html = render(model({
    notes: [note({ title: evil, parts: [part("m1", { name: evil })] })],
    lines: [{ ok: false, text: `${evil}: Файл пустой.` }],
    status: { ok: false, text: evil },
    pasteOpen: true, pasteText: evil,
  }));
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(!html.includes("<a "));
  for (const f of ["ownNotesView.ts", "pages/OwnNotes.tsx", "pages/OwnNotesParts.tsx"]) {
    const src = readFileSync(new URL(`../src/${f}`, import.meta.url), "utf8");
    assert.ok(!src.includes("dangerouslySetInnerHTML"), f);
    assert.ok(!src.includes("innerHTML"), f);
  }
});

test("adding is off with a reason for an ambiguous or unknown subject", () => {
  const off = (subjectState) => render(model({ subjectId: "", subjectState }));
  assert.ok(off("ambiguous").includes("У нескольких предметов в библиотеке одно название"));
  assert.ok(!off("ambiguous").includes(t.subjectNone));
  assert.ok(off("none").includes("Предмет занятия не найден в библиотеке"));
  assert.ok(off("no-subject").includes(t.noSubject));
  assert.ok(off("matched").includes(t.noSubject));
  assert.ok(!off("ambiguous").includes("data-file-drop-target"));
  assert.ok(!off("none").includes("data-file-drop-target"));
});

test("the page no longer matches subject names itself", () => {
  const src = readFileSync(new URL("../src/ownNotesView.ts", import.meta.url), "utf8");
  assert.ok(!src.includes("subjectKey"));
  const page = readFileSync(new URL("../src/pages/OwnNotes.tsx", import.meta.url), "utf8");
  assert.ok(!page.includes("libraryApi"));
  assert.ok(!page.includes("bindings/library"));
});

test("undoCall sends the right call for each action", async () => {
  const calls = [];
  const n = note();
  const api = {
    restore: async (id) => (calls.push(["restore", id]), n),
    reorder: async (id, order) => (calls.push(["reorder", id, order]), n),
    undoReplace: async (id, cur, prev) => (calls.push(["undoReplace", id, cur, prev]), { note: id === "gone" ? null : n }),
  };
  assert.equal(await undoCall(api, { kind: "remove", noteId: "n1" }), n);
  assert.equal(await undoCall(api, { kind: "reorder", noteId: "n1", order: ["m2", "m1"] }), n);
  assert.equal(await undoCall(api, { kind: "replace", noteId: "n1", currentId: "m4", previousId: "m1" }), n);
  assert.equal(await undoCall(api, { kind: "replace", noteId: "gone", currentId: "m4", previousId: "m1" }), null);
  assert.deepEqual(calls.slice(0, 3), [["restore", "n1"], ["reorder", "n1", ["m2", "m1"]], ["undoReplace", "n1", "m4", "m1"]]);
});

test("a new add clears the undo in the page", () => {
  const src = readFileSync(new URL("../src/pages/OwnNotes.tsx", import.meta.url), "utf8");
  assert.match(src, /function applyAdd[\s\S]*?setUndo\(null\)/);
});

test("a Markdown link in a title stays text", () => {
  const html = render(model({ notes: [note({ title: "[клик](https://example.invalid)" })] }));
  assert.ok(!html.includes("<a "));
  assert.ok(html.includes("[клик](https://example.invalid)"));
});
