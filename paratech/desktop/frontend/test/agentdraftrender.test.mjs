// Progressive answer drafts in the agent panel (TASK-148): attach, replace, removal, stale, no HTML.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { draftsFor, initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { entryModel, texts } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const update = (over) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, cards: [], ...over,
});
const reply = { kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "ответ", stopReason: "", workId: "", outcome: "" };
const draft = (over) => ({ threadId: "t1", turn: 1, callId: "c1", texts: ["первая"], ...over });
const apply = (s, ...as) => as.reduce((acc, a) => reducePanel(acc, a), s);
const upd = (over) => ({ type: "update", update: update(over) });
const drf = (over) => ({ type: "draft", draft: draft(over) });
const shown = (s) => draftsFor(s, s.entries[0]);

test("a draft attaches to its request and the last one of a call wins", () => {
  let s = apply(initialPanelState, upd({}), drf({}));
  assert.deepEqual(shown(s).map((d) => d.texts), [["первая"]]);
  s = apply(s, drf({ texts: ["первая", "вторая"] }), drf({ callId: "c2", texts: ["другой"] }));
  assert.deepEqual(shown(s).map((d) => d.texts), [["первая", "вторая"], ["другой"]]);
});

test("the answer card replaces the draft", () => {
  const s = apply(initialPanelState, upd({}), drf({}), upd({ status: "DONE", cards: [reply] }));
  assert.equal(shown(s).length, 0);
  assert.equal(s.drafts.length, 0);
});

test("a stop, an end status and a failed read clear the draft; progress and a problem-only update do not", () => {
  const base = apply(initialPanelState, upd({}), drf({}));
  assert.equal(shown(apply(base, upd({ status: "EXECUTING", progress: true }))).length, 1);
  assert.equal(shown(apply(base, upd({ status: "", problem: "busy" }))).length, 1);
  assert.equal(shown(apply(base, upd({ status: "CANCELLED", stopReason: "user" }))).length, 0);
  assert.equal(shown(apply(base, upd({ status: "STOPPED", problem: "failed" }))).length, 0);
});

test("a stale draft of an older turn is ignored and a newer turn drops older drafts", () => {
  let s = apply(initialPanelState, upd({ turn: 2 }), drf({ turn: 1 }));
  assert.equal(s.drafts.length, 0);
  s = apply(initialPanelState, upd({ turn: 1 }), drf({ turn: 1 }), upd({ turn: 2 }), drf({ turn: 2, callId: "c9" }));
  assert.deepEqual(s.drafts.map((d) => d.turn), [2]);
});

test("a late draft of a finished request is ignored", () => {
  const s = apply(initialPanelState, upd({ status: "CANCELLED" }), drf({}));
  assert.equal(s.drafts.length, 0);
});

test("a draft after a stopped or failed update is ignored", () => {
  const s = apply(initialPanelState, upd({ status: "STOPPED", problem: "failed" }), drf({}));
  assert.equal(s.drafts.length, 0);
});

test("an update of a newer turn drops stored drafts of older turns", () => {
  const s = apply(initialPanelState, upd({ turn: 1 }), drf({ turn: 1 }), upd({ turn: 2, progress: true }));
  assert.equal(s.drafts.length, 0);
});

test("with several drafts only the last one carries the waiting dots", () => {
  const s = apply(initialPanelState, upd({}), drf({}), drf({ callId: "c2", texts: ["другой"] }));
  const e = s.entries[0];
  const html = renderToStaticMarkup(h(AgentRequest, { model: entryModel(e), actions, drafts: draftsFor(s, e) }));
  assert.equal(html.split("ag-wait").length - 1, 1);
  assert.ok(html.indexOf("другой") < html.indexOf("ag-wait"));
});

test("the draft renders with caption, muted text nodes only and the waiting dots after the last line", () => {
  const s = apply(initialPanelState, upd({}), drf({ texts: ["<script>alert(1)</script>", "a <b>bold</b> & c"], callId: "x\" onclick=\"y" }));
  const e = s.entries[0];
  const html = renderToStaticMarkup(h(AgentRequest, { model: entryModel(e), actions, drafts: draftsFor(s, e) }));
  assert.ok(html.includes(texts.draftCaption));
  assert.ok(html.includes("&lt;script&gt;alert(1)&lt;/script&gt;"));
  assert.ok(html.includes("a &lt;b&gt;bold&lt;/b&gt; &amp; c"));
  assert.ok(!html.includes("<script"));
  assert.ok(!html.includes("<b>"));
  assert.ok(!html.includes("onclick"));
  assert.ok(html.indexOf("ag-draft-line") < html.indexOf("ag-wait"));
});
