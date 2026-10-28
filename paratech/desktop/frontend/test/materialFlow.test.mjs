// TASK-339: the behaviour behind the file rows with a fake api and promises the test resolves: the hints of «Входящие»
// one request at a time, the create-then-place of a hint, the undo lines (the double «Вернуть», the ten seconds), the drop
// of a row on a card, «Добавить файл» into a subject.
import { test } from "node:test";
import assert from "node:assert/strict";
import { genericError } from "../.test-build/bindings/library.js";
import { addFileInto, CardDrop, HintScheduler, MaterialFlow } from "../.test-build/materialFlow.js";
import { hintLimit, undoMs } from "../.test-build/libraryInboxView.js";

const tick = () => new Promise((r) => setImmediate(r));

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
    id: "m1", name: "лаба1.pdf", type: "pdf", sizeBytes: 1, subjectId: "", folderId: "", inbox: true, removed: false,
    textStatus: "", textNote: "", createdAt: "", ...over,
  };
}

const hintFor = (id) => ({ kind: "existing", subjectId: "s1", subjectName: "Сети", folderId: "f3", folderName: "Лабораторные", folderKind: "labs", createName: id, createAliases: [] });
const createHint = { kind: "create", subjectId: "", subjectName: "", folderId: "", folderName: "Лабораторные", folderKind: "labs", createName: "Сети", createAliases: ["Сети (лаб.)"] };

// ---- the hints ----

/** An api whose answers the test gives: ask(id) is pending until answer(id, hint) or fail(id). */
function hintApi() {
  const calls = [];
  const waiting = new Map();
  return {
    calls,
    placementHint(id) {
      calls.push(id);
      return new Promise((resolve, reject) => waiting.set(id, { resolve, reject }));
    },
    async answer(id, hint = hintFor(id)) {
      waiting.get(id).resolve(hint);
      waiting.delete(id);
      await tick();
    },
    async fail(id) {
      waiting.get(id).reject(new Error("boom"));
      waiting.delete(id);
      await tick();
    },
  };
}

const files = (n) => Array.from({ length: n }, (_, i) => material({ id: `m${i}` }));

function scheduler(limit) {
  const api = hintApi();
  const snaps = [];
  const s = new HintScheduler(api, (x) => snaps.push(x), limit);
  return { api, snaps, s, last: () => snaps.at(-1) };
}

test("hints are asked one at a time, in the order of the list", async () => {
  const { api, s, last } = scheduler();
  s.update({ key: "a", inbox: files(3), on: true });
  assert.deepEqual(api.calls, ["m0"]);
  assert.deepEqual([...last().pending], ["m0", "m1", "m2"]);
  await tick();
  assert.deepEqual(api.calls, ["m0"], "the next one waits for the answer");
  await api.answer("m0");
  assert.deepEqual(api.calls, ["m0", "m1"]);
  assert.equal(last().hints.get("m0").subjectName, "Сети");
  assert.deepEqual([...last().pending], ["m1", "m2"]);
  await api.answer("m1");
  await api.answer("m2");
  assert.deepEqual(api.calls, ["m0", "m1", "m2"]);
  assert.deepEqual([...last().hints.keys()], ["m0", "m1", "m2"]);
  assert.equal(last().pending.size, 0);
});

test("at most hintLimit files are asked, and a file with a hint is not asked again", async () => {
  const { api, s, last } = scheduler();
  s.update({ key: "a", inbox: files(30), on: true });
  for (let i = 0; i < hintLimit; i++) {
    await api.answer(api.calls.at(-1));
    s.update({ key: "a", inbox: files(30), on: true }); // the page renders again meanwhile
  }
  await tick();
  assert.equal(api.calls.length, hintLimit);
  assert.equal(new Set(api.calls).size, hintLimit);
  assert.equal(last().hints.size, hintLimit);
  assert.equal(last().pending.size, 0);
});

test("an answer is ignored when the subjects changed meanwhile; the file is asked again for the new ones", async () => {
  const { api, s, last } = scheduler();
  s.update({ key: "a", inbox: files(2), on: true });
  s.update({ key: "b", inbox: files(2), on: true });
  assert.deepEqual(api.calls, ["m0"], "still one request at a time");
  await api.answer("m0", hintFor("old"));
  assert.equal(last().hints.size, 0, "the hint of the old subjects is not kept");
  assert.deepEqual(api.calls, ["m0", "m0"]);
  await api.answer("m0", hintFor("new"));
  assert.equal(last().hints.get("m0").createName, "new");
  assert.deepEqual(api.calls, ["m0", "m0", "m1"]);
});

test("an answer for a file that left «Входящие» is ignored, one for a file that stayed is kept", async () => {
  const { api, s, last } = scheduler();
  s.update({ key: "a", inbox: files(3), on: true });
  s.update({ key: "a", inbox: files(3).slice(1), on: true }); // m0 was placed meanwhile
  await api.answer("m0");
  assert.equal(last().hints.has("m0"), false);
  assert.deepEqual(api.calls, ["m0", "m1"]);
  s.update({ key: "a", inbox: [material({ id: "m9" }), ...files(3).slice(1)], on: true }); // a new file came on top
  await api.answer("m1");
  assert.equal(last().hints.has("m1"), true);
  assert.deepEqual(api.calls.slice(2), ["m9"], "the new file follows");
});

test("a failed hint is silent and gives a row no hint; nothing is asked when off, and nothing after dispose", async () => {
  const { api, s, last, snaps } = scheduler();
  s.update({ key: "a", inbox: files(2), on: false });
  assert.deepEqual(api.calls, []);
  assert.equal(snaps.length, 0, "no change, no notice");
  s.update({ key: "a", inbox: files(2), on: true });
  await api.fail("m0");
  assert.equal(last().hints.get("m0").kind, "none");
  assert.deepEqual(api.calls, ["m0", "m1"]);
  s.update({ key: "a", inbox: files(2), on: false });
  assert.equal(last().pending.size, 0);
  s.update({ key: "a", inbox: files(2), on: true });
  const seen = snaps.length;
  s.dispose();
  await api.answer("m1");
  assert.equal(snaps.length, seen, "an answer after dispose is dropped");
  assert.deepEqual(api.calls, ["m0", "m1"]);
  s.attach();
  s.update({ key: "a", inbox: files(3), on: true });
  assert.deepEqual(api.calls, ["m0", "m1", "m1"], "after attach the unanswered file is asked again");
});

test("the same inputs again notify nobody", async () => {
  const { api, s, snaps } = scheduler();
  s.update({ key: "a", inbox: files(1), on: true });
  const n = snaps.length;
  s.update({ key: "a", inbox: files(1), on: true });
  s.update({ key: "a", inbox: files(1), on: true });
  assert.equal(snaps.length, n);
  await api.answer("m0");
  assert.equal(snaps.length, n + 1);
});

// ---- the placing, the hint, the undo lines ----

function flowWith(over = {}) {
  const subjects = over.subjects ?? [subject()];
  const log = { created: [], placed: [], moved: [], removed: [], restored: [], refreshed: 0, added: [] };
  const where = (id, subjectId, folderId) => material({ id, subjectId, folderId, inbox: subjectId === "" });
  const deps = {
    api: {
      createSubject: async (name, semester, aliases) => {
        log.created.push([name, semester, aliases]);
        if (over.createFails) {
          throw new Error(over.createFails);
        }
        return subject({ id: "s-new", name, aliases: [...aliases], folders: over.newFolders ?? subject().folders });
      },
    },
    inboxApi: {
      place: async (id, subjectId, folderId) => {
        log.placed.push([id, subjectId, folderId]);
        if (over.placeFails?.()) {
          throw new Error(over.placeFails());
        }
        await over.placeGate;
        return where(id, subjectId, folderId);
      },
      moveToInbox: async (id) => {
        log.moved.push(id);
        await over.undoGate;
        if (over.undoFails) {
          throw new Error(over.undoFails);
        }
        return where(id, "", "");
      },
      removeMaterial: async (id) => {
        log.removed.push(id);
        return material({ id, removed: true });
      },
      restoreMaterial: async (id) => {
        log.restored.push(id);
        return material({ id });
      },
    },
    subjects: () => subjects,
    addSubject: (s) => {
      log.added.push(s.id);
      subjects.push(s);
    },
    refresh: () => (log.refreshed += 1),
    now: over.now,
    timers: over.timers,
  };
  return { flow: new MaterialFlow(deps), log, subjects };
}

test("a «create» hint creates the subject with the hint's aliases, then places into the folder of its kind", async () => {
  const { flow, log } = flowWith({ subjects: [] });
  assert.equal(await flow.placeHint(material(), createHint), "");
  assert.deepEqual(log.created, [["Сети", "", ["Сети (лаб.)"]]], "name and aliases of the hint, the current semester");
  assert.deepEqual(log.placed, [["m1", "s-new", "f3"]], "the folder whose system is the hint's folderKind");
  assert.deepEqual(log.added, ["s-new"]);
  assert.equal(flow.getState().undo[0].text, "Создан предмет «Сети», файл положен в «Лабораторные».");
  assert.deepEqual(flow.getState().undo[0].back, { kind: "inbox", removeSubject: "s-new" }, "«Вернуть» removes the subject it made");
  assert.equal(log.refreshed, 1);
});

test("a «create» hint for a subject of that name already there reuses it and creates nothing", async () => {
  const { flow, log } = flowWith({ subjects: [subject({ id: "s7", folders: [{ id: "g3", name: "Лабораторные", system: "labs" }] })] });
  assert.equal(await flow.placeHint(material(), createHint), "");
  assert.deepEqual(log.created, []);
  assert.deepEqual(log.placed, [["m1", "s7", "g3"]]);
});

test("a repeat after a refused placing does not create the subject twice", async () => {
  let fail = true;
  const { flow, log } = flowWith({ subjects: [], placeFails: () => (fail ? "Папка не найдена: обновите список." : "") });
  assert.equal(await flow.placeHint(material(), createHint), "Папка не найдена: обновите список.");
  fail = false;
  assert.equal(await flow.placeHint(material(), createHint), "");
  assert.equal(log.created.length, 1);
  assert.equal(log.placed.length, 2);
});

test("the folder of the kind is missing: «Материалы» takes the file; no folder to take it is a plain refusal", async () => {
  const noLabs = flowWith({ subjects: [], newFolders: [{ id: "f4", name: "Материалы", system: "materials" }, { id: "f1", name: "Лекции", system: "lectures" }] });
  assert.equal(await noLabs.flow.placeHint(material(), createHint), "");
  assert.deepEqual(noLabs.log.placed, [["m1", "s-new", "f4"]]);
  const none = flowWith({ subjects: [], newFolders: [{ id: "f1", name: "Лекции", system: "lectures" }] });
  assert.equal(await none.flow.placeHint(material(), createHint), genericError);
  assert.deepEqual(none.log.placed, []);
  assert.equal(none.flow.getState().undo.length, 0);
});

test("a refused creation or an unknown hint places nothing and says why", async () => {
  const refused = flowWith({ subjects: [], createFails: "Предмет с таким названием уже есть." });
  assert.equal(await refused.flow.placeHint(material(), createHint), "Предмет с таким названием уже есть.");
  assert.deepEqual(refused.log.placed, []);
  const odd = flowWith({ subjects: [], createFails: "C:\\secret\\path" });
  assert.equal(await odd.flow.placeHint(material(), createHint), genericError, "a text that is not a known one never reaches the page");
  assert.equal(await odd.flow.placeHint(material(), { ...createHint, kind: "none" }), genericError);
  const existing = flowWith();
  assert.equal(await existing.flow.placeHint(material(), hintFor("x")), "");
  assert.deepEqual(existing.log.placed, [["m1", "s1", "f3"]]);
});

test("«Вернуть» takes the change back once, even when pressed twice at once", async () => {
  let open;
  const gate = new Promise((r) => (open = r));
  const { flow, log } = flowWith({ undoGate: gate });
  await flow.placeInto(material(), "s1", "f1");
  const it = flow.getState().undo[0];
  assert.deepEqual(it.back, { kind: "inbox" });
  const first = flow.undoOne(it);
  const second = flow.undoOne(it);
  assert.equal(flow.getState().undoBusy, true);
  open();
  await Promise.all([first, second]);
  assert.deepEqual(log.moved, ["m1"], "one call to the core");
  assert.equal(flow.getState().undoBusy, false);
  assert.equal(flow.getState().undo.length, 0, "the line is dropped");
  assert.equal(log.refreshed, 2, "the lists are read after the change and after the undo");
});

test("a refused «Вернуть» keeps its line with the reason and can be pressed again", async () => {
  const { flow, log } = flowWith({ undoFails: "Файл не найден: обновите список." });
  await flow.placeInto(material(), "s1", "f1");
  const it = flow.getState().undo[0];
  await flow.undoOne(it);
  assert.equal(flow.getState().undo[0].error, "Файл не найден: обновите список.");
  assert.equal(flow.getState().undoBusy, false);
  await flow.undoOne(flow.getState().undo[0]);
  assert.deepEqual(log.moved, ["m1", "m1"]);
  flow.refreshUndo();
  assert.equal(flow.getState().undo.length, 0, "«Обновить список» ends a line that failed");
});

test("the way back by kind: delete restores, a placed file goes back where it was", async () => {
  const { flow, log } = flowWith();
  const placed = material({ id: "m2", subjectId: "s2", folderId: "g2", inbox: false });
  assert.equal(await flow.remove(placed), "");
  assert.equal(flow.getState().undo[0].text, "Удалено «лаба1.pdf».");
  await flow.undoOne(flow.getState().undo[0]);
  assert.deepEqual(log.restored, ["m2"]);
  // «Переместить сюда» of a file that lies in another subject: «Вернуть» puts it back there.
  assert.equal(await flow.placeInto(placed, "s1", "f3"), "");
  assert.equal(flow.getState().undo[0].text, "Положено в «Сети → Лабораторные».");
  await flow.undoOne(flow.getState().undo[0]);
  assert.deepEqual(log.placed, [["m2", "s1", "f3"], ["m2", "s2", "g2"]]);
});

test("the undo lines go after their own ten seconds and the timer stops with dispose", async () => {
  let now = 1000;
  const timers = [];
  const fake = {
    set: (fn, ms) => {
      const t = { fn, ms, live: true };
      timers.push(t);
      return t;
    },
    clear: (t) => (t.live = false),
  };
  const { flow } = flowWith({ now: () => now, timers: fake });
  await flow.remove(material({ id: "a" }));
  assert.equal(timers.at(-1).ms, undoMs);
  now = 4000;
  await flow.remove(material({ id: "b" }));
  assert.equal(timers.filter((t) => t.live).length, 1, "one timer, for the nearest line");
  assert.equal(timers.at(-1).ms, undoMs - 3000);
  assert.equal(flow.getState().undo.length, 2);
  now = 1000 + undoMs;
  timers.at(-1).fn();
  assert.deepEqual(flow.getState().undo.map((x) => x.materialId), ["b"]);
  const last = timers.at(-1);
  assert.equal(last.ms, 3000);
  flow.dispose();
  assert.equal(last.live, false);
  flow.attach();
  assert.equal(timers.at(-1).live, true, "StrictMode mounts again");
});

test("the state changes notify the subscribers and stay the same object between changes", async () => {
  const { flow } = flowWith();
  let n = 0;
  const off = flow.subscribe(() => (n += 1));
  const before = flow.getState();
  assert.equal(flow.getState(), before);
  await flow.remove(material());
  assert.ok(n > 0);
  assert.notEqual(flow.getState(), before);
  off();
  const seen = n;
  await flow.remove(material({ id: "other" }));
  assert.equal(n, seen);
});

// ---- the drop of a row on a card ----

function cardDrop(inbox) {
  const placed = [];
  const errors = [];
  const gates = [];
  const drop = new CardDrop({
    find: (id) => inbox.find((m) => m.id === id),
    place: (m, subjectId, folderId) => {
      placed.push([m.id, subjectId, folderId]);
      return new Promise((resolve) => gates.push(resolve));
    },
    setError: (t) => errors.push(t),
  });
  return { drop, placed, errors, gates };
}

test("a row dropped on a card is placed into «Материалы» once, however many drops arrive meanwhile", async () => {
  const { drop, placed, errors, gates } = cardDrop([material({ id: "m1" }), material({ id: "m2" })]);
  const first = drop.drop("s1", "m1");
  void drop.drop("s1", "m1");
  void drop.drop("s2", "m2");
  assert.deepEqual(placed, [["m1", "s1", ""]]);
  gates[0]("");
  await first;
  assert.deepEqual(errors, ["", ""]);
  const again = drop.drop("s2", "m2");
  assert.deepEqual(placed.at(-1), ["m2", "s2", ""], "free again after the first finished");
  gates[1]("");
  await again;
});

test("a drop of a file the page does not know does nothing; «Повторить» drops the last one again", async () => {
  const { drop, placed, errors, gates } = cardDrop([material({ id: "m1" })]);
  await drop.drop("s1", "ghost");
  assert.deepEqual(placed, []);
  assert.deepEqual(errors, []);
  await drop.again();
  assert.deepEqual(placed, []);
  const first = drop.drop("s1", "m1");
  gates[0]("Предмет не найден: обновите список.");
  await first;
  assert.equal(errors.at(-1), "Предмет не найден: обновите список.");
  const retry = drop.again();
  assert.deepEqual(placed.at(-1), ["m1", "s1", ""]);
  gates[1]("");
  await retry;
  assert.equal(errors.at(-1), "");
});

// ---- «Добавить файл» into a subject ----

test("«Добавить файл» asks the core for the subject and the folder; a cancelled dialog is silent", async () => {
  const calls = [];
  const reply = (r) => ({ importFileTo: async (...a) => (calls.push(a), r) });
  const subjects = [subject()];
  assert.deepEqual(await addFileInto(reply({ cancelled: true, existing: false, notPlaced: false, material: null }), "s1", "f1", subjects), { kind: "cancelled" });
  const done = await addFileInto(reply({ cancelled: false, existing: false, notPlaced: false, material: material({ inbox: false, subjectId: "s1", folderId: "f4" }) }), "s1", "", subjects);
  assert.deepEqual(calls, [["s1", "f1"], ["s1", ""]]);
  assert.equal(done.kind, "done");
  assert.equal(done.outcome.note, "Файл добавлен в «Сети → Материалы».");
  assert.deepEqual(done.target, { subjectId: "s1", folderId: "f4" }, "the folder is resolved for «Переместить сюда»");
});

test("a refused «Добавить файл» carries the core's text, or a generic one for any other", async () => {
  const refuse = (message) => ({ importFileTo: async () => { throw new Error(message); } });
  for (const text of ["Папка не найдена: обновите список.", "Файл больше 100 МБ.", "Не удалось прочитать файл."]) {
    assert.deepEqual(await addFileInto(refuse(text), "s1", "f1", []), { kind: "refused", text });
  }
  assert.deepEqual(await addFileInto(refuse("open C:\\Users\\x\\a.pdf: denied"), "s1", "f1", []), { kind: "refused", text: genericError });
});

test("a file not placed arrives as «done» with the way to «Входящие»", async () => {
  const api = { importFileTo: async () => ({ cancelled: false, existing: false, notPlaced: true, material: material() }) };
  const r = await addFileInto(api, "s1", "f1", [subject()]);
  assert.equal(r.kind, "done");
  assert.equal(r.outcome.toInbox, true);
  assert.match(r.outcome.note, /положить его в «Лекции» не получилось/);
  assert.equal(r.outcome.unplaced.material.id, "m1", "the line can place the file itself");
});
