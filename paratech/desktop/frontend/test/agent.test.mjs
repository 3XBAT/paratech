// The agent panel (TASK-120): bindings, the reducer of updates, context, Ctrl+K, the cards and plain-text rendering.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { agentApi, draftEvent, emptyFilter, methods, parseDraft, parseHistory, parseState, parseUndo, parseUndoRequest, parseUpdate, updateEvent } from "../.test-build/bindings/agent.js";
import {
  initialPanelState,
  orderedEntries,
  panelMode,
  reducePanel,
} from "../.test-build/agentState.js";
import {
  askInput,
  collectAnswers,
  composerKey,
  contextFor,
  entryModel,
  inputPlacement,
  isFocusPanelKey,
  panelShortcut,
  parseArtifact,
  requestText,
  roundCaption,
  skipAnswers,
  stepText,
  texts,
} from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

function card(over) {
  return {
    kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
    cannotDo: null, text: "", stopReason: "", ...over,
  };
}

function update(over) {
  return {
    threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
    stopReason: "", problem: "", cards: [], ...over,
  };
}

const scope = { text: "x", subjectId: "", lessonId: "", assignmentId: "", screen: "today" };
const ready = { link: "ready", readOnly: false };

function stateWith(...actionsList) {
  return actionsList.reduce(reducePanel, reducePanel(initialPanelState, { type: "link", link: ready }));
}

function modelOf(u) {
  return entryModel({ key: "k", seq: 1, threadId: u.threadId, turn: u.turn, text: "", scope: null, callProblem: "", update: u });
}

function html(u) {
  return renderToStaticMarkup(h(AgentRequest, { model: modelOf(u), actions }));
}

// ---- Bindings ----

test("bound method names match the Go AgentService", () => {
  // The journal methods (History, Undo, UndoRequest) are in agentjournal.go, the chat methods in agentchats.go.
  const go = ["agent.go", "agentjournal.go", "agentchats.go", "agentmentions.go"]
    .map((f) => readFileSync(new URL(`../../internal/input/wails/${f}`, import.meta.url), "utf8"))
    .join("\n");
  const goMethods = [...go.matchAll(/^func \(s \*AgentService\) ([A-Z]\w*)\(/gm)].map((m) => m[1]);
  const prefix = "paratech/desktop/internal/input/wails.AgentService.";
  const ts = Object.values(methods).map((m) => m.slice(prefix.length));
  assert.deepEqual([...ts].sort(), [...goMethods].sort());
  assert.equal(updateEvent, "agent:update");
});

test("the api sends the typed arguments and reads the problem of the reply", async () => {
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return method === methods.state ? { link: "no_key", readOnly: true } : { problem: "offline" };
  });
  assert.deepEqual(await api.state(), { link: "no_key", readOnly: true });
  assert.equal(await api.ask(scope), "offline");
  assert.equal(await api.approvePlan({ threadId: "t", turn: 2 }), "offline");
  assert.deepEqual(calls[1], [methods.ask, scope]);
  assert.deepEqual(calls[2], [methods.approvePlan, { threadId: "t", turn: 2 }]);
  assert.equal(await api.rerun({ threadId: "t", turn: 2 }), "offline");
  assert.deepEqual(calls[3], [methods.rerun, { threadId: "t", turn: 2 }]);
  assert.equal(methods.rerun, "paratech/desktop/internal/input/wails.AgentService.Rerun");
  const odd = agentApi(async () => ({ problem: "something new" }));
  assert.equal(await odd.stop({ threadId: "t", turn: 1 }), "failed");
});

test("history, undo and undoRequest call the binding with the new shapes and parse the replies", async () => {
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === methods.history) {
      return { problem: "", lines: [{ kind: "action", at: "2026-10-22T10:00:00Z", updatedAt: "", subjectId: "s1", actionId: "a1", threadId: "t1", turn: 2, tool: "create_subject", jobId: "", jobType: "", status: "DONE", undoable: true, cannotUndo: false, noUndo: "" }] };
    }
    if (method === methods.undo) {
      return { problem: "", actionId: "a1", status: "UNDO_REFUSED", refusal: "changed" };
    }
    return { problem: "failed", actions: [{ problem: "", actionId: "a2", status: "UNDONE", refusal: "" }] };
  });
  const filter = { ...emptyFilter, kind: "action", days: 7, undoable: true };
  const hist = await api.history(filter);
  assert.deepEqual(calls[0], [methods.history, filter]);
  assert.equal(hist.problem, "");
  assert.equal(hist.lines[0].tool, "create_subject");
  assert.equal(hist.lines[0].undoable, true);
  const undone = await api.undo("a1");
  assert.deepEqual(calls[1], [methods.undo, "a1"]);
  assert.deepEqual(undone, { problem: "", actionId: "a1", status: "UNDO_REFUSED", refusal: "changed" });
  const all = await api.undoRequest({ threadId: "t1", turn: 2 });
  assert.deepEqual(calls[2], [methods.undoRequest, { threadId: "t1", turn: 2 }]);
  assert.equal(all.problem, "failed");
  assert.equal(all.actions[0].status, "UNDONE");
});

test("history, undo and undoRequest replies are parsed defensively", () => {
  assert.deepEqual(parseHistory({ problem: "invalid", lines: null }), { problem: "invalid", lines: [] });
  assert.equal(parseHistory({ problem: "brand new", lines: [] }).problem, "failed");
  const line = parseHistory({ problem: "", lines: [{ kind: "processing" }] }).lines[0];
  assert.equal(line.kind, "processing");
  assert.equal(line.actionId, "");
  assert.equal(line.undoable, false);
  assert.equal(line.turn, 0);
  assert.throws(() => parseHistory({ problem: "", lines: [{ kind: 5 }] }));
  assert.throws(() => parseHistory(null));
  assert.deepEqual(parseUndo({ problem: "unavailable" }), { problem: "unavailable", actionId: "", status: "", refusal: "" });
  assert.equal(parseUndo({ problem: "busy" }).problem, "busy");
  assert.equal(parseUndo({ problem: "", refusal: "busy" }).refusal, "busy");
  assert.deepEqual(parseUndoRequest({ problem: "", actions: null }), { problem: "", actions: [] });
});

test("parseUpdate checks the shape, tolerates null lists and drops unknown cards", () => {
  const raw = {
    threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
    stopReason: "", problem: "",
    cards: [
      { kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "ok", stopReason: "" },
      { kind: "hologram" },
    ],
  };
  const u = parseUpdate(raw);
  assert.equal(u.cards.length, 1);
  assert.equal(u.progress, false); // an older core does not send it
  assert.equal(parseUpdate({ ...raw, progress: true, cards: [] }).progress, true);
  assert.equal(parseUpdate({ ...raw, cards: null }).cards.length, 0);
  assert.throws(() => parseUpdate({ ...raw, turn: "1" }));
  assert.throws(() => parseUpdate(null));
  assert.throws(() => parseState({ link: "weird", readOnly: false }));
});

test("parseDraft reads the draft event: ids, turn and texts as strings only", () => {
  assert.equal(draftEvent, "agent:draft");
  const raw = { threadId: "th1", turn: 2, callId: "c1", texts: ["Ряд сходится", "второе"] };
  assert.deepEqual(parseDraft(raw), raw);
  assert.deepEqual(parseDraft({ ...raw, texts: null }).texts, []);
  assert.throws(() => parseDraft({ ...raw, texts: ["ok", { html: "<b>x</b>" }] }));
  assert.throws(() => parseDraft({ ...raw, turn: "2" }));
  assert.throws(() => parseDraft({ ...raw, callId: 1 }));
  assert.throws(() => parseDraft(null));
});

test("parseUpdate reads the assignments of a reply; null or missing is an empty list, a broken line drops the card", () => {
  const reply = { kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "Два задания.", stopReason: "" };
  const lines = [
    { id: "a2", title: "Эссе", subjectId: "", due: "" },
    { id: "a1", title: "Лабораторная 2", subjectId: "s1", due: "2026-10-24" },
  ];
  const u = parseUpdate(update({ cards: [{ ...reply, assignments: lines }, { ...reply, assignments: null }, reply] }));
  assert.equal(u.cards.length, 3);
  assert.deepEqual(u.cards[0].assignments, lines);
  assert.deepEqual(u.cards[1].assignments, []);
  assert.deepEqual(u.cards[2].assignments, []);
  const broken = parseUpdate(update({ cards: [{ ...reply, assignments: [{ id: "a1", title: 5, subjectId: "", due: "" }] }] }));
  assert.equal(broken.cards.length, 0);
});

test("parseUpdate reads the assignment an action line added (catalog v6); null or missing is none, a broken one drops the card", () => {
  const line = { kind: "action_line", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "act1", answerId: "", cannotDo: null, text: "", stopReason: "" };
  const added = { id: "ag-1", title: "Скинуть проверочную Иванову", subjectId: "s1", due: "2026-10-23", time: "09:00" };
  const u = parseUpdate(update({ cards: [{ ...line, assignment: added }, { ...line, assignment: null }, line] }));
  assert.equal(u.cards.length, 3);
  assert.deepEqual(u.cards[0].assignment, added);
  assert.equal(u.cards[0].actionId, "act1");
  assert.equal(u.cards[1].assignment, null);
  assert.equal(u.cards[2].assignment, null); // an older core
  const broken = parseUpdate(update({ cards: [{ ...line, assignment: { ...added, time: 9 } }] }));
  assert.equal(broken.cards.length, 0);
});

// ---- Reducer ----

test("updates of one request replace each other; other requests stay", () => {
  const s = stateWith(
    { type: "update", update: update({ status: "PLANNING" }) },
    { type: "update", update: update({ threadId: "t2", status: "EXECUTING" }) },
    { type: "update", update: update({ status: "EXECUTING" }) },
  );
  assert.equal(s.entries.length, 2);
  assert.equal(s.entries.find((e) => e.threadId === "t1").update.status, "EXECUTING");
});

test("a finished request is not reopened by a late update of another operation", () => {
  const s = stateWith(
    { type: "update", update: update({ status: "DONE" }) },
    { type: "update", update: update({ status: "EXECUTING" }) },
  );
  assert.equal(s.entries[0].update.status, "DONE");
  // A stopped request can continue: that is a legitimate change.
  const t = stateWith(
    { type: "update", update: update({ status: "STOPPED", stopReason: "user" }) },
    { type: "update", update: update({ status: "EXECUTING" }) },
  );
  assert.equal(t.entries[0].update.status, "EXECUTING");
});

test("a failed Ask arrives with an empty threadId and is matched by scope", () => {
  const s = stateWith(
    { type: "asked", id: 1, text: "первое", scope: { ...scope, screen: "today" } },
    { type: "asked", id: 2, text: "второе", scope: { ...scope, screen: "settings" } },
    { type: "update", update: update({ threadId: "", turn: 0, screen: "settings", status: "", problem: "failed" }) },
  );
  const [first, second] = s.entries;
  assert.equal(first.update, null);
  assert.equal(second.update.problem, "failed");
  assert.equal(second.text, "второе");
  assert.equal(second.threadId, "");
});

test("a failed button operation keeps the plan and its buttons and only attaches the problem", () => {
  const plan = update({ status: "AWAITING_PLAN", cards: [card({ kind: "plan", plan: { summary: "s", steps: [], notes: [] } })] });
  const s = stateWith(
    { type: "update", update: plan },
    { type: "update", update: update({ status: "", cards: [], problem: "invalid" }) },
  );
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].update.status, "AWAITING_PLAN");
  const m = entryModel(s.entries[0]);
  assert.equal(m.cards[0].type, "plan");
  assert.deepEqual(m.cards[0].buttons.map((b) => b.id), ["approve", "revise", "cancel"]);
  assert.equal(m.cards.at(-1).type, "problem");
  // The next real update clears the problem.
  const next = reducePanel(s, { type: "update", update: update({ status: "EXECUTING" }) });
  assert.equal(next.entries[0].update.problem, "");
});

test("a refused button on a stopped request shows the stop card and the problem card", () => {
  const s = stateWith(
    { type: "update", update: update({ status: "STOPPED", stopReason: "offline" }) },
    { type: "update", update: update({ status: "", cards: [], problem: "budget" }) },
  );
  const types = entryModel(s.entries[0]).cards.map((c) => c.type);
  assert.deepEqual(types.slice(-2), ["stopped", "problem"]);
});

test("the first update of a sent request binds to it by scope, and the next ones by thread", () => {
  const s = stateWith(
    { type: "asked", id: 1, text: "вопрос", scope },
    { type: "update", update: update({ status: "PLANNING" }) },
    { type: "update", update: update({ status: "AWAITING_PLAN" }) },
  );
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].text, "вопрос");
  assert.equal(s.entries[0].threadId, "t1");
  assert.equal(s.entries[0].update.status, "AWAITING_PLAN");
});

test("updates arriving in reverse order keep the thread order by turn", () => {
  const s = stateWith(
    { type: "update", update: update({ turn: 2 }) },
    { type: "update", update: update({ turn: 1 }) },
    { type: "update", update: update({ threadId: "t0", turn: 1 }) },
  );
  assert.deepEqual(orderedEntries(s.entries).map((e) => `${e.threadId}:${e.turn}`), ["t1:1", "t1:2", "t0:1"]);
});

test("a refused Ask marks the request; an accepted one changes nothing", () => {
  const sent = stateWith({ type: "asked", id: 7, text: "q", scope });
  assert.equal(reducePanel(sent, { type: "askResult", id: 7, problem: "" }), sent);
  const failed = reducePanel(sent, { type: "askResult", id: 7, problem: "offline" });
  assert.equal(failed.entries[0].callProblem, "offline");
  assert.equal(modelOf(update()).callLine, "");
  assert.equal(entryModel(failed.entries[0]).callLine, "Нет связи с paratech.");
});

// ---- Context and Ctrl+K ----

test("the context label follows the screen and is removable down to «Без предмета»", () => {
  const home = contextFor("today", "", false);
  assert.equal(home.label, "Сегодня");
  assert.equal(home.ask.screen, "today");
  assert.equal(home.removable, true);
  const hw = contextFor("homework", "a1", false);
  assert.equal(hw.label, "Задания");
  assert.deepEqual(hw.ask, { subjectId: "", lessonId: "", assignmentId: "a1", screen: "assignments" });
  assert.equal(contextFor("diagnostics", "", false).ask.screen, "settings");
  const none = contextFor("homework", "a1", true);
  assert.equal(none.label, "Без предмета");
  assert.equal(none.removable, false);
  assert.equal(none.ask.assignmentId, "");
  assert.equal(askInput(none, "что задали").text, "что задали");
});

test("Ctrl+K is read by the physical key, so the Russian layout works; other keys do not", () => {
  const key = (over) => ({ key: "k", code: "KeyK", ctrlKey: true, metaKey: false, altKey: false, shiftKey: false, ...over });
  assert.equal(isFocusPanelKey(key({})), true);
  assert.equal(isFocusPanelKey(key({ key: "K" })), true);
  assert.equal(isFocusPanelKey(key({ key: "л" })), true); // Russian layout: key is «л», code stays KeyK
  assert.equal(isFocusPanelKey(key({ metaKey: true, ctrlKey: false })), true);
  assert.equal(isFocusPanelKey(key({ ctrlKey: false })), false);
  assert.equal(isFocusPanelKey(key({ key: "j", code: "KeyJ" })), false);
  assert.equal(isFocusPanelKey(key({ key: "k", code: "KeyL" })), false);
  assert.equal(isFocusPanelKey(key({ shiftKey: true })), false);
  assert.equal(isFocusPanelKey(key({ altKey: true })), false);
  assert.equal(requestText("  \n "), null);
  assert.equal(requestText(" привет "), "привет");
});

// ---- Plan card ----

test("plan card: caption, steps in impersonal words, buttons only while the plan awaits an answer", () => {
  const plan = {
    summary: "Создать предмет и разложить файл",
    steps: [
      { tool: "create_subject", args: [{ name: "name", type: "label", idKind: "", value: "Матан", values: [] }], deferred: [] },
      { tool: "place_upload", args: [{ name: "upload", type: "id", idKind: "upload", value: "u1", values: [] }], deferred: ["subject"] },
      { tool: "edit_due", args: [{ name: "due", type: "date", idKind: "", value: "2026-11-03", values: [] }], deferred: [] },
    ],
    notes: ["Файл останется во «Входящих»"],
  };
  const m = modelOf(update({ status: "AWAITING_PLAN", cards: [card({ kind: "plan", plan })] })).cards[0];
  assert.equal(m.type, "plan");
  assert.equal(m.caption, "Понял так");
  assert.deepEqual(m.steps, [
    "Создание предмета · Матан",
    "Размещение файла · файл · после предыдущих шагов",
    "Изменение срока · срок 03.11.2026",
  ]);
  assert.deepEqual(m.buttons.map((b) => b.label), ["Да", "Изменить", "Отмена"]);
  const later = modelOf(update({ status: "EXECUTING", cards: [card({ kind: "plan", plan })] })).cards[0];
  assert.deepEqual(later.buttons, []);
  assert.equal(stepText({ tool: "new_tool", args: [], deferred: [] }), "Шаг");
  const out = html(update({ status: "AWAITING_PLAN", cards: [card({ kind: "plan", plan })] }));
  assert.match(out, /Понял так/);
  assert.match(out, />Да</);
});

// ---- Question card ----

test("question card: round N of 2, skip text, defaults and typed answers", () => {
  const questions = {
    round: 2,
    questions: [
      { kind: "choice", text: "Какой формат?", options: ["Word", "Текст"], default: 1, citeRef: "" },
      { kind: "text", text: "Сколько страниц?", options: [], default: 0, citeRef: "" },
    ],
  };
  const u = update({ status: "AWAITING_ANSWERS", cards: [card({ kind: "questions", questions })] });
  const m = modelOf(u).cards[0];
  assert.equal(m.caption, roundCaption(2));
  assert.equal(m.caption, "Уточнение · раунд 2 из 2");
  assert.equal(m.skip, "Пропустить — сделай как считаешь");
  assert.equal(m.active, true);
  assert.deepEqual(skipAnswers(questions.questions), [
    { question: 0, option: 1, text: "" },
    { question: 1, option: 0, text: texts.skipTextAnswer },
  ]);
  assert.equal(collectAnswers(questions.questions, {}, {}), null);
  assert.deepEqual(collectAnswers(questions.questions, { 0: 0 }, { 1: " 5 " }), [
    { question: 0, option: 0, text: "" },
    { question: 1, option: 0, text: "5" },
  ]);
  const out = html(u);
  assert.match(out, /Пропустить — сделай как считаешь/);
  assert.match(out, /Какой формат\?/);
  assert.doesNotMatch(html({ ...u, status: "EXECUTING" }), /Пропустить/);
});

test("question card: default -1 preselects nothing, blocks submit, skip takes the last option", () => {
  const subject = {
    kind: "choice",
    text: "К какому предмету относится задание?",
    options: ["Матан", "Физика", "Без предмета"],
    default: -1,
    citeRef: "",
  };
  const normal = { kind: "choice", text: "Какой формат?", options: ["Word", "Текст"], default: 1, citeRef: "" };
  const qs = [subject, normal];
  assert.equal(collectAnswers(qs, {}, {}), null);
  assert.deepEqual(collectAnswers(qs, { 0: 1 }, {}), [
    { question: 0, option: 1, text: "" },
    { question: 1, option: 1, text: "" },
  ]);
  assert.deepEqual(skipAnswers(qs), [
    { question: 0, option: 2, text: "" },
    { question: 1, option: 1, text: "" },
  ]);
  const u = update({ status: "AWAITING_ANSWERS", cards: [card({ kind: "questions", questions: { round: 1, questions: [subject] } })] });
  const out = html(u);
  assert.doesNotMatch(out, /checked/);
  assert.match(out, /Без предмета/);
});

// ---- Progress card ----

test("progress card: shown while working, with «Стоп» (TASK-143: also while planning)", () => {
  const u = update({ status: "EXECUTING", cards: [card({ kind: "progress", jobId: "j1" })] });
  const m = modelOf(u).cards;
  assert.equal(m.length, 1);
  assert.deepEqual(m[0].buttons.map((b) => b.label), ["Стоп"]);
  const planning = modelOf(update({ status: "PLANNING" })).cards;
  assert.equal(m[0].type, "progress");
  assert.equal(planning.length, 1);
  assert.equal(planning[0].type, "waiting");
  assert.deepEqual(planning[0].buttons.map((b) => b.label), ["Стоп"]);
  assert.match(html(u), /Работа идёт/);
});

// ---- Waiting indicator (TASK-139) ----

const pending = (over) => ({ key: "k", seq: 1, threadId: "", turn: 0, text: "q", scope: null, callProblem: "", update: null, ...over });

test("waiting: shown while the call runs, hidden on refusal and on any card that ends the wait", () => {
  const running = entryModel(pending({}));
  assert.equal(running.waiting, true);
  assert.equal(running.callLine, "");
  const refused = entryModel(pending({ callProblem: "offline" }));
  assert.equal(refused.waiting, false);
  assert.equal(refused.callLine, "Нет связи с paratech.");
  const ends = [
    update({ status: "AWAITING_PLAN", cards: [card({ kind: "plan", plan: { summary: "s", steps: [], notes: [] } })] }),
    update({ status: "AWAITING_ANSWERS", cards: [card({ kind: "questions", questions: { round: 1, questions: [] } })] }),
    update({ cards: [card({ kind: "reply", text: "ok" })] }),
    update({ status: "STOPPED", stopReason: "user" }),
    update({ status: "FAILED", problem: "failed" }),
    update({ status: "DONE" }),
  ];
  for (const u of ends) {
    assert.equal(modelOf(u).waiting, false, u.status);
    assert.doesNotMatch(html(u), /ag-wdot/);
  }
});

test("waiting: job-less PLANNING and EXECUTING give a waiting card, both keep «Стоп» (TASK-143)", () => {
  const planning = modelOf(update({ status: "PLANNING" })).cards;
  assert.deepEqual(planning, [{ type: "waiting", buttons: [{ id: "stop", label: "Стоп" }] }]);
  const executing = modelOf(update({ status: "EXECUTING" })).cards;
  assert.deepEqual(executing.map((c) => c.type), ["waiting"]);
  assert.deepEqual(executing[0].buttons.map((b) => b.label), ["Стоп"]);
  const out = html(update({ status: "EXECUTING" }));
  assert.match(out, />Стоп</);
  assert.doesNotMatch(out, /Работа идёт/);
});

test("waiting: three dots, role=status, hidden text, no visible «Отправка…»", () => {
  const out = renderToStaticMarkup(h(AgentRequest, { model: entryModel(pending({})), actions }));
  assert.match(out, /role="status"/);
  assert.match(out, /Запрос выполняется/);
  assert.equal((out.match(/class="ag-wdot"/g) ?? []).length, 3);
  assert.doesNotMatch(out, /Отправка…|Работа идёт|ag-callline/);
  const card = html(update({ status: "PLANNING" }));
  assert.match(card, /Запрос выполняется/);
  assert.doesNotMatch(card, /Работа идёт/);
});

test("waiting css: staggered breath, static and visible under reduced motion", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /\.ag-wdot\s*\{[^}]*var\(--accent-fill\)[^}]*animation: ag-breath/);
  assert.match(css, /\.ag-wdot:nth-child\(3\)\s*\{\s*animation-delay/);
  assert.match(css, /prefers-reduced-motion: reduce\)\s*\{\s*\.ag-wdot\s*\{\s*animation: none; opacity: 1;/);
});

// ---- Result card ----

test("result card: the artifact reference becomes a label, never a path to open", () => {
  assert.deepEqual(parseArtifact("exports/hw/task-1.docx"), { kind: "Файл", name: "task-1.docx" });
  assert.deepEqual(parseArtifact("transcripts/2026/a.txt"), { kind: "Расшифровка", name: "a.txt" });
  for (const bad of ["", "exports", "exports/", "models/x.bin", "exports/../secret", "exports//x", "exports/C:/x", "exports/a\\b", "/etc/passwd", "exports/\u0000x", "http://evil/x"]) {
    assert.equal(parseArtifact(bad), null, bad);
  }
  const ok = html(update({ cards: [card({ kind: "result", artifact: "exports/hw/task-1.docx" })] }));
  assert.match(ok, /task-1\.docx/);
  assert.doesNotMatch(ok, /<a |href="[^#]/);
  const bad = html(update({ cards: [card({ kind: "result", artifact: "exports/../x" })] }));
  assert.match(bad, /Файл недоступен для показа/);
});

// ---- Answer, reply, cannot do ----

test("answer card says the answer is ready without inventing its content", () => {
  const out = html(update({ cards: [card({ kind: "answer", answerId: "ans1" })] }));
  assert.match(out, /Ответ по материалам/);
  assert.doesNotMatch(out, /ans1/);
});

test("cannot-do card: «Целиком не получится» with both parts", () => {
  const out = html(update({
    cards: [card({ kind: "cannot_do", cannotDo: { yourPart: ["Решить задачу 3"], ourPart: ["Оформить файл"] } })],
  }));
  assert.match(out, /Целиком не получится/);
  assert.match(out, /Решить задачу 3/);
  assert.match(out, /Оформить файл/);
});

// ---- Action line and confirm ----

test("action line: «Отменить» is enabled for a named action and calls the binding", () => {
  const m = modelOf(update({ cards: [card({ kind: "action_line", actionId: "a1" })] })).cards[0];
  assert.equal(m.type, "action");
  assert.equal(m.actionId, "a1");
  assert.equal(m.undo.enabled, true);
  const out = html(update({ cards: [card({ kind: "action_line", actionId: "a1" })] }));
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Отменить</);
  assert.doesNotMatch(out, /Пока недоступно/);
  const none = html(update({ cards: [card({ kind: "action_line", actionId: "" })] }));
  assert.match(none, /<button[^>]*disabled[^>]*>Отменить</);
});

test("action line after «Отменить»: done hides the button, a refusal stays as text, busy disables it", () => {
  const u = update({ cards: [card({ kind: "action_line", actionId: "a1" })] });
  const withNote = (note) =>
    renderToStaticMarkup(h(AgentRequest, { model: modelOf(u), actions: { ...actions, undoNote: () => note } }));
  const done = withNote({ busy: false, done: true, text: "" });
  assert.match(done, /Отменено/);
  assert.doesNotMatch(done, /<button/);
  const refused = withNote({ busy: false, done: false, text: "Изменено после — отменить нельзя." });
  assert.match(refused, /Изменено после — отменить нельзя\./);
  assert.match(refused, />Отменить</);
  const busy = withNote({ busy: true, done: false, text: "" });
  assert.match(busy, /<button[^>]*disabled[^>]*>Отмена…</);
});

test("«Отменить всё» appears with two or more actions to undo and calls undoAll", () => {
  const two = update({ cards: [card({ kind: "action_line", actionId: "a1" }), card({ kind: "action_line", actionId: "a2" })] });
  const out = renderToStaticMarkup(h(AgentRequest, { model: modelOf(two), actions }));
  assert.match(out, />Отменить всё</);
  const one = html(update({ cards: [card({ kind: "action_line", actionId: "a1" })] }));
  assert.doesNotMatch(one, /Отменить всё/);
  const allDone = renderToStaticMarkup(
    h(AgentRequest, { model: modelOf(two), actions: { ...actions, undoNote: () => ({ busy: false, done: true, text: "" }) } }),
  );
  assert.doesNotMatch(allDone, /Отменить всё/);
});

test("confirm card without the confirm calls offers no buttons", () => {
  const out = html(update({ cards: [card({ kind: "confirm", target: { kind: "proposal", id: "p1" } })] }));
  assert.match(out, /Нужно подтверждение/);
  assert.doesNotMatch(out, /<button/);
});

// ---- Stop reasons ----

test("stop reasons: offline continues, failed retries, expired restarts, budget has no button", () => {
  const stop = (reason, over = {}) => modelOf(update({ status: "STOPPED", stopReason: reason, ...over })).cards.at(-1);
  const off = stop("offline");
  assert.equal(off.type, "stopped");
  assert.equal(off.text, "Не завершено: нет связи");
  assert.deepEqual(off.buttons.map((b) => b.label), ["Продолжить", "Отмена"]);
  assert.deepEqual(stop("failed").buttons.map((b) => b.label), ["Повторить"]);
  assert.deepEqual(stop("expired").buttons.map((b) => b.label), ["Начать заново"]);
  assert.deepEqual(stop("budget").buttons, []);
  assert.deepEqual(stop("user").buttons.map((b) => b.label), ["Продолжить", "Отмена"]);
  assert.deepEqual(stop("quota").buttons.map((b) => b.label), ["Продолжить", "Отмена"]);
  // A stopped card and the update's reason are one card, not two.
  const both = modelOf(update({ status: "STOPPED", stopReason: "offline", cards: [card({ kind: "stopped", stopReason: "offline" })] }));
  assert.equal(both.cards.filter((c) => c.type === "stopped").length, 1);
  const out = html(update({ status: "STOPPED", stopReason: "offline" }));
  assert.match(out, /Не завершено: нет связи/);
  assert.match(out, />Продолжить</);
});

test("update problems explain themselves and offer the next step", () => {
  const p = (problem) => modelOf(update({ status: "", problem })).cards.at(-1);
  assert.deepEqual(p("retry_required").buttons.map((b) => b.label), ["Повторить"]);
  assert.deepEqual(p("restart_required").buttons.map((b) => b.label), ["Начать заново"]);
  assert.equal(p("busy").type, "problem");
  assert.equal(p("something unknown").text, "Не удалось выполнить просьбу.");
});

// ---- Offline ----

function panelHtml(state, over = {}) {
  return renderToStaticMarkup(
    h(AgentPanelView, {
      state, ctx: contextFor("today", "", false), draft: "", notice: "", inputRef: { current: null },
      onDraft: noop, onSubmit: noop, onRemoveContext: noop, actionsFor: () => actions, ...over,
    }),
  );
}

test("offline: input disabled, «Нет связи с paratech», last thread visible with the stale note", () => {
  const withThread = reducePanel(
    reducePanel(initialPanelState, { type: "link", link: { link: "no_key", readOnly: true } }),
    { type: "update", update: update({ cards: [card({ kind: "reply", text: "Последний ответ" })] }) },
  );
  const mode = panelMode(withThread);
  assert.equal(mode.inputEnabled, false);
  assert.equal(mode.offline, true);
  assert.equal(mode.placeholder, "Нет связи с paratech");
  assert.equal(mode.staleNote, "нет связи — может быть неактуально");
  const out = panelHtml(withThread);
  assert.match(out, /<textarea[^>]*disabled/);
  assert.match(out, /Нет связи с paratech/);
  assert.match(out, /нет связи — может быть неактуально/);
  assert.match(out, /Последний ответ/);
  // Without a thread there is nothing to call outdated.
  assert.equal(panelMode(reducePanel(initialPanelState, { type: "link", link: { link: "no_hub", readOnly: true } })).staleNote, "");
});

test("a request stopped for lack of connection also reads as offline until it continues", () => {
  const s = stateWith({ type: "update", update: update({ status: "STOPPED", stopReason: "offline" }) });
  assert.equal(panelMode(s).inputEnabled, false);
  const back = reducePanel(s, { type: "update", update: update({ status: "EXECUTING" }) });
  assert.equal(panelMode(back).inputEnabled, true);
});

test("a local offline refusal does not lock the input", () => {
  const s = reducePanel(stateWith({ type: "asked", id: 3, text: "q", scope }), { type: "askResult", id: 3, problem: "offline" });
  assert.equal(panelMode(s).inputEnabled, true);
});

test("online: the input is enabled; before State answers it is not", () => {
  const out = panelHtml(stateWith());
  assert.doesNotMatch(out, /<textarea[^>]*disabled/);
  assert.match(out, /maxLength="4000"|maxlength="4000"/);
  assert.match(out, /Спросить paratech/);
  assert.match(out, /Ctrl\+K/);
  assert.equal(panelMode(initialPanelState).inputEnabled, false);
});

test("send is an icon button with an accessible name; disabled while the draft is empty or offline", () => {
  const empty = panelHtml(stateWith());
  assert.match(empty, /<button[^>]*class="ag-send"[^>]*aria-label="Отправить"[^>]*title="Отправить"[^>]*disabled=""[^>]*><svg/);
  assert.doesNotMatch(empty, />Отправить</);
  const ready = panelHtml(stateWith(), { draft: "привет" });
  assert.match(ready, /<button(?![^>]*disabled)[^>]*class="ag-send"/);
  const offline = panelHtml(reducePanel(initialPanelState, { type: "link", link: { link: "no_key", readOnly: true } }), { draft: "привет" });
  assert.match(offline, /<button[^>]*class="ag-send"[^>]*disabled/);
});

test("the context chip shows the label and the remove button only while removable", () => {
  const out = panelHtml(stateWith());
  assert.match(out, /Сегодня/);
  assert.match(out, /aria-label="Убрать контекст"/);
  const none = panelHtml(stateWith(), { ctx: contextFor("today", "", true) });
  assert.match(none, /Без предмета/);
  assert.doesNotMatch(none, /Убрать контекст/);
});

test("Enter sends, Shift+Enter breaks the line, Enter inside IME composition does neither", () => {
  assert.equal(composerKey({ key: "Enter", shiftKey: false }), "send");
  assert.equal(composerKey({ key: "Enter", shiftKey: true }), "none");
  assert.equal(composerKey({ key: "Enter", shiftKey: false, isComposing: true }), "none");
  assert.equal(composerKey({ key: "Enter", shiftKey: false, isComposing: false, keyCode: 229 }), "none");
  assert.equal(composerKey({ key: "Enter", shiftKey: false, keyCode: 13 }), "send");
  assert.equal(composerKey({ key: "a", shiftKey: false }), "none");
});

test("the second Ctrl+K closes an open panel, the first opens a closed one", () => {
  assert.equal(panelShortcut(false), "open");
  assert.equal(panelShortcut(true), "close");
});

test("the request field is on top before the first request and at the bottom after it", () => {
  assert.equal(inputPlacement(0), "top");
  assert.equal(inputPlacement(1), "bottom");
  const empty = panelHtml(stateWith());
  assert.match(empty, /class="ag ag-in-top"/);
  const asked = panelHtml(stateWith({ type: "asked", id: 1, text: "q", scope }));
  assert.match(asked, /class="ag ag-in-bottom"/);
  // One textarea, not an input: Enter/Shift+Enter need a multi-line field.
  assert.match(asked, /<textarea[^>]*class="ag-input"/);
  assert.doesNotMatch(asked, /<input[^>]*ag-input/);
});

// ---- Plain text ----

test("model text is rendered as plain text, never as markup", () => {
  const evil = `<img src=x onerror=alert(1)><script>alert(2)</script> <b>жирный</b> & "кавычки"`;
  const u = update({
    status: "AWAITING_PLAN",
    cards: [
      card({ kind: "reply", text: evil }),
      card({
        kind: "plan",
        plan: {
          summary: evil,
          steps: [{ tool: "create_subject", args: [{ name: "n", type: "label", idKind: "", value: evil, values: [] }], deferred: [] }],
          notes: [evil],
        },
      }),
      card({
        kind: "questions",
        questions: { round: 1, questions: [{ kind: "choice", text: evil, options: [evil], default: 0, citeRef: "" }] },
      }),
      card({ kind: "cannot_do", cannotDo: { yourPart: [evil], ourPart: [evil] } }),
    ],
  });
  const out = html(u);
  for (const tag of ["<img", "<script", "<b>"]) {
    assert.ok(!out.includes(tag), `${tag} must be escaped`);
  }
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(out.includes("&lt;script&gt;"));
  const userText = renderToStaticMarkup(
    h(AgentRequest, {
      model: entryModel({ key: "l", seq: 1, threadId: "", turn: 0, text: evil, scope, callProblem: "", update: null }),
      actions,
    }),
  );
  assert.ok(!userText.includes("<img"));
});

test("the panel code never builds markup from text", () => {
  // homework.test.mjs bans the HTML-writing APIs in every source file, these included; here only that they are scanned.
  const dir = new URL("../src/agent/", import.meta.url);
  const files = readdirSync(dir).filter((f) => f.endsWith(".tsx"));
  assert.ok(files.length >= 3);
  for (const f of files) {
    assert.doesNotMatch(readFileSync(new URL(f, dir), "utf8"), /DOMParser|document\.write|createContextualFragment/, f);
  }
});
