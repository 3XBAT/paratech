// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-296: the sources of an answer on the agent answer card, their binding and the jump to the place.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { answersApi, errorMessages, methods, parseAnswer, parseSource, errorText } from "../.test-build/bindings/answers.js";
import { AnswerSources, AnswerSourcesView, answerSourcesTexts } from "../.test-build/agent/AnswerSources.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { entryModel } from "../.test-build/agentView.js";
import {
  citationLesson,
  citationOpening,
  citationPlace,
  findNoteLecture,
  maxNoteScanSubjects,
  noFocus,
  noteJumpTexts,
  noteLectureTexts,
  resolveOpening,
} from "../.test-build/knowledgeView.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;
const evil = "<img src=x onerror=alert(1)><b>bold</b>";

const noteSource = (over = {}) => ({
  recordingId: "", section: "ownNote", index: 2, startMs: 0, time: "", title: "", startedAt: "",
  noteId: "n1", materialId: "m1", locatorKind: "page", locatorN: 3, label: "ваш конспект · стр. 3", removed: false, ...over,
});
const recSource = (over = {}) => ({
  recordingId: "r1", section: "transcript", index: 4, startMs: 725000, time: "12:05", title: "Ряды", startedAt: "2026-10-19T09:00:00Z",
  noteId: "", materialId: "", locatorKind: "", locatorN: 0, label: "", removed: false, ...over,
});
const reply = (over = {}) => ({
  id: "a1", question: "Что такое ряд?", subjectId: "s1", createdAt: "2026-10-20T10:00:00Z",
  claims: [{ text: "Ряд — сумма.", fromLessons: true, cites: [{ quote: "сумма членов", source: 1 }] }],
  missing: ["Про сходимость"], sources: [recSource(), noteSource()], ...over,
});

// ---- Binding ----

test("answer(id) calls the bound method and parses the whole view", async () => {
  const calls = [];
  const api = answersApi(async (m, ...args) => {
    calls.push([m, ...args]);
    return reply();
  });
  const v = await api.answer("a1");
  assert.deepEqual(calls, [["paratech/desktop/internal/input/wails.AnswerService.Answer", "a1"]]);
  assert.equal(methods.answer, calls[0][0]);
  assert.equal(v.id, "a1");
  assert.equal(v.question, "Что такое ряд?");
  assert.equal(v.claims[0].cites[0].source, 1);
  assert.deepEqual(v.missing, ["Про сходимость"]);
  assert.deepEqual(v.sources[0], recSource());
  assert.deepEqual(v.sources[1], noteSource());
});

test("empty lists parse", () => {
  const v = parseAnswer(reply({ claims: [], missing: [], sources: [] }));
  assert.deepEqual([v.claims, v.missing, v.sources], [[], [], []]);
});

test("a malformed reply throws", () => {
  const bad = [
    null, "x", 5, [], undefined,
    reply({ id: 1 }),
    reply({ question: undefined }),
    reply({ createdAt: null }),
    reply({ claims: null }),
    reply({ missing: "x" }),
    reply({ missing: [1] }),
    reply({ sources: {} }),
    reply({ claims: [{ text: "t", fromLessons: "yes", cites: [] }] }),
    reply({ claims: [{ text: "t", fromLessons: true, cites: [{ quote: "q", source: "0" }] }] }),
    reply({ claims: [{ text: "t", fromLessons: true, cites: [{ quote: "q", source: 2 }] }] }),
    reply({ claims: [{ text: "t", fromLessons: true, cites: [{ quote: "q", source: -1 }] }] }),
    reply({ claims: [{ text: "t", fromLessons: true, cites: [{ quote: "q", source: 0.5 }] }] }),
    reply({ claims: [null] }),
    reply({ sources: [recSource({ section: "other" })] }),
    reply({ sources: [recSource({ index: -1 })] }),
    reply({ sources: [recSource({ startMs: "0" })] }),
    reply({ sources: [recSource({ time: 5 })] }),
    reply({ sources: [recSource({ removed: "no" })] }),
    reply({ sources: [recSource({ locatorKind: "line" })] }),
    reply({ sources: [(() => { const s = recSource(); delete s.title; return s; })()] }),
    // The note fields must fit the section.
    reply({ sources: [noteSource({ noteId: "" })] }),
    reply({ sources: [noteSource({ materialId: "" })] }),
    reply({ sources: [noteSource({ locatorKind: "" })] }),
    reply({ sources: [noteSource({ locatorN: 0 })] }),
    reply({ sources: [noteSource({ label: "" })] }),
    reply({ sources: [recSource({ noteId: "n1" })] }),
    reply({ sources: [recSource({ locatorN: 1 })] }),
    reply({ sources: [recSource({ label: "x" })] }),
  ];
  for (const raw of bad) {
    assert.throws(() => parseAnswer(raw), undefined, JSON.stringify(raw)?.slice(0, 120));
  }
  assert.throws(() => parseSource(null));
});

test("a removed source never fails the whole card", () => {
  for (const s of [
    noteSource({ removed: true, label: "источник удалён" }),
    noteSource({ removed: true, label: "" }),
    noteSource({ removed: true, label: "", noteId: "", materialId: "", locatorKind: "", locatorN: 0 }),
    recSource({ removed: true, label: "источник удалён" }),
  ]) {
    assert.equal(parseAnswer(reply({ sources: [s], claims: [] })).sources[0].removed, true);
  }
});

test("a failed call rejects and errorText keeps only the core's fixed texts", async () => {
  const api = answersApi(async () => {
    throw new Error(errorMessages.notFound);
  });
  await assert.rejects(api.answer("a1"));
  assert.equal(errorText(new Error(errorMessages.notFound)), errorMessages.notFound);
  assert.equal(errorText(errorMessages.unavailable), errorMessages.unavailable);
  assert.equal(errorText(new Error("open C:\\secret\\db.sqlite: denied")), errorMessages.unavailable);
  assert.equal(errorText(null), errorMessages.unavailable);
});

// ---- Labels and opening ----

test("labels: a note, a recording, a removed source", () => {
  assert.equal(citationPlace(noteSource()), "ваш конспект · стр. 3");
  assert.equal(citationPlace(recSource()), "Расшифровка · 12:05");
  assert.equal(citationPlace(recSource({ section: "summary", time: "" })), "Кратко");
  assert.equal(citationPlace(noteSource({ removed: true, label: "источник удалён" })), "источник удалён");
  assert.equal(citationPlace(noteSource({ removed: true, label: "" })), "источник удалён");
  assert.equal(noteLectureTexts.sourceRemoved, "источник удалён");
});

test("the lesson line is the day and the title, plain text; empty parts are skipped", () => {
  assert.match(citationLesson(recSource()), /^\d\d\.\d\d · Ряды$/);
  assert.equal(citationLesson(recSource({ startedAt: "" })), "Ряды");
  assert.equal(citationLesson(recSource({ title: "  " , startedAt: "" })), "");
  assert.equal(citationLesson(recSource({ title: evil, startedAt: "" })), evil);
});

test("citationOpening carries ids only, for each kind", () => {
  assert.deepEqual(citationOpening(noteSource()), { kind: "note", noteId: "n1", materialId: "m1", locatorKind: "page", locatorN: 3 });
  assert.deepEqual(citationOpening(recSource()), { kind: "recording", recordingId: "r1", focus: { tab: "transcript", index: 4 } });
  assert.deepEqual(citationOpening(recSource({ section: "lectureText", index: 2 })), { kind: "recording", recordingId: "r1", focus: { tab: "lecture", index: 2 } });
  assert.deepEqual(citationOpening(recSource({ section: "summary", index: 1 })), { kind: "recording", recordingId: "r1", focus: { tab: "summary", index: 1 } });
  assert.equal(citationOpening(noteSource({ removed: true })), null);
  assert.equal(citationOpening(recSource({ recordingId: "" })), null);
  assert.equal(citationOpening(noteSource({ materialId: "" })), null);
});

// ---- The list ----

const ready = (sources) => ({ kind: "ready", sources });

test("the list: a button per openable source, plain muted text for a removed one, the core's order", () => {
  const out = html(
    h(AnswerSourcesView, {
      state: ready([noteSource({ removed: true, label: "источник удалён" }), recSource(), noteSource()]),
      onOpen: noop,
    }),
  );
  assert.match(out, /<ul[^>]*aria-label="Источники ответа"/);
  assert.equal(out.match(/<button/g).length, 2);
  assert.match(out, /<li class="ag-src-row ag-src-gone muted">источник удалён<\/li>/);
  assert.ok(out.indexOf("источник удалён") < out.indexOf("Расшифровка · 12:05"));
  assert.ok(out.indexOf("Расшифровка · 12:05") < out.indexOf("ваш конспект · стр. 3"));
  assert.match(out, /<button type="button"[^>]*aria-label="Открыть: Расшифровка · 12:05, \d\d\.\d\d · Ряды"/);
  assert.match(out, /aria-label="Открыть: ваш конспект · стр\. 3"/);
  assert.doesNotMatch(out, /href=|<a /);
});

test("the same source twice is listed twice (no dedupe, no reorder)", () => {
  const out = html(h(AnswerSourcesView, { state: ready([recSource(), recSource()]), onOpen: noop }));
  assert.equal(out.match(/<button/g).length, 2);
});

test("without a click handler a source is plain text, not a dead button", () => {
  const out = html(h(AnswerSourcesView, { state: ready([recSource()]) }));
  assert.doesNotMatch(out, /<button/);
  assert.match(out, /Расшифровка · 12:05/);
});

test("loading, failed and empty states", () => {
  assert.match(html(h(AnswerSourcesView, { state: { kind: "loading" } })), /Загрузка источников…/);
  const failed = html(h(AnswerSourcesView, { state: { kind: "failed", text: errorMessages.unavailable } }));
  assert.match(failed, /Не удалось открыть ответ\. Попробуйте ещё раз\./);
  assert.doesNotMatch(failed, /<button|<ul/);
  assert.equal(html(h(AnswerSourcesView, { state: ready([]) })), "");
  assert.equal(answerSourcesTexts.loading, "Загрузка источников…");
});

test("the click handler gets ids only", () => {
  const got = [];
  const el = AnswerSourcesView({ state: ready([noteSource(), recSource()]), onOpen: (o) => got.push(o) });
  const buttons = [];
  const walk = (n) => {
    if (n === null || typeof n !== "object") return;
    if (Array.isArray(n)) return n.forEach(walk);
    if (n.type === "button") buttons.push(n);
    walk(n.props?.children);
  };
  walk(el);
  assert.equal(buttons.length, 2);
  buttons.forEach((b) => b.props.onClick());
  assert.deepEqual(got, [
    { kind: "note", noteId: "n1", materialId: "m1", locatorKind: "page", locatorN: 3 },
    { kind: "recording", recordingId: "r1", focus: { tab: "transcript", index: 4 } },
  ]);
  for (const o of got) {
    assert.doesNotMatch(JSON.stringify(o), /[\\/]|\.docx|label|title/);
  }
});

test("no HTML is built from a label or a title: markup stays visible text", () => {
  const out = html(
    h(AnswerSourcesView, {
      state: ready([noteSource({ label: evil }), recSource({ title: evil }), noteSource({ removed: true, label: evil })]),
      onOpen: noop,
    }),
  );
  assert.doesNotMatch(out, /<img|<b>/);
  assert.match(out, /&lt;img src=x onerror=alert\(1\)&gt;&lt;b&gt;bold&lt;\/b&gt;/);
});

test("the sources code has no raw HTML, links or paths", () => {
  for (const f of ["agent/AnswerSources.tsx", "bindings/answers.ts", "knowledgeView.ts", "pages/KnowledgeBasePage.tsx", "agent/AgentCards.tsx"]) {
    const s = readFileSync(new URL(`../src/${f}`, import.meta.url), "utf8");
    assert.ok(!s.includes("dangerouslySetInnerHTML") && !s.includes("innerHTML"), f);
  }
  const s = readFileSync(new URL("../src/agent/AnswerSources.tsx", import.meta.url), "utf8");
  assert.ok(!/href=|<a[ >]|window\.open|location\./.test(s));
});

// ---- The card ----

const actionsBase = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const answerCard = (answerId) => ({
  kind: "answer", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId, cannotDo: null,
  text: "", assignments: [], stopReason: "", workId: "", outcome: "", freeWork: null,
});
const update = (cards) => ({
  threadId: "th1", turn: 3, subjectId: "", lessonId: "", assignmentId: "", screen: "", status: "DONE", stopReason: "", problem: "", progress: false, cards,
});
const model = (answerId) => entryModel({ key: "k", seq: 1, threadId: "th1", turn: 3, text: "", scope: null, callProblem: "", update: update([answerCard(answerId)]) });

test("the answer card model carries the answerId", () => {
  const c = model("a1").cards[0];
  assert.equal(c.type, "answer");
  assert.equal(c.answerId, "a1");
  assert.equal(c.caption, "Ответ по материалам");
  assert.equal(c.text, "Ответ с источниками подготовлен.");
});

test("the card keeps its caption and text; without answerId or the call nothing is added and nothing crashes", () => {
  const fake = answersApi(async () => reply());
  for (const [id, actions] of [
    ["", { ...actionsBase, answers: fake }],
    ["a1", actionsBase],
    ["", actionsBase],
  ]) {
    const out = html(h(AgentRequest, { model: model(id), actions }));
    assert.match(out, /Ответ по материалам/);
    assert.match(out, /Ответ с источниками подготовлен\./);
    assert.doesNotMatch(out, /Загрузка источников|ag-src|<ul/);
    assert.doesNotMatch(out, /a1/);
  }
});

test("with the call the card starts by loading the sources", () => {
  const out = html(h(AgentRequest, { model: model("a1"), actions: { ...actionsBase, answers: answersApi(async () => reply()), openSource: noop } }));
  assert.match(out, /Ответ по материалам/);
  assert.match(out, /Загрузка источников…/);
  assert.doesNotMatch(out, /a1/);
});

test("AnswerSources renders nothing without the call or the id", () => {
  assert.equal(html(h(AnswerSources, { answerId: "a1" })), "");
  assert.equal(html(h(AnswerSources, { api: answersApi(async () => reply()), answerId: "" })), "");
});

// ---- The jump to the place ----

const lecture = (key, noteIds) => ({
  key, noteOnly: true, undated: true, anchor: "undated", scheduleLessonId: "", date: "", notes: noteIds.map((id) => ({ id, title: "T", subjectId: "", createdAt: "" })),
  recordingId: "", startedAt: "", durationSeconds: 0, title: "", subject: "", subjectKey: "", kind: "", subjectEdited: false, kindEdited: false,
  transcriptReady: false, summaryReady: false, lectureTextReady: false, librarySubjectId: "", librarySubjectState: "no-subject",
});
const subject = (key) => ({ key, name: key, lessons: 0, latest: "", noSubject: false, subjectId: "" });
const fakeKb = (subjects, byKey, o = {}) => {
  const calls = [];
  return {
    calls,
    subjects: async () => {
      calls.push(["subjects"]);
      if (o.failSubjects) throw new Error("boom");
      return subjects;
    },
    lectures: async (key) => {
      calls.push(["lectures", key]);
      if (o.failOn === key) throw new Error("boom");
      return byKey[key] ?? [];
    },
    lesson: async (id) => {
      calls.push(["lesson", id]);
      if (o.failLesson) throw new Error("boom");
      return { ...lecture("rec", []), recordingId: id, noteOnly: false };
    },
  };
};

test("the note scan finds the lecture in the second subject and stops there", async () => {
  const want = lecture("L2", ["n1", "n2"]);
  const api = fakeKb([subject("a"), subject("b"), subject("c")], { a: [lecture("L1", ["x"])], b: [lecture("L0", []), want], c: [lecture("L3", ["n1"])] });
  assert.equal(await findNoteLecture(api, "n1"), want);
  assert.deepEqual(api.calls, [["subjects"], ["lectures", "a"], ["lectures", "b"]]);
});

test("the note scan: not found is null, a failed call rejects, an empty id asks nothing", async () => {
  assert.equal(await findNoteLecture(fakeKb([subject("a")], { a: [lecture("L1", ["x"])] }), "n1"), null);
  assert.equal(await findNoteLecture(fakeKb([], {}), "n1"), null);
  await assert.rejects(findNoteLecture(fakeKb([subject("a")], {}, { failSubjects: true }), "n1"));
  await assert.rejects(findNoteLecture(fakeKb([subject("a"), subject("b")], {}, { failOn: "b" }), "n1"));
  const api = fakeKb([subject("a")], {});
  assert.equal(await findNoteLecture(api, ""), null);
  assert.deepEqual(api.calls, []);
});

test("the note scan reads at most 101 subjects", async () => {
  const subjects = Array.from({ length: 300 }, (_, i) => subject(`s${i}`));
  const api = fakeKb(subjects, {});
  assert.equal(maxNoteScanSubjects, 101);
  assert.equal(await findNoteLecture(api, "n1"), null);
  assert.equal(api.calls.filter((c) => c[0] === "lectures").length, 101);
  // The 102nd subject is not reached even if it holds the note.
  const late = fakeKb(subjects, { s101: [lecture("L", ["n1"])] });
  assert.equal(await findNoteLecture(late, "n1"), null);
});

test("resolveOpening: a recording is read by its id with the place; a note opens its lecture and marks the file", async () => {
  const api = fakeKb([subject("a")], { a: [lecture("L1", ["n1"])] });
  const rec = await resolveOpening(api, { kind: "recording", recordingId: "r9", focus: { tab: "transcript", index: 4 } });
  assert.equal(rec.lesson.recordingId, "r9");
  assert.deepEqual(rec.focus, { tab: "transcript", index: 4 });
  assert.equal(rec.materialId, "");
  assert.deepEqual(api.calls, [["lesson", "r9"]]);

  const note = await resolveOpening(api, { kind: "note", noteId: "n1", materialId: "m1", locatorKind: "page", locatorN: 3 });
  assert.equal(note.lesson.key, "L1");
  assert.deepEqual(note.focus, noFocus);
  assert.equal(note.materialId, "m1");

  assert.equal(await resolveOpening(api, { kind: "note", noteId: "gone", materialId: "m1", locatorKind: "page", locatorN: 3 }), null);
  await assert.rejects(resolveOpening(fakeKb([], {}, { failLesson: true }), { kind: "recording", recordingId: "r9", focus: noFocus }));
  assert.equal(noteJumpTexts.notFound, "Конспект не найден: обновите список.");
});

test("the wiring: the panel and App pass the opening on; the page takes it once", () => {
  const read = (f) => readFileSync(new URL(`../src/${f}`, import.meta.url), "utf8");
  assert.match(read("agent/AgentPanel.tsx"), /answers,\s*openSource: props\.onOpenSource/);
  assert.match(read("agent/AgentPanel.tsx"), /answersApi\(callByName\)/);
  const app = read("App.tsx");
  assert.match(app, /routeToPage\("knowledge"\)/);
  assert.match(app, /onOpenSource=\{openSource\}/);
  assert.match(app, /setSourceJump\(undefined\)/);
  const page = read("pages/KnowledgeBasePage.tsx");
  assert.match(page, /resolveOpening\(api, pending\)/);
  assert.match(page, /jumpDone\.current\?\.\(\)/);
});
