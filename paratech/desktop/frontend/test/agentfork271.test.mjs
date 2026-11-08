// TASK-271: «Форк» of «Начать заново отсюда»: the binding (ForkChat), the refusal texts, the call guard and what the panel
// does with a success. The dialog's markup is checked in agentrestart249.test.mjs.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { agentApi, methods, parseForkResult } from "../.test-build/bindings/agent.js";
import { ChatBar } from "../.test-build/agent/ChatBar.js";
import {
  beginFork,
  chatSections,
  chatTexts,
  endFork,
  forgetFork,
  forkChangedChat,
  forkField,
  forkFailure,
  forkNotCreatedNote,
  forkOpened,
  forkOpenedNote,
  hideRestart,
  noFork,
  noRestart,
  openRestart,
  restartTexts,
  restartOnChat,
  splitForkTitle,
} from "../.test-build/agentView.js";

const { renderToStaticMarkup } = server;
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
  assert.deepEqual(forkFailure("invalid"), { text: "Этого сообщения уже нет в чате: оно скрыто или изменилось. Чат обновлён.", way: "new" });
  for (const p of ["busy", "failed", "weird", "", "unavailable"]) {
    assert.deepEqual(forkFailure(p), { text: "Не удалось создать новый чат. Можно повторить.", way: "retry" }, p);
  }
  for (const p of ["not_found", "invalid", "busy", "failed", "weird"]) {
    const t = forkFailure(p).text;
    assert.doesNotMatch(t, /[A-Za-z_]{3}/, `no raw code or English in «${p}»`);
    assert.doesNotMatch(t, /(^|[^\p{L}])(я|мы|вы)([^\p{L}]|$)/iu, "impersonal");
    assert.doesNotMatch(t, /Попробуйте/, "no imperative to the student");
  }
  assert.doesNotMatch(forkNotCreatedNote, /Попробуйте/);
  assert.equal(forkNotCreatedNote, "Новый чат не создан. Можно снова выбрать «Начать заново отсюда» → «Форк».");
  assert.equal("forkRetry" in restartTexts, false, "the «Форк» button is the only retry");
});

test("only an invalid refusal sends the panel to read the chat's earlier requests again", () => {
  assert.equal(forkChangedChat("invalid"), true);
  for (const p of ["not_found", "busy", "failed", ""]) {
    assert.equal(forkChangedChat(p), false, p);
  }
});

test("the note after a fork says a new chat opened and the source is as it was; a long title is cut, text only", () => {
  assert.equal(forkOpenedNote("Физика"), "Открыт новый чат. Исходный «Физика» не изменился.");
  assert.equal(forkOpenedNote("  Физика\n  и химия "), "Открыт новый чат. Исходный «Физика и химия» не изменился.");
  assert.equal(forkOpenedNote(""), "Открыт новый чат. Исходный «Без названия» не изменился.");
  const long = forkOpenedNote("а".repeat(80));
  assert.equal(long, `Открыт новый чат. Исходный «${"а".repeat(40)}…» не изменился.`);
  assert.equal(forkOpenedNote("😀".repeat(50)), `Открыт новый чат. Исходный «${"😀".repeat(40)}…» не изменился.`, "cut on code points");
  assert.equal(forkOpenedNote("<b>x</b>"), "Открыт новый чат. Исходный «<b>x</b>» не изменился.", "text stays text");
});

test("the suffix of a fork title is split off so it can stay whole", () => {
  assert.deepEqual(splitForkTitle("Физика (fork)"), { name: "Физика", suffix: " (fork)" });
  assert.deepEqual(splitForkTitle("Физика (fork) (fork)"), { name: "Физика (fork)", suffix: " (fork)" });
  assert.deepEqual(splitForkTitle("Физика"), { name: "Физика", suffix: "" });
  assert.deepEqual(splitForkTitle(" (fork)"), { name: " (fork)", suffix: "" });
});

test("the words of the choice are as the owner fixed them", () => {
  assert.equal(restartTexts.fork, "Форк");
  assert.equal(restartTexts.forkHint, "Будет создан новый чат с историей до этого сообщения. Этот чат не изменится.");
  // TASK-400 (TASK-249, owner accepted): the «Скрыть» line now also says where the text goes; «Форк» above is unchanged.
  assert.equal(
    restartTexts.hideHint,
    "Текст сообщения перейдёт в поле ввода. После отправки сообщения ниже пропадут из чата, и агент их забудет. Сделанные действия не отменяются.",
  );
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
  assert.equal(retry.start, true, "«Форк» pressed again is the retry");
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

test("the cut is on code points: no half of a surrogate pair is left", () => {
  const cut = forkField(empty, "😀".repeat(4001), []).draft;
  assert.equal(Array.from(cut).length, 4000);
  assert.equal(cut, "😀".repeat(4000));
  assert.doesNotMatch(cut, /[\ud800-\udbff](?![\udc00-\udfff])|(?<![\ud800-\udbff])[\udc00-\udfff]/);
});

test("a success closes the dialog, ends the pending «Скрыть» edit of the old chat and fills the new field", () => {
  const typed = { draft: "черновик", pills: [] };
  const opened = openRestart(noRestart, target).flow;
  // Dialog open, nothing pending: the typed text stays, the request follows it.
  const plain = forkOpened(opened, typed, "старый вопрос", target.mentions, target);
  assert.equal(plain.flow.asking, null, "the dialog is closed");
  assert.equal(plain.flow.pending, null);
  assert.deepEqual(plain.field, { draft: "черновик\n\nстарый вопрос", pills: [mention] });

  // A pending edit of the old chat, field unedited (it holds the old message's text, `before` the typed text): the field
  // first returns to what it held, then takes the forked text.
  const edit = hideRestart(opened, typed, null);
  assert.equal(edit.flow.pending.threadId, "th1");
  const reopened = openRestart(edit.flow, target).flow;
  const step = forkOpened(reopened, edit.field, "старый вопрос", target.mentions, target);
  assert.equal(step.flow.pending, null, "the edit belonged to the old chat");
  assert.equal(step.flow.asking, null);
  assert.equal(step.field.draft, "черновик\n\nстарый вопрос", "not the edited old text twice");

  // An empty field before the edit: just the forked text.
  const fromEmpty = hideRestart(opened, empty, null);
  const clean = forkOpened(openRestart(fromEmpty.flow, target).flow, fromEmpty.field, "старый вопрос", target.mentions, target);
  assert.deepEqual(clean.field, { draft: "старый вопрос", pills: [mention] });
});

const lesson = { kind: "lesson", id: "l1", label: "Лекция" };

test("text edited after «Скрыть» of the same message is not lost: it goes into the new field instead of the original", () => {
  const typed = { draft: "черновик", pills: [] };
  const edit = hideRestart(openRestart(noRestart, target).flow, typed, null);
  const edited = { draft: "старый вопрос, но по-другому", pills: [mention, lesson] };
  const step = forkOpened(openRestart(edit.flow, target).flow, edited, "старый вопрос", target.mentions, target);
  assert.equal(step.flow.pending, null);
  assert.deepEqual(step.field, { draft: "черновик\n\nстарый вопрос, но по-другому", pills: [mention, lesson] });
  assert.equal(step.field.draft.split("старый вопрос").length, 2, "the original text is not added again");
  // Cleared by the student: the field of the new chat holds only what was there before the edit.
  const cleared = forkOpened(openRestart(edit.flow, target).flow, empty, "старый вопрос", target.mentions, target);
  assert.deepEqual(cleared.field, { draft: "черновик", pills: [] });
});

test("an edit pending for another message is kept too: before, the edited text, then the forked message", () => {
  const typed = { draft: "черновик", pills: [] };
  const other = { threadId: "th1", turn: 1, text: "первый вопрос", mentions: [lesson] };
  const edit = hideRestart(openRestart(noRestart, other).flow, typed, null);
  assert.equal(edit.flow.pending.turn, 1);
  // The student changed the text of turn 1, then forks from turn 2.
  const edited = { draft: "первый вопрос, уточнён", pills: [lesson] };
  const step = forkOpened(openRestart(edit.flow, target).flow, edited, "старый вопрос", target.mentions, target);
  assert.equal(step.flow.pending, null);
  assert.equal(step.field.draft, "черновик\n\nпервый вопрос, уточнён\n\nстарый вопрос");
  assert.deepEqual(step.field.pills, [lesson, mention]);
  // The same pending edit, unedited field: dropped as before (before + the forked message).
  const plain = forkOpened(openRestart(edit.flow, target).flow, edit.field, "старый вопрос", target.mentions, target);
  assert.deepEqual(plain.field, { draft: "черновик\n\nстарый вопрос", pills: [mention] });
});

test("after a refused send the field is what the student typed: it stays and the forked message follows it once", () => {
  const typed = { draft: "черновик", pills: [] };
  const edit = hideRestart(openRestart(noRestart, target).flow, typed, null);
  const sent = edit.flow.pending;
  const refused = { ...edit.flow, pending: { ...sent, problem: "x", before: { draft: "новое", pills: [] }, unsent: { draft: "старый вопрос", pills: [mention] } } };
  const step = forkOpened(openRestart(refused, target).flow, { draft: "новое", pills: [] }, "старый вопрос", target.mentions, target);
  assert.equal(step.field.draft, "новое\n\nстарый вопрос");
});

test("the old chat is not touched: nothing of it is rewritten and the next chat switch changes nothing", () => {
  const opened = openRestart(noRestart, target).flow;
  const step = forkOpened(opened, empty, "старый вопрос", target.mentions, target);
  assert.deepEqual(target, { threadId: "th1", turn: 2, text: "старый вопрос", mentions: [mention] }, "the target is only read");
  // The effect of the panel that runs when the open chat changes finds nothing left to do.
  const after = restartOnChat(step.flow, "th2");
  assert.deepEqual(after.flow, step.flow);
  assert.equal(after.field, null, "the field is not rewritten by the chat switch");
});

// ---- The chat bar after a fork ----

const noop = () => undefined;
const row = (over) => ({ threadId: "th2", title: "Физика (fork)", archived: false, running: false, lastActive: "2026-11-04T10:00:00Z", ...over });
const bar = (over) =>
  renderToStaticMarkup(
    h(ChatBar, {
      openChat: row(),
      sections: chatSections([row()], []),
      note: "",
      onOpen: noop,
      onNew: noop,
      onRename: async () => "",
      onArchive: noop,
      onRestore: noop,
      ...over,
    }),
  );

test("the suffix of a fork title is its own non-shrinking span; the full title stays in the tooltip and the name", () => {
  const html = bar();
  assert.match(html, /<span class="ag-chat-title"><span class="ag-chat-name">Физика<\/span><span class="ag-chat-sfx"> \(fork\)<\/span><\/span>/);
  assert.match(html, /title="Физика \(fork\)"/);
  assert.match(html, /aria-label="Чаты: Физика \(fork\)"/);
  const plain = bar({ openChat: row({ title: "Физика" }) });
  assert.ok(plain.includes('<span class="ag-chat-name">Физика</span>') && !plain.includes("ag-chat-sfx"));
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /\.ag-chat-sfx \{[^}]*flex: none/);
});

test("the note after a fork is a live line with a small icon button «Открыть исходный чат» (tooltip, no visible word)", () => {
  const note = forkOpenedNote("Физика");
  let opened = 0;
  const html = bar({ note, noteAction: { label: chatTexts.openSource, onClick: () => opened++ } });
  assert.match(html, /<p class="ag-chat-note small" role="status" aria-live="polite">Открыт новый чат\. Исходный «Физика» не изменился\.<\/p>/);
  const tail = html.slice(html.indexOf('<div class="ag-chat-notewrap">'));
  assert.match(tail, /<button[^>]*aria-label="Открыть исходный чат"[^>]*>/);
  assert.match(tail, /<span class="ag-ib-tip" aria-hidden="true">Открыть исходный чат<\/span>/);
  assert.equal(tail.replace(/<span class="ag-ib-tip"[^>]*>[^<]*<\/span>/, "").replace(/<[^>]*>/g, "").replace(note, ""), "", "no visible word on the button");
  assert.equal(opened, 0);
  // No action, or no note: no button.
  assert.ok(!bar({ note }).includes("Открыть исходный чат"));
  assert.ok(!bar({ note: "", noteAction: { label: chatTexts.openSource, onClick: noop } }).includes("Открыть исходный чат"));
});
