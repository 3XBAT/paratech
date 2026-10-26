// TASK-193: follow-ups of 26.10 on the work card, «Сегодня», the turnkey start, «Спросить по шагу» and the free-work card.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { errorTexts, parseWork, turnkeyErrorText } from "../.test-build/bindings/turnkey.js";
import { noAssignmentText } from "../.test-build/bindings/agentConfirm.js";
import { askStepOutcome, triageReason, turnkeyCardModel, turnkeyTexts } from "../.test-build/turnkeyView.js";
import { metaLine, retryOffered, workMeta, notStoppedText } from "../.test-build/workView.js";
import { canOfferSend, SendWorkFlow, sendWorkTexts, initialSendWork } from "../.test-build/sendWorkView.js";
import { startTurnkeyOnce } from "../.test-build/homeworkTurnkey.js";
import { waitingLine } from "../.test-build/todayView.js";
import { FreeWorkFlow, freeWorkModel, initialFreeWorkFlow } from "../.test-build/freeWorkView.js";
import { freeWorkErrors } from "../.test-build/bindings/agent.js";
import { WorkBody, WorkWritingCard } from "../.test-build/agent/WorkCard.js";
import { WorkSendView } from "../.test-build/agent/WorkSend.js";
import { FreeWorkCardView } from "../.test-build/agent/FreeWorkCard.js";

const noop = () => undefined;
const html = (el) => renderToStaticMarkup(el);
const evil = '<img src=x onerror=alert(1)> [x](http://evil.example/a)';
const evilEscaped = "&lt;img src=x onerror=alert(1)&gt; [x](http://evil.example/a)";

function writing(over) {
  return {
    step: "none", generation: 1, topic: "Тема работы", sections: [], done: 0, total: 0, subjectId: "", materialId: "m1",
    result: "r", job: "", active: false, jobState: "", ...over,
  };
}

function work(over) {
  return {
    id: "w1", status: "done", version: 1, writing: null, workType: "referat", problem: "", message: "", questionRound: 0,
    questions: [], hasCard: false, card: { workType: "referat" }, edited: [], feasibility: "", feasibilityReason: "", partial: false,
    assumptions: [], helper: null, answered: [], topic: "", subjectId: "", sendable: false, ...over,
  };
}

const confirmCard = (over = {}) => ({
  kind: "send", targetKind: "proposal", targetId: "p1", state: "waiting", message: "", assignmentId: "a1", title: "Реферат",
  subject: "История", due: "", recipient: "teacher@example.test", emailSubject: "Реферат", attachmentName: "ref.docx",
  attachmentSize: 100, attachmentFormat: "docx", draftVersion: 0, notice: "", deletion: null, ...over,
});

function sendApi(over = {}) {
  const calls = [];
  return {
    calls,
    proposeSendWork: async (id) => {
      calls.push(["proposeSendWork", id]);
      if (over.error) throw over.error;
      return over.card ?? confirmCard();
    },
    // A proposal never confirms: any of these would be a send.
    confirm: async () => {
      calls.push(["confirm"]);
      return confirmCard({ state: "done" });
    },
  };
}

const sendApis = { api: sendApi(), deleteApi: {} };
const doneWork = (over) => work({ writing: writing(), ...over });
const renderBody = (w, extra = {}) =>
  html(h(WorkBody, { work: w, api: {}, onWork: noop, library: { openMaterial: async () => ({ status: "opened", message: "" }) }, ...extra }));

// ---- 1. «Отправить преподавателю» ----

test("parseWork reads topic, subjectId and sendable; an older reply gives the defaults", () => {
  const w = parseWork({ id: "w1", status: "done", version: 1, problem: "", message: "", topic: "Т", subjectId: "s1", sendable: true });
  assert.deepEqual([w.topic, w.subjectId, w.sendable], ["Т", "s1", true]);
  const old = parseWork({ id: "w1", status: "done", version: 1, problem: "", message: "" });
  assert.deepEqual([old.topic, old.subjectId, old.sendable], ["", "", false]);
  assert.throws(() => parseWork({ id: "w1", status: "done", version: 1, problem: "", message: "", sendable: "yes" }));
});

test("the send button is offered only for a sendable ready work", () => {
  assert.equal(canOfferSend(doneWork({ sendable: true })), true);
  assert.equal(canOfferSend(doneWork({ sendable: false })), false);
  assert.equal(canOfferSend(work({ sendable: true, writing: writing({ step: "approve", result: "" }) })), false);
  assert.equal(canOfferSend(work({ sendable: true, writing: null })), false);
});

test("a ready sendable work shows «Отправить преподавателю»; without the send calls nothing about sending is shown", () => {
  const out = renderBody(doneWork({ sendable: true }), { send: sendApis });
  assert.match(out, /Отправить преподавателю/);
  assert.match(out, /только после «Да»/);
  assert.doesNotMatch(renderBody(doneWork({ sendable: true })), /Отправить преподавателю/);
});

test("a work without an assignment shows noAssignmentText and no send button", () => {
  const out = renderBody(doneWork({ sendable: false }), { send: sendApis });
  assert.ok(out.includes(noAssignmentText));
  assert.doesNotMatch(out, /Отправить преподавателю/);
});

test("the proposal makes a confirm card and sends nothing; several clicks make one proposal", async () => {
  const api = sendApi();
  const states = [];
  const flow = new SendWorkFlow(api, "w1", (s) => states.push(s));
  await Promise.all([flow.propose(), flow.propose(), flow.propose()]);
  assert.deepEqual(api.calls, [["proposeSendWork", "w1"]]);
  assert.equal(flow.state.card.kind, "send");
  assert.equal(flow.state.card.state, "waiting");
  assert.equal(flow.state.sentBefore, false);
  // While the card waits for «Да» another proposal is not made.
  await flow.propose();
  assert.equal(api.calls.length, 1);
});

test("a work without an assignment: the refusal text of the core is shown as it is", async () => {
  const flow = new SendWorkFlow(sendApi({ error: new Error(noAssignmentText) }), "w1", noop);
  await flow.propose();
  assert.equal(flow.state.error, noAssignmentText);
  assert.equal(flow.state.card, null);
  const other = new SendWorkFlow(sendApi({ error: new Error("C:\\secret path") }), "w1", noop);
  await other.propose();
  assert.doesNotMatch(other.state.error, /secret/);
});

test("a work sent before warns, and offers a deliberate «Отправить ещё раз»", async () => {
  const api = sendApi();
  const flow = new SendWorkFlow(api, "w1", noop);
  await flow.propose();
  flow.noteCard(confirmCard({ state: "done" }));
  assert.equal(flow.state.sentBefore, true);
  const out = html(h(WorkSendView, { state: flow.state, sendable: true, onSend: noop }));
  assert.ok(out.includes(sendWorkTexts.alreadySent));
  assert.ok(out.includes(sendWorkTexts.again));
  // The new proposal is a new call, and it still waits for its own «Да».
  await flow.propose();
  assert.equal(api.calls.filter((c) => c[0] === "proposeSendWork").length, 2);
  assert.equal(api.calls.filter((c) => c[0] === "confirm").length, 0);

  const fromCore = new SendWorkFlow(sendApi({ card: confirmCard({ state: "done" }) }), "w1", noop);
  await fromCore.propose();
  assert.equal(fromCore.state.sentBefore, true);
});

test("no warning for a work not sent yet; the state shows the plain button", () => {
  const out = html(h(WorkSendView, { state: initialSendWork, sendable: true, onSend: noop }));
  assert.ok(out.includes(sendWorkTexts.button));
  assert.ok(!out.includes(sendWorkTexts.alreadySent));
});

test("texts of the send view are text nodes", () => {
  const out = html(h(WorkSendView, { state: { ...initialSendWork, error: evil }, sendable: true, onSend: noop }));
  assert.ok(out.includes(evilEscaped));
});

// ---- 2. «Сегодня» ----

test("a waiting work send has a human label, not the raw kind", () => {
  const w = waitingLine({ proposalId: "p1", kind: "send_work_email", assignmentId: "a1", title: "Реферат", subject: "История", due: "", recipient: "t@example.test", createdAt: "" });
  assert.equal(w.label, "Работа преподавателю");
  assert.equal(w.icon, "mail");
  assert.doesNotMatch(JSON.stringify(w), /send_work_email/);
});

// ---- 3. The reason of a failed turnkey start ----

test("a triage with a problem shows the core's message; a missing message gets a general line", () => {
  const w = work({ status: "triage", problem: "offline", message: "Нет связи с paratech: разбор не завершён. Повторите позже." });
  assert.equal(turnkeyCardModel(w).problem, w.message);
  assert.equal(triageReason(work({ status: "triage", problem: "refused", message: "" })), turnkeyTexts.triageFailed);
  assert.equal(triageReason(work({ status: "triage" })), "");
});

test("the assignment page seeds the work card with the reply of a failed start, so the reason shows", async () => {
  const failed = work({ id: "w9", status: "triage", problem: "quota", message: "Лимит запросов исчерпан: разбор не завершён." });
  const api = { startForAssignment: async () => failed };
  const state = await startTurnkeyOnce(async (call) => (await call(), true), api, "a1");
  assert.equal(state.phase, "work");
  assert.equal(state.work, failed);
  const out = html(h(WorkWritingCard, { api: { work: async () => work({ id: "w9", status: "triage" }) }, workId: "w9", initial: state.work, library: {}, send: sendApis }));
  assert.ok(out.includes("Лимит запросов исчерпан"));
  assert.ok(out.includes("Повторить"));
});

test("a failed work shows its reason too", () => {
  const out = renderBody(work({ status: "failed", problem: "refused", message: "Ответ разбора не прошёл проверку. Можно повторить." }));
  assert.ok(out.includes("Ответ разбора не прошёл проверку"));
});

// ---- 4. «Спросить по шагу» ----

test("the new AskStep texts are known and shown as they are; no «backend missing» mode is left", async () => {
  const texts = [
    "Нет связи с paratech: вопрос не отправлен. Повторите позже.",
    "Лимит запросов исчерпан: ответить на вопрос сейчас нельзя.",
    "Ответ на вопрос не прошёл проверку. Спросите ещё раз или переформулируйте вопрос.",
  ];
  for (const t of texts) {
    assert.ok(errorTexts.includes(t), t);
    assert.equal(turnkeyErrorText(new Error(t)), t);
    const r = await askStepOutcome({ askStep: async () => { throw new Error(t); } }, "w1", 0, "q");
    assert.deepEqual(r, { kind: "error", text: t });
  }
  const ok = await askStepOutcome({ askStep: async () => evil }, "w1", 0, "q");
  assert.deepEqual(ok, { kind: "answer", text: evil });
});

// ---- 5. Free work in failed state, the failed stop ----

const ref = { threadId: "th1", turn: 3, callId: "toolu_01" };
function freeApi(cards) {
  const calls = [];
  return {
    calls,
    freeWorkCard: async () => {
      calls.push("card");
      return cards.shift() ?? { state: "waiting", workId: "" };
    },
    freeWork: async () => {
      calls.push("freeWork");
      return { state: "failed", workId: "" };
    },
  };
}

test("a stored failed free work offers «Обновить»", async () => {
  const api = freeApi([{ state: "failed", workId: "" }, { state: "waiting", workId: "" }]);
  const flow = new FreeWorkFlow(api, ref, noop);
  await flow.load();
  assert.equal(flow.state.phase, "failed");
  assert.equal(flow.state.error, freeWorkErrors.failed);
  const out = html(h(FreeWorkCardView, { state: flow.state, onDontShow: noop, onYes: noop, onNo: noop, onReload: noop }));
  assert.match(out, /Обновить/);
  assert.doesNotMatch(out, /Сделать|Не надо|checkbox/);
  assert.ok(out.includes(freeWorkErrors.failed));
  // The press cannot replay the failure.
  await flow.answer(true);
  assert.deepEqual(api.calls, ["card"]);
  // «Обновить» reads again.
  await flow.load();
  assert.equal(flow.state.phase, "waiting");
});

test("a second press that finds the failure stored also ends in the failed state", async () => {
  const api = freeApi([{ state: "waiting", workId: "" }, { state: "failed", workId: "" }]);
  const flow = new FreeWorkFlow(api, ref, noop);
  await flow.load();
  await flow.answer(true);
  assert.equal(flow.state.phase, "failed");
  assert.equal(freeWorkModel(flow.state).kind, "unknown");
  assert.equal(flow.state.busy, false);
  assert.equal(initialFreeWorkFlow.phase, "loading");
});

test("«Повторить» is not offered next to a failed stop, nor next to any error of the stop call", () => {
  assert.equal(retryOffered(notStoppedText, false), false);
  assert.equal(retryOffered("Не удалось выполнить. Попробуйте ещё раз.", true), false);
  assert.equal(retryOffered("Не удалось выполнить. Попробуйте ещё раз.", false), true);
  assert.equal(retryOffered("", false), false);
  const running = work({ status: "writing", writing: writing({ step: "section", active: true, result: "" }) });
  const out = renderBody(running, { error: notStoppedText });
  assert.ok(out.includes(notStoppedText));
  assert.doesNotMatch(out, /Повторить/);
  assert.match(renderBody(running, { error: "Не удалось выполнить. Попробуйте ещё раз." }), /Повторить/);
});

// ---- 6. Subject and topic ----

test("subject and topic come from the DTO; a removed subject is «Входящие»", () => {
  const subjects = new Map([["s1", "История"]]);
  assert.deepEqual(workMeta(work({ topic: " Тема ", subjectId: "s1" }), subjects), { subject: "История", topic: "Тема" });
  assert.equal(metaLine(workMeta(work({ topic: "Тема", subjectId: "s1" }), subjects)), "Предмет: История · Тема: Тема");
  assert.equal(workMeta(work({ subjectId: "gone" }), subjects).subject, "Входящие");
  assert.equal(metaLine(workMeta(work(), subjects)), "");
  // The writing's own topic and subject stand in when the DTO has none.
  assert.deepEqual(workMeta(work({ writing: writing({ topic: "Из письма", subjectId: "s1" }) }), subjects), { subject: "История", topic: "Из письма" });
});

test("WorkBody shows the subject and the topic as text", () => {
  const out = renderBody(doneWork({ topic: "Тема", subjectId: "s1" }), { subjects: new Map([["s1", "История"]]) });
  assert.match(out, /Предмет: История · Тема: Тема/);
  const bad = renderBody(doneWork({ topic: evil, subjectId: "s1" }), { subjects: new Map([["s1", evil]]) });
  assert.ok(bad.includes(evilEscaped));
  assert.ok(!bad.includes("<img"));
  assert.doesNotMatch(renderBody(doneWork()), /Предмет:/);
});
