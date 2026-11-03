// «Остановить» at the waiting line and «Повторить» at the last request (TASK-143): progress merge, who offers what,
// the busy-refused request and the double-click guard.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  blockingEntry,
  initialPanelState,
  lastEntry,
  orderedEntries,
  PressTracker,
  reducePanel,
} from "../.test-build/agentState.js";
import { entryModel, texts } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

function card(over) {
  return {
    kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
    cannotDo: null, text: "", stopReason: "", workId: "", outcome: "", ...over,
  };
}

function update(over) {
  return {
    threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
    stopReason: "", problem: "", progress: false, cards: [], ...over,
  };
}

const last = { last: true, blocked: false };
const entryOf = (u, over = {}) => ({ key: "k", seq: 1, threadId: u.threadId, turn: u.turn, text: "", scope: null, callProblem: "", update: u, ...over });
const modelOf = (u, ctx = last) => entryModel(entryOf(u), ctx);
const buttonIds = (m) => m.cards.flatMap((c) => ("buttons" in c ? c.buttons.map((b) => b.id) : []));

// ---- progress merge ----

test("an update with progress changes only the status and the stop reason; the cards stay", () => {
  const plan = card({ kind: "reply", text: "ответ" });
  let s = reducePanel(initialPanelState, { type: "update", update: update({ status: "PLANNING", cards: [plan] }) });
  s = reducePanel(s, { type: "update", update: update({ status: "EXECUTING", progress: true, problem: "failed", cards: [] }) });
  const u = s.entries[0].update;
  assert.equal(s.entries.length, 1);
  assert.equal(u.status, "EXECUTING");
  assert.equal(u.cards.length, 1);
  assert.equal(u.problem, "", "problem is not moved by a progress update");
  s = reducePanel(s, { type: "update", update: update({ status: "STOPPED", stopReason: "user", progress: true }) });
  assert.equal(s.entries[0].update.stopReason, "user");
  assert.equal(s.entries[0].update.cards.length, 1);
});

test("a progress update of an unknown request is shown as it is", () => {
  const s = reducePanel(initialPanelState, { type: "update", update: update({ status: "EXECUTING", progress: true }) });
  assert.equal(s.entries.length, 1);
});

test("a running request shows neither the old stopped card nor the old problem", () => {
  const stopped = card({ kind: "stopped", stopReason: "user" });
  let s = reducePanel(initialPanelState, { type: "update", update: update({ status: "STOPPED", stopReason: "user", cards: [stopped] }) });
  s = reducePanel(s, { type: "update", update: update({ status: "EXECUTING", progress: true }) });
  const m = entryModel(s.entries[0], last);
  assert.equal(m.cards.some((c) => c.type === "stopped"), false);
  assert.deepEqual(buttonIds(m), ["stop"]);

  s = reducePanel(initialPanelState, { type: "update", update: update({ status: "FAILED", problem: "retry_required" }) });
  s = reducePanel(s, { type: "update", update: update({ status: "PLANNING", progress: true }) });
  assert.equal(entryModel(s.entries[0], last).cards.some((c) => c.type === "problem"), false);
});

test("a problem-only update on a running request shows the problem; a progress update clears it", () => {
  for (const problem of ["failed", "stale"]) {
    let s = reducePanel(initialPanelState, { type: "update", update: update({ status: "EXECUTING", cards: [card()] }) });
    s = reducePanel(s, { type: "update", update: update({ status: "", problem }) });
    assert.equal(entryModel(s.entries[0], last).cards.some((c) => c.type === "problem"), true, problem);
    s = reducePanel(s, { type: "update", update: update({ status: "EXECUTING", progress: true }) });
    assert.equal(s.entries[0].update.problem, "");
    assert.equal(entryModel(s.entries[0], last).cards.some((c) => c.type === "problem"), false);
  }
});

test("a successful rerun clears the old problem of its source", () => {
  let s = reducePanel(initialPanelState, { type: "update", update: update({ status: "DONE", problem: "rerun_blocked" }) });
  s = reducePanel(s, { type: "rerunAsked", from: { threadId: "t1", turn: 1, text: "x", scope: null } });
  assert.equal(s.entries.find((e) => e.key === "thread:t1:1").update.problem, "");
});

test("history fills an entry that still awaits its first update", () => {
  let s = reducePanel(initialPanelState, { type: "rerunAsked", from: { threadId: "t1", turn: 1, text: "x", scope: null } });
  s = reducePanel(s, { type: "history", requests: [update({ turn: 2, status: "PLANNING" })] });
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].update.status, "PLANNING");
});

// ---- Стоп ----

test("«Остановить» is offered at the waiting line only while PLANNING or EXECUTING", () => {
  for (const status of ["PLANNING", "EXECUTING"]) {
    const m = modelOf(update({ status }));
    assert.deepEqual(buttonIds(m), ["stop"], status);
    assert.equal(m.cards[0].type, "waiting");
  }
  for (const status of ["AWAITING_PLAN", "AWAITING_ANSWERS", "DONE", "FAILED", "CANCELLED", "STOPPED"]) {
    assert.ok(!buttonIds(modelOf(update({ status }))).includes("stop"), status);
  }
});

test("«Остановить» is a real quiet icon button; the stop card follows a stopped request", () => {
  const html = renderToStaticMarkup(h(AgentRequest, { model: modelOf(update({ status: "EXECUTING" })), actions }));
  // TASK-368: the stop icon, named by the aria-label and the tooltip (no visible word).
  assert.match(html, /<button[^>]*class="ia"[^>]*aria-label="Остановить"[^>]*><svg[^>]*><use href="#i-stop"><\/use><\/svg><span class="ia-tip" aria-hidden="true">Остановить<\/span><\/button>/);
  const stopped = modelOf(update({ status: "STOPPED", stopReason: "user" }));
  assert.deepEqual(buttonIds(stopped), ["resume", "cancel"]);
  assert.equal(stopped.cards[0].text, "Остановлено. Можно продолжить.");
});

test("the buttons are off while a call runs", () => {
  const html = renderToStaticMarkup(h(AgentRequest, { model: modelOf(update({ status: "EXECUTING" })), actions: { ...actions, busy: true } }));
  assert.match(html, /<button[^>]*aria-label="Остановить"[^>]*disabled=""/);
});

// ---- Повторить ----

test("«Повторить» for DONE, FAILED, CANCELLED and STOPPED of the last request", () => {
  for (const status of ["DONE", "FAILED", "CANCELLED", "STOPPED"]) {
    assert.equal(modelOf(update({ status })).rerun, true, status);
  }
  for (const status of ["PLANNING", "AWAITING_PLAN", "AWAITING_ANSWERS", "EXECUTING"]) {
    assert.equal(modelOf(update({ status })).rerun, false, status);
  }
});

test("the new-request «Повторить» is not offered where the stop card offers the next step", () => {
  assert.equal(modelOf(update({ status: "FAILED", stopReason: "failed" })).rerun, false);
  assert.equal(modelOf(update({ status: "STOPPED", stopReason: "expired" })).rerun, false);
  assert.equal(modelOf(update({ status: "FAILED", problem: "retry_required" })).rerun, false);
  assert.equal(modelOf(update({ status: "FAILED", problem: "restart_required" })).rerun, false);
  // After rerun_blocked it stays (the core checks again) and the reason stays shown.
  const blocked = modelOf(update({ status: "STOPPED", problem: "rerun_blocked" }));
  assert.equal(blocked.rerun, true);
  assert.match(blocked.cards.find((c) => c.type === "problem").text, /Повтор недоступен/);
  // The stop reasons whose stop card offers «Повторить» (id continue) never get a second one.
  for (const stopReason of ["offline", "quota"]) {
    assert.equal(modelOf(update({ status: "STOPPED", stopReason })).rerun, false, stopReason);
  }
  // «Остановить» offers «Продолжить» (resume): the rerun, another attempt, stays. Budget offers nothing.
  for (const stopReason of ["user", "budget"]) {
    assert.equal(modelOf(update({ status: "STOPPED", stopReason })).rerun, true, stopReason);
  }
});

test("never two «Повторить» under one request: the stop card's button and the rerun exclude each other", () => {
  const variants = [
    { status: "STOPPED", stopReason: "user" },
    { status: "STOPPED", stopReason: "offline" },
    { status: "STOPPED", stopReason: "quota" },
    { status: "STOPPED", stopReason: "offline", problem: "retry_required" },
    { status: "STOPPED", stopReason: "budget" },
    { status: "FAILED", stopReason: "failed" },
    { status: "STOPPED", stopReason: "expired" },
    { status: "FAILED", problem: "retry_required" },
    { status: "FAILED", problem: "restart_required" },
    { status: "STOPPED", problem: "rerun_blocked" },
    { status: "DONE" },
  ];
  for (const v of variants) {
    const m = modelOf(update(v));
    const labels = m.cards.flatMap((c) => ("buttons" in c ? c.buttons.map((b) => b.label) : []));
    const repeats = labels.filter((l) => l === "Повторить").length + (m.rerun ? 1 : 0);
    assert.ok(repeats <= 1, JSON.stringify(v));
  }
  // The stop card's button is the one shown, and it is still the continue call.
  const stopped = modelOf(update({ status: "STOPPED", stopReason: "offline" }));
  assert.deepEqual(stopped.cards.at(-1).buttons.map((b) => [b.id, b.label]), [["continue", "Повторить"], ["cancel", "Отменить запрос"]]);
  assert.equal(stopped.rerun, false);
  const html = renderToStaticMarkup(h(AgentRequest, { model: stopped, actions }));
  assert.equal(html.split(">Повторить<").length - 1, 1);
  assert.doesNotMatch(html, /Повторить ответ/);
  // A retry_required problem on a stopped-offline card shows one «Повторить» (the problem's retry), not two.
  const withProblem = modelOf(update({ status: "STOPPED", stopReason: "offline", problem: "retry_required" }));
  assert.deepEqual(withProblem.cards.flatMap((c) => ("buttons" in c ? c.buttons.map((b) => b.id) : [])), ["cancel", "retry"]);
  assert.equal(withProblem.rerun, false);
  assert.equal(renderToStaticMarkup(h(AgentRequest, { model: withProblem, actions })).split(">Повторить<").length - 1, 1);
});

test("«Остановить» offers «Продолжить» and keeps the rerun under the last request", () => {
  const m = modelOf(update({ status: "STOPPED", stopReason: "user" }));
  assert.deepEqual(m.cards.at(-1).buttons.map((b) => [b.id, b.label]), [["resume", "Продолжить"], ["cancel", "Отменить запрос"]]);
  assert.equal(m.rerun, true);
  const html = renderToStaticMarkup(h(AgentRequest, { model: m, actions }));
  assert.equal(html.split(">Повторить<").length - 1, 0);
  assert.match(html, /aria-label="Повторить ответ"/);
});

test("only the last request offers «Повторить»", () => {
  const s = [update({ turn: 1 }), update({ turn: 2 })].reduce((acc, u) => reducePanel(acc, { type: "update", update: u }), initialPanelState);
  const entries = orderedEntries(s.entries);
  assert.equal(lastEntry(s.entries).turn, 2);
  assert.equal(entryModel(entries[0], { last: false, blocked: false }).rerun, false);
  assert.equal(entryModel(entries[1], { last: true, blocked: false }).rerun, true);
  assert.equal(entryModel(entries[0]).rerun, false, "without the thread context nothing is offered");
});

test("«Повторить ответ» is an icon button with the counter-clockwise arrow, a tooltip and the same name (TASK-365)", () => {
  const html = renderToStaticMarkup(h(AgentRequest, { model: modelOf(update()), actions }));
  assert.match(html, /<button[^>]*aria-label="Повторить ответ"[^>]*><svg[^>]*><use href="#i-retry"><\/use><\/svg><span class="ag-ib-tip" aria-hidden="true">Повторить ответ<\/span><\/button>/);
  assert.equal(texts.rerunTitle, "Повторить ответ");
});

test("a rerun refused over effects says why, impersonally; stale and busy keep their texts", () => {
  const text = (problem) => modelOf(update({ status: "DONE", problem })).cards.find((c) => c.type === "problem").text;
  assert.equal(text("rerun_blocked"), "Повтор недоступен: запрос уже изменил данные. Можно отменить действия или задать вопрос заново.");
  assert.equal(text("stale"), "Запрос устарел: нужно задать его заново.");
  assert.equal(text("busy"), "Предыдущий запрос ещё выполняется.");
});

test("the new request of a rerun is a new entry; the old one loses «Повторить»", () => {
  let s = reducePanel(initialPanelState, { type: "update", update: update({ turn: 1 }) });
  s = reducePanel(s, { type: "update", update: update({ turn: 2, status: "PLANNING" }) });
  const entries = orderedEntries(s.entries);
  assert.equal(entries.length, 2);
  assert.equal(entryModel(entries[0], { last: false, blocked: false }).rerun, false);
});

test("a rerun shows the new request at once with the same text; the update fills it", () => {
  const sc = { text: "сделай", subjectId: "", lessonId: "", assignmentId: "", screen: "today" };
  let s = reducePanel(initialPanelState, { type: "asked", id: 1, text: "сделай", scope: sc });
  s = reducePanel(s, { type: "update", update: update({ turn: 1 }) });
  s = reducePanel(s, { type: "rerunAsked", from: { threadId: "t1", turn: 1, text: "сделай", scope: sc } });
  s = reducePanel(s, { type: "rerunAsked", from: { threadId: "t1", turn: 1, text: "сделай", scope: sc } });
  assert.equal(s.entries.length, 2, "a repeat adds nothing");
  const fresh = orderedEntries(s.entries).at(-1);
  assert.equal(fresh.key, "thread:t1:2");
  assert.equal(fresh.text, "сделай");
  assert.equal(fresh.update, null);
  assert.equal(entryModel(fresh, last).waiting, true);
  const filled = reducePanel(s, { type: "update", update: update({ turn: 2, status: "PLANNING" }) });
  assert.equal(filled.entries.length, 2);
  assert.equal(orderedEntries(filled.entries).at(-1).update.status, "PLANNING");
  assert.equal(orderedEntries(filled.entries).at(-1).text, "сделай");
  // A refused call takes it back; a filled one stays.
  assert.equal(reducePanel(s, { type: "rerunRefused", key: "thread:t1:2" }).entries.length, 1);
  assert.equal(reducePanel(filled, { type: "rerunRefused", key: "thread:t1:2" }).entries.length, 2);
  // A refusal that arrives as a problem update of the old request takes it back too.
  const blocked = reducePanel(s, { type: "update", update: update({ turn: 1, status: "", problem: "rerun_blocked" }) });
  assert.equal(blocked.entries.length, 1);
});

// ---- Busy-refused request ----

const scope = { text: "сделай", subjectId: "", lessonId: "", assignmentId: "", screen: "today" };

function busyState() {
  let s = reducePanel(initialPanelState, { type: "update", update: update({ status: "EXECUTING" }) });
  s = reducePanel(s, { type: "asked", id: 1, text: "сделай", scope });
  s = reducePanel(s, { type: "update", update: update({ threadId: "", turn: 0, status: "", problem: "busy" }) });
  return s;
}

test("a request refused as busy offers «Остановить» for the running request and «Отправить ещё раз»", () => {
  const s = busyState();
  const refused = orderedEntries(s.entries).at(-1);
  assert.equal(refused.text, "сделай");
  const blocking = blockingEntry(s.entries, refused);
  assert.equal(blocking.threadId, "t1");
  assert.deepEqual(buttonIds(entryModel(refused, { last: true, blocked: true })), ["stop", "resend"]);
  assert.deepEqual(buttonIds(entryModel(refused, { last: true, blocked: false })), ["resend"]);
  assert.deepEqual(buttonIds(entryModel(refused, { last: false, blocked: true })), [], "only while it is the last one");
  assert.equal(entryModel(refused, last).rerun, false);
  // The icons carry no word: the line says what they do, and the stop icon names the request it stops (TASK-371).
  const both = entryModel(refused, { last: true, blocked: true }).cards[0];
  assert.equal(both.text, "Предыдущий запрос ещё выполняется. Его можно остановить или отправить этот ещё раз.");
  assert.deepEqual(both.buttons.map((b) => [b.id, b.label]), [["stop", "Остановить предыдущий запрос"], ["resend", "Отправить ещё раз"]]);
  assert.equal(entryModel(refused, last).cards[0].text, "Предыдущий запрос ещё выполняется. Этот можно отправить ещё раз.");
});

test("once the running request is stopped it no longer blocks", () => {
  let s = busyState();
  s = reducePanel(s, { type: "update", update: update({ status: "STOPPED", stopReason: "user" }) });
  assert.equal(blockingEntry(s.entries, orderedEntries(s.entries).at(-1)), undefined);
});

test("a refused request without text can only stop the blocker", () => {
  const u = update({ threadId: "", turn: 0, status: "", problem: "busy" });
  const only = entryModel(entryOf(u, { key: "orphan:x", threadId: "" }), { last: true, blocked: true });
  assert.deepEqual(buttonIds(only), ["stop"]);
  assert.equal(only.cards[0].text, "Предыдущий запрос ещё выполняется. Его можно остановить.");
});

// ---- Double click ----

test("a second press on the same update is not a second call", () => {
  const t = new PressTracker();
  const u = update({ status: "EXECUTING" });
  let calls = 0;
  const press = () => {
    if (t.mark("k", u)) {
      calls++;
    }
  };
  press();
  press();
  assert.equal(calls, 1);
  assert.equal(t.busy("k", u), true);
  assert.equal(t.busy("k", update({ status: "STOPPED" })), false, "a newer update frees the buttons");
  t.release("k");
  press();
  assert.equal(calls, 2, "a refused call frees the buttons");
});

test("both «Остановить» of a busy-refused request are one mark on the blocker; «Отправить ещё раз» has its own key", () => {
  const s = busyState();
  const refused = orderedEntries(s.entries).at(-1);
  const blocking = blockingEntry(s.entries, refused);
  const t = new PressTracker();
  assert.equal(t.mark(blocking.key, blocking.update), true);
  assert.equal(t.mark(blocking.key, blocking.update), false, "the blocker's own «Остановить» is a repeat");
  assert.equal(t.mark(refused.key, refused.update), true, "resend is separate");
  const stopped = reducePanel(s, { type: "update", update: update({ status: "STOPPED", stopReason: "user" }) });
  const now = stopped.entries.find((x) => x.key === blocking.key);
  assert.equal(t.busy(blocking.key, now.update), false, "the next update of the blocker frees it");
});
