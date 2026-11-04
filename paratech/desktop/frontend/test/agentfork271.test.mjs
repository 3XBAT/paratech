// TASK-271: «Форк» of «Начать заново отсюда»: the binding (ForkChat), the refusal texts, the call guard and what the panel
// does with a success. The dialog's markup is checked in agentrestart249.test.mjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { agentApi, methods, parseForkResult } from "../.test-build/bindings/agent.js";
import {
  beginFork,
  endFork,
  forgetFork,
  forkField,
  forkFailure,
  forkOpened,
  hideRestart,
  noFork,
  noRestart,
  openRestart,
  restartTexts,
  restartOnChat,
} from "../.test-build/agentView.js";

const chat = { threadId: "th2", title: "Физика (fork)", archived: false, lastActive: "2026-11-04T10:00:00Z", running: false, fill: null };
const mention = { kind: "subject", id: "s1", label: "Физика" };
const target = { threadId: "th1", turn: 2, text: "старый вопрос", mentions: [mention] };
const empty = { draft: "", pills: [] };

// ---- The binding ----

test("a fork result carries the new chat and the text on success", () => {
  assert.deepEqual(parseForkResult({ problem: "", chat, text: "что задали?" }), { problem: "", chat, text: "что задали?" });
  assert.equal(parseForkResult({ problem: "", chat, text: "" }).text, "", "an empty text is still a text");
});

test("every problem reads as itself with no chat and no text, whatever else came with it", () => {
  for (const p of ["invalid", "not_found", "busy", "failed"]) {
    assert.deepEqual(parseForkResult({ problem: p, chat: {}, text: "" }), { problem: p, chat: null, text: "" }, p);
    assert.deepEqual(parseForkResult({ problem: p, chat, text: "утечка" }), { problem: p, chat: null, text: "" }, `${p} drops the chat and the text`);
  }
});

test("an unknown or missing problem and malformed data read as failed; never a throw", () => {
  const failed = { problem: "failed", chat: null, text: "" };
  assert.deepEqual(parseForkResult({ problem: "weird", chat, text: "x" }), failed);
  assert.deepEqual(parseForkResult({ chat, text: "x" }), failed, "no problem field");
  assert.deepEqual(parseForkResult({ problem: 5, chat, text: "x" }), failed);
  for (const bad of [null, undefined, 42, "x", [], true]) {
    assert.deepEqual(parseForkResult(bad), failed);
  }
});

test("a success without a usable chat or without a string text reads as failed", () => {
  const failed = { problem: "failed", chat: null, text: "" };
  assert.deepEqual(parseForkResult({ problem: "", text: "x" }), failed, "no chat");
  assert.deepEqual(parseForkResult({ problem: "", chat: null, text: "x" }), failed);
  assert.deepEqual(parseForkResult({ problem: "", chat: {}, text: "x" }), failed, "an empty chat object");
  assert.deepEqual(parseForkResult({ problem: "", chat: { ...chat, threadId: "" }, text: "x" }), failed, "a chat that cannot be opened");
  assert.deepEqual(parseForkResult({ problem: "", chat: 3, text: "x" }), failed);
  assert.deepEqual(parseForkResult({ problem: "", chat }), failed, "no text");
  assert.deepEqual(parseForkResult({ problem: "", chat, text: null }), failed);
  assert.deepEqual(parseForkResult({ problem: "", chat, text: 7 }), failed);
});

test("the api calls ForkChat with the chat and the number and parses the reply", async () => {
  assert.equal(methods.forkChat, "paratech/desktop/internal/input/wails.AgentService.ForkChat");
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { problem: "", chat, text: "q" };
  });
  assert.deepEqual(await api.forkChat("th1", 2), { problem: "", chat, text: "q" });
  assert.deepEqual(calls, [[methods.forkChat, "th1", 2]]);
  assert.deepEqual(await agentApi(async () => 42).forkChat("th1", 2), { problem: "failed", chat: null, text: "" });
});

// ---- Texts: why, and the way on ----

test("each refusal says why in plain Russian and offers the way out", () => {
  assert.deepEqual(forkFailure("not_found"), { text: "Этот чат уже недоступен.", way: "new" });
  assert.deepEqual(forkFailure("invalid"), { text: "Это сообщение нельзя взять за начало нового чата.", way: null });
  for (const p of ["busy", "failed", "weird", "", "unavailable"]) {
    assert.deepEqual(forkFailure(p), { text: "Не удалось создать новый чат. Попробуйте ещё раз.", way: "retry" }, p);
  }
  for (const p of ["not_found", "invalid", "busy", "failed", "weird"]) {
    const t = forkFailure(p).text;
    assert.doesNotMatch(t, /[A-Za-z_]{3}/, `no raw code or English in «${p}»`);
    assert.doesNotMatch(t, /(^|[^\p{L}])(я|мы|вы)([^\p{L}]|$)/iu, "impersonal");
  }
});

test("the words of the choice are as the owner fixed them", () => {
  assert.equal(restartTexts.fork, "Форк");
  assert.equal(restartTexts.forkHint, "Будет создан новый чат с историей до этого сообщения. Этот чат не изменится.");
  assert.equal(restartTexts.hideHint, "Сообщения после этого агент забудет. Сделанные действия не отменяются.");
  assert.doesNotMatch(restartTexts.fork + restartTexts.forkHint, /fork/i, "the suffix « (fork)» comes from the core");
});

// ---- The call guard ----

test("a second click while the call runs starts nothing; the refusal is shown until the next try", () => {
  const first = beginFork(noFork);
  assert.equal(first.start, true);
  assert.deepEqual(first.state, { busy: true, failure: null });
  const again = beginFork(first.state);
  assert.equal(again.start, false, "double click");
  assert.equal(again.state, first.state);
  const failed = endFork("failed");
  assert.equal(failed.busy, false);
  assert.equal(failed.failure.way, "retry");
  const retry = beginFork(failed);
  assert.equal(retry.start, true, "«Повторить» starts a new call");
  assert.equal(retry.state.failure, null, "the old refusal goes away while it runs");
});

test("opening or closing the dialog forgets an old refusal but not a call still running", () => {
  assert.deepEqual(forgetFork(endFork("not_found")), noFork);
  const running = beginFork(noFork).state;
  assert.equal(forgetFork(running), running);
  assert.equal(forgetFork(noFork), noFork);
});

// ---- A success ----

test("the field of the new chat gets the request's text and its mentions", () => {
  assert.deepEqual(forkField(empty, "старый вопрос", [mention]), { draft: "старый вопрос", pills: [mention] });
  // Only blanks in the field count as empty.
  assert.equal(forkField({ draft: " \n", pills: [] }, "старый вопрос", []).draft, "старый вопрос");
});

test("typed text in the field is not lost: the request follows it after an empty line", () => {
  const f = forkField({ draft: "почти готово  \n", pills: [mention] }, "старый вопрос", [mention, { kind: "lesson", id: "l1", label: "Лекция" }]);
  assert.equal(f.draft, "почти готово\n\nстарый вопрос");
  assert.deepEqual(f.pills.map((p) => p.id), ["s1", "l1"], "a pill is kept once");
});

test("the field never exceeds its limit and the typed text is the part that stays whole", () => {
  const f = forkField({ draft: "а".repeat(3000), pills: [] }, "б".repeat(3000), []);
  assert.equal(f.draft.length, 4000);
  assert.ok(f.draft.startsWith("а".repeat(3000)));
  const many = Array.from({ length: 12 }, (_, i) => ({ kind: "subject", id: `s${i}`, label: "x" }));
  assert.equal(forkField(empty, "т", many).pills.length, 8, "at most 8 mentions");
});

test("a success closes the dialog, drops the pending «Скрыть» edit of the old chat and fills the new field", () => {
  const typed = { draft: "черновик", pills: [] };
  const opened = openRestart(noRestart, target).flow;
  // Dialog open, nothing pending: the typed text stays, the request follows it.
  const plain = forkOpened(opened, typed, "th2", "старый вопрос", target.mentions);
  assert.equal(plain.flow.asking, null, "the dialog is closed");
  assert.equal(plain.flow.pending, null);
  assert.deepEqual(plain.field, { draft: "черновик\n\nстарый вопрос", pills: [mention] });

  // A pending edit of the old chat (the field holds the old message's text, `before` the typed text): the edit is dropped
  // and the field first returns to what it held, then takes the forked text.
  const edit = hideRestart(opened, typed, null);
  assert.equal(edit.flow.pending.threadId, "th1");
  const reopened = openRestart(edit.flow, target).flow;
  const step = forkOpened(reopened, edit.field, "th2", "старый вопрос", target.mentions);
  assert.equal(step.flow.pending, null, "the edit belonged to the old chat");
  assert.equal(step.flow.asking, null);
  assert.equal(step.field.draft, "черновик\n\nстарый вопрос", "not the edited old text twice");

  // An empty field before the edit: just the forked text.
  const fromEmpty = hideRestart(opened, empty, null);
  const clean = forkOpened(openRestart(fromEmpty.flow, target).flow, fromEmpty.field, "th2", "старый вопрос", target.mentions);
  assert.deepEqual(clean.field, { draft: "старый вопрос", pills: [mention] });
});

test("the old chat is not touched: nothing of it is rewritten and the next chat switch changes nothing", () => {
  const opened = openRestart(noRestart, target).flow;
  const step = forkOpened(opened, empty, "th2", "старый вопрос", target.mentions);
  assert.deepEqual(target, { threadId: "th1", turn: 2, text: "старый вопрос", mentions: [mention] }, "the target is only read");
  // The effect of the panel that runs when the open chat changes finds nothing left to do.
  const after = restartOnChat(step.flow, "th2");
  assert.deepEqual(after.flow, step.flow);
  assert.equal(after.field, null, "the field is not rewritten by the chat switch");
});
