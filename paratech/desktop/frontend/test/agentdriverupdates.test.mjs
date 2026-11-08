// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// BUG-20261108-agent-cards (D1, panel side): the Go Driver sends in EVERY update of a request the tool cards first and the
// step's own cards after (e.g. [answer, confirm, reply]). These tests pin what the panel does with such a list.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { entryModel, visibleEntries } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);

const blank = {
  kind: "", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null,
  text: "", assignments: [], assignment: null, stopReason: "", workId: "", outcome: "",
};
const answer = { ...blank, kind: "answer", answerId: "a1" };
const confirm = { ...blank, kind: "confirm", target: { kind: "proposal", id: "p1" } };
const action = { ...blank, kind: "action_line", actionId: "act1" };
const reply = { ...blank, kind: "reply", text: "Готово." };

const update = (over) => ({
  threadId: "th1", turn: 3, subjectId: "", lessonId: "", assignmentId: "", screen: "", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, cards: [], ...over,
});
const live = (s, u) => reducePanel(s, { type: "update", update: u });
const types = (e) => entryModel(e).cards.map((c) => c.type);

// ---- (a) the second update's cards replace the first's ----

test("a later update of the same request replaces its cards: the accumulated list shows as it is, no duplicates", () => {
  let s = live(initialPanelState, update({ cards: [answer] }));
  s = live(s, update({ cards: [answer, confirm] }));
  assert.equal(s.entries.length, 1);
  assert.deepEqual(types(s.entries[0]), ["answer", "confirm", "waiting"]);
  s = live(s, update({ status: "DONE", cards: [answer, confirm, action, reply] }));
  assert.equal(s.entries.length, 1);
  assert.deepEqual(s.entries[0].update.cards, [answer, confirm, action, reply]);
  assert.deepEqual(types(s.entries[0]), ["answer", "confirm", "action", "reply"]);
});

test("DONE after DONE: a longer list of the same finished request replaces the shorter one", () => {
  let s = live(initialPanelState, update({ status: "DONE", cards: [answer] }));
  s = live(s, update({ status: "DONE", cards: [answer, confirm, reply] }));
  assert.equal(s.entries.length, 1);
  assert.deepEqual(types(s.entries[0]), ["answer", "confirm", "reply"]);
});

test("a progress update between two lists moves only the status and keeps the cards", () => {
  let s = live(initialPanelState, update({ cards: [answer, confirm] }));
  s = live(s, update({ progress: true, cards: [] }));
  assert.deepEqual(s.entries[0].update.cards, [answer, confirm]);
  s = live(s, update({ cards: [answer, confirm, reply] }));
  assert.deepEqual(types(s.entries[0]), ["answer", "confirm", "reply", "waiting"]);
});

// ---- (b) nothing is hidden because the request is final ----

const fakeApi = { card: async () => { throw new Error("not read in a static render"); } };
const actions = {
  press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined,
  answers: { answer: async () => { throw new Error("not read in a static render"); } },
  confirm: { api: fakeApi, deleteApi: {} },
  openAssignment: noop,
};

test("a DONE request shows its answer card, its confirm card, its action line with «Отменить» and its reply", () => {
  const s = live(initialPanelState, update({ status: "DONE", cards: [answer, confirm, action, reply] }));
  const model = entryModel(s.entries[0], { last: true, blocked: false });
  const out = html(h(AgentRequest, { model, actions }));
  assert.match(out, /Ответ по материалам/);
  // The confirm card is mounted (its own read is pending in a static render); the fallback «ждёт подтверждения» is not used.
  assert.match(out, /<p class="ag-sched-load small muted" role="status">Загрузка…<\/p>/);
  assert.match(out, /Готово\./);
  // The undo icon of the action line is there and not disabled, as for a running request.
  const undo = out.match(/<button[^>]*aria-label="Отменить"[^>]*>/);
  assert.ok(undo, "«Отменить» is shown");
  assert.doesNotMatch(undo[0], /disabled/);
});

test("the confirm card names its target for a DONE request, so «Да» and «Нет» stay usable", () => {
  const s = live(initialPanelState, update({ status: "DONE", cards: [confirm] }));
  const c = entryModel(s.entries[0]).cards[0];
  assert.equal(c.type, "confirm");
  assert.deepEqual(c.target, { kind: "proposal", id: "p1" });
});

test("without the core calls a DONE request's confirm card is still a quiet line, not nothing", () => {
  const s = live(initialPanelState, update({ status: "DONE", cards: [confirm] }));
  const out = html(h(AgentRequest, { model: entryModel(s.entries[0]), actions: { ...actions, confirm: undefined } }));
  assert.match(out, /ag-confirm/);
});

// ---- (c) the entry keeps its cards when the chat is left and opened again ----

test("opening another chat and coming back shows the same entry with its cards", () => {
  let s = live(initialPanelState, update({ status: "DONE", cards: [answer, confirm, reply] }));
  s = live(s, update({ threadId: "th2", turn: 1, status: "DONE", cards: [reply] }));
  assert.deepEqual(visibleEntries(s.entries, "th2").map((e) => e.key), ["thread:th2:1"]);
  const back = visibleEntries(s.entries, "th1");
  assert.deepEqual(back.map((e) => e.key), ["thread:th1:3"]);
  assert.deepEqual(types(back[0]), ["answer", "confirm", "reply"]);
});

test("a snapshot or the history of a chat that already has its request leaves the entry untouched", () => {
  const s = live(initialPanelState, update({ status: "DONE", cards: [answer, confirm, reply] }));
  assert.equal(reducePanel(s, { type: "snapshot", update: update({ status: "DONE", cards: [reply] }), text: "q" }), s);
  // Also a snapshot of another turn of the same chat.
  assert.equal(reducePanel(s, { type: "snapshot", update: update({ turn: 2, status: "DONE", cards: [reply] }), text: "q" }), s);
  const h2 = reducePanel(s, { type: "history", requests: [update({ status: "DONE", cards: [reply] })] });
  assert.deepEqual(h2.entries[0].update.cards, [answer, confirm, reply]);
});

test("a finished request stays finished: a late running update does not take its cards away", () => {
  let s = live(initialPanelState, update({ status: "DONE", cards: [answer, confirm, reply] }));
  s = live(s, update({ status: "EXECUTING", cards: [answer] }));
  assert.deepEqual(s.entries[0].update.cards, [answer, confirm, reply]);
  assert.equal(s.entries[0].update.status, "DONE");
});
