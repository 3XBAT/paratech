// TASK-399: the waiting row of a queued question (ticket), «Отменить этот запрос» and its outcomes, the late-queued update
// that must never become a failed Ask, and D3 (the resend leaves one entry).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  hasTicket,
  initialPanelState,
  maxQueuedQuestions,
  orderedEntries,
  queueChat,
  queueIsFull,
  reducePanel,
} from "../.test-build/agentState.js";
import { isRunningEntry, syncRunStarts } from "../.test-build/agentElapsed.js";
import { entryModel, texts, withdrawStep } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { ariaLabels, buttonTag, isOff, isOn, visibleText } from "./iconbtn.mjs";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const scope = { text: "второй вопрос", subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId: "t1", mentions: [] };
const update = (over) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, retry: 0, cards: [], ticket: "", queued: false, ...over,
});
const lateQueued = (over) => update({ turn: 0, status: "", ticket: "q7", queued: true, ...over });
const apply = (s, ...as) => as.reduce(reducePanel, s);
const asked = (id, sc = scope) => ({ type: "asked", id, text: sc.text, scope: sc });
const queuedResult = (id, ticket = "q7") => ({ type: "askResult", id, problem: "", queued: true, ticket });
const running = update({ status: "EXECUTING" });

const readPanel = () => readFileSync(new URL("../src/agent/AgentPanel.tsx", import.meta.url), "utf8");

const render = (entry, props = {}, acts = actions) =>
  renderToStaticMarkup(h(AgentRequest, { model: entryModel(entry, { last: true, blocked: false }), actions: acts, ...props }));

// ---- The waiting row ----

test("askResult queued: the question is accepted (no call problem) and waits under its ticket", () => {
  const s = apply(initialPanelState, update_(running), asked(1), queuedResult(1));
  const e = s.entries.find((x) => x.text === "второй вопрос");
  assert.equal(e.callProblem, "");
  assert.equal(e.ticket, "q7");
  assert.equal(queueChat(e), "t1");
  assert.ok(hasTicket(e));
  assert.equal(entryModel(e).callLine, "");
  assert.equal(entryModel(e).queued, true);
});

function update_(u) {
  return { type: "update", update: u };
}

test("askResult: a started question and an unknown id change nothing; a refusal still marks the call", () => {
  const sent = apply(initialPanelState, asked(1));
  assert.equal(reducePanel(sent, { type: "askResult", id: 1, problem: "" }), sent);
  assert.equal(reducePanel(sent, { type: "askResult", id: 1, problem: "", queued: false, ticket: "" }), sent);
  assert.equal(reducePanel(sent, queuedResult(9)), sent);
  assert.equal(reducePanel(sent, { type: "askResult", id: 1, problem: "offline" }).entries[0].callProblem, "offline");
});

test("the waiting row: «ждёт предыдущий запрос», still dots, the stop icon and «Отменить этот запрос», no timer", () => {
  const s = apply(initialPanelState, update_(running), asked(1), queuedResult(1));
  const e = s.entries.find((x) => x.text === "второй вопрос");
  const html = render(e, { startedAt: undefined }, { ...actions, withdraw: noop });
  assert.ok(html.includes('<span class="ag-queued small muted">ждёт предыдущий запрос</span>'));
  assert.ok(html.includes("ag-wait queued"));
  assert.deepEqual(ariaLabels(html), ["Остановить предыдущий запрос", "Отменить этот запрос"]);
  // Icons carry no visible word: the words are the tooltip and the name.
  assert.ok(html.includes('<span class="ia-tip" aria-hidden="true">Отменить этот запрос</span>'));
  assert.equal(visibleText(html).includes("Отменить этот запрос"), false);
  assert.ok(!html.includes("ag-elapsed"));
  assert.ok(isOn(html, "Отменить этот запрос"));
  assert.equal(texts.withdraw, "Отменить этот запрос");
});

test("the withdraw icon is danger-coloured by the icon rules only, and shows a spinner while its call runs", () => {
  const e = { ...apply(initialPanelState, asked(1)).entries[0], ticket: "q7" };
  const html = render(e, {}, { ...actions, withdraw: noop });
  assert.match(buttonTag(html, "Отменить этот запрос"), /class="ia ia-danger"/);
  const busy = render(e, {}, { ...actions, withdraw: noop, withdrawBusy: true });
  assert.ok(isOff(busy, "Отменить этот запрос"));
  assert.match(busy, /<span class="ia-spin"/);
  // A press elsewhere (busy) switches both icons off.
  const off = render(e, {}, { ...actions, withdraw: noop, busy: true });
  assert.ok(isOff(off, "Отменить этот запрос") && isOff(off, "Остановить предыдущий запрос"));
});

test("a row without the withdraw handler (an entry that is not waiting) has no such icon", () => {
  const e = { ...apply(initialPanelState, asked(1)).entries[0], ticket: "q7" };
  assert.deepEqual(ariaLabels(render(e)), ["Остановить предыдущий запрос"]);
  // Not queued, no ticket: plain dots, no row.
  const plain = render(apply(initialPanelState, asked(1)).entries[0], {}, { ...actions, withdraw: noop });
  assert.ok(!plain.includes("ждёт предыдущий запрос"));
  assert.ok(!plain.includes("Отменить этот запрос"));
});

test("a waiting question has no timer; once started it does", () => {
  const e = { ...apply(initialPanelState, asked(1)).entries[0], ticket: "q7" };
  assert.equal(isRunningEntry([e], e), false);
  assert.deepEqual(syncRunStarts({}, [e], 100), {});
  const started = apply(initialPanelState, asked(1), queuedResult(1), update_(update({ turn: 2, status: "PLANNING", ticket: "q7" })));
  assert.equal(isRunningEntry(started.entries, started.entries[0]), true);
});

// ---- Started: the update with the ticket replaces the row ----

test("an update that carries the ticket replaces the waiting entry (same seq, text kept), not another one", () => {
  const sc2 = { ...scope, text: "третий" };
  const s = apply(initialPanelState, asked(1), queuedResult(1, "q7"), asked(2, sc2), queuedResult(2, "q8"));
  // The second ticket starts first: it must find ITS entry, not the oldest.
  const next = reducePanel(s, update_(update({ turn: 2, status: "PLANNING", ticket: "q8" })));
  assert.equal(next.entries.length, 2);
  const third = next.entries.find((x) => x.text === "третий");
  assert.equal(third.key, "thread:t1:2");
  assert.equal(third.update.status, "PLANNING");
  const second = next.entries.find((x) => x.text === "второй вопрос");
  assert.equal(second.update, null);
  assert.equal(second.ticket, "q7");
  assert.equal(entryModel(second).queued, true);
  assert.equal(entryModel(third).queued, false);
});

test("a ticketed entry is never picked for an update without its ticket", () => {
  const waiting = apply(initialPanelState, update_(running), asked(1), queuedResult(1));
  // The running request's own next update (no ticket) must not take the waiting question's place.
  const next = reducePanel(waiting, update_(update({ turn: 2, status: "PLANNING" })));
  const e = next.entries.find((x) => x.text === "второй вопрос");
  assert.equal(e.update, null);
  assert.equal(e.key.startsWith("local:"), true);
  assert.equal(next.entries.find((x) => x.key === "thread:t1:2").text, "");
  // A failed Ask of the scope (no thread) does not take it either: it becomes its own refused entry.
  const failed = reducePanel(waiting, update_(update({ threadId: "", turn: 0, status: "", problem: "failed" })));
  assert.equal(failed.entries.find((x) => x.text === "второй вопрос").update, null);
});

test("a queued question that failed to start (its ticket on a failed update) turns into the refusal", () => {
  const waiting = apply(initialPanelState, asked(1), queuedResult(1));
  const next = reducePanel(waiting, update_(update({ turn: 0, status: "", problem: "failed", ticket: "q7" })));
  assert.equal(next.entries.length, 1);
  assert.equal(next.entries[0].text, "второй вопрос");
  assert.equal(next.entries[0].update.problem, "failed");
  assert.equal(entryModel(next.entries[0]).queued, false);
});

test("later updates of the started request keep replacing it by thread and turn", () => {
  const s = apply(
    initialPanelState,
    asked(1),
    queuedResult(1),
    update_(update({ turn: 2, status: "PLANNING", ticket: "q7" })),
    update_(update({ turn: 2, status: "EXECUTING", progress: true })),
    update_(update({ turn: 2, status: "DONE" })),
  );
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].update.status, "DONE");
  assert.equal(s.entries[0].text, "второй вопрос");
});

// ---- The late-queued update ----

test("a queued update (turn 0, no status) gives its ticket to the sent question; never an orphan, a failure or a problem", () => {
  const s = apply(initialPanelState, update_(running), asked(1), update_(lateQueued()));
  assert.equal(s.entries.length, 2);
  const e = s.entries.find((x) => x.text === "второй вопрос");
  assert.equal(e.ticket, "q7");
  assert.equal(e.update, null);
  assert.equal(e.callProblem, "");
  assert.equal(queueChat(e), "t1");
  assert.ok(!s.entries.some((x) => x.key.startsWith("orphan:")));
  // The ask result that follows (queued false: the Driver found the chat busy only when it ran) changes nothing.
  const after = reducePanel(s, { type: "askResult", id: 1, problem: "", queued: false, ticket: "" });
  assert.equal(after, s);
  assert.equal(entryModel(e).queued, true);
});

test("a queued update with no sent question, or for a ticket already known, is ignored", () => {
  const none = apply(initialPanelState, update_(running), update_(lateQueued()));
  assert.equal(none.entries.length, 1);
  assert.ok(!none.entries.some((x) => x.key.startsWith("orphan:")));
  assert.equal(none.entries[0].update.status, "EXECUTING");
  const known = apply(initialPanelState, asked(1), queuedResult(1), asked(2), update_(lateQueued()));
  assert.equal(known.entries.find((x) => x.text === "второй вопрос" && x.ticket === undefined) !== undefined, true, "the other question stays without a ticket");
  assert.equal(known.entries.filter((x) => x.ticket === "q7").length, 1);
  // A repeat of the same queued update changes nothing.
  const again = reducePanel(known, update_(lateQueued()));
  assert.equal(again, known);
});

test("a queued update of another chat or scope does not touch a question sent elsewhere", () => {
  const other = { ...scope, threadId: "t2" };
  const s = apply(initialPanelState, asked(1, other), update_(lateQueued({ threadId: "t1" })));
  assert.equal(s.entries[0].ticket, undefined);
  const screen = apply(initialPanelState, asked(1), update_(lateQueued({ screen: "settings" })));
  assert.equal(screen.entries[0].ticket, undefined);
});

test("a queued update never reaches the compaction early return of reducePanel", () => {
  const s = apply(initialPanelState, asked(1), update_({ ...lateQueued(), compaction: true, compacting: true }));
  // A compaction report is no request: still dropped before anything else.
  assert.equal(s.entries[0].ticket, undefined);
  assert.equal(s.cuts.length, 0);
  assert.equal(s.drafts.length, 0);
});

// ---- Withdraw outcomes ----

test("withdraw: withdrawn takes the row away and the text back into an EMPTY field only", () => {
  assert.deepEqual(withdrawStep("withdrawn", true), { discard: true, restore: true, note: "" });
  const kept = withdrawStep("withdrawn", false);
  assert.equal(kept.discard, true);
  assert.equal(kept.restore, false, "a typed draft is never overwritten");
  assert.equal(kept.note, texts.withdrawKept);
  assert.match(kept.note, /Запрос отменён/);
});

test("withdraw: started does nothing (the update with the ticket replaces the row)", () => {
  assert.deepEqual(withdrawStep("started", true), { discard: false, restore: false, note: "" });
  assert.deepEqual(withdrawStep("started", false), { discard: false, restore: false, note: "" });
});

test("withdraw: not_found and invalid mean the question will never run: row away, text back like withdrawn", () => {
  for (const r of ["not_found", "invalid"]) {
    assert.deepEqual(withdrawStep(r, true), withdrawStep("withdrawn", true), r);
    assert.deepEqual(withdrawStep(r, false), withdrawStep("withdrawn", false), r);
  }
});

test("withdraw: failed and a thrown call keep the row and say so, with the way on", () => {
  for (const r of ["failed", "thrown"]) {
    const step = withdrawStep(r, true);
    assert.equal(step.discard, false);
    assert.equal(step.restore, false);
    assert.equal(step.note, "Не удалось отменить запрос. Можно нажать ещё раз.");
  }
});

test("discard removes the waiting row, and only that one", () => {
  const s = apply(initialPanelState, update_(running), asked(1), queuedResult(1), asked(2), queuedResult(2, "q8"));
  const gone = reducePanel(s, { type: "discard", key: "local:1" });
  assert.deepEqual(orderedEntries(gone.entries).map((x) => x.text), ["", "второй вопрос"]);
  assert.equal(gone.entries.find((x) => x.text === "второй вопрос").ticket, "q8");
});

test("AgentPanel wires withdraw once per press, to the chat of the question, and gates the text restore", () => {
  // The panel is a React container, not rendered in these tests: its wiring is read like the other panel tests do.
  const src = readPanel();
  assert.match(src, /pressed\.current\.mark\(waitingKey, null\)/);
  assert.match(src, /api\.withdraw\(chat, e\.ticket \?\? ""\)/);
  assert.match(src, /withdrawStep\(still \? result : "started", fieldEmpty\)/);
  assert.match(src, /draftRef\.current\.trim\(\) === "" && pillsRef\.current\.length === 0/);
  assert.match(src, /\{ type: "askResult", id, problem, queued, ticket \}/);
});

// ---- Five waiting questions (the 6th is refused as busy) ----

test("queueIsFull counts the waiting questions of the refused one's chat", () => {
  let s = apply(initialPanelState, update_(running));
  for (let i = 1; i <= maxQueuedQuestions; i++) {
    s = apply(s, asked(i, { ...scope, text: `в ${i}` }), queuedResult(i, `q${i}`));
  }
  const sixth = apply(s, asked(6, { ...scope, text: "шестой" }), update_(update({ threadId: "", turn: 0, status: "", problem: "busy" })));
  const refused = sixth.entries.find((x) => x.text === "шестой");
  assert.equal(refused.update.problem, "busy");
  assert.equal(queueIsFull(sixth.entries, refused), true);
  assert.equal(queueIsFull(sixth.entries.filter((x) => x.ticket !== "q5"), refused), false, "four wait: not the queue limit");
  assert.equal(maxQueuedQuestions, 5);
  // Another chat's queue does not count.
  const elsewhere = { ...refused, scope: { ...refused.scope, threadId: "t2" } };
  assert.equal(queueIsFull(sixth.entries, elsewhere), false);
});

test("the busy line of the sixth question says why and what to do, with the existing icons", () => {
  const refused = {
    key: "local:6", seq: 6, threadId: "", turn: 0, text: "шестой", scope, callProblem: "",
    update: update({ threadId: "", turn: 0, status: "", problem: "busy" }),
  };
  const full = entryModel(refused, { last: true, blocked: true, queueFull: true });
  assert.equal(
    full.cards[0].text,
    "В очереди уже пять вопросов, этот не принят. Предыдущий запрос ещё выполняется. Его можно остановить или отправить этот ещё раз.",
  );
  assert.deepEqual(full.cards[0].buttons.map((b) => b.id), ["stop", "resend"]);
  const noStop = entryModel(refused, { last: true, blocked: false, queueFull: true });
  assert.equal(noStop.cards[0].text, "В очереди уже пять вопросов, этот не принят. Предыдущий запрос ещё выполняется. Этот можно отправить ещё раз.");
  // Busy for another reason: the old line.
  assert.equal(entryModel(refused, { last: true, blocked: true }).cards[0].text, "Предыдущий запрос ещё выполняется. Его можно остановить или отправить этот ещё раз.");
});

// ---- D3: the resend leaves one entry ----

test("D3: after the resend the refused entry is gone: the question appears once and no busy line stays", () => {
  const refusedState = apply(
    initialPanelState,
    update_(running),
    asked(1),
    update_(update({ threadId: "", turn: 0, status: "", problem: "busy" })),
  );
  const refused = refusedState.entries.find((x) => x.text === "второй вопрос");
  assert.equal(refused.update.problem, "busy");
  // What the panel does on «Отправить ещё раз»: discard the refused entry, then send the question again.
  const resent = apply(refusedState, { type: "discard", key: refused.key }, asked(2), queuedResult(2, "q9"));
  const same = resent.entries.filter((x) => x.text === "второй вопрос");
  assert.equal(same.length, 1);
  assert.equal(same[0].update, null);
  assert.equal(same[0].ticket, "q9");
  const html = renderToStaticMarkup(
    h(AgentRequest, { model: entryModel(same[0], { last: true, blocked: false }), actions: { ...actions, withdraw: noop } }),
  );
  assert.ok(!html.includes("Предыдущий запрос ещё выполняется"));
  // Once it runs and is answered, no refusal line is left anywhere in the thread.
  const answered = apply(resent, update_(update({ turn: 2, status: "DONE", ticket: "q9" })));
  for (const e of answered.entries) {
    assert.ok(!JSON.stringify(entryModel(e, { last: true, blocked: false }).cards).includes("ещё выполняется"), e.key);
  }
  assert.equal(answered.entries.filter((x) => x.text === "второй вопрос").length, 1);
});

test("D3: if the resend is itself refused, the new entry shows its own busy line (still one entry with the text)", () => {
  const s = apply(
    initialPanelState,
    update_(running),
    asked(1),
    update_(update({ threadId: "", turn: 0, status: "", problem: "busy" })),
    { type: "discard", key: "local:1" },
    asked(2),
    update_(update({ threadId: "", turn: 0, status: "", problem: "busy" })),
  );
  const same = s.entries.filter((x) => x.text === "второй вопрос");
  assert.equal(same.length, 1);
  assert.equal(same[0].update.problem, "busy");
});

test("AgentPanel discards the refused entry before it sends the question again", () => {
  const src = readPanel();
  const at = src.indexOf('id === "resend"');
  assert.ok(at > 0);
  const block = src.slice(at, at + 900);
  assert.ok(block.indexOf('dispatch({ type: "discard", key: e.key })') > 0);
  assert.ok(block.indexOf('dispatch({ type: "discard", key: e.key })') < block.indexOf("send(withoutRestartFrom(e.scope))"));
});
