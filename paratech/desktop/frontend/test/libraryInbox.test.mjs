// TASK-339: the actions on the files of the library: the bindings, the row menu, the hint where a file of «Входящие» goes,
// the panel «Разложить», the undo lines, the drag of a row to a subject, «Добавить файл» into a subject.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { genericError } from "../.test-build/bindings/library.js";
import { inboxMethods, libraryInboxApi, noHint, parsePlacementHint } from "../.test-build/bindings/libraryInbox.js";
import { hasFiles } from "../.test-build/dropFiles.js";
import {
  defaultFolderId,
  errorWay,
  fileMenu,
  folderChoices,
  hintLimit,
  hintLine,
  hintTargets,
  importToNote,
  isMaterialDrag,
  materialDragType,
  panelChoice,
  placeLabel,
  placedNote,
  primaryAction,
  readMaterialDrop,
  removedNote,
  subjectsKey,
  targetFolderId,
  undoDrop,
  undoFail,
  undoMax,
  undoMs,
  undoNextDelay,
  undoPrune,
  undoPush,
  dropCue,
  addToFolderLabel,
  folderAnchor,
} from "../.test-build/libraryInboxView.js";
import { PlacePanel, UndoLines } from "../.test-build/pages/FileRows.js";
import { InboxList, SubjectDetail, SubjectList, SubjectTree } from "../.test-build/pages/SubjectsParts.js";

const noop = () => undefined;
const evil = "<img src=x onerror=alert(1)>";

function subject(over = {}) {
  return {
    id: "s1", name: "Сети", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026",
    folders: [
      { id: "f4", name: "Материалы", system: "materials" },
      { id: "f1", name: "Лекции", system: "lectures" },
      { id: "f3", name: "Лабораторные", system: "labs" },
      { id: "f9", name: "Шпаргалки", system: "" },
    ],
    ...over,
  };
}

function material(over = {}) {
  return {
    id: "m1", name: "лаба1.pdf", type: "pdf", sizeBytes: 2048, subjectId: "", folderId: "", inbox: true, removed: false,
    textStatus: "", textNote: "", createdAt: "2026-10-22T10:00:00Z", ...over,
  };
}

const existingHint = { kind: "existing", subjectId: "s1", subjectName: "Сети", folderId: "f3", folderName: "Лабораторные", folderKind: "labs", createName: "" };
const createHint = { kind: "create", subjectId: "", subjectName: "", folderId: "", folderName: "Лекции", folderKind: "lectures", createName: "Сети" };

function actions(over = {}) {
  return {
    subjects: [subject()], hints: new Map(), hintPending: new Set(),
    open: async () => "", place: async () => "", placeHint: async () => "", toInbox: async () => "", remove: async () => "",
    createSubject: async () => "", refresh: noop, ...over,
  };
}

const html = (el) => renderToStaticMarkup(el);

// ---- bindings ----

test("the bound names exist in the Go LibraryService", () => {
  const go =
    readFileSync(new URL("../../internal/input/wails/library.go", import.meta.url), "utf8") +
    readFileSync(new URL("../../internal/input/wails/library_inbox.go", import.meta.url), "utf8");
  const goMethods = new Set([...go.matchAll(/^func \(s \*LibraryService\) ([A-Z]\w*)\(/gm)].map((m) => m[1]));
  const prefix = "paratech/desktop/internal/input/wails.LibraryService.";
  for (const m of Object.values(inboxMethods)) {
    assert.ok(m.startsWith(prefix) && goMethods.has(m.slice(prefix.length)), m);
  }
  // The kinds the page reads are the kinds Go sends.
  assert.match(go, /PlacementNone\s+= "none"/);
  assert.match(go, /PlacementExisting = "existing"/);
  assert.match(go, /PlacementCreate\s+= "create"/);
});

test("the api calls the methods by name with plain ids and parses the replies", async () => {
  const calls = [];
  const m = { id: "m1", name: "a.pdf", type: "pdf", sizeBytes: 1, subjectId: "s1", folderId: "f1", inbox: false, removed: false };
  const api = libraryInboxApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === inboxMethods.placementHint) {
      return existingHint;
    }
    if (method === inboxMethods.importFileTo) {
      return { cancelled: false, existing: true, material: m };
    }
    return m;
  });
  assert.equal((await api.place("m1", "s1", "")).subjectId, "s1");
  await api.moveToInbox("m1");
  await api.removeMaterial("m1");
  await api.restoreMaterial("m1");
  assert.equal((await api.placementHint("m1")).kind, "existing");
  assert.equal((await api.importFileTo("s1", "f3")).existing, true);
  assert.deepEqual(calls.map((c) => c.map((x, i) => (i === 0 ? x.split(".").pop() : x))), [
    ["Place", "m1", "s1", ""],
    ["MoveToInbox", "m1"],
    ["RemoveMaterial", "m1"],
    ["RestoreMaterial", "m1"],
    ["PlacementHint", "m1"],
    ["ImportFileTo", "s1", "f3"],
  ]);
});

test("a malformed or incomplete hint is no hint", () => {
  assert.deepEqual(parsePlacementHint(null), noHint);
  assert.deepEqual(parsePlacementHint("existing"), noHint);
  assert.equal(parsePlacementHint({ kind: "weird", subjectId: "s1" }).kind, "none");
  assert.equal(parsePlacementHint({ kind: "existing", subjectId: "s1", subjectName: "Сети" }).kind, "none");
  assert.equal(parsePlacementHint({ kind: "create", createName: "  ", folderKind: "labs" }).kind, "none");
  assert.equal(parsePlacementHint({ kind: "create", createName: "Сети" }).kind, "none");
  assert.deepEqual(parsePlacementHint(existingHint), existingHint);
  assert.deepEqual(parsePlacementHint(createHint), createHint);
});

// ---- the row menu ----

test("the menu by state: a file of «Входящие» is placed, a placed one is moved or returned", () => {
  assert.deepEqual(fileMenu(false).map((i) => [i.key, i.label]), [["open", "Открыть"], ["place", "Разложить"], ["remove", "Удалить"]]);
  assert.deepEqual(fileMenu(true).map((i) => [i.key, i.label]), [
    ["open", "Открыть"], ["place", "Переместить…"], ["toInbox", "Вернуть во «Входящие»"], ["remove", "Удалить"],
  ]);
  assert.deepEqual(primaryAction(false), { key: "place", label: "Разложить" });
  assert.deepEqual(primaryAction(true), { key: "open", label: "Открыть" });
});

test("a row of «Входящие» has the visible «Разложить», «⋯» and a drag handle; a row in a folder has «Открыть»", () => {
  const inbox = html(h(InboxList, { materials: [material()], actions: actions() }));
  assert.match(inbox, /<button[^>]*aria-label="Разложить: лаба1\.pdf"[^>]*>Разложить<\/button>/);
  assert.match(inbox, /aria-label="Ещё действия: лаба1\.pdf"/);
  assert.match(inbox, /class="lrow-grip" draggable="true"/);
  const folder = html(
    h(SubjectDetail, {
      subject: subject(), materials: [material({ subjectId: "s1", folderId: "f1", inbox: false })], folderForm: false, busy: false, error: "",
      onCreateFolder: noop, onCancelFolder: noop, actions: actions(),
    }),
  );
  assert.match(folder, /aria-label="Открыть: лаба1\.pdf"/);
  assert.match(folder, /aria-label="Ещё действия: лаба1\.pdf"/);
  assert.doesNotMatch(folder, /draggable/);
  // Without actions the rows only show the files.
  assert.doesNotMatch(html(h(InboxList, { materials: [material()] })), /<button/);
});

test("the names are text nodes in the rows, the labels and the hint", () => {
  const out =
    html(h(InboxList, { materials: [material({ name: evil })], actions: actions({ hints: new Map([["m1", { ...existingHint, subjectName: evil, folderName: evil }]]) }) })) +
    html(h(UndoLines, { items: [{ id: 1, materialId: "m1", text: removedNote(evil), back: { kind: "restore" }, expires: 1, error: "" }], busy: false, onUndo: noop, onRefresh: noop }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
  for (const f of ["../src/pages/FileRows.tsx", "../src/pages/useMaterialActions.ts", "../src/pages/NameForm.tsx", "../src/libraryInboxView.ts", "../src/bindings/libraryInbox.ts"]) {
    assert.doesNotMatch(readFileSync(new URL(f, import.meta.url), "utf8"), /dangerouslySetInnerHTML|innerHTML|insertAdjacentHTML/, f);
  }
});

// ---- the hint ----

test("the hint line: «Похоже на» with «Положить», or «Создать предмет … и положить», or nothing", () => {
  assert.deepEqual(hintLine(existingHint), { text: "Похоже на: Сети → Лабораторные", action: "Положить" });
  assert.deepEqual(hintLine(createHint), { text: "", action: "Создать предмет „Сети“ и положить" });
  assert.equal(hintLine(noHint), null);
  assert.equal(hintLine(undefined), null);
});

test("the hint is shown under the name only for a file of «Входящие» and as a button; nothing places itself", () => {
  const withHint = html(h(InboxList, { materials: [material()], actions: actions({ hints: new Map([["m1", existingHint]]) }) }));
  assert.match(withHint, /Похоже на: Сети → Лабораторные ·/);
  assert.match(withHint, /aria-label="Положить: лаба1\.pdf"[^>]*>Положить<\/button>/);
  const create = html(h(InboxList, { materials: [material()], actions: actions({ hints: new Map([["m1", createHint]]) }) }));
  assert.match(create, />Создать предмет „Сети“ и положить<\/button>/);
  // A file in a folder has no hint line even when a hint is known; a failed hint (none) shows nothing; a pending one shows «…».
  const folder = html(h(SubjectDetail, { subject: subject(), materials: [material({ subjectId: "s1", folderId: "f1", inbox: false })], folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop, actions: actions({ hints: new Map([["m1", existingHint]]) }) }));
  assert.doesNotMatch(folder, /Похоже на/);
  assert.doesNotMatch(html(h(InboxList, { materials: [material()], actions: actions({ hints: new Map([["m1", noHint]]) }) })), /Похоже|lrow-hint/);
  assert.match(html(h(InboxList, { materials: [material()], actions: actions({ hintPending: new Set(["m1"]) }) })), /lrow-hint small muted" aria-hidden="true">…</);
});

test("at most hintLimit files are hinted, those known are not asked again, removed ones never", () => {
  const files = Array.from({ length: 30 }, (_, i) => material({ id: `m${i}` }));
  assert.equal(hintLimit, 20);
  assert.equal(hintTargets(files, new Set()).length, 20);
  assert.deepEqual(hintTargets(files, new Set(["m0", "m1"])).slice(0, 2), ["m2", "m3"]);
  assert.equal(hintTargets(files, new Set(files.slice(0, 20).map((m) => m.id))).length, 0);
  assert.deepEqual(hintTargets([material({ id: "a", removed: true }), material({ id: "b" })], new Set()), ["b"]);
  assert.equal(subjectsKey(null), "");
  assert.notEqual(subjectsKey([subject()]), subjectsKey([subject(), subject({ id: "s2" })]));
});

// ---- the panel ----

test("the panel offers the subjects and the folders of the chosen one; «Материалы» is the default", () => {
  const s = subject();
  assert.deepEqual(folderChoices(s).map((f) => f.title), ["Лекции", "Лабораторные", "Материалы", "Шпаргалки"]);
  assert.deepEqual(folderChoices(undefined), []);
  assert.equal(defaultFolderId(s), "f4");
  assert.equal(defaultFolderId(s, "f3"), "f3");
  assert.equal(defaultFolderId(s, "gone"), "f4");
  assert.equal(defaultFolderId(subject({ folders: [{ id: "x", name: "Мои", system: "" }] })), "x");
  assert.equal(defaultFolderId(undefined), "");
  const other = subject({ id: "s2", name: "Физика" });
  assert.deepEqual(panelChoice([s, other], "s2", ""), { subjectId: "s2", folderId: "f4" });
  assert.deepEqual(panelChoice([s, other], "gone", "f3"), { subjectId: "s1", folderId: "f3" });
  assert.deepEqual(panelChoice([], "s1", "f3"), { subjectId: "", folderId: "" });
});

test("the panel: two selects, «Положить» and «Отмена»; without subjects it says so and offers «Создать предмет»", () => {
  const out = html(h(PlacePanel, { subjects: [subject(), subject({ id: "s2", name: evil })], subjectId: "s1", folderId: "f3", busy: false, onSubmit: noop, onCancel: noop, onCreateSubject: async () => "" }));
  assert.match(out, />Предмет</);
  assert.match(out, />Папка</);
  assert.match(out, /<option value="f3" selected="">Лабораторные<\/option>/);
  assert.match(out, />Положить<\/button>/);
  assert.match(out, />Отмена<\/button>/);
  assert.ok(!out.includes("<img"));
  const none = html(h(PlacePanel, { subjects: [], subjectId: "", folderId: "", busy: false, onSubmit: noop, onCancel: noop, onCreateSubject: async () => "" }));
  assert.match(none, /Предметов пока нет/);
  assert.match(none, />Создать предмет<\/button>/);
  assert.doesNotMatch(none, /<select/);
});

test("the panel's «Положить» is off while the call runs", () => {
  const out = html(h(PlacePanel, { subjects: [subject()], subjectId: "s1", folderId: "f4", busy: true, onSubmit: noop, onCancel: noop, onCreateSubject: async () => "" }));
  assert.match(out, /<button[^>]*disabled=""[^>]*>Положить<\/button>/);
});

// ---- notes and refusals ----

test("where a file lies is worded from the subjects; the notes", () => {
  const subjects = [subject()];
  assert.equal(placeLabel(subjects, "s1", "f3"), "Сети → Лабораторные");
  assert.equal(placeLabel(subjects, "s1", "gone"), "Сети");
  assert.equal(placeLabel(subjects, "gone", "f3"), "");
  assert.equal(placedNote("Сети → Лабораторные"), "Положено в «Сети → Лабораторные».");
  assert.equal(placedNote(""), "Файл положен.");
  assert.equal(removedNote("лаба1.pdf"), "Удалено «лаба1.pdf».");
});

test("a refusal either says the list is out of date («Обновить список») or can be repeated", () => {
  for (const t of ["Предмет не найден: обновите список.", "Папка не найдена: обновите список.", "Файл не найден: обновите список.", "Неверный идентификатор: обновите список.", "Предмет с таким названием уже есть."]) {
    assert.equal(errorWay(t), "refresh", t);
  }
  for (const t of ["Данные сейчас меняются. Попробуйте ещё раз.", "Не удалось прочитать библиотеку. Попробуйте ещё раз.", genericError]) {
    assert.equal(errorWay(t), "retry", t);
  }
});

// ---- undo ----

const entry = (id, materialId, text = "t") => ({ id, materialId, text, back: { kind: "inbox" } });

test("undo lines: a second delete keeps the first, each has its own ten seconds, the newest undoMax stay", () => {
  let list = undoPush([], entry(1, "a"), 1000);
  list = undoPush(list, entry(2, "b"), 4000);
  assert.deepEqual(list.map((x) => x.materialId), ["a", "b"]);
  assert.equal(list[0].expires, 1000 + undoMs);
  assert.equal(undoNextDelay(list, 5000), 6000);
  // The first goes at 11 000, the second is still there.
  assert.deepEqual(undoPrune(list, 11_000).map((x) => x.materialId), ["b"]);
  assert.deepEqual(undoPrune(list, 14_000), []);
  assert.equal(undoNextDelay([], 0), null);
  assert.equal(undoNextDelay(list, 20_000), 0);
  // A push prunes the lines that are over.
  assert.deepEqual(undoPush(list, entry(3, "c"), 12_000).map((x) => x.materialId), ["b", "c"]);
  // The same file again replaces its line; the fourth line pushes out the oldest.
  assert.deepEqual(undoPush(list, entry(3, "a", "new"), 5000).map((x) => [x.materialId, x.text]), [["b", "t"], ["a", "new"]]);
  let many = [];
  for (let i = 0; i < undoMax + 2; i++) {
    many = undoPush(many, entry(i, `f${i}`), 100 + i);
  }
  assert.deepEqual(many.map((x) => x.materialId), ["f2", "f3", "f4"]);
});

test("a refused «Вернуть» keeps its line with the reason; a done one is dropped", () => {
  const list = undoPush([], entry(1, "a"), 0);
  const failed = undoFail(list, 1, "Файл не найден: обновите список.");
  assert.equal(failed[0].error, "Файл не найден: обновите список.");
  assert.deepEqual(undoDrop(failed, 1), []);
  const out = html(h(UndoLines, { items: failed, busy: false, onUndo: noop, onRefresh: noop }));
  assert.match(out, /role="status" aria-live="polite"/);
  assert.match(out, /Файл не найден: обновите список\./);
  assert.match(out, />Обновить список<\/button>/);
  assert.doesNotMatch(out, />Вернуть<\/button>/);
  const ok = html(h(UndoLines, { items: list, busy: false, onUndo: noop, onRefresh: noop }));
  assert.match(ok, />Вернуть<\/button>/);
  // The live region is there before any line comes.
  assert.match(html(h(UndoLines, { items: [], busy: false, onUndo: noop, onRefresh: noop })), /<div class="undo-lines" role="status" aria-live="polite"><\/div>/);
});

test("the undo timer is cleaned up with the component", () => {
  const src = readFileSync(new URL("../src/pages/useMaterialActions.ts", import.meta.url), "utf8");
  assert.match(src, /const timer = setTimeout\([\s\S]*?return \(\) => clearTimeout\(timer\)/);
});

// ---- the drag of a row to a card ----

test("the drag carries only the id of a file of «Входящие»; a drag of files from the disk is not it", () => {
  assert.equal(materialDragType, "application/x-paratech-material");
  assert.equal(isMaterialDrag({ types: ["text/plain", materialDragType] }), true);
  assert.equal(isMaterialDrag({ types: ["Files"] }), false);
  assert.equal(isMaterialDrag(null), false);
  // The window's drop zone shows its overlay only for files, never for this drag.
  assert.equal(hasFiles({ types: [materialDragType] }), false);
  const ids = new Set(["m1", "m2"]);
  assert.equal(readMaterialDrop("m1", ids), "m1");
  assert.equal(readMaterialDrop("m3", ids), null);
  assert.equal(readMaterialDrop("", ids), null);
  assert.equal(readMaterialDrop(undefined, ids), null);
  assert.equal(readMaterialDrop("x".repeat(200), new Set(["x".repeat(200)])), null);
  assert.equal(dropCue("Сети"), "Положить в «Сети»");
});

test("the window drop zone ignores the internal drag", () => {
  const src = readFileSync(new URL("../src/pages/DropZone.tsx", import.meta.url), "utf8");
  assert.match(src, /if \(hasFiles\(e\.dataTransfer\)\) \{\s*e\.preventDefault\(\);\s*depth\.current = nextDepth\(depth\.current, "enter"\)/);
  assert.match(src, /const dragover = [\s\S]*?isMaterialDrag\(e\.dataTransfer\)/);
  assert.match(src, /const leave = [\s\S]*?isMaterialDrag\(e\.dataTransfer\)/);
  assert.match(src, /const drop = [\s\S]*?isMaterialDrag\(e\.dataTransfer\)/);
});

test("the cards show the cue while a row is dragged and otherwise «Открыть»", () => {
  const drop = (dragging) => ({ dragging, inboxIds: new Set(["m1"]), onDrop: noop, onEnd: noop });
  const dragging = html(h(SubjectList, { subjects: [subject()], onOpen: noop, onAdd: noop, drop: drop(true) }));
  assert.match(dragging, /Положить в «Сети»/);
  assert.doesNotMatch(dragging, />Открыть</);
  const idle = html(h(SubjectList, { subjects: [subject()], onOpen: noop, onAdd: noop, drop: drop(false) }));
  assert.match(idle, />Открыть</);
  assert.doesNotMatch(idle, /Положить в/);
});

// ---- «Добавить файл» into a subject ----

test("the line after «Добавить файл» on a subject page: added, cancelled, already there, already elsewhere", () => {
  const subjects = [subject(), subject({ id: "s2", name: "Физика", folders: [{ id: "g1", name: "Материалы", system: "materials" }, { id: "g2", name: "Лекции", system: "lectures" }] })];
  const target = { subjectId: "s1", folderId: "f3" };
  const mat = (over) => material({ subjectId: "s1", folderId: "f3", inbox: false, ...over });
  assert.equal(importToNote({ cancelled: true, existing: false, material: null }, target, subjects), "");
  assert.equal(importToNote({ cancelled: false, existing: false, material: mat() }, target, subjects), "Файл добавлен в «Сети → Лабораторные».");
  // «Материалы» when no folder is given.
  assert.equal(importToNote({ cancelled: false, existing: false, material: mat({ folderId: "f4" }) }, { subjectId: "s1", folderId: "" }, subjects), "Файл добавлен в «Сети → Материалы».");
  assert.equal(importToNote({ cancelled: false, existing: true, material: mat() }, target, subjects), "Файл уже есть в списке.");
  assert.equal(importToNote({ cancelled: false, existing: true, material: mat({ folderId: "f4" }) }, { subjectId: "s1", folderId: "" }, subjects), "Файл уже есть в списке.");
  assert.equal(importToNote({ cancelled: false, existing: true, material: mat({ subjectId: "s2", folderId: "g2" }) }, target, subjects), "Этот файл уже есть: «Физика → Лекции».");
  assert.equal(importToNote({ cancelled: false, existing: true, material: mat({ subjectId: "", folderId: "", inbox: true }) }, target, subjects), "Этот файл уже есть: «Входящие».");
  assert.equal(targetFolderId(subjects, "s1", ""), "f4");
  assert.equal(targetFolderId(subjects, "s1", "f3"), "f3");
});

test("every folder of a subject has «+» with the folder in its name, and a section the tree can scroll to", () => {
  const out = html(h(SubjectDetail, { subject: subject(), materials: [], folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop, onAddFile: noop }));
  assert.match(out, /aria-label="Добавить файл в папку «Лекции»"/);
  assert.match(out, /aria-label="Добавить файл в папку «Шпаргалки»"/);
  assert.equal((out.match(/aria-label="Добавить файл в папку/g) ?? []).length, 4);
  assert.match(out, new RegExp(`id="${folderAnchor("f1")}"`));
  assert.equal(addToFolderLabel("Лекции"), "Добавить файл в папку «Лекции»");
  // Without the handler there is no button.
  assert.doesNotMatch(html(h(SubjectDetail, { subject: subject(), materials: [], folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop })), /Добавить файл/);
});

test("the subject page has «Добавить файл» in its header and the page asks the core for the subject and folder", () => {
  const page = readFileSync(new URL("../src/pages/SubjectsPage.tsx", import.meta.url), "utf8");
  assert.match(page, /addFileTo\(""\)/);
  assert.match(page, /inboxApi\.importFileTo\(openId, folderId\)/);
  assert.match(page, /onAddFile=\{\(folderId\) => void addFileTo\(folderId\)\}/);
});

// ---- «Осень 2026» and the tree ----

test("the semester is a heading without the folder icon; the folders of the open subject are buttons that scroll to their section", () => {
  const out = html(h(SubjectTree, { subjects: [subject()], currentId: "s1", counts: new Map(), busy: false, onOpen: noop, onAddSubject: noop, onAddFolder: noop }));
  assert.match(out, /<button type="button" class="ti sem" aria-expanded="true"><svg[^>]*><use href="#i-chev"><\/use><\/svg><span class="nm">Осень 2026<\/span><\/button>/);
  assert.equal((out.match(/class="ti fold"/g) ?? []).length, 4);
  // The semester row has no folder icon; the folder rows have.
  const sem = out.slice(out.indexOf('class="ti sem"'), out.indexOf("</button>", out.indexOf('class="ti sem"')));
  assert.doesNotMatch(sem, /i-folder/);
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /\.tree \.lv1 \.ti \{ padding-left: 20px; \}/);
  assert.match(css, /\.tree \.lv2 \.ti \{ padding-left: 34px; \}/);
  assert.match(css, /\.tree \.ti\.sem \{[^}]*text-transform: uppercase/);
  const parts = readFileSync(new URL("../src/pages/SubjectsParts.tsx", import.meta.url), "utf8");
  assert.match(parts, /getElementById\(folderAnchor\(folderId\)\)\?\.scrollIntoView\?\./);
  assert.match(parts, /typeof document === "undefined"/);
});

test("the card of files does not clip the «⋯» menu", () => {
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  assert.match(css, /\.card\.lrows \{ overflow: visible; \}/);
});
