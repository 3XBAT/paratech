// TASK-249: «Начать заново отсюда» → «Скрыть»: where the action is offered, the dialog, why «Скрыть» can be off,
// the pending edit, restartFrom in the request and what the panel does with the outcome.
import { test } from "node:test";
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { initialPanelState, isFinalStatus, maxKnownTurn, reducePanel, restartOutcome } from "../.test-build/agentState.js";
import {
  askInput,
  cancelRestartEdit,
  closeRestartDialog,
  dialogKey,
  earlierItems,
  forkFailure,
  hideRestart,
  liveRestartTarget,
  noRestart,
  openRestart,
  refuseRestart,
  restartBlock,
  restartDraft,
  restartLabel,
  restartOnChat,
  restartOnOutsideDraft,
  restartRefusal,
  restartRefusalText,
  restartTarget,
  restartTexts,
  restoreUnsent,
  spendRestart,
  withoutRestartFrom,
  withoutRestarted,
  withRestart,
} from "../.test-build/agentView.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";
import { RestartDialog } from "../.test-build/agent/RestartDialog.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

const update = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
  stopReason: "", problem: "", progress: false, cards: [], fill: null, ...over,
});
const apply = (s, ...us) => us.reduce((acc, u) => reducePanel(acc, { type: "update", update: u }), s);
const ctx = { label: "Сегодня", removable: false, ask: { subjectId: "", lessonId: "", assignmentId: "", screen: "today" } };
const live = { inputEnabled: true, offline: false };
const req = (turn, over) => ({ turn, text: `вопрос ${turn}`, mentions: [], reply: `ответ ${turn}`, current: false, ...over });
const mention = { kind: "subject", id: "s1", label: "Физика" };
// A thread with two finished requests; the field is empty.
const twoDone = () => apply(initialPanelState, update({ turn: 1 }), update({ turn: 2 }));
const withText = (s) => ({ ...s, entries: s.entries.map((e) => ({ ...e, text: `вопрос ${e.turn}` })) });

// ---- Where the action is offered ----

test("the action needs the open chat, a request number from 1 and a text to put into the field", () => {
  assert.equal(restartTarget("a", 1, "текст", []).turn, 1);
  assert.equal(restartTarget("", 1, "текст", []), null, "no open chat");
  assert.equal(restartTarget("a", 0, "текст", []), null, "no number yet");
  assert.equal(restartTarget("a", 1.5, "текст", []), null);
  assert.equal(restartTarget("a", 2, "  \n", []), null, "nothing to edit");
  const many = Array.from({ length: 12 }, (_, i) => ({ kind: "subject", id: `s${i}`, label: "x" }));
  assert.equal(restartTarget("a", 1, "т", many).mentions.length, 8, "at most 8 mentions, as the core allows");
});

test("earlier requests carry their target (number, text, mentions); a request without text has none", () => {
  const items = earlierItems([req(1, { mentions: [mention] }), req(2, { text: "" }), req(3)], [], "a");
  assert.deepEqual(items.map((i) => i.restart?.turn ?? null), [1, null, 3]);
  assert.deepEqual(items[0].restart.mentions, [mention]);
  assert.equal(items[0].restart.threadId, "a");
});

test("a live request of the open chat is offered only with a known number; the unnamed and other chats' are not", () => {
  const s = withText(twoDone());
  const e2 = s.entries.find((e) => e.turn === 2);
  assert.equal(liveRestartTarget(e2, [], "a").turn, 2);
  assert.equal(liveRestartTarget(e2, [], "b"), null, "another chat is open");
  // Just sent: the core has not named it, so it has no number.
  const sent = reducePanel(initialPanelState, { type: "asked", id: 1, text: "q", scope: { text: "q", subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId: "a" } });
  assert.equal(liveRestartTarget(sent.entries[0], [], "a"), null);
  // A request restored after a restart has no text of its own: the history supplies it.
  const bare = { ...e2, text: "" };
  assert.equal(liveRestartTarget(bare, [], "a"), null);
  // The placeholder «Повторить» shows has a thread and the next number, but the core has not named it: no action.
  const placeholder = reducePanel(s, { type: "rerunAsked", from: { threadId: "a", turn: 2, text: "вопрос 2", scope: null } });
  const ph = placeholder.entries.find((e) => e.turn === 3);
  assert.ok(ph !== undefined && ph.update === null, "the placeholder exists");
  assert.equal(liveRestartTarget(ph, [req(3)], "a"), null);
  assert.equal(liveRestartTarget(bare, [req(2, { mentions: [mention] })], "a").text, "вопрос 2");
  assert.deepEqual(liveRestartTarget(bare, [req(2, { mentions: [mention] })], "a").mentions, [mention]);
});

test("the accessible name starts with the visible words and names the message, cut to 40 characters", () => {
  assert.equal(restartLabel({ text: "Что задали\nпо физике" }), "Начать заново отсюда: Что задали по физике");
  const long = restartLabel({ text: "а".repeat(60) });
  assert.ok(long.startsWith(`${restartTexts.action}: `) && long.endsWith("…"));
  assert.equal(Array.from(long).length, restartTexts.action.length + 2 + 41);
});

// ---- When «Скрыть» is available ----

test("«Скрыть» is available when the current request is over, whichever way it ended", () => {
  for (const status of ["DONE", "FAILED", "CANCELLED", "STOPPED"]) {
    assert.equal(isFinalStatus(status), true);
    const s = apply(initialPanelState, update({ turn: 1, status }));
    assert.equal(restartBlock(s.entries, "a", live), null, status);
  }
  assert.equal(restartBlock([], "a", live), null, "a chat with only history has nothing running");
});

test("a running request blocks «Скрыть», says why in Russian and offers «Остановить»", () => {
  for (const status of ["PLANNING", "EXECUTING"]) {
    const s = apply(initialPanelState, update({ turn: 1 }), update({ turn: 2, status }));
    const b = restartBlock(s.entries, "a", live);
    assert.match(b.reason, /^Агент ещё работает над запросом\. Дождитесь конца или остановите его\.$/);
    assert.equal(b.fix.kind, "stop");
    assert.equal(b.fix.label, "Остановить");
    assert.equal(b.fix.entry.turn, 2, "the stop goes to the running request");
  }
});

test("a request waiting for the student blocks «Скрыть» and offers to cancel it", () => {
  const plan = restartBlock(apply(initialPanelState, update({ status: "AWAITING_PLAN" })).entries, "a", live);
  assert.match(plan.reason, /план/);
  assert.deepEqual([plan.fix.kind, plan.fix.label], ["cancel", "Отменить запрос"]);
  const answers = restartBlock(apply(initialPanelState, update({ status: "AWAITING_ANSWERS" })).entries, "a", live);
  assert.match(answers.reason, /вопрос/);
  assert.equal(answers.fix.kind, "cancel");
  // After the student's «Да» the update is a running one, then the end: the block follows the latest update.
  const later = apply(initialPanelState, update({ status: "AWAITING_PLAN" }), update({ status: "DONE" }));
  assert.equal(restartBlock(later.entries, "a", live), null);
});

test("a request just sent, no connection and a panel still loading block «Скрыть» with a reason and nothing to press", () => {
  const sent = reducePanel(initialPanelState, { type: "asked", id: 1, text: "q", scope: { text: "q", subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId: "a" } });
  const b = restartBlock(sent.entries, "a", live);
  assert.match(b.reason, /отправляется/);
  assert.equal(b.fix, null);
  const off = restartBlock([], "a", { inputEnabled: false, offline: true });
  assert.match(off.reason, /Нет связи с paratech/);
  assert.equal(off.fix, null);
  assert.match(restartBlock([], "a", { inputEnabled: false, offline: false }).reason, /загружается/);
});

test("requests of another chat and refused ones do not block", () => {
  const other = apply(initialPanelState, update({ threadId: "b", status: "EXECUTING" }));
  assert.equal(restartBlock(other.entries, "a", live), null);
  const refused = apply(initialPanelState, update({ turn: 1 }), update({ threadId: "", turn: 0, status: "", problem: "busy" }));
  assert.equal(restartBlock(refused.entries, "a", live), null);
});

// ---- The flow: dialog, «Скрыть», «Отмена», «Отменить», chat switch ----

const target = { threadId: "a", turn: 2, text: "старый вопрос", mentions: [mention] };
const typed = { draft: "почти готовый черновик", pills: [{ kind: "lesson", id: "l1", label: "Лекция" }] };

test("the dialog opens from the action and changes nothing else; «Отмена» and Esc close it with nothing changed", () => {
  const opened = openRestart(noRestart, target);
  assert.deepEqual(opened.flow, { asking: target, pending: null });
  assert.equal(opened.field, null, "the field is not touched by opening");
  const closed = closeRestartDialog(opened.flow);
  assert.deepEqual(closed.flow, noRestart);
  assert.equal(closed.field, null);
  assert.deepEqual(dialogKey({ key: "Escape", shiftKey: false }, 1, 2), { act: "cancel" });
});

test("«Скрыть» puts the text and mentions into the field and makes the edit pending", () => {
  const step = hideRestart(openRestart(noRestart, target).flow, typed, null);
  assert.equal(step.flow.asking, null, "the dialog is closed");
  assert.equal(step.flow.pending.turn, 2);
  assert.equal(step.flow.pending.threadId, "a");
  assert.equal(step.flow.pending.problem, "");
  assert.deepEqual(step.field, { draft: "старый вопрос", pills: [mention] });
  assert.deepEqual(step.flow.pending.before, typed, "what the field held is kept");
});

test("«Скрыть» does nothing while it is blocked", () => {
  const opened = openRestart(noRestart, target).flow;
  const step = hideRestart(opened, typed, { reason: "занят", fix: null });
  assert.deepEqual(step.flow, opened);
  assert.equal(step.field, null);
  assert.equal(hideRestart(noRestart, typed, null).flow.pending, null, "no dialog, no edit");
});

test("the text is cut to the field's limit", () => {
  assert.equal(restartDraft({ ...target, text: "я".repeat(5000) }).length, 4000);
});

test("a second pick keeps the very first content of the field for «Отменить»", () => {
  const first = hideRestart(openRestart(noRestart, target).flow, typed, null).flow;
  const again = hideRestart(openRestart(first, { ...target, turn: 1, text: "ещё старее" }).flow, { draft: "старый вопрос", pills: [mention] }, null);
  assert.equal(again.flow.pending.turn, 1);
  assert.deepEqual(again.flow.pending.before, typed);
});

test("«Отменить» clears the pending edit and returns the field to what it was (empty if it was)", () => {
  const pending = hideRestart(openRestart(noRestart, target).flow, typed, null).flow;
  const back = cancelRestartEdit(pending);
  assert.equal(back.flow.pending, null);
  assert.deepEqual(back.field, typed);
  const fromEmpty = hideRestart(openRestart(noRestart, target).flow, { draft: "", pills: [] }, null).flow;
  assert.deepEqual(cancelRestartEdit(fromEmpty).field, { draft: "", pills: [] });
  assert.equal(cancelRestartEdit(noRestart).field, null, "nothing pending, nothing to restore");
});

test("another chat, a new chat or the open chat vanishing clears the pending edit and closes the dialog", () => {
  const pending = hideRestart(openRestart(noRestart, target).flow, typed, null).flow;
  const other = restartOnChat(pending, "b");
  assert.equal(other.flow.pending, null);
  assert.deepEqual(other.field, typed);
  const same = restartOnChat(pending, "a");
  assert.equal(same.flow.pending.turn, 2, "the same chat keeps its edit");
  const dialog = restartOnChat(openRestart(noRestart, target).flow, "b");
  assert.equal(dialog.flow.asking, null);
  assert.equal(restartOnChat(pending, "").flow.pending, null, "no chat open");
});

test("sending spends the pending edit; a refusal brings it back with the reason and the text", () => {
  const pending = hideRestart(openRestart(noRestart, target).flow, typed, null).flow;
  const sent = spendRestart(pending);
  assert.equal(sent.flow.pending, null);
  assert.equal(sent.field, null, "the send empties the field itself");
  const back = refuseRestart(sent.flow, pending.pending, "busy");
  assert.equal(back.flow.pending.turn, 2);
  assert.match(back.flow.pending.problem, /занят/);
  assert.deepEqual(back.field, { draft: "старый вопрос", pills: [mention] });
});

test("refusal texts say why in plain Russian and where to go on", () => {
  assert.match(restartRefusalText("busy"), /Дождитесь конца или остановите/);
  assert.match(restartRefusalText("invalid"), /чат изменился/);
  assert.match(restartRefusalText("offline"), /нет связи/);
  for (const p of ["failed", "unavailable", "", "weird"]) {
    assert.match(restartRefusalText(p), /Можно попробовать ещё раз/);
  }
  for (const p of ["busy", "invalid", "offline", "closed", "failed", ""]) {
    assert.ok(restartRefusalText(p).startsWith("Не удалось "), `the opening of «${p}» is the same`);
  }
  for (const p of ["busy", "invalid", "offline", "closed", "failed"]) {
    assert.doesNotMatch(restartRefusalText(p).replace("paratech", ""), /[A-Za-z]{3}/, `no codes or English in «${p}»`);
  }
});

// ---- The request ----

test("the next send carries restartFrom and the open chat's id; an ordinary send has no restartFrom key", () => {
  const pending = hideRestart(openRestart(noRestart, target).flow, typed, null).flow.pending;
  const base = { ...askInput(ctx, "новый текст", [mention]), threadId: "a" };
  const ask = withRestart(base, pending, "a");
  assert.equal(ask.restartFrom, 2);
  assert.equal(ask.threadId, "a");
  assert.equal(ask.text, "новый текст");
  assert.deepEqual(ask.mentions, [mention]);
  assert.equal("restartFrom" in withRestart(base, null, "a"), false);
  assert.equal("restartFrom" in withRestart(base, pending, "b"), false, "an edit of another chat is not sent");
  assert.equal("restartFrom" in withRestart({ ...base, threadId: "" }, pending, ""), false, "restartFrom needs a chat");
});

// ---- What the panel does with the outcome ----

const sendScope = (over) => ({ text: "новый", subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId: "a", restartFrom: 2, ...over });

test("the highest known number counts live and earlier requests of the chat only", () => {
  const s = twoDone();
  assert.equal(maxKnownTurn(s.entries, [{ turn: 5 }], "a"), 5);
  assert.equal(maxKnownTurn(s.entries, [], "a"), 2);
  assert.equal(maxKnownTurn(s.entries, [], "b"), 0);
});

test("the core naming the new request is the success: the hidden requests leave the view, the new one stays", () => {
  const scope = sendScope();
  let s = reducePanel(twoDone(), { type: "asked", id: 1, text: "новый", scope });
  assert.deepEqual(restartOutcome(scope, s.entries, 2), { state: "wait" });
  s = apply(s, update({ turn: 3, status: "EXECUTING" }));
  assert.deepEqual(restartOutcome(scope, s.entries, 2), { state: "accepted" });
  const after = reducePanel(s, { type: "restarted", threadId: "a", from: 2, upTo: 2 });
  assert.deepEqual(after.entries.map((e) => e.turn).sort(), [1, 3], "request 2 is hidden, 1 and the new one stay");
  // Other chats are untouched.
  const mixed = reducePanel(apply(s, update({ threadId: "b", turn: 2 })), { type: "restarted", threadId: "a", from: 1, upTo: 2 });
  assert.deepEqual(mixed.entries.map((e) => `${e.threadId}${e.turn}`).sort(), ["a3", "b2"]);
});

test("hidden requests take their drafts and cut replies with them", () => {
  let s = twoDone();
  s = { ...s, drafts: [{ threadId: "a", turn: 2, callId: "c", texts: ["x"], reply: true }], cuts: [{ threadId: "a", turn: 2, text: "" }] };
  const after = reducePanel(s, { type: "restarted", threadId: "a", from: 2, upTo: 2 });
  assert.deepEqual([after.drafts.length, after.cuts.length], [0, 0]);
});

test("a refusal arrives as a failed Ask (no thread) or a refused call: the problem code is read, the entry found by its scope", () => {
  const scope = sendScope();
  let s = reducePanel(twoDone(), { type: "asked", id: 1, text: "новый", scope });
  const busy = apply(s, update({ threadId: "", turn: 0, status: "", problem: "busy" }));
  const o = restartOutcome(scope, busy.entries, 2);
  assert.equal(o.state, "refused");
  assert.equal(o.problem, "busy");
  const taken = reducePanel(busy, { type: "discard", key: o.key });
  assert.equal(taken.entries.some((e) => e.key === o.key), false, "the failed request is taken back, its text returns to the field");
  assert.equal(taken.entries.length, 2);
  // A refusal of the call itself.
  const call = reducePanel(s, { type: "askResult", id: 1, problem: "invalid" });
  assert.deepEqual(restartOutcome(scope, call.entries, 2), { state: "refused", key: "local:1", problem: "invalid" });
  // An earlier failed request with the same text does not stand for this one.
  const other = sendScope();
  assert.deepEqual(restartOutcome(other, call.entries, 2), { state: "wait" });
});

test("a request named with a number that is not newer than the known ones is not taken for the new one", () => {
  const scope = sendScope();
  const s = apply(reducePanel(initialPanelState, { type: "asked", id: 1, text: "новый", scope }), update({ turn: 2, status: "EXECUTING" }));
  assert.deepEqual(restartOutcome(scope, s.entries, 2), { state: "wait" });
});

test("the earlier list drops the requests from the chosen one on", () => {
  assert.deepEqual(withoutRestarted([req(1), req(2), req(3)], 2).map((r) => r.turn), [1]);
  assert.deepEqual(withoutRestarted([req(1)], 5).map((r) => r.turn), [1]);
});

// ---- What is drawn ----

const barProps = {
  openChat: { threadId: "a", title: "Чат", archived: false, running: false, lastActive: "" },
  sections: { active: [], archived: [] },
  note: "",
  onOpen: noop,
  onNew: noop,
  onRename: async () => "",
  onArchive: noop,
  onRestore: noop,
};

const panel = (over) =>
  renderToStaticMarkup(
    h(AgentPanelView, {
      state: initialPanelState,
      ctx,
      draft: "",
      notice: "",
      inputRef: { current: null },
      onDraft: noop,
      onSubmit: noop,
      onRemoveContext: noop,
      actionsFor: () => ({ press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined }),
      chat: barProps,
      ...over,
    }),
  );

const restartUi = (over) => ({ targetFor: () => null, open: noop, dialog: null, pending: null, ...over });

test("each earlier message with a number has the quiet action; one without a number has none", () => {
  const earlier = earlierItems([req(1), req(2, { text: "" })], [], "a");
  const html = panel({ earlier, restart: restartUi() });
  // TASK-365: an icon of the action row; the tooltip holds the name, the aria-label adds the start of the message.
  assert.equal(html.split('class="ag-ib ag-ib-restart"').length - 1, 1);
  assert.ok(html.includes("Начать заново отсюда"));
  assert.ok(html.includes('aria-label="Начать заново отсюда: вопрос 1"'));
  assert.ok(!html.includes('aria-modal'), "the dialog is closed");
  assert.ok(!panel({ earlier }).includes("ag-restart"), "no restart UI, no action");
});

test("a live request of the open chat shows the action under its text", () => {
  const state = withText(twoDone());
  const html = panel({ state, restart: restartUi({ targetFor: (e) => liveRestartTarget(e, [], "a") }) });
  assert.equal(html.split('class="ag-ib ag-ib-restart"').length - 1, 2);
});

test("the pending edit shows a note by the field with «Отменить»; the refusal is written there", () => {
  const html = panel({ draft: "старый", restart: restartUi({ pending: { problem: "", onCancel: noop } }) });
  assert.ok(html.includes("Правка сообщения. После отправки всё, что ниже него, пропадёт из чата."));
  assert.ok(!html.includes("Вернуть текст сообщения"), "nothing to bring back without a refusal");
  // TASK-368: an icon button (undo, the default colour) named «Отменить правку» by aria-label and tooltip, no visible word.
  assert.ok(/<button[^>]*aria-label="Отменить правку"[^>]*><svg[^>]*><use href="#i-undo"><\/use><\/svg><span class="ia-tip" aria-hidden="true">Отменить правку<\/span><\/button>/.test(html));
  assert.ok(/aria-describedby="[^"]+"/.test(html), "the field points at the note");
  const failed = panel({ draft: "старый", restart: restartUi({ pending: { problem: restartRefusalText("busy"), onCancel: noop } }) });
  assert.ok(failed.includes("Дождитесь конца или остановите его и отправьте снова"));
  assert.ok(!panel({ draft: "x", restart: restartUi() }).includes("Правка сообщения"));
});

const dialog = (over) =>
  renderToStaticMarkup(
    h(RestartDialog, { block: null, fixBusy: false, forkBusy: false, forkFailure: null, onHide: noop, onFork: noop, onNewChat: noop, onCancel: noop, onFix: noop, ...over }),
  );

// TASK-271 changed this test: the dialog used to have no «Форк» ("there is no «Форк»"); now it has the choice, each button
// with its own line of what it does (the body paragraph became one line per choice).
test("the dialog is a modal with «Скрыть», «Форк» and «Отмена», each choice with its line", () => {
  const html = dialog();
  assert.ok(html.includes('role="dialog"') && html.includes('aria-modal="true"'));
  assert.ok(/aria-labelledby="[^"]+"/.test(html));
  assert.ok(html.includes("Начать заново отсюда?"));
  // TASK-400: the «Скрыть» line is the longer text accepted by the owner (it names the field and the forgetting).
  assert.ok(
    html.includes("Текст сообщения перейдёт в поле ввода. После отправки сообщения ниже пропадут из чата, и агент их забудет. Сделанные действия не отменяются."),
  );
  assert.ok(html.includes("Будет создан новый чат с историей до этого сообщения. Этот чат не изменится."));
  assert.ok(/<button[^>]*class="btn primary sm"[^>]*>Скрыть<\/button>/.test(html));
  assert.ok(/<button[^>]*class="btn sm"[^>]*>(?:<span[^>]*>)*Форк/.test(html), "«Форк» is a plain (secondary) button");
  assert.ok(html.includes(">Отмена</button>"));
  assert.ok(!html.includes("fork"), "the English suffix is the core's, never the page's");
  assert.ok(!html.includes("disabled"), "nothing is off when the request is over");
  assert.equal(html.split("btn primary").length - 1, 1, "one accent button");
  // The order is the owner's: Скрыть, Форк, Отмена; each line is the description of its own button.
  assert.ok(html.indexOf(">Скрыть<") < html.indexOf("Форк") && html.indexOf("Форк") < html.indexOf(">Отмена<"));
  const hide = /<button[^>]*aria-describedby="([^"]+)"[^>]*>Скрыть<\/button>/.exec(html);
  assert.ok(hide && html.includes(`id="${hide[1]}"`) && html.split(`id="${hide[1]}"`)[1].startsWith(' class="ag-restart-body">Текст сообщения перейдёт в поле ввода.'));
  const fork = /<button[^>]*aria-describedby="([^"]+)"[^>]*>(?:<span[^>]*>)*Форк/.exec(html);
  assert.ok(fork && html.split(`id="${fork[1]}"`)[1].startsWith(' class="ag-restart-body">Будет создан новый чат'));
  assert.notEqual(hide[1], fork[1]);
});

test("«Форк» is available while the request runs, «Скрыть» is not; it never needs the reason", () => {
  for (const status of ["EXECUTING", "AWAITING_PLAN"]) {
    const block = restartBlock(apply(initialPanelState, update({ status })).entries, "a", live);
    const html = dialog({ block });
    assert.ok(/<button[^>]*disabled=""[^>]*>Скрыть<\/button>/.test(html), status);
    const fork = /<button([^>]*)>(?:<span[^>]*>)*Форк/.exec(html);
    assert.ok(fork && !fork[1].includes("disabled"), `«Форк» is on while ${status}`);
  }
  const offline = dialog({ block: restartBlock([], "a", { inputEnabled: false, offline: true }) });
  assert.ok(!/<button[^>]*disabled=""[^>]*>(?:<span[^>]*>)*Форк/.test(offline), "even without a link");
});

test("while «Форк» runs its button says so inline and is off; «Отмена» stays", () => {
  const html = dialog({ forkBusy: true });
  assert.ok(/<button[^>]*disabled=""[^>]*aria-busy="true"[^>]*>|<button[^>]*aria-busy="true"[^>]*disabled=""[^>]*>/.test(html));
  assert.ok(html.includes("Создаётся…"));
  assert.ok(html.includes(">Отмена</button>"));
  assert.ok(!dialog().includes("aria-busy"), "idle: not busy");
  // No full-width banner: the busy state adds no status/alert block.
  assert.ok(!html.includes('role="alert"') && !html.includes('role="status"'));
});

// TASK-271 changed this test (UX review): the separate bottom-row «Повторить» is gone, the «Форк» button is the one retry;
// «Новый чат» sits in the «Форк» option, and an invalid refusal now says why and offers «Новый чат».
test("a failed «Форк» says why under it; the «Форк» button is the retry, «Новый чат» is in the same option", () => {
  const retry = dialog({ forkFailure: forkFailure("failed") });
  assert.ok(/<p[^>]*role="alert"[^>]*>Не удалось создать новый чат\. Можно повторить\.<\/p>/.test(retry));
  assert.ok(!retry.includes(">Повторить<"), "one retry only: the «Форк» button");
  assert.ok(!retry.includes(">Новый чат<"));
  const fork = /<button([^>]*)>(?:<span[^>]*>)*Форк/.exec(retry);
  assert.ok(fork && !fork[1].includes("disabled"), "«Форк» is enabled again");
  const gone = dialog({ forkFailure: forkFailure("not_found") });
  assert.ok(gone.includes("Этот чат уже недоступен."));
  assert.ok(/<button[^>]*>Новый чат<\/button>/.test(gone));
  const invalid = dialog({ forkFailure: forkFailure("invalid") });
  assert.ok(invalid.includes("Этого сообщения уже нет в чате: оно скрыто или изменилось. Чат обновлён."));
  assert.ok(/<button[^>]*>Новый чат<\/button>/.test(invalid));
  for (const html of [retry, gone, invalid]) {
    assert.ok(html.includes(">Отмена</button>"), "the dialog can always be left");
    assert.ok(!html.includes(">Повторить<"));
  }
  // «Новый чат» is inside the «Форк» option block, not in the row with «Отмена».
  for (const html of [gone, invalid]) {
    const forkBlock = html.slice(html.indexOf("Форк"), html.indexOf(">Отмена<"));
    assert.ok(forkBlock.includes(">Новый чат<"), "in the «Форк» option");
    const lastRow = html.slice(html.lastIndexOf('class="ag-actions"'));
    assert.ok(!lastRow.includes("Новый чат"), "not next to «Отмена»");
  }
  assert.ok(!dialog().includes('role="alert"'), "no failure, no alert");
});

test("a blocked «Скрыть» points at its line and at the reason; an available one at its line only", () => {
  const running = restartBlock(apply(initialPanelState, update({ status: "EXECUTING" })).entries, "a", live);
  const blocked = dialog({ block: running });
  const m = /<button[^>]*aria-describedby="([^" ]+) ([^" ]+)"[^>]*>Скрыть<\/button>/.exec(blocked);
  assert.ok(m, "two ids");
  assert.ok(blocked.includes(`id="${m[1]}"`) && blocked.includes(`id="${m[2]}"`));
  assert.ok(new RegExp(`id="${m[2]}"[^>]*role="status"`).test(blocked), "the second id is the reason");
  assert.ok(/<button[^>]*aria-describedby="[^" ]+"[^>]*>Скрыть<\/button>/.test(dialog()), "free: its line only");
});

test("the dialog keeps a visible border in forced colors; the css has no glued rules", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.ok(/@media \(forced-colors: active\) \{ \.ag-restart-dlg \{ border: 1px solid CanvasText; \} \}/.test(css));
  assert.ok(!/\}\.[a-z]/.test(css.replace(/@media[^\n]*/g, "")), "every rule starts on its own line");
});

test("while the request runs «Скрыть» is off with the reason in view and «Остановить» next to it", () => {
  const running = restartBlock(apply(initialPanelState, update({ status: "EXECUTING" })).entries, "a", live);
  const html = dialog({ block: running });
  assert.ok(/<button[^>]*disabled=""[^>]*>Скрыть<\/button>/.test(html));
  assert.ok(html.includes("Агент ещё работает над запросом. Дождитесь конца или остановите его."));
  assert.ok(/<button[^>]*>Остановить<\/button>/.test(html));
  assert.ok(html.includes(">Отмена</button>"), "the dialog can always be left");
  // The stop call is on its way: the button says so and is off.
  const stopping = dialog({ block: running, fixBusy: true });
  assert.ok(/<button[^>]*disabled=""[^>]*>Останавливается…<\/button>/.test(stopping));
});

test("while the student's answer is awaited the way out is «Отменить запрос»", () => {
  const waiting = restartBlock(apply(initialPanelState, update({ status: "AWAITING_PLAN" })).entries, "a", live);
  const html = dialog({ block: waiting });
  assert.ok(/<button[^>]*disabled=""[^>]*>Скрыть<\/button>/.test(html));
  assert.ok(html.includes(">Отменить запрос</button>"));
});

test("with no connection the reason is written and there is nothing to press but «Отмена»", () => {
  const html = dialog({ block: restartBlock([], "a", { inputEnabled: false, offline: true }) });
  assert.ok(html.includes("Нет связи с paratech. Начать заново можно, когда связь появится."));
  assert.ok(!html.includes(">Остановить<") && !html.includes("Отменить запрос"));
});

test("the panel draws the dialog over its thread only while it is open", () => {
  const open = panel({
    restart: restartUi({ dialog: { block: null, fixBusy: false, forkBusy: false, forkFailure: null, onHide: noop, onFork: noop, onNewChat: noop, onCancel: noop, onFix: noop } }),
  });
  assert.ok(open.includes('role="dialog"'));
  assert.ok(!panel({ restart: restartUi() }).includes('role="dialog"'));
});

test("keys of the dialog: Esc cancels, Tab and Shift+Tab stay among the buttons", () => {
  assert.deepEqual(dialogKey({ key: "Tab", shiftKey: false }, 0, 2), { act: "focus", index: 1 });
  assert.deepEqual(dialogKey({ key: "Tab", shiftKey: false }, 1, 2), { act: "focus", index: 0 }, "wraps to the first");
  assert.deepEqual(dialogKey({ key: "Tab", shiftKey: true }, 0, 2), { act: "focus", index: 1 }, "wraps to the last");
  assert.deepEqual(dialogKey({ key: "Tab", shiftKey: true }, 1, 2), { act: "focus", index: 0 });
  assert.deepEqual(dialogKey({ key: "Tab", shiftKey: false }, -1, 3), { act: "focus", index: 0 }, "focus outside comes in");
  assert.deepEqual(dialogKey({ key: "Tab", shiftKey: true }, -1, 3), { act: "focus", index: 2 });
  assert.equal(dialogKey({ key: "Enter", shiftKey: false }, 0, 2), null);
  assert.equal(dialogKey({ key: "Tab", shiftKey: false }, 0, 0), null, "no buttons, nothing to trap");
});

// ---- A refused restart (TASK-249 review) ----

const pendingFlow = () => hideRestart(openRestart(noRestart, target).flow, { draft: "", pills: [] }, null).flow;
const sentText = { draft: "правка студента", pills: [mention] };

test("a refusal with an untouched field puts the sent text back and keeps the edit pending with the reason", () => {
  const flow = spendRestart(pendingFlow()).flow;
  const r = restartRefusal(flow, pendingFlow().pending, "busy", { open: true, field: { draft: "", pills: [] }, unsent: sentText });
  assert.equal(r.discard, true);
  assert.deepEqual(r.step.field, sentText, "what the student sent returns, not the old message");
  assert.match(r.step.flow.pending.problem, /^Не удалось отправить: агент ещё занят/);
  assert.equal(r.step.flow.pending.unsent, null);
  assert.equal(r.step.flow.pending.turn, 2, "the edit still points at the same message");
});

test("a refusal with a busy field leaves the field alone, keeps the reason and the refused text for «Вернуть текст сообщения»", () => {
  const flow = spendRestart(pendingFlow()).flow;
  const busy = { draft: "уже набрано заново", pills: [] };
  const r = restartRefusal(flow, pendingFlow().pending, "busy", { open: true, field: busy, unsent: sentText });
  assert.equal(r.step.field, null, "typed text is not overwritten");
  assert.equal(r.discard, true);
  const p = r.step.flow.pending;
  assert.match(p.problem, /занят/);
  assert.deepEqual(p.unsent, sentText);
  assert.deepEqual(p.before, busy, "«Отменить» returns the field as it is now");
  // Pills alone make the field busy as well.
  const pills = restartRefusal(flow, pendingFlow().pending, "offline", { open: true, field: { draft: "", pills: [mention] }, unsent: sentText });
  assert.equal(pills.step.field, null);
  assert.deepEqual(pills.step.flow.pending.unsent, sentText);
  // The note offers the action only while there is something to bring back.
  const html = panel({ draft: "уже набрано заново", restart: restartUi({ pending: { problem: p.problem, onCancel: noop, onRestoreText: noop } }) });
  // «insert» (an arrow into a field), not the undo of the other button.
  assert.ok(/<button[^>]*aria-label="Вернуть текст сообщения"[^>]*><svg[^>]*><use href="#i-insert"><\/use><\/svg><span class="ia-tip" aria-hidden="true">Вернуть текст сообщения<\/span><\/button>/.test(html));
  assert.ok(html.includes("Не удалось отправить: агент ещё занят"));
});

test("«Вернуть текст сообщения» replaces the field with the refused text; «Отменить» then brings the typed text back", () => {
  const flow = spendRestart(pendingFlow()).flow;
  const busy = { draft: "уже набрано заново", pills: [] };
  const refused = restartRefusal(flow, pendingFlow().pending, "busy", { open: true, field: busy, unsent: sentText }).step.flow;
  const back = restoreUnsent(refused, busy);
  assert.deepEqual(back.field, sentText);
  assert.equal(back.flow.pending.unsent, null, "the action is gone once used");
  assert.equal(back.flow.pending.problem, "");
  assert.deepEqual(cancelRestartEdit(back.flow).field, busy);
  assert.equal(restoreUnsent(pendingFlow(), busy).field, null, "nothing refused, nothing to bring back");
  assert.equal(restoreUnsent(noRestart, busy).field, null);
});

test("a refusal restores nothing when the chat is closed or another edit has started", () => {
  const flow = spendRestart(pendingFlow()).flow;
  const closed = restartRefusal(flow, pendingFlow().pending, "busy", { open: false, field: { draft: "", pills: [] }, unsent: sentText });
  assert.deepEqual(closed.step, { flow, field: null });
  assert.equal(closed.discard, false, "the refused request stays where it was");
  const again = pendingFlow();
  const other = restartRefusal(again, pendingFlow().pending, "busy", { open: true, field: { draft: "x", pills: [] }, unsent: sentText });
  assert.equal(other.step.flow, again, "the newer edit is kept as it is");
  assert.equal(other.discard, false);
});

test("«Отправить ещё раз» of a refused restart sends an ordinary request", () => {
  const scope = sendScope({ restartFrom: 2 });
  const plain = withoutRestartFrom(scope);
  assert.equal("restartFrom" in plain, false);
  assert.equal(plain.text, scope.text);
  assert.equal(plain.threadId, "a");
  assert.equal(scope.restartFrom, 2, "the original scope is not changed");
});

test("a hint text from outside ends the pending edit and its pills; with no edit the field is left to the caller", () => {
  const step = restartOnOutsideDraft(pendingFlow(), "подсказка");
  assert.equal(step.flow.pending, null);
  assert.deepEqual(step.field, { draft: "подсказка", pills: [] });
  const none = restartOnOutsideDraft(noRestart, "подсказка");
  assert.deepEqual(none, { flow: noRestart, field: null });
});

test("the action under a message is always visible (quiet, not hidden) and its tooltip says what it does", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  // TASK-365: the text link became an icon of the row (.ag-ib): muted, never hidden, 28px; the name is the tooltip.
  const rule = css.split("\n").filter((l) => l.startsWith(".ag-ib { "));
  assert.equal(rule.length, 1);
  assert.match(rule[0], /color: var\(--muted\)/);
  assert.match(rule[0], /width: 28px; height: 28px/);
  assert.doesNotMatch(rule[0], /opacity/);
  const html = panel({ earlier: earlierItems([req(1)], [], "a"), restart: restartUi() });
  assert.ok(html.includes(`<span class="ag-ib-tip" aria-hidden="true">${restartTexts.action}</span>`));
  assert.ok(!/<button[^>]*ag-ib-restart[^>]*title=/.test(html), "the tooltip replaces the native hint");
});

// TASK-271 changed this test: with two choices the one body line became one line per button, so each of «Скрыть» and
// «Форк» points at its own line and the dialog itself is named by its title only. UX review: a blocked «Скрыть» also
// points at the reason (second id), so the reason is read with the disabled button; «Форк» keeps its one line.
test("each choice points at its own line; a blocked «Скрыть» also at the reason, which is a live region", () => {
  const running = restartBlock(apply(initialPanelState, update({ status: "EXECUTING" })).entries, "a", live);
  const html = dialog({ block: running });
  assert.ok(!/role="dialog"[^>]*aria-describedby/.test(html), "the dialog is named by its title");
  const pointers = [...html.matchAll(/<button[^>]*aria-describedby="([^"]+)"/g)].map((m) => m[1]);
  assert.equal(pointers.length, 2, "«Скрыть» and «Форк»");
  assert.equal(pointers[0].split(" ").length, 2, "«Скрыть» while blocked: its line and the reason");
  assert.equal(pointers[1].split(" ").length, 1, "«Форк»: its line only");
  assert.ok(html.includes('role="status"'));
});

test("while «Отменить запрос» runs it says «Отменяется…» and is off", () => {
  const waiting = restartBlock(apply(initialPanelState, update({ status: "AWAITING_PLAN" })).entries, "a", live);
  const html = dialog({ block: waiting, fixBusy: true });
  assert.ok(/<button[^>]*disabled=""[^>]*>Отменяется…<\/button>/.test(html));
});

test("texts are impersonal and the dialog's words are as agreed", () => {
  assert.equal(restartTexts.title, "Начать заново отсюда?");
  assert.equal(restartTexts.hide, "Скрыть");
  assert.equal(restartTexts.cancel, "Отмена");
  for (const t of Object.values(restartTexts)) {
    assert.doesNotMatch(t, /(^|[^\p{L}])(я|мы|вы)([^\p{L}]|$)/iu);
  }
});
