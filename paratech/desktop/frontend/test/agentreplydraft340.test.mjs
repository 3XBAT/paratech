// TASK-340: the reply types out as it is generated (agent:draft with reply: true).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { cutFor, draftSize, draftsFor, endSignature, initialPanelState, reducePanel, replyDraftText, splitDrafts } from "../.test-build/agentState.js";
import { entryModel, replyStatus, texts } from "../.test-build/agentView.js";
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
  renderToStaticMarkup(
    h(AgentRequest, {
      model: entryModel(s.entries[0], { last, blocked: false, replyCut: cutFor(s, s.entries[0]) !== undefined }),
      actions,
      drafts: draftsFor(s, s.entries[0]),
      cut: cutFor(s, s.entries[0]),
    }),
  );

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

test("a failed request whose reply was typing says the reply broke off, with the same «Повторить», and shows no fragment", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})), upd({ status: "STOPPED", stopReason: "failed" }));
  const html = render(s);
  assert.ok(!html.includes("ag-reply-draft"));
  assert.ok(!html.includes("ag-reply-kept"));
  assert.ok(!html.includes("Привет"));
  assert.ok(html.includes("Ответ прервался. Можно повторить."));
  assert.ok(!html.includes("Не получилось выполнить шаг"));
  assert.ok(html.includes("Повторить"));
});

test("a failure without a shown reply draft keeps today's text", () => {
  const s = apply(initialPanelState, upd({}), drf(answerDraft({})), upd({ status: "STOPPED", stopReason: "failed" }));
  const html = render(s);
  assert.ok(html.includes("Не получилось выполнить шаг"));
  assert.ok(!html.includes("Ответ прервался"));
});

// ---- «Стоп» keeps what was read ----

const stopped = (...extra) => apply(initialPanelState, upd({}), drf(typing({ texts: ["Привет, мир"] })), upd({ status: "STOPPED", stopReason: "user" }), ...extra);

test("«Стоп» keeps the reply text as UI-only state, shown muted above the stopped line with a caption and no cursor", () => {
  const s = stopped();
  assert.equal(s.drafts.length, 0);
  assert.equal(cutFor(s, s.entries[0]).text, "Привет, мир");
  const html = render(s);
  assert.ok(html.includes('<section class="ag-card ag-reply-kept"><p class="ag-text muted">Привет, мир</p><p class="ag-draft-cap small muted">Ответ остановлен</p></section>'));
  assert.ok(!html.includes("ag-cursor"));
  assert.ok(!html.includes("ag-reply-draft"));
  assert.ok(html.indexOf("ag-reply-kept") < html.indexOf("ag-stop"));
  assert.ok(html.includes("Остановлено"));
  assert.ok(html.includes(">Продолжить<"));
  assert.ok(!html.includes(">Повторить<"));
  assert.ok(!html.includes("Ответ прервался"));
});

test("the kept text is a plain text node", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({ texts: ["<img src=x onerror=y>"] })), upd({ status: "STOPPED", stopReason: "user" }));
  const html = render(s);
  assert.ok(html.includes("&lt;img src=x onerror=y&gt;"));
  assert.ok(!html.includes("<img"));
});

test("a repeated stopped update, a progress-free problem and a late draft keep the text", () => {
  const s = stopped(upd({ status: "STOPPED", stopReason: "user" }), upd({ status: "", problem: "busy" }), drf(typing({ texts: ["поздно"] })));
  assert.equal(cutFor(s, s.entries[0]).text, "Привет, мир");
});

test("the next update of the thread removes the kept text: Повторить a stopped request, Повторить a finished one, Отменить, a new turn", () => {
  for (const next of [
    upd({ status: "EXECUTING", progress: true }),
    upd({ status: "EXECUTING" }),
    upd({ status: "CANCELLED" }),
    upd({ status: "DONE", cards: [replyCard] }),
    upd({ turn: 2, status: "PLANNING" }),
  ]) {
    assert.equal(stopped(next).cuts.length, 0);
  }
  const rerun = stopped({ type: "rerunAsked", from: { threadId: "t1", turn: 1, text: "вопрос", scope: null } });
  assert.equal(rerun.cuts.length, 0);
});

test("a new request in the chat removes the kept text; another chat does not", () => {
  const scope = (threadId) => ({ subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId, mentions: [] });
  assert.equal(stopped({ type: "asked", id: 1, text: "ещё", scope: scope("t1") }).cuts.length, 0);
  assert.equal(stopped({ type: "asked", id: 1, text: "ещё", scope: scope("t2") }).cuts.length, 1);
  assert.equal(stopped(upd({ threadId: "t2", turn: 1, status: "EXECUTING" })).cuts.length, 1);
});

test("other stop reasons drop the fragment but remember that the reply was cut", () => {
  for (const stopReason of ["offline", "quota", "failed"]) {
    const s = apply(initialPanelState, upd({}), drf(typing({})), upd({ status: "STOPPED", stopReason }));
    assert.equal(cutFor(s, s.entries[0]).text, "");
    assert.ok(!render(s).includes("Привет"));
  }
  const none = apply(initialPanelState, upd({}), upd({ status: "STOPPED", stopReason: "user" }));
  assert.equal(none.cuts.length, 0);
  assert.ok(!render(none).includes("ag-reply-kept"));
});

test("«Стоп» without a reply text yet shows no kept block", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({ texts: [""] })), upd({ status: "STOPPED", stopReason: "user" }));
  assert.equal(s.cuts.length, 0);
});

// ---- «Стоп» stays reachable ----

test("the waiting row with «Стоп» is sticky to the bottom of the thread on the panel background", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  const rule = css.match(/\.ag-think\s*\{[^}]*\}/)?.[0] ?? "";
  assert.match(rule, /position:\s*sticky/);
  assert.match(rule, /bottom:\s*0/);
  assert.match(rule, /background:\s*var\(--bg\)/);
  // The thread is the scroll container and the field is outside it, so the row cannot overlap the input.
  assert.match(css, /\.ag-thread\s*\{[^}]*overflow-y:\s*auto/);
});

// ---- Screen reader ----

test("one status per running request: «Ответ печатается…» with a draft, never the reply text", () => {
  const html = render(apply(initialPanelState, upd({}), drf(typing({ texts: ["секретный текст ответа"] }))));
  assert.equal(html.match(/role="status"/g).length, 1);
  assert.ok(html.includes('<span class="ag-vh" role="status">Ответ печатается…</span>'));
  assert.ok(!/role="status"[^>]*>[^<]*секретный/.test(html));
  assert.ok(!html.includes('aria-live="polite"'));
});

test("before the first word the waiting dots are the only status and an empty one is ready", () => {
  const html = render(apply(initialPanelState, upd({}), drf(typing({ texts: [""] }))));
  assert.ok(html.includes(texts.waiting));
  assert.ok(html.includes('<span class="ag-vh" role="status"></span>'));
});

test("the status text: typing, then ready once the final reply card is there; none for a request without a draft", () => {
  const off = { typing: false, wasTyping: false, replyCard: false, running: false };
  assert.equal(replyStatus({ ...off, typing: true, wasTyping: true, running: true }), "Ответ печатается…");
  assert.equal(replyStatus({ ...off, wasTyping: true, replyCard: true }), "Ответ готов");
  assert.equal(replyStatus({ ...off, wasTyping: true, replyCard: true, running: true }), "Ответ готов");
  // A reply that came without a draft (history, a short answer) announces nothing new.
  assert.equal(replyStatus({ ...off, replyCard: true }), null);
  // Running without a draft: an empty region waits in place, the dots speak.
  assert.equal(replyStatus({ ...off, running: true }), "");
  // Stopped or failed after typing: no «готов».
  assert.equal(replyStatus({ ...off, wasTyping: true }), null);
});

test("a finished reply from the history has no status region", () => {
  const done = apply(initialPanelState, upd({ status: "DONE", cards: [replyCard] }));
  const html = render(done);
  assert.ok(!html.includes("Ответ готов"));
  assert.ok(!html.includes("role="));
});

// ---- Follow to the end ----

test("the panel follows to the end when the final card replaces the draft", () => {
  const typingNow = apply(initialPanelState, upd({}), drf(typing({})));
  const done = apply(typingNow, upd({ status: "DONE", cards: [replyCard] }));
  assert.notEqual(endSignature(typingNow.entries[0]), endSignature(done.entries[0]));
  // The same step again does not move it.
  assert.equal(endSignature(done.entries[0]), endSignature(apply(done, upd({ status: "DONE", cards: [replyCard] })).entries[0]));
  assert.equal(endSignature(undefined), "||0");
  // The effect keys on it and still follows only a reader who is at the bottom.
  const view = readFileSync(new URL("../src/agent/AgentPanelView.tsx", import.meta.url), "utf8");
  assert.match(view, /if \(atBottom\.current\) \{\s*end\.current\?\.scrollIntoView\?\.\(\{ block: "end", behavior: "auto" \}\);\s*\}\s*\}, \[size, lastSig\]\)/);
});

// ---- Order ----

test("the reply draft sits before the answer drafts", () => {
  const s = apply(initialPanelState, upd({}), drf(answerDraft({})), drf(typing({})));
  const html = render(s);
  assert.ok(html.indexOf("ag-reply-draft") < html.indexOf("ag-draft"));
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
  // The reply sits where its final card will be, before the answer drafts.
  assert.ok(b.indexOf("ag-reply-draft") < b.indexOf("ag-draft"));
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
  // One accent: the cursor takes the text colour, softened.
  const rule = css.match(/\.ag-cursor\s*\{[^}]*\}/)?.[0] ?? "";
  assert.match(rule, /background:\s*currentColor/);
  assert.match(rule, /opacity:\s*\.6/);
  assert.doesNotMatch(rule, /accent/);
  assert.match(css, /@media \(prefers-reduced-motion: reduce\)\s*\{\s*\.ag-cursor\s*\{\s*animation:\s*none;/);
});
