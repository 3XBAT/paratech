// «Мой конспект» on the lecture page (TASK-154): view model, duplicates, remove/undo, no HTML injection.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  addLines, checkPaste, maxPasteBytes, movedOrder, orderOf, ownNotesTexts as t, partRows, pickSubjectId, recordingTarget,
  undoCall, visibleNotes, withNote, photoPages, stepPage, pageAlt, viewerAnnounce, noTextPagesText, orderAfter, pageTextAddedText,
} from "../.test-build/ownNotesView.js";
import { OwnNotesView, AiNoteLabel } from "../.test-build/pages/OwnNotesParts.js";

const evil = '<img src=x onerror=alert(1)>';
const part = (id, over = {}) => ({ materialId: id, name: `${id}.docx`, type: "docx", textStatus: "extracted", removed: false, missing: false, ...over });
const note = (over = {}) => ({
  id: "n1", subjectId: "s1", title: "Конспект", anchor: {}, parts: [part("m1"), part("m2"), part("m3")], removed: false,
  createdAt: "", updatedAt: "", ...over,
});
const noop = () => undefined;
const actions = { onPastePage: async () => true, onAddFiles: noop, onTogglePaste: noop, onPasteText: noop, onPasteSave: noop, onReplace: noop, onMove: noop, onRemove: noop, onUndo: noop };
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

test("a part shows its pieces: sections, then pages, then fragments, with Russian plurals", () => {
  const text = (over) => partRows(note({ parts: [part("a", over)] }))[0].pieces;
  assert.equal(text({ sections: 1 }), "1 раздел");
  assert.equal(text({ sections: 2 }), "2 раздела");
  assert.equal(text({ sections: 5 }), "5 разделов");
  assert.equal(text({ sections: 11 }), "11 разделов");
  assert.equal(text({ sections: 21 }), "21 раздел");
  assert.equal(text({ pages: 12 }), "12 стр.");
  assert.equal(text({ fragments: 1 }), "1 фрагмент");
  assert.equal(text({ fragments: 3 }), "3 фрагмента");
  assert.equal(text({ fragments: 14 }), "14 фрагментов");
  // Precedence.
  assert.equal(text({ sections: 2, pages: 5, fragments: 7 }), "2 раздела");
  assert.equal(text({ sections: 0, pages: 5, fragments: 5 }), "5 стр.");
  // Nothing to show.
  assert.equal(text({ sections: 0, pages: 0, fragments: 0 }), "");
  assert.equal(text({}), "");
  assert.equal(text({ sections: -3, pages: 1.5, fragments: NaN }), "");
  assert.equal(text({ sections: "5", fragments: null }), "");
});

test("parsePart normalises hostile piece counts", async () => {
  const { parsePart } = await import("../.test-build/bindings/notes.js");
  const p = (over) => parsePart({ materialId: "m", ...over });
  assert.deepEqual([p({}).sections, p({}).pages, p({}).fragments], [0, 0, 0]);
  const bad = p({ sections: -1, pages: 2.5, fragments: "9" });
  assert.deepEqual([bad.sections, bad.pages, bad.fragments], [0, 0, 0]);
  assert.equal(p({ sections: Infinity }).sections, 0);
  assert.equal(p({ fragments: 1e12 }).fragments, 1_000_000);
  assert.equal(p({ sections: 4, fragments: 4 }).sections, 4);
});

test("pieces render as a text node", () => {
  const html = render(model({ notes: [note({ parts: [part("a", { name: evil, sections: 3 })] })] }));
  assert.ok(html.includes("3 раздела"));
  assert.ok(!html.includes("<img"));
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

test("orderAfter puts the new part right after its page", () => {
  assert.deepEqual(orderAfter(["a", "p", "b", "n"], "p", "n"), ["a", "p", "n", "b"]);
  assert.deepEqual(orderAfter(["n", "a", "p"], "p", "n"), ["a", "p", "n"]);
  assert.equal(orderAfter(["a", "p", "n", "b"], "p", "n"), null, "already adjacent");
  assert.equal(orderAfter(["a", "b", "n"], "p", "n"), null, "page not found");
  assert.equal(orderAfter(["a", "p"], "p", "n"), null, "new part not found");
  assert.equal(pageTextAddedText(2), "Текст страницы 2 добавлен");
});

test("onPastePage stores the text for the note and moves it next to the page", () => {
  const src = readFileSync(new URL("../src/pages/OwnNotes.tsx", import.meta.url), "utf8");
  assert.match(src, /onPastePage:[\s\S]*?recordingTarget\([^)]*noteId\)[\s\S]*?orderAfter\([\s\S]*?api\.reorder\(noteId/);
});

const photo = (id, over = {}) =>
  part(id, { name: `IMG_${id}.jpg`, type: "jpeg", textStatus: "no_text", pageUrl: `/media/notepage/${id}`, ...over });

test("photo pages are numbered among photo parts only and use the page address", () => {
  const n = note({ parts: [part("d1"), photo("p1"), part("d2"), photo("p2", { textStatus: "extracted" })] });
  const pages = photoPages(n);
  assert.deepEqual(pages.map((p) => [p.materialId, p.number, p.noText, p.src]), [
    ["p1", 1, true, "/media/notepage/p1"],
    ["p2", 2, false, "/media/notepage/p2"],
  ]);
  assert.equal(pages[0].alt, "Страница 1: фото, текст не распознан");
  assert.equal(pages[1].alt, "Страница 2: фото");
  for (const p of pages) {
    assert.ok(!p.alt.includes("IMG_") && !p.src.includes("IMG_") && !p.src.startsWith("data:") && !p.src.startsWith("blob:"));
  }
  const rows = partRows(n);
  assert.deepEqual(rows.map((r) => r.page?.number ?? 0), [0, 1, 0, 2]);
  assert.equal(rows[1].meta, "JPEG");
  assert.equal(pageAlt(3, true), "Страница 3: фото, текст не распознан");
});

test("a foreign or empty pageUrl is no page", () => {
  const n = note({ parts: [photo("p1", { pageUrl: "https://x.invalid/a.png" }), photo("p2", { pageUrl: "" }), part("p3")] });
  assert.deepEqual(photoPages(n), []);
});

test("viewer paging clamps at the edges and keys map to pages", () => {
  assert.equal(stepPage(0, 3, "ArrowLeft"), 0);
  assert.equal(stepPage(2, 3, "ArrowRight"), 2);
  assert.equal(stepPage(0, 3, "ArrowRight"), 1);
  assert.equal(stepPage(2, 3, "ArrowLeft"), 1);
  assert.equal(stepPage(1, 3, "Home"), 0);
  assert.equal(stepPage(1, 3, "End"), 2);
  assert.equal(stepPage(0, 0, "End"), 0);
  assert.equal(viewerAnnounce(2, 5), "Страница 2 из 5");
  assert.equal(noTextPagesText(0), "");
  assert.equal(noTextPagesText(2), "2 стр. без текста");
});

test("a note with photos shows thumbnails, the no-text label and button, and the header count", () => {
  const n = note({ noTextPages: 1, parts: [photo("p1", { name: evil }), photo("p2", { textStatus: "extracted" })] });
  const html = render(model({ notes: [n] }));
  assert.ok(html.includes('src="/media/notepage/p1"'));
  assert.ok(html.includes('loading="lazy"'));
  assert.ok(html.includes("1 стр. без текста"));
  assert.equal(html.split("текст не распознан").length - 1 >= 2, true); // alt and label
  assert.ok(html.includes(`aria-label="${t.paste}: Страница 1"`)); // only the no-text page has the button
  assert.ok(!html.includes(`aria-label="${t.paste}: Страница 2"`));
  assert.ok(!html.includes("src=\"IMG_"));
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"), "the evil name is shown, escaped");
  assert.ok(!html.includes("<img src=x"), "no raw element from the name");
  const all = html.split("onerror=").length - 1;
  const escaped = html.split("&lt;img src=x onerror=").length - 1;
  assert.equal(all, escaped, "onerror= appears only inside the escaped text");
  assert.ok(html.includes(">Выше<") && html.includes(">Ниже<"));
  assert.ok(!html.includes("Распознать"));
  assert.ok(!render(model()).includes("data-thumb"));
});
test("the viewer is a labelled modal dialog with the page image from its address", async () => {
  const { PageViewer } = await import("../.test-build/pages/PageViewer.js");
  const pages = photoPages(note({ parts: [photo("p1"), photo("p2")] }));
  const html = renderToStaticMarkup(h(PageViewer, { pages, start: 1, onClose: noop }));
  assert.ok(html.includes('role="dialog"') && html.includes('aria-modal="true"'));
  assert.ok(html.includes('aria-label="Просмотр страниц"'));
  assert.ok(html.includes('src="/media/notepage/p2"'));
  assert.ok(html.includes("Страница 2 из 2"));
  assert.ok(html.includes("Назад") && html.includes("Вперёд") && html.includes("Закрыть"));
  assert.ok(!html.includes("IMG_"));
});

test("a Markdown link in a title stays text", () => {
  const html = render(model({ notes: [note({ title: "[клик](https://example.invalid)" })] }));
  assert.ok(!html.includes("<a "));
  assert.ok(html.includes("[клик](https://example.invalid)"));
});
