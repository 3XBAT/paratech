// TASK-399: «Повторная попытка N из 3» beside the timer, a cut reply is not «no connection» (D4), the offline card is
// «Повторить» (Retry) with the single fallback to Continue.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { cutFor, draftsFor, initialPanelState, panelMode, reducePanel, rereadsLink } from "../.test-build/agentState.js";
import { contextFor, entryModel, maxAutoRetries, retryAttemptText, retryWatchStep, stoppedOffline, texts } from "../.test-build/agentView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";
import { ariaLabels } from "./iconbtn.mjs";

const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const ready = { link: "ready", readOnly: false };

const update = (over) => ({
  threadId: "t1", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "EXECUTING",
  stopReason: "", problem: "", progress: false, retry: 0, cards: [], ticket: "", queued: false, ...over,
});
const apply = (s, ...as) => as.reduce(reducePanel, s);
const upd = (over) => ({ type: "update", update: update(over) });
const typing = (over) => ({ type: "draft", draft: { threadId: "t1", turn: 1, callId: "step:reply", texts: ["Длинный ответ, который уже виден"], reply: true, ...over } });
const online = () => reducePanel(initialPanelState, { type: "link", link: ready });

const render = (s, props = {}) => {
  const e = s.entries[0];
  return renderToStaticMarkup(
    h(AgentRequest, {
      model: entryModel(e, { last: true, blocked: false, replyCut: cutFor(s, e) !== undefined }),
      actions,
      drafts: draftsFor(s, e),
      cut: cutFor(s, e),
      ...props,
    }),
  );
};
const retryIcons = (html) => (html.match(/aria-label="Повторить"/g) ?? []).length;
const panel = (state) =>
  renderToStaticMarkup(
    h(AgentPanelView, {
      state, ctx: contextFor("today", "", false), draft: "", notice: "", inputRef: { current: null },
      onDraft: noop, onSubmit: noop, onRemoveContext: noop, actionsFor: () => actions,
    }),
  );

// ---- «Повторная попытка N из 3» ----

test("the retry line: muted text next to the timer, 1..3, no box and no colour", () => {
  assert.equal(maxAutoRetries, 3);
  for (const n of [1, 2, 3]) {
    const s = apply(initialPanelState, upd({ progress: true, retry: n }));
    const html = render(s, { startedAt: 0, clock: () => 12_000 });
    assert.ok(html.includes(`<span class="ag-retry small muted" role="status">Повторная попытка ${n} из 3</span>`), String(n));
    assert.ok(html.indexOf("ag-elapsed") < html.indexOf("ag-retry"), "after the timer");
    assert.ok(html.indexOf("ag-retry") < html.indexOf('aria-label="Остановить"'), "before the stop icon");
    assert.equal(retryAttemptText(n), `Повторная попытка ${n} из 3`);
  }
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  const rule = css.match(/\.ag-retry\s*\{[^}]*\}/)?.[0] ?? "";
  assert.ok(rule !== "");
  assert.doesNotMatch(rule, /background|border|box-shadow|color|accent|animation/);
});

test("the retry line is there without a timer too, is a text node and shows nothing for 0 or garbage", () => {
  const html = render(apply(initialPanelState, upd({ retry: 2 })));
  assert.ok(html.includes("Повторная попытка 2 из 3"));
  for (const retry of [0, 4, -1, 1.5, undefined]) {
    assert.ok(!render(apply(initialPanelState, upd({ retry }))).includes("Повторная попытка"), String(retry));
  }
  assert.equal(entryModel(apply(initialPanelState, upd({ retry: 3 })).entries[0]).retry, 3);
});

test("the retry line also stands in the header of a progress card", () => {
  const card = { kind: "progress", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "", stopReason: "", workId: "", outcome: "" };
  const html = render(apply(initialPanelState, upd({ retry: 1, cards: [card] })), { startedAt: 0, clock: () => 5_000 });
  assert.ok(html.includes("Повторная попытка 1 из 3"));
});

test("the progress update moves the attempt and keeps the cards; the answer or the stop takes the line away", () => {
  let s = apply(initialPanelState, upd({ retry: 0 }));
  assert.equal(s.entries[0].update.retry, 0);
  s = apply(s, upd({ progress: true, retry: 1 }));
  assert.equal(s.entries[0].update.retry, 1);
  s = apply(s, upd({ progress: true, retry: 2 }));
  assert.equal(s.entries[0].update.retry, 2);
  assert.ok(render(s).includes("Повторная попытка 2 из 3"));
  // The retried call goes out and the next step of progress says no retry runs.
  const clear = apply(s, upd({ progress: true, retry: 0 }));
  assert.equal(clear.entries[0].update.retry, 0);
  assert.ok(!render(clear).includes("Повторная попытка"));
  // The answer (a full update) replaces the whole update.
  const done = apply(s, upd({ status: "DONE" }));
  assert.equal(done.entries[0].update.retry, 0);
  assert.ok(!render(done).includes("Повторная попытка"));
  // The stop: even a stop update that still carried the attempt shows no line.
  const stopped = apply(s, upd({ status: "STOPPED", stopReason: "user", retry: 2 }));
  assert.ok(!render(stopped).includes("Повторная попытка"));
  assert.equal(entryModel(stopped.entries[0]).retry, 0);
});

test("a progress update keeps the stop reason and problem moves as before, with the retry copied", () => {
  const s = apply(initialPanelState, upd({ cards: [] }), upd({ progress: true, status: "PLANNING", retry: 3, problem: "busy" }));
  const u = s.entries[0].update;
  assert.equal(u.status, "PLANNING");
  assert.equal(u.problem, "");
  assert.equal(u.retry, 3);
});

// ---- D4: a cut reply is not «no connection» ----

test("D4: a Hub failure after shown text keeps the text, says «Ответ оборван. Можно повторить.» with ONE retry icon, and the field works", () => {
  const s = apply(online(), upd({}), typing(), upd({ status: "STOPPED", stopReason: "failed", problem: "failed" }));
  const html = render(s);
  assert.ok(html.includes("Длинный ответ, который уже виден"));
  assert.ok(html.includes("Ответ оборван. Можно повторить."));
  assert.equal(retryIcons(html), 1);
  assert.ok(!html.includes("Повторить ответ"));
  const mode = panelMode(s);
  assert.equal(mode.inputEnabled, true);
  assert.equal(mode.offline, false);
  assert.equal(mode.placeholder, "Спросить paratech");
  const whole = panel(s);
  assert.doesNotMatch(whole, /Нет связи с paratech/);
  assert.doesNotMatch(whole, /<textarea[^>]*disabled/);
  assert.doesNotMatch(whole, /нет связи — может быть неактуально/);
  assert.equal(texts.replyCut, "Ответ оборван. Можно повторить.");
});

test("D4: the same for a stop offline with the link ok: text stays, one retry icon, the field accepts a new question", () => {
  const s = apply(online(), upd({}), typing(), upd({ status: "STOPPED", stopReason: "offline" }));
  const html = render(s);
  assert.ok(html.includes("Длинный ответ"));
  assert.ok(html.includes("Ответ оборван. Можно повторить."));
  assert.equal(retryIcons(html), 1);
  assert.equal(panelMode(s).inputEnabled, true);
  assert.doesNotMatch(panel(s), /Нет связи с paratech/);
  // A new question can be asked: the field is enabled and the send button is on with a draft.
  const withDraft = renderToStaticMarkup(
    h(AgentPanelView, { state: s, ctx: contextFor("today", "", false), draft: "ещё вопрос", notice: "", inputRef: { current: null }, onDraft: noop, onSubmit: noop, onRemoveContext: noop, actionsFor: () => actions }),
  );
  assert.doesNotMatch(withDraft, /<button(?=[^>]*class="ag-send")[^>]*disabled/);
});

test("D4: the banner and the lock come from the link state only", () => {
  const s = apply(online(), upd({ status: "STOPPED", stopReason: "offline" }));
  assert.equal(panelMode(s).offline, false);
  const down = reducePanel(s, { type: "link", link: { link: "no_hub", readOnly: true } });
  assert.equal(panelMode(down).offline, true);
  assert.equal(panelMode(down).inputEnabled, false);
  assert.match(panel(down), /Нет связи с paratech/);
  // Back online without any request activity: the lock is gone.
  const back = reducePanel(down, { type: "link", link: ready });
  assert.equal(panelMode(back).inputEnabled, true);
  // A failed read of State is read-only: nothing is sent into the dark.
  const unknown = reducePanel(s, { type: "link", link: { link: "unknown", readOnly: true } });
  assert.equal(panelMode(unknown).inputEnabled, false);
});

test("D4: an update that ends a request offline makes the panel read the link state again", () => {
  assert.equal(rereadsLink(update({ status: "STOPPED", stopReason: "offline" })), true);
  const card = { kind: "stopped", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "", cannotDo: null, text: "", stopReason: "offline", workId: "", outcome: "" };
  assert.equal(rereadsLink(update({ status: "STOPPED", stopReason: "", cards: [card] })), true);
  for (const over of [{ status: "STOPPED", stopReason: "user" }, { status: "STOPPED", stopReason: "failed" }, { status: "EXECUTING" }, { status: "DONE" }, { status: "", problem: "offline" }]) {
    assert.equal(rereadsLink(update(over)), false, JSON.stringify(over));
  }
  const src = readFileSync(new URL("../src/agent/AgentPanel.tsx", import.meta.url), "utf8");
  assert.match(src, /if \(rereadsLink\(update\)\) \{\s*void loadState\(\);/);
  // The refusal of an Ask as offline re-reads it, as before.
  assert.match(src, /if \(problem === "offline"\) \{\s*void loadState\(\);/);
});

// ---- The offline card is «Повторить» ----

test("the offline card: the text and hint are kept, the icon is «Повторить» (Retry) and «Отменить запрос»", () => {
  const m = entryModel(apply(initialPanelState, upd({ status: "STOPPED", stopReason: "offline" })).entries[0], { last: true, blocked: false });
  const stop = m.cards.at(-1);
  assert.equal(stop.type, "stopped");
  assert.equal(stop.text, "Не завершено: нет связи. Можно повторить, когда связь вернётся.");
  assert.deepEqual(stop.buttons.map((b) => [b.id, b.label]), [["retry", "Повторить"], ["cancel", "Отменить запрос"]]);
  assert.equal(m.rerun, false, "one «Повторить» only");
  const html = render(apply(initialPanelState, upd({ status: "STOPPED", stopReason: "offline" })));
  assert.deepEqual(ariaLabels(html), ["Повторить", "Отменить запрос"]);
  assert.ok(html.includes('<span class="ia-tip" aria-hidden="true">Повторить</span>'));
  assert.ok(!html.includes(">Повторить<button"));
});

test("the quota keeps Continue; «Остановить» keeps «Продолжить»; failed keeps Retry", () => {
  const ids = (reason) => entryModel(apply(initialPanelState, upd({ status: "STOPPED", stopReason: reason })).entries[0], { last: true, blocked: false }).cards.at(-1).buttons.map((b) => b.id);
  assert.deepEqual(ids("quota"), ["continue", "cancel"]);
  assert.deepEqual(ids("user"), ["resume", "cancel"]);
  assert.deepEqual(ids("failed"), ["retry"]);
});

// ---- Retry refused: Continue once ----

test("retryWatchStep: only the refusal of the watched request with the problem «invalid» leads to Continue", () => {
  const w = { threadId: "t1", turn: 1 };
  // The refusal Retry gives for "no failed step to retry" (ErrInvalidTransition -> "invalid"): no status, only the problem.
  assert.equal(retryWatchStep(w, update({ status: "", problem: "invalid" })), "refused");
  // Cards on the refusal (tool cards of the request) do not change what it is.
  assert.equal(retryWatchStep(w, update({ status: "", problem: "invalid", cards: [{ kind: "reply" }] })), "refused");
  // Any other problem is shown, never turned into Continue (budget, busy, stale, failed, retry_required ...).
  for (const problem of ["budget", "busy", "stale", "failed", "retry_required", "restart_required", "rerun_blocked", ""]) {
    assert.equal(retryWatchStep(w, update({ status: "", problem })), "went_on", problem);
  }
});

test("retryWatchStep: the request moving on ends the watch; other requests and a repeat of the stop do not", () => {
  const w = { threadId: "t1", turn: 1 };
  for (const status of ["EXECUTING", "PLANNING", "DONE", "FAILED", "AWAITING_PLAN", "CANCELLED"]) {
    assert.equal(retryWatchStep(w, update({ status })), "went_on", status);
  }
  assert.equal(retryWatchStep(w, update({ status: "STOPPED", stopReason: "offline" })), "keep");
  assert.equal(retryWatchStep(w, update({ threadId: "t2", status: "", problem: "invalid" })), "keep");
  assert.equal(retryWatchStep(w, update({ turn: 2, status: "", problem: "invalid" })), "keep");
  assert.equal(retryWatchStep(w, update({ turn: 0, status: "", queued: true, ticket: "q1" })), "keep");
  assert.equal(retryWatchStep(w, { ...update({ status: "", problem: "invalid" }), compaction: true }), "keep");
});

test("stoppedOffline: only a stopped request whose reason is offline starts the watch", () => {
  assert.equal(stoppedOffline(update({ status: "STOPPED", stopReason: "offline" })), true);
  assert.equal(stoppedOffline(update({ status: "STOPPED", stopReason: "failed" })), false);
  assert.equal(stoppedOffline(update({ status: "STOPPED", stopReason: "user" })), false);
  assert.equal(stoppedOffline(update({ status: "FAILED", stopReason: "offline" })), false);
  assert.equal(stoppedOffline(update({ status: "EXECUTING" })), false);
  assert.equal(stoppedOffline(null), false);
});

test("AgentPanel watches Retry of an offline stop, hides the refusal and presses Continue once", () => {
  const src = readFileSync(new URL("../src/agent/AgentPanel.tsx", import.meta.url), "utf8");
  // The press sets the watch only for an offline stop and drops it when the call itself is refused.
  assert.match(src, /id === "retry" && stoppedOffline\(e\.update\)/);
  assert.match(src, /retryWatch\.current = \{ threadId: e\.threadId, turn: e\.turn, key: e\.key \}/);
  // The update is judged before it reaches the reducer; the refusal is not dispatched and calls Continue once.
  const judged = src.indexOf("retryWatchStep(w, update)");
  const dispatched = src.indexOf('dispatch({ type: "update", update })');
  assert.ok(judged > 0 && judged < dispatched);
  assert.match(src, /if \(watched === "refused"\) \{\s*continueAfterRetry\(w\);\s*return;/);
  assert.match(src, /retryWatch\.current = null;\s*if \(watched === "refused"\)/);
  assert.match(src, /api\.continueTurn\(\{ threadId: w\.threadId, turn: w\.turn \}\)/);
});
