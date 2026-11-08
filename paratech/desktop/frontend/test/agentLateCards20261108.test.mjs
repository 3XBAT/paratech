// TASK-400 fix: the Driver sends reply, then late cards (confirm, action line, schedule), then the stop line.
// The typing reply and a kept (cut-off) one sit above the first late card; late cards keep their React key.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { cutFor, draftsFor, initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { cardKey, entryModel, firstLateCard, isLateCard, texts } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const update = (over) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, cards: [], ...over,
});
const base = { plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "", stopReason: "", workId: "", outcome: "" };
const replyCard = { ...base, kind: "reply", text: "итог ответа" };
const confirmCard = { ...base, kind: "confirm", target: { kind: "proposal", id: "p1" } };
const deleteCard = { ...base, kind: "delete_assignment", target: { kind: "assignment", id: "a9" } };
const actionCard = { ...base, kind: "action_line", actionId: "act1" };
const typing = { threadId: "t1", turn: 1, callId: "step:reply", texts: ["печатается текст"], reply: true };
const answerDraft = { threadId: "t1", turn: 1, callId: "c1", texts: ["проверяется"], reply: false };
const apply = (s, ...as) => as.reduce((acc, a) => reducePanel(acc, a), s);
const upd = (over) => ({ type: "update", update: update(over) });
const drf = (d) => ({ type: "draft", draft: d });

const render = (s) =>
  renderToStaticMarkup(
    h(AgentRequest, {
      model: entryModel(s.entries[0], { last: true, blocked: false, replyCut: cutFor(s, s.entries[0]) !== undefined }),
      actions,
      drafts: draftsFor(s, s.entries[0]),
      cut: cutFor(s, s.entries[0]),
    }),
  );
const models = (cards) => entryModel(apply(initialPanelState, upd({ status: "DONE", cards })).entries[0], { last: true, blocked: false }).cards;

test("isLateCard: confirm (also delete_assignment), action line, schedule and the stop line; not the reply", () => {
  const cards = models([replyCard, confirmCard, deleteCard, actionCard, { ...base, kind: "schedule_subjects", target: { kind: "schedule_subjects", id: "tok" } }]);
  assert.deepEqual(cards.map((c) => c.type), ["reply", "confirm", "confirm", "action", "schedule"]);
  assert.deepEqual(cards.map(isLateCard), [false, true, true, true, true]);
  assert.equal(isLateCard({ type: "stopped", text: "", buttons: [] }), true);
  assert.equal(isLateCard({ type: "problem", text: "", buttons: [] }), false);
  assert.equal(isLateCard({ type: "waiting", buttons: [] }), false);
  assert.equal(firstLateCard(cards), 1);
  assert.equal(firstLateCard(cards.slice(0, 1)), -1);
});

test("a request with [reply, confirm] renders the reply before the confirm card", () => {
  const html = render(apply(initialPanelState, upd({ status: "DONE", cards: [replyCard, confirmCard] })));
  assert.ok(html.includes("итог ответа"));
  assert.ok(html.includes(texts.confirmPending));
  assert.ok(html.indexOf("итог ответа") < html.indexOf(texts.confirmPending));
});

test("a typing reply draft is drawn before the confirm card, not after it", () => {
  const s = apply(initialPanelState, upd({ cards: [confirmCard] }), drf(typing));
  const html = render(s);
  assert.ok(html.includes("ag-reply-draft"));
  assert.ok(html.indexOf("ag-reply-draft") < html.indexOf(texts.confirmPending));
  assert.equal(html.split("ag-reply-draft").length - 1, 1);
});

test("answer drafts sit before the late cards too, after the reply draft; with no late card they stay at the end", () => {
  const s = apply(initialPanelState, upd({ cards: [actionCard, confirmCard] }), drf(answerDraft), drf(typing));
  const html = render(s);
  assert.ok(html.indexOf("ag-reply-draft") < html.indexOf(texts.draftCaption));
  assert.ok(html.indexOf(texts.draftCaption) < html.indexOf("ag-act"));
  assert.ok(html.indexOf(texts.draftCaption) < html.indexOf(texts.confirmPending));
  const plain = apply(initialPanelState, upd({ cards: [{ ...base, kind: "reply", text: "уже есть" }] }), drf(typing));
  const out = render(plain);
  assert.ok(out.indexOf("уже есть") < out.indexOf("ag-reply-draft"));
});

test("the draft goes before the stop line when there is no late card", () => {
  const s = apply(initialPanelState, upd({ status: "STOPPED", stopReason: "user" }), drf(typing));
  // A draft after the stop is ignored by the panel; render the pieces directly.
  const model = entryModel(s.entries[0], { last: true, blocked: false });
  const html = renderToStaticMarkup(h(AgentRequest, { model, actions, drafts: [typing] }));
  assert.ok(model.cards.some((c) => c.type === "stopped"));
  assert.ok(html.indexOf("ag-reply-draft") < html.indexOf("ag-stop"));
});

test("the key of a confirm card is the same with and without the reply card above it", () => {
  const without = models([confirmCard]);
  const withReply = models([replyCard, confirmCard]);
  assert.equal(cardKey(without[0], 0), cardKey(withReply[1], 1));
  assert.equal(cardKey(without[0], 0), "confirm:proposal:p1");
  // Two different targets differ; a delete_assignment card is keyed by its assignment.
  const del = models([deleteCard]);
  assert.equal(cardKey(del[0], 0), "confirm:assignment:a9");
  assert.notEqual(cardKey(del[0], 0), cardKey(without[0], 0));
});

test("action and schedule cards are keyed by action id and token; a confirm without target falls back to kind and caption", () => {
  const cards = models([actionCard, { ...base, kind: "schedule_subjects", target: { kind: "schedule_subjects", id: "tok" } }, { ...base, kind: "confirm", target: null }]);
  assert.equal(cardKey(cards[0], 0), "action:act1");
  assert.equal(cardKey(cards[0], 5), "action:act1");
  assert.equal(cardKey(cards[1], 1), "schedule:tok");
  assert.equal(cardKey(cards[2], 2), `confirm:${cards[2].caption}`);
  // Cards that are not late keep the index and caption in the key; so does an action line with no id.
  const reply = models([replyCard])[0];
  assert.equal(cardKey(reply, 0), "reply::0");
  assert.equal(cardKey(reply, 3), "reply::3");
  assert.equal(cardKey(models([{ ...actionCard, actionId: "" }])[0], 4), "action::4");
});

test("a kept cut-off text lands above the late cards and above the stop line", () => {
  const s = apply(
    initialPanelState,
    upd({}),
    drf({ ...typing, texts: ["обрезанный текст"] }),
    upd({ status: "STOPPED", stopReason: "user", cards: [actionCard, confirmCard] }),
  );
  assert.equal(cutFor(s, s.entries[0]).text, "обрезанный текст");
  const html = render(s);
  const kept = html.indexOf("ag-reply-kept");
  assert.ok(kept >= 0);
  assert.ok(kept < html.indexOf(texts.confirmPending));
  assert.ok(kept < html.indexOf("ag-stop"));
  assert.ok(html.indexOf("ag-reply-kept") < html.indexOf(texts.undo));
  assert.equal(html.split("ag-reply-kept").length - 1, 1);
});

test("a kept text without late cards still sits above the stop line, and at the end with no line", () => {
  const s = apply(initialPanelState, upd({}), drf({ ...typing, texts: ["обрезанный текст"] }), upd({ status: "STOPPED", stopReason: "offline" }));
  const html = render(s);
  assert.ok(html.indexOf("ag-reply-kept") < html.indexOf("ag-stop"));
});
