// A long multi-paragraph reply (TASK-223): paragraph breaks survive, markup stays literal text, the CSS keeps it readable.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { parseCard } from "../.test-build/bindings/agent.js";
import { entryModel } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const paragraph = (n) => `Абзац ${n}: ` + "слово ".repeat(100).trim();
const text = [1, 2, 3, 4, 5].map(paragraph).join("\n\n") + "\n\n<b>x</b> " + "я".repeat(300);

function render(body) {
  const card = parseCard({
    kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
    cannotDo: null, text: body, stopReason: "", assignments: [],
  });
  const u = { threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "", problem: "", cards: [card] };
  const model = entryModel({ key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "", update: u });
  return renderToStaticMarkup(h(AgentRequest, { model, actions }));
}

test("a long reply keeps its paragraph breaks and shows markup as literal text", () => {
  assert.ok(text.length > 3000);
  const out = render(text);
  assert.ok(!out.includes("<b>"));
  assert.ok(out.includes("&lt;b&gt;x&lt;/b&gt;"));
  const m = out.match(/<p class="ag-text">([\s\S]*?)<\/p>/);
  assert.ok(m);
  assert.equal((m[1].match(/\n\n/g) ?? []).length, 5);
  assert.ok(m[1].includes("Абзац 5:"));
});

test("the stylesheet keeps blank lines, wraps long words and scrolls the thread", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /\.ag-text\s*\{[^}]*white-space:\s*pre-wrap/);
  assert.match(css, /\.ag-card p\s*\{[^}]*overflow-wrap:\s*anywhere/);
  assert.match(css, /\.ag-thread\s*\{[^}]*overflow-y:\s*auto/);
});
