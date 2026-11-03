// TASK-339, the UX round: «Входящие» on top of the list, the panel that does not guess a subject (with «Новый предмет…»),
// «Вернуть» that also removes the subject a hint made, the result of a folder's «+» inside the folder, the line of a file
// that did not land («Разложить» in the line), the texts and the tooltips.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { libraryApi, methods } from "../.test-build/bindings/library.js";
import { MaterialFlow } from "../.test-build/materialFlow.js";
import {
  createdNote,
  fileMenu,
  fileTexts,
  importOutcome,
  newSubjectValue,
  panelBack,
  panelChoice,
  panelCreated,
  panelEscape,
  panelPickSubject,
  panelStart,
} from "../.test-build/libraryInboxView.js";
import { texts } from "../.test-build/libraryView.js";
import { AddFileLines, PlacePanel } from "../.test-build/pages/FileRows.js";
import { InboxList, LibraryActions, SubjectDetail } from "../.test-build/pages/SubjectsParts.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);

function subject(over = {}) {
  return {
    id: "s1", name: "Сети", aliases: [], semester: "2026-autumn", semesterLabel: "Осень 2026",
    folders: [
      { id: "f4", name: "Материалы", system: "materials" },
      { id: "f1", name: "Лекции", system: "lectures" },
      { id: "f3", name: "Лабораторные", system: "labs" },
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

const create = async () => ({ error: "", subject: null });
const panel = (over = {}) =>
  html(h(PlacePanel, { subjects: [subject(), subject({ id: "s2", name: "Физика" })], subjectId: "", folderId: "", busy: false, onSubmit: noop, onCancel: noop, onCreateSubject: create, ...over }));

// ---- 1. «Входящие» ----
// TASK-341: «Входящие» is the first item of the tree and its own view (no list page to scroll to any more), so the three
// tests of the section anchor and of the pending scroll are replaced by tests of the two forms of InboxList.

test("on the screen without subjects «Входящие» is a section above the empty state, and nothing while empty", () => {
  const actions = { subjects: [], hints: new Map(), hintPending: new Set(), open: noop, place: noop, placeHint: noop, toInbox: noop, remove: noop, createSubject: create, refresh: noop };
  const full = html(h(InboxList, { materials: [material()], actions }));
  assert.match(full, /<h2 class="sec-h">Входящие/);
  assert.match(full, /лаба1\.pdf/);
  assert.equal(html(h(InboxList, { materials: [] })), "");
  assert.equal(html(h(InboxList, { materials: [material({ removed: true })] })), "", "a deleted file does not count");
});

test("the view «Входящие» has no heading of its own (the head names it) and tells how files get there while empty", () => {
  const view = (materials, over = {}) => html(h(InboxList, { materials, bare: true, ...over }));
  const full = view([material()]);
  assert.doesNotMatch(full, /<h2/);
  assert.match(full, /лаба1\.pdf/);
  assert.match(full, new RegExp(texts.inboxHint.slice(0, 30)), "the hint line stays under the rows");
  const empty = view([]);
  assert.match(empty, /Здесь собираются файлы без предмета/);
  assert.doesNotMatch(empty, /<h2|<button|<ul/);
  assert.match(view([], { loading: true }), /Загрузка…/);
  assert.doesNotMatch(view([], { loading: true }), /собираются/, "an empty list is not announced before the answer");
});

test("«Добавить файл» in the head: the tooltip says where the file goes, in «Входящие» and in a subject", () => {
  const head = (addTip) => html(h(LibraryActions, { onAddFile: noop, addTip, busy: false }));
  assert.match(head(fileTexts.addToInboxTip), /<button[^>]*title="Файл попадёт во «Входящие»"[^>]*>(<svg.*?<\/svg>)?Добавить файл<\/button>/);
  assert.match(head(fileTexts.addToMaterialsTip), /title="Файл попадёт в «Материалы»"/);
  // TASK-353: «База знаний» moved to the tree and «Билеты» to the head of a subject; the head of files holds only the add button.
  const out = head(fileTexts.addToInboxTip);
  assert.doesNotMatch(out, /База знаний|Билеты/);
  assert.match(html(h(LibraryActions, { onAddFile: noop, addTip: "", busy: true })), /<button[^>]*disabled=""[^>]*>(<svg.*?<\/svg>)?Добавить файл/, "off while a call runs");
});

test("Esc in the panel steps back out of the «Новый предмет…» form and closes the panel only otherwise", () => {
  assert.equal(panelEscape(panelStart("", "")), "close");
  assert.equal(panelEscape(panelPickSubject(panelStart("", ""), newSubjectValue)), "back");
  const creating = panelPickSubject(panelStart("s1", "f1"), newSubjectValue);
  const back = panelBack(creating);
  assert.equal(back.creating, false);
  assert.equal(panelEscape(back), "close", "the second Esc closes");
});

// ---- 2. the panel ----

test("without a hint the panel starts on «Выберите предмет» and «Положить» is off", () => {
  const out = panel();
  assert.match(out, /<option value="" selected="">Выберите предмет<\/option>/);
  assert.match(out, /<button[^>]*disabled=""[^>]*>Положить<\/button>/);
  assert.match(out, /<select[^>]*disabled=""[^>]*>/, "the folder select waits for a subject");
  // «Новый предмет…» is the last option of the subject select.
  const subjectSelect = out.slice(out.indexOf("<select"), out.indexOf("</select>"));
  assert.ok(subjectSelect.trimEnd().endsWith(`<option value="${newSubjectValue}">Новый предмет…</option>`));
});

test("with a hint (or the place of a placed file) the panel keeps the preselect and «Положить» works", () => {
  const out = panel({ subjectId: "s1", folderId: "f3" });
  assert.match(out, /<option value="s1" selected="">Сети<\/option>/);
  assert.match(out, /<option value="f3" selected="">Лабораторные<\/option>/);
  assert.doesNotMatch(out, /<button[^>]*disabled=""[^>]*>Положить<\/button>/);
  // A subject without a wished folder opens on «Материалы».
  assert.match(panel({ subjectId: "s1", folderId: "" }), /<option value="f4" selected="">Материалы<\/option>/);
  assert.deepEqual(panelChoice([subject()], "s1", ""), { subjectId: "s1", folderId: "f4" });
});

test("«Новый предмет…» opens the name form; a created subject is selected, not placed into; cancel returns", () => {
  let st = panelStart("", "");
  st = panelPickSubject(st, "s2");
  assert.deepEqual(st, { subjectId: "s2", folderId: "", creating: false });
  st = panelPickSubject({ ...st, folderId: "f3" }, newSubjectValue);
  assert.equal(st.creating, true);
  assert.equal(st.subjectId, "s2", "the choice before it stays for «Отмена»");
  assert.deepEqual(panelPickSubject(st, "s1"), { subjectId: "s1", folderId: "", creating: false }, "another subject closes the form, the folder resets");
  assert.deepEqual(panelCreated("s-new"), { subjectId: "s-new", folderId: "", creating: false });
  // With the created subject in the list, the panel opens on it with «Материалы» and «Положить» is on.
  const made = subject({ id: "s-new", name: "Новый" });
  assert.deepEqual(panelChoice([subject(), made], "s-new", ""), { subjectId: "s-new", folderId: "f4" });
});

test("no subjects at all: the same «Создать предмет» path, no selects", () => {
  const out = panel({ subjects: [] });
  assert.match(out, /Предметов пока нет/);
  assert.match(out, />Создать предмет<\/button>/);
  assert.doesNotMatch(out, /<select/);
});

// ---- 3. «Вернуть» after a «create» hint ----

const createHint = { kind: "create", subjectId: "", subjectName: "", folderId: "", folderName: "Лабораторные", folderKind: "labs", createName: "Сети", createAliases: [] };

function flowWith(over = {}) {
  const subjects = over.subjects ?? [];
  const log = { removed: [], moved: [], refreshed: 0 };
  const flow = new MaterialFlow({
    api: {
      createSubject: async (name) => subject({ id: "s-new", name }),
      removeSubject: async (id) => {
        log.removed.push(id);
        if (over.removeFails) {
          throw new Error(over.removeFails);
        }
      },
    },
    inboxApi: {
      place: async (id, subjectId, folderId) => material({ id, subjectId, folderId, inbox: false }),
      moveToInbox: async (id) => {
        log.moved.push(id);
        return material({ id });
      },
      removeMaterial: async (id) => material({ id, removed: true }),
      restoreMaterial: async (id) => material({ id }),
    },
    subjects: () => subjects,
    addSubject: (s) => subjects.push(s),
    refresh: () => (log.refreshed += 1),
  });
  return { flow, log };
}

test("«Вернуть» after a created subject names the subject and the folder, puts the file back and removes the empty subject", async () => {
  const { flow, log } = flowWith();
  assert.equal(await flow.placeHint(material(), createHint), "");
  const it = flow.getState().undo[0];
  assert.equal(it.text, "Создан предмет «Сети», файл положен в «Лабораторные».");
  assert.equal(it.text, createdNote("Сети", "Лабораторные"));
  await flow.undoOne(it);
  assert.deepEqual(log.moved, ["m1"]);
  assert.deepEqual(log.removed, ["s-new"]);
  assert.equal(flow.getState().undo.length, 0);
  assert.equal(log.refreshed, 2);
});

test("a subject that was there before is not removed by «Вернуть»", async () => {
  const { flow, log } = flowWith({ subjects: [subject({ id: "s7", folders: [{ id: "g3", name: "Лабораторные", system: "labs" }] })] });
  assert.equal(await flow.placeHint(material(), createHint), "");
  assert.equal(flow.getState().undo[0].text, "Положено в «Сети → Лабораторные».");
  await flow.undoOne(flow.getState().undo[0]);
  assert.deepEqual(log.moved, ["m1"]);
  assert.deepEqual(log.removed, []);
});

test("a refusal to remove the subject is silent: the file is back in «Входящие» and the line is gone", async () => {
  const { flow, log } = flowWith({ removeFails: "Сначала уберите отсюда все файлы, в том числе удалённые." });
  await flow.placeHint(material(), createHint);
  await flow.undoOne(flow.getState().undo[0]);
  assert.deepEqual(log.removed, ["s-new"]);
  assert.deepEqual(log.moved, ["m1"]);
  assert.equal(flow.getState().undo.length, 0, "no error line");
  assert.equal(flow.getState().undoBusy, false);
});

test("a file that is not back (the move refused) keeps the subject", async () => {
  const { flow, log } = flowWith();
  await flow.placeHint(material(), createHint);
  const failing = new MaterialFlow({
    api: { createSubject: async () => subject(), removeSubject: async (id) => log.removed.push(id) },
    inboxApi: { moveToInbox: async () => { throw new Error("Файл не найден: обновите список."); } },
    subjects: () => [],
    addSubject: noop,
    refresh: noop,
  });
  failing.pushUndo("m1", "t", { kind: "inbox", removeSubject: "s-new" });
  await failing.undoOne(failing.getState().undo[0]);
  assert.deepEqual(log.removed, [], "the subject is removed only after the file is back");
  assert.equal(failing.getState().undo[0].error, "Файл не найден: обновите список.");
});

test("a repeat after a refused placing still counts the subject as made here", async () => {
  let fail = true;
  const subjects = [];
  const log = { removed: [] };
  const flow = new MaterialFlow({
    api: { createSubject: async (name) => subject({ id: "s-new", name }), removeSubject: async (id) => log.removed.push(id) },
    inboxApi: {
      place: async (id, s, f) => {
        if (fail) {
          throw new Error("Папка не найдена: обновите список.");
        }
        return material({ id, subjectId: s, folderId: f, inbox: false });
      },
      moveToInbox: async (id) => material({ id }),
    },
    subjects: () => subjects,
    addSubject: (s) => subjects.push(s),
    refresh: noop,
  });
  assert.notEqual(await flow.placeHint(material(), createHint), "");
  fail = false;
  assert.equal(await flow.placeHint(material(), createHint), "");
  assert.match(flow.getState().undo[0].text, /^Создан предмет «Сети»/);
  await flow.undoOne(flow.getState().undo[0]);
  assert.deepEqual(log.removed, ["s-new"]);
});

test("the binding RemoveSubject takes the id of the subject only", async () => {
  const calls = [];
  await libraryApi(async (...a) => (calls.push(a), null)).removeSubject("s1");
  assert.deepEqual(calls, [[methods.removeSubject, "s1"]]);
  assert.ok(methods.removeSubject.endsWith(".RemoveSubject"));
});

// ---- 4. the words ----

test("the glossary and the quotes: «Переместить сюда», «…» everywhere, «Разложить…» in the menu only", () => {
  assert.deepEqual(fileMenu(false).map((i) => i.label), ["Открыть", "Разложить…", "Удалить"]);
  assert.equal(fileTexts.place, "Разложить", "the visible button of the row");
  assert.deepEqual(fileMenu(true).map((i) => i.label), ["Открыть", "Переместить…", "Вернуть во «Входящие»", "Удалить"]);
  const there = material({ subjectId: "s2", folderId: "g2", inbox: false });
  const out = html(h(AddFileLines, addLines({ note: "x", elsewhere: { material: there, label: "Физика → Лекции", openSubjectId: "s2" } })));
  assert.match(out, />Переместить сюда<\/button>/);
  assert.doesNotMatch(out, /Переложить/);
});

// ---- 5. the result of a folder's «+» ----

function addLines(over = {}) {
  return {
    note: "", elsewhere: null, toInbox: false, busy: false, error: "",
    onMoveHere: noop, onOpenSubject: noop, onOpenInbox: noop, onRetry: noop, onRefresh: noop, ...over,
  };
}

test("the result of a folder's «+» is drawn inside that folder's section, under its header", () => {
  const extra = (id) => (id === "f1" ? h("p", { className: "marker" }, "итог") : null);
  const out = html(h(SubjectDetail, { subject: subject(), materials: [], folderForm: false, busy: false, error: "", onCreateFolder: noop, onCancelFolder: noop, onAddFile: noop, folderExtra: extra }));
  const start = out.indexOf('id="folder-f1"');
  const end = out.indexOf("<section", start + 1);
  const mark = out.indexOf('class="marker"');
  assert.ok(start >= 0 && mark > start && (end < 0 || mark < end), "inside the section of «Лекции»");
  assert.ok(mark > out.indexOf("Добавить файл в папку «Лекции»"), "after its header");
  assert.equal((out.match(/class="marker"/g) ?? []).length, 1);
});

// ---- 6. the file that did not land ----

test("a file that did not land: a warning line with the reason, «Разложить» and «Перейти во «Входящие»»", () => {
  const stored = { cancelled: false, existing: false, notPlaced: true, material: material() };
  const outcome = importOutcome(stored, { subjectId: "s1", folderId: "f1" }, [subject()]);
  assert.equal(outcome.note, "Файл добавлен во «Входящие»: положить его в «Лекции» не получилось.");
  assert.deepEqual(outcome.unplaced, { material: material(), subjectId: "s1", folderId: "f1" });
  const placing = { subjects: [subject()], place: async () => "", createSubject: create };
  const out = html(h(AddFileLines, addLines({ note: outcome.note, toInbox: true, unplaced: outcome.unplaced, placing })));
  assert.match(out, /class="result s-wait"/, "a warning, not the green success line");
  assert.doesNotMatch(out, /s-ok/);
  assert.match(out, />Разложить<\/button>/);
  assert.match(out, />Перейти во «Входящие»<\/button>/);
  // The panel opens on a click (state), so the preselect it starts from is the target.
  assert.deepEqual(panelChoice([subject()], outcome.unplaced.subjectId, outcome.unplaced.folderId), { subjectId: "s1", folderId: "f1" });
  // «Материалы» when the button gave no folder: the resolved one is carried.
  assert.equal(importOutcome(stored, { subjectId: "s1", folderId: "" }, [subject()]).unplaced.folderId, "f4");
  // Without the means to place there is only the way to «Входящие».
  assert.doesNotMatch(html(h(AddFileLines, addLines({ note: outcome.note, toInbox: true }))), />Разложить</);
});

// ---- minors ----

test("the inbox footer tells how to place a file and the grip has a tooltip", () => {
  // The cards are gone (TASK-341): the file is dragged onto a subject of the tree.
  assert.match(texts.inboxHint, /Разложить можно кнопкой справа или перетащив значок файла на предмет слева\.$/);
  assert.equal(fileTexts.dragTitle, "Перетащите на предмет слева");
});

test("the tooltips of «Добавить файл» say where the file goes", () => {
  assert.equal(fileTexts.addToInboxTip, "Файл попадёт во «Входящие»");
  assert.equal(fileTexts.addToMaterialsTip, "Файл попадёт в «Материалы»");
});
