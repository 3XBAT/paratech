// The writing cards of a turnkey work (TASK-132): the binding, the card of each step, the plan editor, polling and
// plain-text rendering. Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { mock, test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorTexts, genericError, methods, notFoundText, parseWork, turnkeyApi, turnkeyErrorText } from "../.test-build/bindings/turnkey.js";
import { parseCard, parseUpdate } from "../.test-build/bindings/agent.js";
import { entryModel } from "../.test-build/agentView.js";
import {
  acceptWork,
  addItem,
  approvePlan,
  blockedNumber,
  checkEditor,
  canMove,
  canRemove,
  canSubmitNote,
  editorMode,
  openTalk,
  reviseCaption,
  reviseWork,
  sectionTexts,
  talkTarget,
  regenerateBlocked,
  rewriteWork,
  savePlanAt,
  staleText,
  notStoppedText,
  canStop,
  openLabel,
  openTarget,
  openWork,
  openPdf,
  pdfOffered,
  pdfTarget,
  exportPdf,
  stopWork,
  isStopped,
  singleFlight,
  startPolling,
  editorFromSections,
  isEdited,
  isRunning,
  limits,
  moveItem,
  nextStopping,
  noteOf,
  nextActionError,
  retryWriting,
  pollDelay,
  pollMs,
  progressLine,
  removeItem,
  retryMs,
  reviseOptions,
  toOutlineItems,
  workCardModel,
  workTexts,
} from "../.test-build/workView.js";
import { WorkBody } from "../.test-build/agent/WorkCard.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

function section(over) {
  return {
    number: 1, title: "Введение", words: 300, note: "", written: false, paragraphs: [], sources: [],
    kind: "", part: "", bullets: [], columns: [], ...over,
  };
}

function writing(over) {
  return {
    step: "none", generation: 1, topic: "Тема работы", sections: [], done: 0, total: 0, subjectId: "", materialId: "",
    result: "", job: "", presentation: false, template: "", questions: [], talkMaterialId: "", talkResult: "",
    lab: false, manual: "", placeholders: [], ...over,
  };
}

function work(over) {
  return { id: "w1", status: "writing", version: 1, writing: null, problem: "", message: "", ...over };
}

// The Go reply has more fields than the page reads; extra fields are ignored.
function raw(over) {
  return { ...work(over), assignmentId: "a", materialId: "", round: 1, questions: [], hasCard: true, edited: [] };
}

const evil = '<img src=x onerror=alert(1)> [x](http://evil.example/a) <a href="http://evil.example">z</a>';

function render(w, api = fakeApi()) {
  return renderToStaticMarkup(h(WorkBody, { work: w, api, onWork: noop }));
}

function fakeApi(log = []) {
  const reply = async (...args) => {
    log.push(args);
    return work();
  };
  return {
    work: reply,
    startWriting: reply,
    approveOutline: reply,
    editOutline: reply,
    regenerate: reply,
  };
}

// ---- Binding ----

test("bound method names match the Go TurnkeyService", () => {
  const src = readFileSync(new URL("../../internal/input/wails/turnkey.go", import.meta.url), "utf8");
  for (const [key, name] of Object.entries(methods)) {
    const method = name.slice(name.lastIndexOf(".") + 1);
    assert.match(src, new RegExp(`func \\(s \\*TurnkeyService\\) ${method}\\(`), key);
  }
  assert.ok(Object.values(methods).every((m) => m.startsWith("paratech/desktop/internal/input/wails.TurnkeyService.")));
});

test("parseWork reads a well-formed work and ignores the fields the cards do not use", () => {
  const w = parseWork(
    raw({
      writing: writing({
        step: "approve",
        sections: [section({ paragraphs: ["Абзац"], sources: [{ text: "Источник", materialId: "", verify: true }] })],
      }),
    }),
  );
  assert.equal(w.id, "w1");
  assert.equal(w.writing.step, "approve");
  assert.equal(w.writing.sections[0].sources[0].verify, true);
  assert.equal(parseWork(raw()).writing, null);
});

test("parseWork tolerates null lists", () => {
  const w = parseWork(raw({ writing: writing({ sections: null }) }));
  assert.deepEqual(w.writing.sections, []);
  const s = parseWork(raw({ writing: writing({ sections: [section({ paragraphs: null, sources: null })] }) }));
  assert.deepEqual(s.writing.sections[0].paragraphs, []);
  assert.deepEqual(s.writing.sections[0].sources, []);
});

test("parseWork rejects unknown or malformed shapes", () => {
  assert.throws(() => parseWork(null));
  assert.throws(() => parseWork([]));
  assert.throws(() => parseWork("w1"));
  assert.throws(() => parseWork(raw({ status: "lost" })));
  assert.throws(() => parseWork(raw({ version: "1" })));
  assert.throws(() => parseWork(raw({ version: -1 })));
  assert.throws(() => parseWork(raw({ message: 5 })));
  assert.throws(() => parseWork(raw({ writing: writing({ step: "flying" }) })));
  assert.throws(() => parseWork(raw({ writing: writing({ done: 1.5 }) })));
  assert.throws(() => parseWork(raw({ writing: writing({ sections: "x" }) })));
  assert.throws(() => parseWork(raw({ writing: writing({ sections: [section({ title: 7 })] }) })));
  assert.throws(() => parseWork(raw({ writing: writing({ sections: [section({ paragraphs: [1] })] }) })));
  assert.throws(() => parseWork(raw({ writing: writing({ sections: [section({ sources: [{ text: "a", materialId: "" }] })] }) })));
  assert.throws(() => parseWork(raw({ writing: "oops" })));
});

test("the API calls the bound names with the ids and the items, and parses the reply", async () => {
  const calls = [];
  const api = turnkeyApi(async (...args) => {
    calls.push(args);
    return raw();
  });
  await api.work("w1");
  await api.startWriting("w1");
  await api.approveOutline("w1");
  await api.editOutline("w1", [{ number: 0, title: "Новый", words: 100, extra: "x" }]);
  await api.regenerate("w1", 2, "короче");
  assert.deepEqual(calls, [
    [methods.work, "w1"],
    [methods.startWriting, "w1"],
    [methods.approveOutline, "w1"],
    [methods.editOutline, "w1", [{ number: 0, title: "Новый", words: 100 }]],
    [methods.regenerate, "w1", 2, "короче"],
  ]);
  const bad = turnkeyApi(async () => ({ id: 1 }));
  await assert.rejects(bad.work("w1"));
});

test("errors: the fixed texts of the core are shown as they are, anything else is generic", () => {
  for (const t of errorTexts) {
    assert.equal(turnkeyErrorText(new Error(t)), t);
    assert.equal(turnkeyErrorText(t), t);
  }
  assert.equal(turnkeyErrorText(new Error("sql: no rows")), genericError);
  assert.equal(turnkeyErrorText(undefined), genericError);
  const src = readFileSync(new URL("../../internal/input/wails/turnkey.go", import.meta.url), "utf8");
  for (const t of errorTexts) {
    assert.ok(src.includes(`"${t}"`), t);
  }
});

// ---- The card in the panel ----

test("a work card reaches the panel with its workId; an older core without the field has no card", () => {
  const base = { plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "", stopReason: "" };
  const c = parseCard({ ...base, kind: "work", workId: "w1", outcome: "confirmed" });
  assert.equal(c.workId, "w1");
  const u = { threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "", problem: "", cards: [c, parseCard({ ...base, kind: "work" })] };
  const m = entryModel({ key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "", update: parseUpdate(u) });
  assert.deepEqual(m.cards, [{ type: "work", workId: "w1" }]);
});

test("without the turnkey calls a work card shows nothing", () => {
  const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
  const out = renderToStaticMarkup(h(AgentRequest, { model: { key: "k", userText: "", callLine: "", waiting: false, cards: [{ type: "work", workId: "w1" }] }, actions }));
  assert.ok(!out.includes("ag-card"));
});

// ---- Card by step ----

test("a confirmed work without writing offers «Написать работу»; other statuses show nothing", () => {
  const m = workCardModel(work({ status: "confirmed" }));
  assert.equal(m.type, "start");
  assert.equal(m.button, "Написать работу");
  for (const status of ["triage", "questions", "review", "helper", "failed", "writing", "done"]) {
    assert.deepEqual(workCardModel(work({ status })), { type: "none" }, status);
  }
  assert.match(render(work({ status: "confirmed" })), /Написать работу/);
  assert.equal(render(work({ status: "review" })), "");
  assert.equal(render(work({ status: "confirmed", writing: writing({ step: "none", result: "" }) })), "");
});

test("progress: «Раздел N из M» counts the section being written and never exceeds M", () => {
  assert.equal(progressLine(writing({ step: "section", done: 0, total: 5 })), "Раздел 1 из 5");
  assert.equal(progressLine(writing({ step: "section", done: 2, total: 5 })), "Раздел 3 из 5");
  assert.equal(progressLine(writing({ step: "section", done: 5, total: 5 })), "Раздел 5 из 5");
  assert.equal(progressLine(writing({ step: "section", done: 9, total: 5 })), "Раздел 5 из 5");
  assert.equal(progressLine(writing({ step: "assemble", done: 5, total: 5 })), "Раздел 5 из 5");
  assert.equal(progressLine(writing({ step: "outline", done: 0, total: 0 })), workTexts.planning);
  assert.equal(progressLine(writing({ step: "section", done: 0, total: 0 })), workTexts.planning);
  const out = render(work({ writing: writing({ step: "section", done: 1, total: 4 }) }));
  assert.match(out, /Раздел 2 из 4/);
  assert.match(out, /ag-dot/);
});

test("a running step does not show the material of the work as a result", () => {
  const sections = [section({ title: "Глава А", written: true, paragraphs: ["СЕКРЕТНЫЙ АБЗАЦ"], sources: [{ text: "СЕКРЕТНЫЙ ИСТОЧНИК", materialId: "", verify: true }] })];
  for (const step of ["outline", "section", "assemble"]) {
    const out = render(work({ writing: writing({ step, sections, done: 1, total: 1, materialId: "m1", result: "exports/old.docx" }) }));
    assert.ok(!out.includes("СЕКРЕТНЫЙ"), step);
    assert.ok(!out.includes("Глава А"), step);
    assert.ok(!out.includes(".docx"), step);
    assert.ok(!out.includes("<button"), step);
  }
});

test("approve: the editor with the sections, both buttons, the limits", () => {
  const w = work({ writing: writing({ step: "approve", sections: [section({ number: 1, title: "Введение", words: 300 }), section({ number: 2, title: "Основная часть", words: 1500 })] }) });
  assert.equal(workCardModel(w).caption, "План готов, утвердите");
  const out = render(w);
  assert.match(out, /План готов, утвердите/);
  assert.match(out, /value="Введение"/);
  assert.match(out, /value="Основная часть"/);
  assert.match(out, /value="1500"/);
  assert.match(out, /maxLength="150"/);
  assert.match(out, /Утвердить план/);
  assert.match(out, /Сохранить план/);
  assert.match(out, /Добавить раздел/);
  assert.match(out, /Убрать раздел/);
  assert.match(out, /Выше/);
  assert.match(out, /Ниже/);
  // Nothing edited: «Сохранить план» waits, «Утвердить план» does not.
  const save = /<button[^>]*disabled=""[^>]*>Сохранить план/.test(out);
  const approve = /<button[^>]*disabled=""[^>]*>Утвердить план/.test(out);
  assert.equal(save, true);
  assert.equal(approve, false);
});

test("approve with no sections does not crash and cannot be approved", () => {
  const out = render(work({ writing: writing({ step: "approve", sections: [] }) }));
  assert.match(out, /Нужно от 1 до 12 разделов/);
  assert.match(out, /<button[^>]*disabled=""[^>]*>Утвердить план/);
});

test("blocked: the text, the note field with its limit and «Сгенерировать заново»", () => {
  const out = render(work({ writing: writing({ step: "blocked", sections: [section({ written: true }), section({ number: 2 })] }) }));
  assert.match(out, /Не удалось написать раздел\. Можно сгенерировать заново\./);
  assert.match(out, /Дополнительные указания/);
  assert.match(out, /maxLength="1000"/);
  assert.match(out, /Сгенерировать заново/);
});

test("blocked outline (no sections) says so and offers to make the plan again", () => {
  const out = render(work({ writing: writing({ step: "blocked", sections: [] }) }));
  assert.match(out, /Не удалось составить план\. Можно составить заново\./);
  assert.ok(!out.includes("написать раздел"));
});

test("blocked regenerate sends 0 only without sections, else the first section not written", async () => {
  const log = [];
  const api = fakeApi(log);
  const blocked = (sections) => work({ writing: writing({ step: "blocked", sections }) });
  assert.equal(blockedNumber(blocked([]).writing), 0);
  await regenerateBlocked(api, blocked([]), "  план  ");
  const some = [section({ number: 1, written: true }), section({ number: 2, written: true }), section({ number: 3 }), section({ number: 4 })];
  await regenerateBlocked(api, blocked(some), "короче");
  await regenerateBlocked(api, blocked(some.slice(0, 2)), "");
  assert.deepEqual(log, [["w1", 0, "план"], ["w1", 3, "короче"], ["w1", 0, ""]]);
});

test("a presentation is offered «Написать работу» like any confirmed work (TASK-195)", () => {
  assert.equal(workCardModel(work({ status: "confirmed", workType: "presentation" })).type, "start");
  assert.equal(workCardModel(work({ status: "confirmed", workType: "referat" })).type, "start");
  assert.match(render(work({ status: "confirmed", workType: "presentation" })), /Написать работу/);
  assert.equal(parseWork({ ...raw(), card: { workType: "presentation" } }).workType, "presentation");
  assert.equal(parseWork(raw()).workType, "");
});

// ---- Steps after a plan edit ----

function callApi(log, over = {}) {
  const mk = (name, ret) => async (...args) => {
    log.push([name, ...args]);
    const r = over[name];
    if (r instanceof Error) {
      throw r;
    }
    return r ?? ret;
  };
  return {
    work: mk("work", work({ version: 9 })),
    startWriting: mk("startWriting", work({ version: 3 })),
    approveOutline: mk("approveOutline", work({ version: 3 })),
    editOutlineAt: mk("editOutlineAt", work({ version: 2 })),
    regenerate: mk("regenerate", work()),
  };
}

const planItems = [{ number: 1, title: " Введение ", words: "300" }];

test("approve: EditOutlineAt (with the version) when edited then ApproveOutline; only the final reply is returned", async () => {
  const log = [];
  const r = await approvePlan(callApi(log), "w1", 4, planItems, true);
  assert.deepEqual(log, [["editOutlineAt", "w1", 4, [{ number: 1, title: "Введение", words: 300 }]], ["approveOutline", "w1"]]);
  assert.equal(r.ok, true);
  assert.equal(r.work.version, 3);

  const plain = [];
  await approvePlan(callApi(plain), "w1", 4, planItems, false);
  assert.deepEqual(plain, [["approveOutline", "w1"]]);
});

test("rewrite: EditOutlineAt then StartWriting; a failing StartWriting keeps the error and re-reads the work", async () => {
  const log = [];
  const r = await rewriteWork(callApi(log, { startWriting: new Error(errorTexts[3]) }), "w1", 4, planItems, true);
  assert.deepEqual(log.map((c) => c[0]), ["editOutlineAt", "startWriting", "work"]);
  assert.equal(r.ok, false);
  assert.equal(turnkeyErrorText(r.error), errorTexts[3]);
  assert.equal(r.fresh.version, 9);
});

test("a failing EditOutlineAt stops the sequence and re-reads nothing", async () => {
  const log = [];
  const r = await rewriteWork(callApi(log, { editOutlineAt: new Error("x") }), "w1", 4, planItems, true);
  assert.deepEqual(log.map((c) => c[0]), ["editOutlineAt"]);
  assert.equal(r.ok, false);
  assert.equal(r.fresh, null);
});

test("a stale plan edit shows the core's text and re-reads the card (approve, rewrite, save)", async () => {
  for (const run of [
    (api) => approvePlan(api, "w1", 4, planItems, true),
    (api) => rewriteWork(api, "w1", 4, planItems, true),
    (api) => savePlanAt(api, "w1", 4, planItems),
  ]) {
    const log = [];
    const r = await run(callApi(log, { editOutlineAt: new Error(staleText) }));
    assert.deepEqual(log.map((c) => c[0]), ["editOutlineAt", "work"]);
    assert.equal(r.ok, false);
    assert.equal(turnkeyErrorText(r.error), "Работа изменилась: обновите карточку.");
    assert.equal(r.fresh.version, 9);
  }
});

test("save plan: EditOutlineAt with the shown version; the saved work is the result", async () => {
  const log = [];
  const r = await savePlanAt(callApi(log), "w1", 4, planItems);
  assert.deepEqual(log, [["editOutlineAt", "w1", 4, [{ number: 1, title: "Введение", words: 300 }]]]);
  assert.equal(r.ok, true);
  assert.equal(r.work.version, 2);
});

test("an unreadable work after a failed step gives no fresh work", async () => {
  const log = [];
  const r = await approvePlan(callApi(log, { approveOutline: new Error("x"), work: new Error("y") }), "w1", 4, planItems, true);
  assert.equal(r.ok, false);
  assert.equal(r.fresh, null);
});

test("single flight: a second call while one runs is dropped", async () => {
  const flight = singleFlight();
  let release;
  let calls = 0;
  const first = flight(() => new Promise((res) => { calls++; release = res; }));
  assert.equal(await flight(async () => { calls++; }), false);
  release();
  assert.equal(await first, true);
  assert.equal(await flight(async () => { calls++; }), true);
  assert.equal(calls, 2);
  await assert.rejects(flight(async () => { throw new Error("x"); }));
  assert.equal(await flight(async () => undefined), true);
});

test("done: the file line, the notice, the sections as details, the marker of a source to verify", () => {
  const sections = [
    section({ number: 1, title: "Введение", written: true, paragraphs: ["Первый абзац", "Второй абзац"], sources: [{ text: "Иванов И. Книга", materialId: "m1", verify: false }, { text: "Петров П. Статья", materialId: "", verify: true }] }),
    section({ number: 2, title: "Пустой", written: false }),
  ];
  const w = work({ status: "done", writing: writing({ step: "none", result: "exports/work.docx", topic: "Тема про Х", sections }) });
  const m = workCardModel(w);
  assert.equal(m.type, "done");
  assert.deepEqual(m.file, { kind: ".docx", name: "Тема про Х" });
  assert.equal(m.notice, "Сгенерировано — прочитайте перед сдачей");
  assert.equal(m.sections.length, 1);
  const out = render(w);
  assert.match(out, /\.docx/);
  assert.match(out, /Тема про Х/);
  assert.match(out, /Сгенерировано — прочитайте перед сдачей/);
  assert.match(out, /<details[^>]*><summary>1\. Введение<\/summary>/);
  assert.match(out, /Первый абзац/);
  assert.equal(out.split("проверить источник").length - 1, 1);
  assert.match(out, /Петров П\. Статья<span[^>]*> · проверить источник/);
  assert.match(out, /Доработать/);
  assert.match(out, /Изменить план/);
  assert.ok(!/Открыть в Word|Остановить|Отправить преподавателю/.test(out));
  assert.equal(workCardModel(work({ writing: writing({ step: "none", result: "x", topic: "  " }) })).file.name, workTexts.fallbackName);
});

// ---- Plan editor ----

test("the items sent to EditOutline keep the order and send number 0 for a new section", () => {
  const sections = [section({ number: 1, title: "A", words: 100 }), section({ number: 2, title: "B", words: 200 }), section({ number: 3, title: "C", words: 300 })];
  let items = editorFromSections(sections);
  assert.equal(isEdited(items, sections), false);

  items = moveItem(items, 2, -1);
  assert.deepEqual(items.map((i) => i.title), ["A", "C", "B"]);
  assert.equal(isEdited(items, sections), true);

  items = addItem(removeItem(items, 0));
  items[2] = { ...items[2], title: "  Новый ", words: "250" };
  assert.deepEqual(toOutlineItems(items), [
    { number: 3, title: "C", words: 300 },
    { number: 2, title: "B", words: 200 },
    { number: 0, title: "Новый", words: 250 },
  ]);
});

test("moving at the edge and adding past the limit change nothing", () => {
  const items = editorFromSections([section({ number: 1 }), section({ number: 2, title: "B" })]);
  assert.deepEqual(moveItem(items, 0, -1), items);
  assert.deepEqual(moveItem(items, 1, 1), items);
  assert.deepEqual(moveItem(items, 5, 1), items);
  let many = [];
  for (let i = 0; i < limits.maxSections + 3; i++) {
    many = addItem(many);
  }
  assert.equal(many.length, limits.maxSections);
});

test("the editor check follows the domain: 1..12 sections, a title of 150 runes, 50..3000 words, 15000 in total", () => {
  assert.deepEqual([limits.maxSections, limits.minWords, limits.maxWords, limits.totalWordsMax, limits.titleMax, limits.noteMax], [12, 50, 3000, 15000, 150, 1000]);
  const ok = [{ number: 1, title: "A", words: "100" }];
  assert.equal(checkEditor(ok).ok, true);
  assert.equal(checkEditor([]).ok, false);
  assert.equal(checkEditor(Array.from({ length: 13 }, () => ok[0])).ok, false);
  assert.equal(checkEditor(Array.from({ length: 12 }, () => ok[0])).ok, true);
  const empty = checkEditor([{ number: 1, title: "   ", words: "100" }]);
  assert.equal(empty.ok, false);
  assert.deepEqual(empty.badTitles, [true]);
  assert.equal(checkEditor([{ number: 1, title: "x".repeat(151), words: "100" }]).ok, false);
  assert.equal(checkEditor([{ number: 1, title: "x".repeat(150), words: "100" }]).ok, true);
  // 150 astral characters are 300 UTF-16 units but 150 runes.
  assert.equal(checkEditor([{ number: 1, title: "😀".repeat(150), words: "100" }]).ok, true);
  assert.equal(checkEditor([{ number: 1, title: "😀".repeat(151), words: "100" }]).ok, false);
  for (const words of ["0", "49", "3001", "20001", "1.5", "-3", "", "abc", "1e3", " "]) {
    const c = checkEditor([{ number: 1, title: "A", words }]);
    assert.equal(c.ok, false, words);
    assert.deepEqual(c.badWords, [true], words);
  }
  for (const words of ["50", "3000", " 50 "]) {
    assert.equal(checkEditor([{ number: 1, title: "A", words }]).ok, true, words);
  }
  const five = (words) => Array.from({ length: 5 }, () => ({ number: 1, title: "A", words }));
  assert.equal(checkEditor(five("3000")).ok, true);
  const over = checkEditor([...five("3000"), { number: 1, title: "A", words: "50" }]);
  assert.equal(over.ok, false);
  assert.match(over.problem, /15000/);
});

test("revise: the choice is every section or one section; the note is trimmed and cut", () => {
  const opts = reviseOptions([section({ number: 1, title: "A" }), section({ number: 4, title: "B" })]);
  assert.deepEqual(opts.map((o) => o.value), [0, 1, 4]);
  assert.equal(opts[0].label, "Вся работа");
  assert.deepEqual(opts.slice(1).map((o) => o.label), ["Раздел 1. A", "Раздел 4. B"]);
  assert.equal(noteOf("  короче  "), "короче");
  assert.equal(noteOf("x".repeat(1500)).length, 1000);
  assert.equal([...noteOf("😀".repeat(1200))].length, 1000);
});

// ---- Polling and stale replies ----

test("polling: while a step runs every 3 s, slowly after a failure, stops when nothing runs", () => {
  const running = work({ writing: writing({ step: "section" }) });
  assert.equal(isRunning(running), true);
  assert.equal(pollDelay(running, false), pollMs);
  assert.equal(pollDelay(running, true), retryMs);
  assert.ok(retryMs > pollMs);
  assert.equal(pollDelay(null, true), retryMs);
  assert.equal(pollDelay(null, false), pollMs);
  for (const step of ["approve", "blocked", "none"]) {
    const w = work({ writing: writing({ step }) });
    assert.equal(isRunning(w), false, step);
    assert.equal(pollDelay(w, false), null, step);
    assert.equal(pollDelay(w, true), null, step);
  }
  assert.equal(pollDelay(work({ status: "confirmed" }), false), null);
  assert.equal(isRunning(null), false);
});

// Lets the pending promises of a read settle.
const settle = () => new Promise((res) => setImmediate(res));

function poller(reads) {
  const seen = { works: [], errors: [], reads: 0 };
  let latest = null;
  const stop = startPolling({
    read: async () => {
      const r = reads[Math.min(seen.reads++, reads.length - 1)];
      if (r instanceof Error) {
        throw r;
      }
      return r;
    },
    onWork: (w) => {
      latest = w;
      seen.works.push(w);
    },
    onError: (t) => seen.errors.push(t),
    current: () => latest,
  });
  return { seen, stop };
}

test("poller: reads every 3 s while running, stops by itself once nothing runs", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const run = work({ writing: writing({ step: "section" }) });
  const done = work({ version: 2, writing: writing({ step: "none", result: "x" }) });
  const { seen } = poller([run, run, done]);
  await settle();
  assert.equal(seen.reads, 1);
  t.mock.timers.tick(pollMs - 1);
  await settle();
  assert.equal(seen.reads, 1);
  t.mock.timers.tick(1);
  await settle();
  assert.equal(seen.reads, 2);
  t.mock.timers.tick(pollMs);
  await settle();
  assert.equal(seen.reads, 3);
  t.mock.timers.tick(pollMs * 5);
  await settle();
  assert.equal(seen.reads, 3);
});

test("poller: a failed read retries slowly; stop cancels the next read", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const run = work({ writing: writing({ step: "section" }) });
  const { seen, stop } = poller([new Error("net"), run]);
  await settle();
  assert.deepEqual(seen.errors, [genericError]);
  t.mock.timers.tick(pollMs);
  await settle();
  assert.equal(seen.reads, 1);
  t.mock.timers.tick(retryMs - pollMs);
  await settle();
  assert.equal(seen.reads, 2);
  stop();
  t.mock.timers.tick(pollMs * 3);
  await settle();
  assert.equal(seen.reads, 2);
});

test("poller: «Работа не найдена» is shown and not retried", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { seen } = poller([new Error(notFoundText)]);
  await settle();
  assert.deepEqual(seen.errors, [notFoundText]);
  t.mock.timers.tick(retryMs * 4);
  await settle();
  assert.equal(seen.reads, 1);
});

test("an older reply never replaces a newer one", () => {
  const newer = work({ version: 5 });
  assert.equal(acceptWork(newer, work({ version: 4 })), newer);
  const same = work({ version: 5 });
  assert.equal(acceptWork(newer, same), same);
  const next = work({ version: 6 });
  assert.equal(acceptWork(newer, next), next);
  assert.equal(acceptWork(null, next), next);
});

// ---- Plain text ----

test("model text stays text in every step: markup, markdown and links are not built", () => {
  const sections = [
    section({ number: 1, title: evil, written: true, paragraphs: [evil], sources: [{ text: evil, materialId: "", verify: true }] }),
  ];
  const states = [
    work({ writing: writing({ step: "approve", topic: evil, sections }) }),
    work({ writing: writing({ step: "none", topic: evil, result: "exports/a.docx", sections }) }),
    work({ writing: writing({ step: "blocked", topic: evil, sections }) }),
    work({ writing: writing({ step: "section", topic: evil, sections, done: 0, total: 1 }) }),
  ];
  for (const w of states) {
    const out = render(w);
    assert.ok(!out.includes("<img"), w.writing.step);
    assert.ok(!/<a[ >]/.test(out), w.writing.step);
    // An input value holds the escaped text (`href=&quot;`), so only an element with a real href attribute is wrong.
    assert.ok(!/<[a-z]+ [^>]*href="http/.test(out.replace(/value="[^"]*"/g, "")), w.writing.step);
    assert.ok(!out.includes("onerror=alert(1)>"), w.writing.step);
  }
  const done = render(states[1]);
  assert.ok(done.includes("&lt;img src=x onerror=alert(1)&gt; [x](http://evil.example/a) &lt;a href=&quot;http://evil.example&quot;&gt;z&lt;/a&gt;"), done);
  const approve = render(states[0]);
  assert.ok(approve.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

test("the new sources use no HTML-writing API, links, browser storage or console", () => {
  for (const f of ["../src/agent/WorkCard.tsx", "../src/workView.ts", "../src/bindings/turnkey.ts"]) {
    const text = readFileSync(new URL(f, import.meta.url), "utf8");
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML", "href=", "window.open", "localStorage", "sessionStorage", "console.", "marked"]) {
      assert.ok(!text.includes(banned), `${f} has ${banned}`);
    }
  }
});

test("a failed call keeps its error across the re-mount: it is shown on a running step with «Повторить»", () => {
  const running = work({ writing: writing({ step: "section", generation: 2, done: 0, total: 3 }) });
  const out = renderToStaticMarkup(h(WorkBody, { work: running, api: fakeApi(), onWork: noop, error: "Не удалось запустить", onError: noop }));
  assert.match(out, /Не удалось запустить/);
  assert.match(out, /Повторить/);
  assert.match(out, /Раздел 1 из 3/);
  // Without an error a running step has no button.
  assert.ok(!render(running).includes("<button"));
});

test("the action error is cleared by a new call or a success, never by a poll", () => {
  assert.equal(nextActionError("", { type: "fail", text: "e" }), "e");
  assert.equal(nextActionError("e", { type: "poll" }), "e");
  assert.equal(nextActionError("e", { type: "start" }), "");
  assert.equal(nextActionError("e", { type: "ok" }), "");
  // EditOutline replies (the step changes), then StartWriting fails: the failure comes last and stays.
  let s = nextActionError("", { type: "start" });
  s = nextActionError(s, { type: "ok" });
  s = nextActionError(s, { type: "fail", text: "e" });
  s = nextActionError(s, { type: "poll" });
  assert.equal(s, "e");
});

test("«Повторить» calls StartWriting with the work id", async () => {
  const log = [];
  const api = { ...fakeApi(), startWriting: async (...a) => (log.push(a), work()) };
  await retryWriting(api, "w9");
  assert.deepEqual(log, [["w9"]]);
});

test("buttons without a binding are not offered", () => {
  const src = readFileSync(new URL("../src/workView.ts", import.meta.url), "utf8") + readFileSync(new URL("../src/agent/WorkCard.tsx", import.meta.url), "utf8");
  // «Открыть в Word» and «Остановить» have bindings since TASK-177/178 and are offered (TASK-191).
  for (const label of ["Отправить преподавателю"]) {
    assert.ok(!src.includes(label), label);
  }
});

// ---- «Остановить», «Открыть в Word / PowerPoint» (TASK-191) ----

const doneWriting = (over) => writing({ step: "none", result: "ok", materialId: "m1", ...over });
const library = { openMaterial: async () => ({ status: "opened", message: "Файл открывается в программе по умолчанию." }) };

function renderWith(w, extra = {}) {
  return renderToStaticMarkup(h(WorkBody, { work: w, api: fakeApi(), onWork: noop, ...extra }));
}

test("«Остановить» shows only while a job is active and is disabled while stopping", () => {
  const running = work({ writing: writing({ step: "section", total: 2, active: true }) });
  assert.equal(canStop(running), true);
  assert.ok(renderWith(running).includes("Остановить"));
  const stopping = renderWith(running, { stopping: true });
  assert.ok(stopping.includes("Остановка…"));
  assert.ok(!stopping.includes(">Остановить<"));
  assert.ok(/<button[^>]*disabled[^>]*>[^<]*(<[^>]*>)*[^<]*Остановка…/.test(stopping), stopping);
  const idle = work({ writing: writing({ step: "section", total: 2, active: false }) });
  assert.equal(canStop(idle), false);
  assert.ok(!renderWith(idle).includes("Остановить"));
});

test("a refused stop shows the core's text and does not offer «Повторить»", () => {
  const running = work({ writing: writing({ step: "section", total: 2, active: true }) });
  const out = renderWith(running, { error: notStoppedText, onError: noop });
  assert.ok(out.includes("Не удалось остановить написание. Попробуйте ещё раз."));
  assert.ok(!out.includes("Повторить"));
  assert.ok(out.includes("Остановить"));
  assert.ok(renderWith(running, { error: "Другая ошибка", onError: noop }).includes("Повторить"));
});

test("one poller serves a pending stop: it reads while active, backs off after a failure, ends once inactive", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const act = (active) => work({ version: 1, writing: writing({ step: "section", active }) });
  assert.equal(pollDelay(act(true), false), pollMs);
  assert.equal(pollDelay(act(true), true), retryMs);
  assert.equal(pollDelay(act(false), false), retryMs);
  const { seen } = poller([act(true), new Error("net"), act(true), act(false)]);
  await settle();
  assert.equal(seen.reads, 1);
  t.mock.timers.tick(pollMs);
  await settle();
  assert.equal(seen.reads, 2);
  t.mock.timers.tick(pollMs);
  await settle();
  assert.equal(seen.reads, 2, "after a failed read the wait is retryMs");
  t.mock.timers.tick(retryMs - pollMs);
  await settle();
  t.mock.timers.tick(pollMs);
  await settle();
  assert.equal(seen.reads, 4);
  t.mock.timers.tick(retryMs - 1);
  await settle();
  assert.equal(seen.reads, 4, "a stopped card is read only slowly");
  t.mock.timers.tick(1);
  await settle();
  assert.equal(seen.reads, 5, "a stopped card keeps a slow read-only poll");
});

test("«Остановка…» is dropped once the job is gone: stop, job ends, «Продолжить» shows «Остановить» again", () => {
  const act = (active) => work({ writing: writing({ step: "section", total: 2, active }) });
  let s = false;
  s = true; // «Остановить» pressed
  s = nextStopping(s, act(true));
  assert.equal(s, true, "still active: stopping");
  s = nextStopping(s, act(false));
  assert.equal(s, false, "job gone");
  s = nextStopping(s, act(true));
  assert.equal(s, false, "«Продолжить»: active again, not stopping");
  const out = renderWith(act(true), { stopping: s });
  assert.ok(out.includes("Остановить"));
  assert.ok(!out.includes("Остановка…"));
  assert.equal(nextStopping(true, null), false);
});

test("a writing step without a job is «Написание остановлено» with «Продолжить», no dot, slow read-only polling", () => {
  const stopped = work({ writing: writing({ step: "section", total: 3, done: 1, active: false }) });
  assert.equal(isStopped(stopped), true);
  assert.equal(isRunning(stopped), false);
  assert.equal(pollDelay(stopped, false), retryMs);
  assert.equal(pollDelay(stopped, true), retryMs);
  const m = workCardModel(stopped);
  assert.equal(m.type, "stopped");
  assert.equal(m.line, "Раздел 2 из 3");
  const out = render(stopped);
  assert.ok(out.includes("Написание остановлено"));
  assert.ok(out.includes("Продолжить"));
  assert.ok(!out.includes("ag-dot"));
  assert.ok(!out.includes("Остановить"));
  // A started job (active true) is running; the undefined flag is only a fixture case (parseWork always gives a boolean).
  assert.equal(workCardModel(work({ writing: writing({ step: "section", active: true }) })).type, "running");
  assert.equal(isStopped(work({ writing: writing({ step: "approve", active: false }) })), false);
});

test("«Продолжить» calls StartWriting with the work id", async () => {
  const log = [];
  const api = { ...fakeApi(), startWriting: async (...a) => (log.push(a), work()) };
  await retryWriting(api, "w3");
  assert.deepEqual(log, [["w3"]]);
});

test("stopWork: the id goes to the core; watch only while the job is still active; a refusal rejects", async () => {
  const log = [];
  const still = work({ writing: writing({ step: "section", active: true }) });
  const gone = work({ writing: writing({ step: "section", active: false }) });
  const mk = (r) => ({ stopWriting: async (...a) => (log.push(a), r instanceof Error ? Promise.reject(r) : r) });
  const a = await stopWork(mk(still), "w5");
  assert.deepEqual(log, [["w5"]]);
  assert.equal(a.watch, true);
  assert.equal(a.work, still);
  assert.equal((await stopWork(mk(gone), "w5")).watch, false);
  await assert.rejects(stopWork(mk(new Error(notStoppedText)), "w5"), (e) => turnkeyErrorText(e) === notStoppedText);
});

test("«Открыть в Word» for a ready work, «Открыть в PowerPoint» for a presentation; the copy caption is shown", () => {
  const paper = work({ workType: "referat", writing: doneWriting() });
  const slides = work({ workType: "presentation", writing: doneWriting() });
  assert.equal(openLabel(paper), "Открыть в Word");
  assert.equal(openLabel(slides), "Открыть в PowerPoint");
  const a = renderWith(paper, { library });
  assert.ok(a.includes("Открыть в Word"));
  assert.ok(a.includes("правки в ней в приложение не возвращаются"));
  assert.ok(renderWith(slides, { library }).includes("Открыть в PowerPoint"));
  // Not offered without a ready file or without the binding.
  assert.ok(!renderWith(work({ workType: "referat", writing: doneWriting({ result: "" }) }), { library }).includes("Открыть в"));
  assert.ok(!renderWith(paper).includes("Открыть в"));
});

test("opening sends the id only, once per call; a refusal is a fixed text", async () => {
  const calls = [];
  const lib = { openMaterial: async (...a) => (calls.push(a), { status: "refused", message: "Этот файл нельзя открыть отсюда." }) };
  const w = work({ workType: "lab_report", writing: doneWriting({ materialId: "m7" }) });
  assert.equal(openTarget(w), "m7");
  const v = await openWork(lib, w);
  assert.deepEqual(calls, [["m7"]]);
  assert.deepEqual(v, { kind: "fail", text: "Этот файл нельзя открыть отсюда." });
  const ok = await openWork(library, w);
  assert.equal(ok.kind, "ok");
  assert.equal(await openWork(lib, work({ writing: doneWriting({ result: "" }) })), null);
  assert.equal(calls.length, 1, "no file, no call");
});

test("every button call goes through the in-flight guard and the card keeps its exports", () => {
  const src = readFileSync(new URL("../src/agent/WorkCard.tsx", import.meta.url), "utf8");
  const calls = src.match(/onClick=\{\(\) => void [^(]*\(/g) ?? [];
  assert.ok(calls.length >= 8);
  assert.ok(calls.every((c) => c.includes("void run(")), calls.join("\n"));
  assert.match(src, /export function WorkWritingCard\(props: \{ api: TurnkeyApi; workId: string;/);
});

test("a double click on «Остановить» is one call", async () => {
  const flight = singleFlight();
  let calls = 0;
  let release;
  const stop = () => flight(() => new Promise((res) => { calls++; release = res; }));
  const first = stop();
  assert.equal(await stop(), false);
  release();
  await first;
  assert.equal(calls, 1);
});

// ---- Presentation and lab report (TASK-195) ----

const slide = (n, kind, over) => section({ number: n, title: `Слайд-${n}`, words: 60, kind, written: true, ...over });
const deck = (extra = {}) => [
  slide(1, "title"),
  ...Array.from({ length: 8 }, (_, i) => slide(i + 2, "bullets", extra)),
  slide(10, "conclusion"),
];
const talkQuestions = [{ question: "Почему так?", answer: "Потому что." }];

test("parseWork reads the fields of TASK-194 and takes a missing field as empty or false", () => {
  const full = parseWork(
    raw({
      writing: {
        ...writing({ step: "none" }),
        presentation: true, template: "plain", talkMaterialId: "t1", talkResult: "talk.docx", lab: true, manual: "not_read",
        questions: [{ question: "Q", answer: "A" }],
        placeholders: [{ section: 2, title: "Результаты", prompt: "Вставьте график" }],
        sections: [{ ...section({ kind: "two_columns", part: "results", bullets: ["b"], columns: [{ heading: "H", bullets: ["x"] }] }) }],
      },
    }),
  ).writing;
  assert.equal(full.presentation, true);
  assert.equal(full.talkMaterialId, "t1");
  assert.equal(full.manual, "not_read");
  assert.deepEqual(full.questions, [{ question: "Q", answer: "A" }]);
  assert.deepEqual(full.placeholders, [{ section: 2, title: "Результаты", prompt: "Вставьте график" }]);
  assert.equal(full.sections[0].kind, "two_columns");
  assert.equal(full.sections[0].part, "results");
  assert.deepEqual(full.sections[0].columns, [{ heading: "H", bullets: ["x"] }]);

  // An older core sends none of them.
  const old = { step: "none", generation: 1, topic: "T", sections: [{ number: 1, title: "A", words: 100, note: "", written: true, paragraphs: [], sources: [] }], done: 0, total: 0, subjectId: "", materialId: "", result: "", job: "" };
  const w = parseWork(raw({ writing: old })).writing;
  assert.deepEqual([w.presentation, w.lab, w.template, w.talkMaterialId, w.talkResult, w.manual], [false, false, "", "", "", ""]);
  assert.deepEqual([w.questions, w.placeholders], [[], []]);
  assert.deepEqual([w.sections[0].kind, w.sections[0].part, w.sections[0].bullets, w.sections[0].columns], ["", "", [], []]);
  // null lists and an empty manual are accepted.
  assert.equal(parseWork(raw({ writing: { ...old, questions: null, placeholders: null, manual: "" } })).writing.manual, "");

  for (const bad of [{ presentation: "yes" }, { manual: "weird" }, { questions: [{ question: 1, answer: "a" }] }, { placeholders: [{ prompt: 5 }] }, { talkMaterialId: 7 }]) {
    assert.throws(() => parseWork(raw({ writing: { ...old, ...bad } })), bad.toString());
  }
  for (const bad of [{ kind: "huge" }, { part: "x" }, { bullets: [1] }, { columns: [{ heading: 1, bullets: [] }] }, { columns: "x" }]) {
    assert.throws(() => parseWork(raw({ writing: { ...old, sections: [{ ...old.sections[0], ...bad }] } })), JSON.stringify(bad));
  }
});

test("progress of a presentation is «Слайд N из M»", () => {
  const w = (over) => writing({ step: "section", presentation: true, total: 9, done: 2, ...over });
  assert.equal(progressLine(w()), "Слайд 3 из 9");
  assert.equal(progressLine(w({ step: "assemble" })), "Слайд 9 из 9");
  assert.equal(progressLine(writing({ step: "section", total: 4, done: 0 })), "Раздел 1 из 4");
  assert.match(render(work({ writing: w({ active: true }) })), /Слайд 3 из 9/);
});

test("slide editor: 8..12 slides, 15..250 words, 600..1200 in total, a title of 120 runes", () => {
  const item = (words, title = "A") => ({ number: 1, title, words });
  const ok = (n, words) => Array.from({ length: n }, () => item(words));
  assert.equal(checkEditor(ok(8, "75"), "presentation").ok, true);
  assert.equal(checkEditor(ok(12, "100"), "presentation").ok, true);
  assert.equal(checkEditor(ok(7, "100"), "presentation").ok, false);
  assert.match(checkEditor(ok(7, "100"), "presentation").problem, /от 8 до 12 слайдов/);
  assert.equal(checkEditor(ok(13, "50"), "presentation").ok, false);
  // a slide of 14 and of 251 words
  assert.equal(checkEditor([...ok(7, "100"), item("14")], "presentation").ok, false);
  assert.equal(checkEditor([...ok(7, "100"), item("15")], "presentation").ok, true);
  assert.equal(checkEditor([...ok(7, "80"), item("250")], "presentation").ok, true);
  assert.equal(checkEditor([...ok(7, "80"), item("251")], "presentation").ok, false);
  // totals: 8 x 74 = 592 < 600; 8 x 75 = 600; 12 x 100 = 1200; 12 x 101 over
  assert.match(checkEditor(ok(8, "74"), "presentation").problem, /не меньше 600/);
  assert.equal(checkEditor(ok(12, "101"), "presentation").ok, false);
  assert.match(checkEditor(ok(12, "101"), "presentation").problem, /не больше 1200/);
  // titles: 120 runes pass, 121 do not (the paper allows 150)
  assert.equal(checkEditor([...ok(7, "100"), item("100", "😀".repeat(120))], "presentation").ok, true);
  assert.equal(checkEditor([...ok(7, "100"), item("100", "x".repeat(121))], "presentation").ok, false);
  assert.equal(checkEditor([{ number: 1, title: "x".repeat(121), words: "100" }]).ok, true);
  // the paper keeps its limits
  assert.equal(checkEditor([{ number: 1, title: "A", words: "20" }]).ok, false);
  assert.equal(checkEditor([{ number: 1, title: "A", words: "20" }], "lab").ok, false);
  // what is sent keeps the slide words
  assert.deepEqual(toOutlineItems([{ number: 3, title: " A ", words: "20" }], "presentation"), [{ number: 3, title: "A", words: 20 }]);
  assert.equal(isEdited(editorFromSections([slide(1, "title")]), [slide(1, "title")], "presentation"), false);
});

test("the title and the conclusion slide cannot be removed or moved; a new slide goes before the conclusion", () => {
  const items = editorFromSections(deck());
  assert.equal(items.length, 10);
  assert.equal(canRemove(items, 0), false);
  assert.equal(canRemove(items, 9), false);
  assert.equal(canRemove(items, 4), true);
  assert.deepEqual(removeItem(items, 0), items);
  assert.deepEqual(removeItem(items, 9), items);
  assert.equal(removeItem(items, 4).length, 9);
  assert.equal(canMove(items, 0, 1), false);
  assert.equal(canMove(items, 1, -1), false);
  assert.equal(canMove(items, 8, 1), false);
  assert.equal(canMove(items, 9, -1), false);
  assert.equal(canMove(items, 3, 1), true);
  assert.deepEqual(moveItem(items, 1, -1), items);
  assert.deepEqual(moveItem(items, 8, 1), items);
  assert.deepEqual(moveItem(items, 2, 1).map((i) => i.number), [1, 2, 4, 3, 5, 6, 7, 8, 9, 10]);
  const added = addItem(items, "presentation");
  assert.equal(added.length, 11);
  assert.equal(added[9].number, 0);
  assert.equal(added[9].words, "60");
  assert.equal(added[10].kind, "conclusion");
  assert.equal(checkEditor(added.map((i, n) => (n === 9 ? { ...i, title: "Новый" } : i)), "presentation").ok, true);
  // the maximum of 12 slides
  assert.equal(addItem(addItem(addItem(items, "presentation"), "presentation"), "presentation").length, 12);
  assert.equal(addItem(addItem(addItem(addItem(items, "presentation"), "presentation"), "presentation"), "presentation").length, 12);
});

test("lab report editor: the last section with the part results or conclusions cannot be removed", () => {
  const lab = editorFromSections([
    section({ number: 1, part: "goal" }), section({ number: 2, part: "results" }), section({ number: 3, part: "conclusions" }),
  ]);
  assert.equal(canRemove(lab, 0), true);
  assert.equal(canRemove(lab, 1), false);
  assert.equal(canRemove(lab, 2), false);
  assert.equal(removeItem(lab, 1).length, 3);
  const twoResults = editorFromSections([section({ number: 1, part: "results" }), section({ number: 2, part: "results" }), section({ number: 3, part: "conclusions" })]);
  assert.equal(canRemove(twoResults, 0), true);
  assert.equal(removeItem(twoResults, 0).length, 2);
  // the paper: everything can go
  assert.equal(canRemove(editorFromSections([section({ number: 1 })]), 0), true);
  // the editor mode follows the writing
  assert.deepEqual([editorMode(null), editorMode(writing({ lab: true })), editorMode(writing({ presentation: true }))], ["paper", "lab", "presentation"]);
  // the editor in the card disables the buttons of a fixed slide
  const out = render(work({ writing: writing({ step: "approve", presentation: true, sections: deck() }) }));
  assert.ok(out.split("disabled").length - 1 >= 6, "move and remove buttons of the title and the conclusion are disabled");
  assert.match(out, /Заголовок|aria-label="Название раздела"/);
});

test("a ready presentation: the list of slides, bullets, columns, notes, questions and the buttons", () => {
  const sections = [
    slide(1, "title", { paragraphs: ["Привет, коллеги"] }),
    slide(2, "bullets", { bullets: ["Тезис один", "Тезис два"], paragraphs: ["Заметка про тезисы"] }),
    slide(3, "two_columns", { columns: [{ heading: "Плюсы", bullets: ["П1"] }, { heading: "", bullets: ["М1"] }] }),
    slide(4, "conclusion", { bullets: ["Итог"] }),
  ];
  const w = work({ workType: "presentation", writing: doneWriting({ presentation: true, talkMaterialId: "t9", talkResult: "talk", questions: talkQuestions, sections }) });
  const m = workCardModel(w);
  assert.equal(m.kind, "presentation");
  assert.equal(m.file.kind, ".pptx");
  assert.deepEqual(m.sections.map((s) => s.heading), ["1. Слайд-1", "2. Слайд-2", "3. Слайд-3", "4. Слайд-4"]);
  assert.equal(talkTarget(w), "t9");
  const out = renderWith(w, { library });
  assert.equal(out.split("<details").length - 1, 5, "four slides and the questions");
  assert.match(out, /<details[^>]*><summary>2\. Слайд-2<\/summary>/);
  assert.match(out, /<li>Тезис один<\/li><li>Тезис два<\/li>/);
  assert.match(out, /Плюсы/);
  assert.match(out, /Колонка 2/);
  assert.match(out, /Заметки докладчика/);
  assert.match(out, /Заметка про тезисы/);
  assert.match(out, /<summary>Вероятные вопросы<\/summary>/);
  assert.match(out, /Почему так\?/);
  assert.match(out, /Потому что\./);
  assert.match(out, /Открыть в PowerPoint/);
  assert.match(out, /Открыть текст выступления/);
  assert.match(out, /правки в ней в приложение не возвращаются/);
  assert.match(out, /Доработать/);
  // not ready: no questions, no talk button
  const early = work({ workType: "presentation", writing: doneWriting({ presentation: true, sections }) });
  assert.equal(talkTarget(early), "");
  const o2 = renderWith(early, { library });
  assert.ok(!o2.includes("Открыть текст выступления"));
  assert.ok(!o2.includes("Вероятные вопросы"));
  // without the binding nothing is offered
  assert.ok(!renderWith(w).includes("Открыть текст выступления"));
  // a paper has neither
  assert.ok(!renderWith(work({ writing: doneWriting() }), { library }).includes("Открыть текст выступления"));
});

test("«Открыть текст выступления» opens by id only, a refusal is a fixed text", async () => {
  const calls = [];
  const lib = { openMaterial: async (...a) => (calls.push(a), { status: "refused", message: "Этот файл нельзя открыть отсюда." }) };
  const w = work({ writing: doneWriting({ presentation: true, talkMaterialId: "t5", materialId: "m1" }) });
  assert.deepEqual(await openTalk(lib, w), { kind: "fail", text: "Этот файл нельзя открыть отсюда." });
  assert.deepEqual(calls, [["t5"]]);
  assert.equal(await openTalk(lib, work({ writing: doneWriting({ presentation: true }) })), null);
  assert.equal(await openTalk(lib, work({ writing: doneWriting({ talkMaterialId: "t5" }) })), null);
  assert.equal(calls.length, 1);
  const src = readFileSync(new URL("../src/agent/WorkCard.tsx", import.meta.url), "utf8");
  assert.match(src, /void run\(openTalkText\)/);
});

const labSections = [
  section({ number: 1, title: "Цель работы", part: "goal", written: true, paragraphs: ["Изучить закон."] }),
  section({ number: 2, title: "Измерения", part: "results", written: true, paragraphs: ["Измерения таковы.", "▢ Вставьте таблицу измерений"] }),
  section({ number: 3, title: "Итог", part: "conclusions", written: true, paragraphs: ["Выводы."] }),
  section({ number: 4, title: "Прочее", part: "other", written: true, paragraphs: ["Ещё текст."] }),
];
const placeholders = [{ section: 2, title: "Измерения", prompt: "Вставьте таблицу измерений" }];
const labWork = (over) => work({ workType: "lab_report", writing: doneWriting({ lab: true, manual: "read", placeholders, sections: labSections, ...over }) });

test("a ready lab report: part labels, highlighted placeholders, «Что доделать самому», lines about the manual", () => {
  const out = renderWith(labWork());
  for (const label of ["Цель", "Результаты", "Выводы"]) {
    assert.match(out, new RegExp(`<p class="ag-sub">${label}</p>`));
  }
  assert.equal(out.split('<p class="ag-sub">').length - 1, 4, "three parts and the todo caption; «other» has no label");
  assert.match(out, /<p class="ag-text ag-wk-ph">▢ Вставьте таблицу измерений<\/p>/);
  assert.match(out, /<p class="ag-text">Измерения таковы\.<\/p>/);
  assert.match(out, /Что доделать самому/);
  assert.match(out, /2\. Измерения: Вставьте таблицу измерений/);
  assert.match(out, /Выводы зависят от ваших данных/);
  assert.ok(!out.includes("Методичка не прочитана"));
  assert.ok(!out.includes("В методичке нет текста"));
  // The manual: not read, no text; "" before the outline and the other states say nothing.
  assert.match(renderWith(labWork({ manual: "not_read" })), /Методичка не прочитана: отчёт написан по тексту задания\./);
  assert.match(renderWith(labWork({ manual: "no_text" })), /В методичке нет текста, который можно прочитать: отчёт написан по тексту задания\./);
  for (const manual of ["", "none"]) {
    const o = renderWith(labWork({ manual }));
    assert.ok(!o.includes("Методичка") && !o.includes("В методичке"), manual);
    assert.match(o, /Выводы зависят от ваших данных/);
  }
  // A paper and a presentation have no such block.
  assert.ok(!renderWith(work({ writing: doneWriting({ sections: labSections }) })).includes("Что доделать самому"));
  assert.match(renderWith(labWork(), { library }), /Открыть в Word/);
  assert.match(renderWith(labWork()), /Доработать/);
});

test("model text of the new fields stays text", () => {
  const sections = [
    slide(1, "two_columns", { title: evil, bullets: [evil], columns: [{ heading: evil, bullets: [evil] }], paragraphs: [evil], part: "results" }),
  ];
  const w = work({
    writing: doneWriting({ presentation: true, lab: true, manual: "not_read", sections, questions: [{ question: evil, answer: evil }], placeholders: [{ section: 1, title: evil, prompt: evil }] }),
  });
  const out = renderWith(w, { library });
  assert.ok(!out.includes("<img"));
  assert.ok(!/<a[ >]/.test(out));
  assert.ok(!out.includes("onerror=alert(1)>"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.deepEqual(sectionTexts([section({ paragraphs: ["▢ x", "y"] })])[0].marks, [true, false]);
});

test("«Замечания»: any finished work revises with Regenerate(id, 0 or N, note); an empty note sends nothing", async () => {
  const log = [];
  const api = { ...fakeApi(), regenerate: async (...a) => (log.push(a), work()) };
  assert.equal(reviseWork(api, "w1", 0, "   \n "), null);
  assert.equal(canSubmitNote(""), false);
  assert.equal(canSubmitNote(" \n "), false);
  assert.equal(canSubmitNote(" короче "), true);
  await reviseWork(api, "w1", 0, "  переписать\nвсё  ");
  await reviseWork(api, "w1", 4, "короче");
  await reviseWork(api, "w1", 2, "😀".repeat(1200));
  assert.deepEqual(log.slice(0, 2), [["w1", 0, "переписать\nвсё"], ["w1", 4, "короче"]]);
  assert.equal([...log[2][2]].length, 1000);
  assert.equal(log.length, 3);

  assert.equal(reviseCaption(0), "Работа будет написана заново целиком с учётом замечаний (платно)");
  assert.equal(reviseCaption(3), "");

  const slideOpts = reviseOptions(deck(), "presentation");
  assert.deepEqual(slideOpts.slice(0, 2).map((o) => o.label), ["Вся работа", "Слайд 1. Слайд-1"]);
  assert.equal(reviseOptions(labSections, "lab")[1].label, "Раздел 1. Цель работы");

  // Every kind of a ready work offers «Доработать»; the card calls the note path through run and a textarea.
  for (const w of [work({ writing: doneWriting() }), work({ writing: doneWriting({ presentation: true, sections: deck() }) }), labWork()]) {
    assert.match(renderWith(w), /Доработать/);
  }
  const src = readFileSync(new URL("../src/agent/WorkCard.tsx", import.meta.url), "utf8");
  assert.match(src, /<textarea id=\{noteId\}/);
  assert.match(src, /disabled=\{busy \|\| !canSubmitNote\(note\)\} onClick=\{\(\) => void run\(reviseNow\)\}/);
  assert.match(src, /reviseWork\(api, work\.id, reviseNumber, note\)/);
  // «Дополнительные указания» of a blocked work is unchanged.
  assert.match(render(work({ writing: writing({ step: "blocked", sections: [section()] }) })), /Дополнительные указания/);
});

test("a double click on «Доработать по замечаниям» is one call", async () => {
  const flight = singleFlight();
  const log = [];
  const api = { ...fakeApi(), regenerate: async (...a) => (log.push(a), work()) };
  let release;
  const slow = { ...api, regenerate: (...a) => new Promise((res) => { log.push(a); release = () => res(work()); }) };
  const click = () => flight(async () => { await reviseWork(slow, "w1", 0, "замечание"); });
  const first = click();
  assert.equal(await click(), false);
  release();
  await first;
  assert.equal(log.length, 1);
});

// ---- «Скачать PDF» (TASK-199) ----

const pdfPaper = (over) => work({ workType: "referat", writing: doneWriting(over) });

test("pdf: parseWork reads pdfMaterialId, missing is empty, a wrong type throws", () => {
  assert.equal(parseWork(raw({ writing: writing({ pdfMaterialId: "p1" }) })).writing.pdfMaterialId, "p1");
  assert.equal(parseWork(raw({ writing: writing() })).writing.pdfMaterialId, "");
  assert.throws(() => parseWork(raw({ writing: writing({ pdfMaterialId: 5 }) })));
});

test("pdf: exportPdf calls ExportPDF with the id and parses the work", async () => {
  const calls = [];
  const api = turnkeyApi(async (...a) => (calls.push(a), raw({ writing: writing({ step: "none", result: "ok", pdfMaterialId: "p1" }) })));
  const w = await api.exportPdf("w1");
  assert.deepEqual(calls, [[methods.exportPdf, "w1"]]);
  assert.equal(w.writing.pdfMaterialId, "p1");
  await assert.rejects(turnkeyApi(async () => ({ id: 1 })).exportPdf("w1"));
});

test("pdf: the core's PDF error texts are fixed and shown as they are", () => {
  for (const t of ["Не удалось сохранить PDF: на компьютере нет нужного компонента Windows (WebView2).", "Не удалось сохранить PDF. Попробуйте ещё раз."]) {
    assert.ok(errorTexts.includes(t));
    assert.equal(turnkeyErrorText(new Error(t)), t);
  }
});

test("pdf: «Скачать PDF» shows for a ready paper and lab report, not for a presentation or a running work", () => {
  assert.ok(renderWith(pdfPaper()).includes(">Скачать PDF<"));
  assert.ok(renderWith(labWork()).includes(">Скачать PDF<"));
  const deckWork = work({ workType: "presentation", writing: doneWriting({ presentation: true }) });
  assert.equal(pdfOffered(deckWork), false);
  assert.ok(!renderWith(deckWork, { library }).includes("PDF"));
  const running = work({ workType: "referat", writing: writing({ step: "section", total: 2, active: true }) });
  assert.ok(!renderWith(running, { library }).includes("PDF"));
  assert.ok(!renderWith(pdfPaper({ result: "" })).includes("PDF"));
});

test("pdf: a saved PDF shows the line and «Открыть PDF» at once, with the copy caption", () => {
  const out = renderWith(pdfPaper({ pdfMaterialId: "p1" }), { library });
  assert.ok(out.includes("PDF сохранён в библиотеке рядом с файлом Word."));
  assert.ok(out.includes(">Открыть PDF<"));
  assert.ok(!out.includes(">Скачать PDF<"));
  assert.ok(out.includes("правки в ней в приложение не возвращаются"));
  // A new generation has no PDF: the download button is back.
  const fresh = renderWith(pdfPaper({ pdfMaterialId: "", generation: 2 }), { library });
  assert.ok(fresh.includes(">Скачать PDF<") && !fresh.includes("Открыть PDF"));
});

test("pdf: «Открыть PDF» opens the PDF id only; a refusal is a fixed text", async () => {
  const calls = [];
  const lib = { openMaterial: async (...a) => (calls.push(a), { status: "refused", message: "Этот файл нельзя открыть отсюда." }) };
  const w = pdfPaper({ pdfMaterialId: "p7" });
  assert.equal(pdfTarget(w), "p7");
  assert.deepEqual(await openPdf(lib, w), { kind: "fail", text: "Этот файл нельзя открыть отсюда." });
  assert.deepEqual(calls, [["p7"]]);
  assert.equal(await openPdf(lib, pdfPaper()), null);
  assert.equal(calls.length, 1);
});

test("pdf: an error of the call shows in the error line and the button stays", () => {
  const out = renderWith(pdfPaper(), { error: "Не удалось сохранить PDF. Попробуйте ещё раз.", onError: noop });
  assert.ok(out.includes("Не удалось сохранить PDF. Попробуйте ещё раз."));
  assert.ok(out.includes(">Скачать PDF<"));
  assert.ok(!out.includes("Повторить"));
});

test("pdf: a double click is one call; no call without a PDF to make", async () => {
  const log = [];
  let release;
  const api = { exportPdf: (...a) => new Promise((res) => { log.push(a); release = () => res(pdfPaper({ pdfMaterialId: "p1" })); }) };
  const w = pdfPaper();
  const flight = singleFlight();
  const click = () => flight(async () => { await exportPdf(api, w); });
  const first = click();
  assert.equal(await click(), false);
  release();
  await first;
  assert.deepEqual(log, [["w1"]]);
  assert.equal(exportPdf(api, pdfPaper({ pdfMaterialId: "p1" })), null);
  assert.equal(exportPdf(api, work({ workType: "presentation", writing: doneWriting({ presentation: true }) })), null);
});

test("pdf: the button reads «Сохранение PDF…» while the call runs and goes through the guard", () => {
  const src = readFileSync(new URL("../src/agent/WorkCard.tsx", import.meta.url), "utf8");
  assert.match(src, /pdfBusy \? workTexts\.savingPdf : workTexts\.downloadPdf/);
  assert.match(src, /onClick=\{\(\) => void run\(savePdf\)\}/);
  assert.equal(workTexts.savingPdf, "Сохранение PDF…");
});
