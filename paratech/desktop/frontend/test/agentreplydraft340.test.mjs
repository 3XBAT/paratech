// TASK-340: the reply types out as it is generated (agent:draft with reply: true).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { draftSize, draftsFor, initialPanelState, reducePanel, replyDraftText, splitDrafts } from "../.test-build/agentState.js";
import { entryModel, texts } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const update = (over) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, cards: [], ...over,
});
const replyCard = { kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "итог", stopReason: "", workId: "", outcome: "" };
const typing = (over) => ({ threadId: "t1", turn: 1, callId: "step:reply", texts: ["Привет"], reply: true, ...over });
const answerDraft = (over) => ({ threadId: "t1", turn: 1, callId: "c1", texts: ["проверяется"], reply: false, ...over });
const apply = (s, ...as) => as.reduce((acc, a) => reducePanel(acc, a), s);
const upd = (over) => ({ type: "update", update: update(over) });
const drf = (d) => ({ type: "draft", draft: d });
const shown = (s) => draftsFor(s, s.entries[0]);
const render = (s, last = true) =>
  renderToStaticMarkup(h(AgentRequest, { model: entryModel(s.entries[0], { last, blocked: false }), actions, drafts: draftsFor(s, s.entries[0]) }));

test("each reply event replaces the previous text of the draft", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})), drf(typing({ texts: ["Привет,\nмир"] })));
  assert.equal(s.drafts.length, 1);
  assert.equal(replyDraftText(shown(s)[0]), "Привет,\nмир");
});

test("a reply draft and an answer draft of one turn coexist, keyed by callId", () => {
  let s = apply(initialPanelState, upd({}), drf(answerDraft({})), drf(typing({})));
  s = apply(s, drf(typing({ texts: ["Привет, мир"] })), drf(answerDraft({ texts: ["a", "b"] })));
  assert.equal(s.drafts.length, 2);
  const parts = splitDrafts(shown(s));
  assert.equal(replyDraftText(parts.reply), "Привет, мир");
  assert.deepEqual(parts.answers.map((d) => d.texts), [["a", "b"]]);
});

test("the reply card, the next step and a stopped or failed update clear the reply draft", () => {
  const base = apply(initialPanelState, upd({}), drf(typing({})));
  assert.equal(shown(apply(base, upd({ status: "DONE", cards: [replyCard] }))).length, 0);
  assert.equal(shown(apply(base, upd({ status: "EXECUTING", cards: [replyCard] }))).length, 0);
  assert.equal(shown(apply(base, upd({ status: "STOPPED", stopReason: "user" }))).length, 0);
  assert.equal(shown(apply(base, upd({ status: "STOPPED", stopReason: "failed", problem: "failed" }))).length, 0);
  assert.equal(apply(base, upd({ status: "STOPPED", stopReason: "failed" })).drafts.length, 0);
});

test("progress and a refused button leave the reply draft; a late draft after the stop is ignored", () => {
  const base = apply(initialPanelState, upd({}), drf(typing({})));
  assert.equal(shown(apply(base, upd({ progress: true }))).length, 1);
  assert.equal(shown(apply(base, upd({ status: "", problem: "busy" }))).length, 1);
  const stopped = apply(base, upd({ status: "STOPPED", stopReason: "user" }), drf(typing({ texts: ["поздно"] })));
  assert.equal(stopped.drafts.length, 0);
});

test("a reply draft of an older turn is ignored", () => {
  const s = apply(initialPanelState, upd({ turn: 2 }), drf(typing({ turn: 1 })));
  assert.equal(s.drafts.length, 0);
  const t = apply(initialPanelState, upd({ turn: 1 }), drf(typing({ turn: 1 })), upd({ turn: 2, progress: true }), drf(typing({ turn: 2, texts: ["новый"] })));
  assert.deepEqual(t.drafts.map((d) => d.turn), [2]);
});

test("a stopped request shows its card with the buttons and no typing text", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})), upd({ status: "STOPPED", stopReason: "failed" }));
  const html = render(s);
  assert.ok(!html.includes("ag-reply-draft"));
  assert.ok(!html.includes("Привет"));
  assert.ok(html.includes("Не получилось выполнить шаг"));
  assert.ok(html.includes("Повторить"));
});

test("the reply draft looks like the reply card: same classes, no caption, cursor hidden from readers, silent live region", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})));
  const html = render(s);
  assert.ok(html.includes('<section class="ag-card ag-reply-draft" aria-live="off"><p class="ag-text">Привет<span class="ag-cursor" aria-hidden="true"></span></p></section>'));
  assert.ok(!html.includes(texts.draftCaption));
  assert.ok(!html.includes("ag-draft"));
});

test("while the reply types, the waiting dots are gone and «Стоп» stays in the same row", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})));
  const html = render(s);
  assert.ok(!html.includes("ag-wait"));
  assert.ok(!html.includes(texts.waiting));
  assert.match(html, /<div class="ag-think">\s*<div class="ag-actions"><button[^>]*>Стоп<\/button><\/div><\/div>/);
  assert.ok(html.indexOf("ag-reply-draft") < html.indexOf("ag-think"));
});

test("before the first word (empty text) the dots stay and there is no bubble", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({ texts: [""] })));
  const html = render(s);
  assert.ok(!html.includes("ag-reply-draft"));
  assert.ok(html.includes("ag-wait"));
});

test("answer drafts keep their caption and dots; with a reply draft beside them the dots go", () => {
  const only = apply(initialPanelState, upd({}), drf(answerDraft({})));
  const a = render(only);
  assert.ok(a.includes(texts.draftCaption));
  assert.ok(a.includes("ag-wait"));
  const both = apply(only, drf(typing({})));
  const b = render(both);
  assert.ok(b.includes(texts.draftCaption));
  assert.ok(!b.includes("ag-wait"));
  assert.ok(b.indexOf("ag-draft") < b.indexOf("ag-reply-draft"));
});

test("model text in the reply draft is a plain text node and keeps its line breaks", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({ texts: ["<script>alert(1)</script>\n\n**b** <img src=x onerror=y>"] })));
  const html = render(s);
  assert.ok(html.includes("&lt;script&gt;alert(1)&lt;/script&gt;\n\n**b** &lt;img src=x onerror=y&gt;"));
  assert.ok(!html.includes("<script"));
  assert.ok(!html.includes("<img"));
  assert.ok(!html.includes("<b>"));
});

test("draftSize grows with every reply event so the panel follows the text", () => {
  const one = [typing({ texts: ["Привет"] })];
  const more = [typing({ texts: ["Привет, мир"] })];
  assert.ok(draftSize(more) > draftSize(one));
  assert.equal(draftSize([]), 0);
  assert.ok(draftSize([...one, answerDraft({})]) > draftSize(one));
});

test("the cursor blinks and stands still under reduced motion", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /\.ag-cursor\s*\{[^}]*animation:\s*ag-caret/);
  assert.match(css, /@keyframes ag-caret/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\)\s*\{\s*\.ag-cursor\s*\{\s*animation:\s*none;/);
});
