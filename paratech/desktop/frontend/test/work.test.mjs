// The writing cards of a turnkey work (TASK-132): the binding, the card of each step, the plan editor, polling and
// plain-text rendering. Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { errorTexts, genericError, methods, parseWork, turnkeyApi, turnkeyErrorText } from "../.test-build/bindings/turnkey.js";
import { parseCard, parseUpdate } from "../.test-build/bindings/agent.js";
import { entryModel } from "../.test-build/agentView.js";
import {
  acceptWork,
  addItem,
  checkEditor,
  editorFromSections,
  isEdited,
  isRunning,
  limits,
  moveItem,
  noteOf,
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
  return { number: 1, title: "Введение", words: 300, note: "", written: false, paragraphs: [], sources: [], ...over };
}

function writing(over) {
  return {
    step: "none", generation: 1, topic: "Тема работы", sections: [], done: 0, total: 0, subjectId: "", materialId: "",
    result: "", job: "", ...over,
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
  const out = renderToStaticMarkup(h(AgentRequest, { model: { key: "k", userText: "", callLine: "", cards: [{ type: "work", workId: "w1" }] }, actions }));
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
  assert.match(out, /maxLength="200"/);
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
  assert.match(out, /Нужно от 1 до 30 разделов/);
  assert.match(out, /<button[^>]*disabled=""[^>]*>Утвердить план/);
});

test("blocked: the text, the note field with its limit and «Сгенерировать заново»", () => {
  const out = render(work({ writing: writing({ step: "blocked" }) }));
  assert.match(out, /Не удалось написать раздел\. Можно сгенерировать заново\./);
  assert.match(out, /Дополнительные указания/);
  assert.match(out, /maxLength="300"/);
  assert.match(out, /Сгенерировать заново/);
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

test("the editor check: sections 1..30, a title, words 1..20000 as an integer", () => {
  const ok = [{ number: 1, title: "A", words: "100" }];
  assert.equal(checkEditor(ok).ok, true);
  assert.equal(checkEditor([]).ok, false);
  assert.equal(checkEditor(Array.from({ length: 31 }, () => ok[0])).ok, false);
  assert.equal(checkEditor(Array.from({ length: 30 }, () => ok[0])).ok, true);
  const empty = checkEditor([{ number: 1, title: "   ", words: "100" }]);
  assert.equal(empty.ok, false);
  assert.deepEqual(empty.badTitles, [true]);
  assert.equal(checkEditor([{ number: 1, title: "x".repeat(201), words: "100" }]).ok, false);
  for (const words of ["0", "20001", "1.5", "-3", "", "abc", "1e3", " "]) {
    const c = checkEditor([{ number: 1, title: "A", words }]);
    assert.equal(c.ok, false, words);
    assert.deepEqual(c.badWords, [true], words);
  }
  for (const words of ["1", "20000", " 50 "]) {
    assert.equal(checkEditor([{ number: 1, title: "A", words }]).ok, true, words);
  }
});

test("revise: the choice is every section or one section; the note is trimmed and cut", () => {
  const opts = reviseOptions([section({ number: 1, title: "A" }), section({ number: 4, title: "B" })]);
  assert.deepEqual(opts.map((o) => o.value), [0, 1, 4]);
  assert.equal(opts[0].label, "Весь текст");
  assert.equal(noteOf("  короче  "), "короче");
  assert.equal(noteOf("x".repeat(500)).length, 300);
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

test("buttons without a binding are not offered", () => {
  const src = readFileSync(new URL("../src/workView.ts", import.meta.url), "utf8") + readFileSync(new URL("../src/agent/WorkCard.tsx", import.meta.url), "utf8");
  for (const label of ["Открыть в Word", "Остановить", "Отправить преподавателю"]) {
    assert.ok(!src.includes(label), label);
  }
});
