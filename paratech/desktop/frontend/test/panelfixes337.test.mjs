// TASK-337: deadlines never show seconds, the agent reply has no caption, the thinking indicator is a small live row.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { formatTimeMinutes } from "../.test-build/activityView.js";
import { dueText } from "../.test-build/homeworkView.js";
import { assignmentMeta } from "../.test-build/summaryView.js";
import { dueText as todayDueText } from "../.test-build/todayView.js";
import { assignmentRow, entryModel, texts } from "../.test-build/agentView.js";
import { confirmCardModel } from "../.test-build/agentConfirmView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const seconds = /\d:\d\d:\d\d/;
// A local 15:50:37 so that the test does not depend on the time zone of the machine.
const withSeconds = new Date(2026, 9, 27, 15, 50, 37).toISOString();

test("a deadline reads «до 27.10.2026, 15:50» without seconds", () => {
  assert.equal(dueText(withSeconds), "до 27.10.2026, 15:50");
  assert.equal(dueText(new Date(2026, 9, 27, 15, 50, 0).toISOString()), "до 27.10.2026, 15:50");
  assert.equal(formatTimeMinutes(withSeconds), "27.10.2026, 15:50");
});

test("missing or unreadable deadline keeps its old text", () => {
  assert.equal(dueText(""), "Без срока");
  assert.equal(dueText("not a date"), "до —");
  assert.equal(formatTimeMinutes(""), "—");
  assert.equal(formatTimeMinutes("not a date"), "—");
});

test("no deadline path emits seconds: assignment list/page, summary, «Сегодня», agent rows, confirm card", () => {
  const now = new Date(2026, 9, 20, 12, 0, 0);
  const row = assignmentRow({ id: "a1", title: "ДЗ", subjectId: "", due: withSeconds }, {}, now);
  const confirm = confirmCardModel({
    kind: "deadline", targetKind: "proposal", targetId: "p1", state: "waiting", message: "", assignmentId: "a1", title: "ДЗ",
    subject: "", due: withSeconds, recipient: "", emailSubject: "", attachmentName: "", attachmentSize: 0, attachmentFormat: "",
    draftVersion: 0, notice: "", deletion: null,
  });
  const shown = [
    dueText(withSeconds),
    assignmentMeta({ due: withSeconds, userEdited: true }),
    todayDueText(withSeconds),
    row.due,
    ...confirm.rows.map((r) => r.value),
  ];
  assert.ok(row.due.startsWith("до "), row.due);
  for (const s of shown) {
    assert.doesNotMatch(s, seconds, s);
  }
});

test("source: homeworkView.dueText goes through the minutes helper, not formatTime", () => {
  const src = readFileSync(new URL("../src/homeworkView.ts", import.meta.url), "utf8");
  assert.match(src, /`до \$\{formatTimeMinutes\(due\)\}`/);
  assert.doesNotMatch(src, /formatTime\(/);
});

// ---- The reply has no caption ----

const reply = (text) => ({
  kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
  cannotDo: null, text, stopReason: "", workId: "", outcome: "",
});
const entryWith = (cards, status = "DONE") => ({
  key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "",
  update: { threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status, stopReason: "", problem: "", cards },
});

test("the reply card is the text alone: no «Ответ» caption, other cards keep theirs", () => {
  const model = entryModel(entryWith([reply("Два задания.")]));
  assert.deepEqual(model.cards.map((c) => c.type), ["reply"]);
  assert.equal("caption" in model.cards[0], false);
  assert.equal("replyCaption" in texts, false);
  const out = renderToStaticMarkup(h(AgentRequest, { model, actions }));
  assert.match(out, /<p class="ag-text">Два задания\.<\/p>/);
  assert.doesNotMatch(out, /ag-card-h|>Ответ</);
  const answer = { ...reply(""), kind: "answer", answerId: "x1" };
  const other = renderToStaticMarkup(h(AgentRequest, { model: entryModel(entryWith([answer])), actions }));
  assert.match(other, /<h3 class="ag-card-h">Ответ по материалам<\/h3>/);
});

// ---- The thinking indicator ----

test("thinking: a small row with a polite live text, no banner, «Стоп» next to the dots", () => {
  const model = entryModel(entryWith([], "EXECUTING"));
  assert.deepEqual(model.cards.map((c) => c.type), ["waiting"]);
  const out = renderToStaticMarkup(h(AgentRequest, { model, actions }));
  // role="status" is a polite live region by itself.
  assert.match(out, /<span class="ag-wait" role="status">/);
  assert.match(out, /<span class="ag-vh">Агент отвечает…<\/span>/);
  assert.match(out, /<div class="ag-think">/);
  assert.doesNotMatch(out, /ag-card|Запрос выполняется/);
  assert.match(out, />Стоп</);
});

test("thinking css: reduced motion keeps the dots still and visible; the card style of the waiting banner is gone", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /prefers-reduced-motion: reduce\)\s*\{\s*\.ag-wdot\s*\{\s*animation: none; opacity: 1;/);
  assert.doesNotMatch(css, /ag-card-wait/);
  const rule = css.match(/\.ag-think\s*\{[^}]*\}/)?.[0] ?? "";
  assert.doesNotMatch(rule, /animation/);
});
