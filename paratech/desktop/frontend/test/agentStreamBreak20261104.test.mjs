// BUG 2026-11-04 (agent stream): a broken reply keeps the text already shown; the elapsed timer; the queued request.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { blockingEntry, cutFor, draftsFor, initialPanelState, queuedBehind, reducePanel } from "../.test-build/agentState.js";
import { elapsedLabel, elapsedSeconds, isRunningEntry, syncRunStarts } from "../.test-build/agentElapsed.js";
import { entryModel, formatElapsed, texts } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";
import { Elapsed } from "../.test-build/agent/Elapsed.js";
import { contextFor } from "../.test-build/agentView.js";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };

const update = (over) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, cards: [], ...over,
});
const typing = (over) => ({ threadId: "t1", turn: 1, callId: "step:reply", texts: ["Длинный ответ, который уже виден"], reply: true, ...over });
const apply = (s, ...as) => as.reduce((acc, a) => reducePanel(acc, a), s);
const upd = (over) => ({ type: "update", update: update(over) });
const drf = (d) => ({ type: "draft", draft: d });
const replyCard = { kind: "reply", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "итог", stopReason: "", workId: "", outcome: "" };

const render = (s, extra = {}, i = 0) => {
  const e = s.entries[i];
  return renderToStaticMarkup(
    h(AgentRequest, {
      model: entryModel(e, { last: true, blocked: false, replyCut: cutFor(s, e) !== undefined, ...extra.ctx }),
      actions,
      drafts: draftsFor(s, e),
      cut: cutFor(s, e),
      ...extra.props,
    }),
  );
};
const retryButtons = (html) => (html.match(/aria-label="Повторить"/g) ?? []).length;
const ended = (over) => apply(initialPanelState, upd({}), drf(typing({})), upd({ status: "STOPPED", ...over }));

// ---- Part 1: the shown text stays ----

test("a reply that broke off (offline) keeps its text above «Ответ оборван.» with one retry icon and no second caption", () => {
  const s = ended({ stopReason: "offline" });
  assert.equal(s.drafts.length, 0);
  assert.equal(cutFor(s, s.entries[0]).text, "Длинный ответ, который уже виден");
  const html = render(s);
  assert.ok(html.includes('<section class="ag-card ag-reply-kept"><p class="ag-text muted">Длинный ответ, который уже виден</p></section>'));
  assert.ok(html.includes("Ответ оборван."));
  assert.ok(!html.includes(texts.replyStopped));
  assert.ok(!html.includes("Можно повторить"));
  assert.ok(!html.includes("ag-cursor"));
  assert.ok(html.indexOf("ag-reply-kept") < html.indexOf("ag-stop"));
  assert.equal(retryButtons(html), 1);
  assert.ok(!html.includes("Повторить ответ"));
});

test("a reply that failed keeps its text and offers the retry icon of the failed card once", () => {
  const s = ended({ stopReason: "failed", problem: "failed" });
  const html = render(s);
  assert.ok(html.includes("Длинный ответ, который уже виден"));
  assert.equal(html.split("Ответ оборван.").length - 1, 1);
  // The generic problem line under the cut line would be a second line.
  assert.ok(!html.includes("Не удалось выполнить запрос."));
  assert.equal(retryButtons(html), 1);
  assert.ok(!html.includes(texts.replyStopped));
});

test("a failed request with no stop reason keeps the text above the problem line, which says the reply broke off", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})), upd({ status: "FAILED", problem: "failed" }));
  const html = render(s);
  assert.ok(html.indexOf("Длинный ответ") < html.indexOf("Ответ оборван."));
  assert.equal(html.split("Ответ оборван.").length - 1, 1);
  // The failure of a request with nothing to show keeps its generic line.
  const plain = apply(initialPanelState, upd({ status: "FAILED", problem: "failed" }));
  assert.ok(render(plain).includes("Не удалось выполнить запрос."));
});

test("«Остановить» keeps the text with its caption and «Остановлено», no «Ответ оборван.»", () => {
  const s = ended({ stopReason: "user" });
  const html = render(s);
  assert.ok(html.includes('<p class="ag-draft-cap small muted">Ответ остановлен</p>'));
  assert.ok(html.includes("Остановлено"));
  assert.ok(!html.includes("Ответ оборван"));
});

test("quota, expired and budget keep the text and their own stop line", () => {
  const lines = { quota: "Не завершено: закончились минуты обработки.", expired: "Запрос устарел.", budget: "Остановлено: лимит запроса" };
  for (const [stopReason, line] of Object.entries(lines)) {
    const html = render(ended({ stopReason }));
    assert.ok(html.includes("Длинный ответ, который уже виден"), stopReason);
    assert.ok(html.includes(line), stopReason);
    assert.ok(!html.includes("Ответ оборван"), stopReason);
    assert.ok(!html.includes(texts.replyStopped), stopReason);
  }
});

test("the kept text is a plain text node for every end", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({ texts: ["<img src=x onerror=y>"] })), upd({ status: "STOPPED", stopReason: "offline" }));
  const html = render(s);
  assert.ok(html.includes("&lt;img src=x onerror=y&gt;"));
  assert.ok(!html.includes("<img"));
});

test("the end update is the same whether it comes before or after the last draft: nothing shown is lost", () => {
  const before = apply(initialPanelState, upd({}), drf(typing({ texts: ["Привет"] })), upd({ status: "STOPPED", stopReason: "offline" }), drf(typing({ texts: ["Привет, мир"] })));
  assert.equal(cutFor(before, before.entries[0]).text, "Привет");
  assert.equal(before.drafts.length, 0);
  const after = apply(initialPanelState, upd({}), drf(typing({ texts: ["Привет"] })), drf(typing({ texts: ["Привет, мир"] })), upd({ status: "STOPPED", stopReason: "offline" }));
  assert.equal(cutFor(after, after.entries[0]).text, "Привет, мир");
});

test("a repeat of the end update, a refused button and a late draft keep the text", () => {
  const s = apply(
    ended({ stopReason: "offline" }),
    upd({ status: "STOPPED", stopReason: "offline" }),
    upd({ status: "STOPPED", stopReason: "offline", progress: true }),
    upd({ status: "", problem: "busy" }),
    drf(typing({ texts: ["поздно"] })),
  );
  assert.equal(cutFor(s, s.entries[0]).text, "Длинный ответ, который уже виден");
  assert.ok(render(s).includes("Длинный ответ"));
});

test("an end update marked as a step of progress still ends the draft and keeps the text", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})), upd({ status: "STOPPED", stopReason: "offline", progress: true }));
  assert.equal(s.drafts.length, 0);
  assert.equal(cutFor(s, s.entries[0]).text, "Длинный ответ, который уже виден");
});

test("an update of an older turn (late, or the history) does not clear the kept text of the newer one", () => {
  const s = apply(
    initialPanelState,
    upd({ turn: 1, status: "DONE", cards: [replyCard] }),
    upd({ turn: 2, status: "EXECUTING" }),
    drf(typing({ turn: 2 })),
    upd({ turn: 2, status: "STOPPED", stopReason: "offline" }),
  );
  assert.equal(s.cuts.length, 1);
  assert.equal(apply(s, upd({ turn: 1, status: "DONE", cards: [replyCard] })).cuts.length, 1);
  assert.equal(apply(s, upd({ turn: 1, status: "STOPPED", stopReason: "user" })).cuts.length, 1);
  assert.equal(apply(s, { type: "history", requests: [update({ turn: 1, status: "DONE", cards: [replyCard] })] }).cuts.length, 1);
  // The newer turn's own next update does end it.
  assert.equal(apply(s, upd({ turn: 2, status: "EXECUTING" })).cuts.length, 0);
  assert.equal(apply(s, upd({ turn: 3, status: "PLANNING" })).cuts.length, 0);
});

test("«Повторить» / «Продолжить» replace the kept text: the next update of the request ends the record", () => {
  for (const stopReason of ["offline", "failed", "user"]) {
    const s = ended({ stopReason });
    assert.equal(s.cuts.length, 1);
    const again = apply(s, upd({ status: "EXECUTING" }));
    assert.equal(again.cuts.length, 0, stopReason);
    assert.ok(!render(again).includes("ag-reply-kept"));
    assert.ok(!render(again).includes("Длинный ответ"));
  }
});

test("a reply that had no text yet leaves nothing to keep and the normal stop line", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({ texts: [""] })), upd({ status: "STOPPED", stopReason: "offline" }));
  assert.equal(s.cuts.length, 0);
  const html = render(s);
  assert.ok(html.includes("Не завершено: нет связи."));
  assert.ok(!html.includes("ag-reply-kept"));
  assert.ok(!html.includes("Ответ оборван"));
  assert.equal(retryButtons(html), 1);
});

test("the problem's own «Повторить» is the only one on a cut reply", () => {
  const s = ended({ stopReason: "failed", problem: "retry_required" });
  const html = render(s);
  assert.ok(html.includes("Длинный ответ"));
  assert.ok(html.includes("Ответ оборван."));
  assert.equal(retryButtons(html), 1);
});

test("the text of an old, longer cut stays after the old wording is gone", () => {
  const model = entryModel(ended({ stopReason: "offline" }).entries[0], { last: true, blocked: false, replyCut: true });
  assert.equal(model.cards.find((c) => c.type === "stopped").text, "Ответ оборван.");
  assert.equal(texts.replyCut, "Ответ оборван.");
});

// ---- Part 2: the elapsed timer ----

test("formatElapsed: seconds, then minutes and seconds, never garbage", () => {
  const cases = [[0, "0с"], [1, "1с"], [59, "59с"], [60, "1м 0с"], [65, "1м 5с"], [125, "2м 5с"], [3599, "59м 59с"], [3600, "60м 0с"], [-5, "0с"], [Number.NaN, "0с"], [Number.POSITIVE_INFINITY, "0с"], [1.9, "1с"]];
  for (const [seconds, want] of cases) {
    assert.equal(formatElapsed(seconds), want, String(seconds));
  }
});

test("elapsed time is computed from the timestamps and never negative", () => {
  assert.equal(elapsedSeconds(1000, 1999), 0);
  assert.equal(elapsedSeconds(1000, 2000), 1);
  assert.equal(elapsedSeconds(1000, 66000), 65);
  assert.equal(elapsedSeconds(5000, 1000), 0);
  assert.equal(elapsedSeconds(Number.NaN, 1000), 0);
  assert.equal(elapsedLabel(0, 125000), "2м 5с");
});

const local = (over) => ({ key: "local:1", seq: 1, threadId: "", turn: 0, text: "q", scope: { subjectId: "", lessonId: "", assignmentId: "", screen: "today", threadId: "", mentions: [] }, callProblem: "", update: null, ...over });
const named = (seq, status, over = {}) => ({ key: `thread:t1:${seq}`, seq, threadId: "t1", turn: seq, text: "", scope: null, callProblem: "", update: update({ turn: seq, status, ...over }), ...over.entry });

test("the timer starts at the local send, survives the first update, stops on a stop and restarts at 0 on a new run (fake clock)", () => {
  let clock = 100_000;
  const at = () => clock;
  let starts = syncRunStarts({}, [local({})], at());
  assert.deepEqual(starts, { 1: 100_000 });

  // The same request named by the core keeps its seq: the start stays.
  clock = 107_000;
  const running = [named(1, "EXECUTING")];
  starts = syncRunStarts(starts, running, at());
  assert.equal(starts[1], 100_000);
  assert.equal(elapsedLabel(starts[1], at()), "7с");
  // Nothing changed: the same object (no state update, no re-render).
  assert.equal(syncRunStarts(starts, running, at()), starts);

  // A stop removes it...
  clock = 110_000;
  starts = syncRunStarts(starts, [named(1, "STOPPED", { stopReason: "user" })], at());
  assert.deepEqual(starts, {});

  // ...and «Продолжить» counts from 0 again.
  clock = 120_000;
  starts = syncRunStarts(starts, [named(1, "EXECUTING")], at());
  assert.equal(elapsedLabel(starts[1], at()), "0с");
  clock = 185_000;
  assert.equal(elapsedLabel(starts[1], at()), "1м 5с");
});

test("the timer is gone for every state that is not working: stopped, failed, done, a card to answer, a failed call", () => {
  for (const status of ["STOPPED", "FAILED", "DONE", "CANCELLED", "AWAITING_PLAN", "AWAITING_ANSWERS", ""]) {
    assert.deepEqual(syncRunStarts({ 1: 5 }, [named(1, status)], 9), {}, status);
  }
  assert.deepEqual(syncRunStarts({ 1: 5 }, [local({ callProblem: "offline" })], 9), {});
  assert.deepEqual(syncRunStarts({}, [named(1, "PLANNING")], 9), { 1: 9 });
  // A request that left the thread is forgotten.
  assert.deepEqual(syncRunStarts({ 7: 1 }, [], 9), {});
});

test("two running requests of different chats have timers of their own", () => {
  const a = named(1, "EXECUTING");
  const b = { ...named(2, "EXECUTING"), key: "thread:t2:1", threadId: "t2" };
  b.update = update({ threadId: "t2", turn: 1, status: "EXECUTING" });
  const first = syncRunStarts({}, [a], 1000);
  const both = syncRunStarts(first, [a, b], 4000);
  assert.deepEqual(both, { 1: 1000, 2: 4000 });
});

test("the timer is a quiet, muted, hidden-from-readers text beside the dots; it is no live region", () => {
  const s = apply(initialPanelState, upd({}));
  const html = render(s, { props: { startedAt: 10_000, clock: () => 75_000 } });
  assert.ok(html.includes('<span class="ag-elapsed small muted" aria-hidden="true">1м 5с</span>'));
  const wait = html.match(/<span class="ag-wait" role="status">.*?<\/span><\/span>/s)[0];
  assert.ok(!wait.includes("ag-elapsed"));
  // The two regions of a running request (the dots and the empty reply status) are the same as before the timer.
  assert.equal(html.match(/role="status"/g).length, 2);
  assert.ok(!/role="status"[^>]*>[^<]*1м/.test(html));
  assert.ok(html.indexOf("ag-wait") < html.indexOf("ag-elapsed") && html.indexOf("ag-elapsed") < html.indexOf("aria-label=\"Остановить\""));
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  const rule = css.match(/\.ag-elapsed\s*\{[^}]*\}/)?.[0] ?? "";
  assert.match(rule, /tabular-nums/);
  assert.doesNotMatch(rule, /accent|animation/);
});

test("the timer stays next to «Остановить» while the reply types, and is absent without a start", () => {
  const s = apply(initialPanelState, upd({}), drf(typing({})));
  const html = render(s, { props: { startedAt: 0, clock: () => 12_000 } });
  assert.ok(!html.includes("ag-wait"));
  assert.ok(html.includes(">12с<"));
  assert.match(html, /<div class="ag-think"><span class="ag-elapsed small muted" aria-hidden="true">12с<\/span><div class="ag-actions">/);
  assert.ok(!render(s).includes("ag-elapsed"));
});

test("the timer shows on a request sent and not yet answered, and not on a refused one or an ended one", () => {
  const sent = { ...apply(initialPanelState, { type: "asked", id: 1, text: "q", scope: local({}).scope }) };
  const html = render(sent, { props: { startedAt: 0, clock: () => 3000 } });
  assert.ok(html.includes(">3с<"));
  assert.ok(html.includes("ag-wait"));
  const stopped = ended({ stopReason: "offline" });
  assert.ok(!render(stopped).includes("ag-elapsed"));
});

test("the panel gives a running request its timer and a finished one none", () => {
  const view = (state) =>
    renderToStaticMarkup(
      h(AgentPanelView, {
        state, ctx: contextFor("today", "", false), draft: "", notice: "", inputRef: { current: null },
        onDraft: noop, onSubmit: noop, onRemoveContext: noop, actionsFor: () => actions, clock: () => 50_000,
      }),
    );
  assert.ok(view(apply(initialPanelState, upd({}))).includes('aria-hidden="true">0с</span>'));
  assert.ok(!view(apply(initialPanelState, upd({ status: "DONE", cards: [replyCard] }))).includes("ag-elapsed"));
  assert.ok(!view(ended({ stopReason: "user" })).includes("ag-elapsed"));
});

test("Elapsed renders from the injected clock; the ticking code hides while the window is hidden and reads the timestamps", () => {
  assert.equal(renderToStaticMarkup(h(Elapsed, { since: 1000, clock: () => 126_000 })), '<span class="ag-elapsed small muted" aria-hidden="true">2м 5с</span>');
  const src = readFileSync(new URL("../src/agent/Elapsed.tsx", import.meta.url), "utf8");
  assert.match(src, /document\.hidden/);
  assert.match(src, /visibilitychange/);
  assert.match(src, /elapsedLabel\(props\.since, now\)/);
  assert.doesNotMatch(src, /aria-live|role=/);
});

// ---- Part 3: the queued request ----

test("a request waits behind a running request of the same chat only while it has no update", () => {
  const running = named(1, "EXECUTING");
  const sent = local({ seq: 2, scope: { ...local({}).scope, threadId: "t1" } });
  assert.equal(queuedBehind([running, sent], sent), running);
  assert.equal(queuedBehind([running, sent], running), undefined);
  // Another chat, a new chat, a failed call, a request that already has its update: not queued.
  assert.equal(queuedBehind([running, { ...sent, scope: { ...sent.scope, threadId: "t2" } }], { ...sent, scope: { ...sent.scope, threadId: "t2" } }), undefined);
  assert.equal(queuedBehind([running, local({ seq: 2 })], local({ seq: 2 })), undefined);
  assert.equal(queuedBehind([running, { ...sent, callProblem: "offline" }], { ...sent, callProblem: "offline" }), undefined);
  assert.equal(queuedBehind([running, { ...sent, update: update({ status: "PLANNING", turn: 2 }) }], { ...sent, update: update({ status: "PLANNING", turn: 2 }) }), undefined);
  // The running request ended: the label goes at once.
  assert.equal(queuedBehind([named(1, "DONE"), sent], sent), undefined);
  assert.equal(queuedBehind([named(1, "AWAITING_PLAN"), sent], sent), undefined);
});

test("a queued request shows «ждёт предыдущий запрос», the dots and a stop icon for the running one, and no timer", () => {
  const running = named(1, "EXECUTING");
  const sent = local({ seq: 2, scope: { ...local({}).scope, threadId: "t1" } });
  const html = renderToStaticMarkup(
    h(AgentRequest, { model: entryModel(sent, { last: true, blocked: false, queued: queuedBehind([running, sent], sent) !== undefined }), actions, startedAt: undefined }),
  );
  assert.ok(html.includes('<span class="ag-queued small muted">ждёт предыдущий запрос</span>'));
  assert.ok(html.includes("ag-wait"));
  assert.ok(html.includes('aria-label="Остановить предыдущий запрос"'));
  assert.ok(!html.includes("ag-elapsed"));
  assert.equal(texts.queued, "ждёт предыдущий запрос");
  // Not queued: the plain dots, no label, no stop.
  const plain = renderToStaticMarkup(h(AgentRequest, { model: entryModel(sent, { last: true, blocked: false }), actions }));
  assert.ok(!plain.includes("ждёт предыдущий запрос"));
  assert.ok(!plain.includes("Остановить предыдущий запрос"));
});

test("the queued label is not an announcement and its stop is disabled while a press is running", () => {
  const sent = local({ seq: 2 });
  const model = entryModel(sent, { last: true, blocked: false, queued: true });
  const html = renderToStaticMarkup(h(AgentRequest, { model, actions: { ...actions, busy: true } }));
  assert.match(html, /<button[^>]*aria-label="Остановить предыдущий запрос"[^>]*disabled/);
  assert.ok(!/role="status"[^>]*>[^<]*ждёт/.test(html));
});

test("«Остановить предыдущий запрос» of a queued request stops the running request of its chat", () => {
  const running = named(1, "EXECUTING");
  const sent = local({ seq: 2, scope: { ...local({}).scope, threadId: "t1" } });
  assert.equal(blockingEntry([running, sent], sent), running);
  // The panel routes the press of an unanswered request to that blocker (the stop of a busy-refused one works the same way).
  const panel = readFileSync(new URL("../src/agent/AgentPanel.tsx", import.meta.url), "utf8");
  assert.match(panel, /const named = e\.threadId !== "" && e\.update !== null;/);
  assert.match(panel, /if \(id === "stop" && blocking\)/);
});

test("the label goes with the running request: when it ends the queued one shows plain dots and starts its timer", () => {
  const sent = local({ seq: 2, scope: { ...local({}).scope, threadId: "t1" } });
  const before = [named(1, "EXECUTING"), sent];
  assert.equal(isRunningEntry(before, sent), false);
  const starts = syncRunStarts({ 1: 1000 }, before, 5000);
  assert.deepEqual(starts, { 1: 1000 });
  const after = [named(1, "DONE"), sent];
  assert.equal(queuedBehind(after, sent), undefined);
  assert.equal(isRunningEntry(after, sent), true);
  assert.deepEqual(syncRunStarts(starts, after, 9000), { 2: 9000 });
  const panelState = { ...initialPanelState, entries: after };
  assert.ok(!entryModel(sent, { last: true, blocked: false, queued: false }).queued);
  assert.ok(panelState.entries.length === 2);
});

test("the running request's own stop stays in the flow right above the queued one (no sticky row to lose)", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.doesNotMatch(css.match(/\.ag-think\s*\{[^}]*\}/)?.[0] ?? "", /sticky|fixed|absolute/);
});
