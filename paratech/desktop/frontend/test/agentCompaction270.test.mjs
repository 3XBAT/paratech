// TASK-270: the quiet «Уплотнение контекста…» note per chat and the ring after a compaction.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { compactingFromList, compactingFromUpdate, compactingOf, fillOf, fillsFromUpdate, ringModel, ringTexts } from "../.test-build/agentView.js";
import { parseUpdate } from "../.test-build/bindings/agent.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";
import { CompactionNote } from "../.test-build/agent/ContextRing.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

const wire = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
  stopReason: "", problem: "", progress: false, cards: [], fill: null, ...over,
});
const fill = (percent, level = "normal", overflow = false) => ({ percent, level, overflow });
const start = (threadId = "a") => parseUpdate(wire({ threadId, compaction: true, compacting: true, turn: 0, status: "" }));
const end = (f, threadId = "a") => parseUpdate(wire({ threadId, compaction: true, compacting: false, turn: 0, status: "", fill: f }));
const chat = (threadId) => ({ threadId, title: "Чат", archived: false, lastActive: "2026-10-27T10:00:00Z", running: false, fill: null });

const panel = (over) =>
  renderToStaticMarkup(
    h(AgentPanelView, {
      state: initialPanelState,
      ctx: { label: "Сегодня", removable: false, ask: { subjectId: "", lessonId: "", assignmentId: "", screen: "today" } },
      draft: "",
      notice: "",
      inputRef: { current: null },
      onDraft: noop,
      onSubmit: noop,
      onRemoveContext: noop,
      actionsFor: () => ({ press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined }),
      ...over,
    }),
  );

test("the note appears on compacting:true for the open chat and goes on compacting:false", () => {
  const on = compactingFromUpdate({}, start());
  assert.equal(compactingOf(on, "a"), true);
  const off = compactingFromUpdate(on, end(fill(12)));
  assert.equal(compactingOf(off, "a"), false);
  assert.deepEqual(off, {});
  // A failed compaction (no fill) also ends the note.
  assert.deepEqual(compactingFromUpdate(on, end(null)), {});
});

test("an update of another chat does not show the note in the open chat", () => {
  const on = compactingFromUpdate({}, start("b"));
  assert.equal(compactingOf(on, "b"), true);
  assert.equal(compactingOf(on, "a"), false);
  // Two chats compact independently.
  const both = compactingFromUpdate(on, start("a"));
  assert.equal(compactingOf(compactingFromUpdate(both, end(null, "b")), "a"), true);
  assert.equal(compactingOf(compactingFromUpdate(both, end(null, "b")), "b"), false);
});

test("no chat open shows nothing", () => {
  assert.equal(compactingOf({ a: true }, ""), false);
});

test("an Ask without a chat (empty threadId) never shows a note", () => {
  const prev = {};
  assert.equal(compactingFromUpdate(prev, start("")), prev);
  assert.equal(compactingFromUpdate(prev, end(fill(5), "")), prev);
  const busy = { a: true };
  assert.equal(compactingFromUpdate(busy, start("")), busy);
});

test("a real update of the chat clears a stuck flag; another chat's real update does not", () => {
  const stuck = { a: true, b: true };
  const next = compactingFromUpdate(stuck, parseUpdate(wire({ threadId: "a", status: "PLANNING" })));
  assert.deepEqual(next, { b: true });
  const progress = compactingFromUpdate(stuck, parseUpdate(wire({ threadId: "a", progress: true, status: "EXECUTING" })));
  assert.deepEqual(progress, { b: true });
});

test("the list prunes chats that are gone", () => {
  const prev = { a: true, gone: true };
  assert.deepEqual(compactingFromList(prev, [chat("a"), chat("c")]), { a: true });
  assert.deepEqual(compactingFromList(prev, []), {});
});

test("the same reference when nothing changes", () => {
  const prev = { a: true };
  assert.equal(compactingFromUpdate(prev, start("a")), prev);
  assert.equal(compactingFromUpdate(prev, end(null, "b")), prev);
  assert.equal(compactingFromUpdate(prev, parseUpdate(wire({ threadId: "b" }))), prev);
  assert.equal(compactingFromList(prev, [chat("a"), chat("b")]), prev);
  const empty = {};
  assert.equal(compactingFromUpdate(empty, parseUpdate(wire({}))), empty);
  assert.equal(compactingFromList(empty, []), empty);
});

test("the always-rendered role=status container is empty when idle and carries the text while compacting", () => {
  const idle = renderToStaticMarkup(h(CompactionNote, { active: false }));
  assert.equal(idle, '<div class="ag-compact small" role="status" aria-live="polite"></div>');

  const busy = renderToStaticMarkup(h(CompactionNote, { active: true }));
  assert.match(busy, /^<div class="ag-compact small" role="status" aria-live="polite">/);
  assert.equal(busy, '<div class="ag-compact small" role="status" aria-live="polite">Уплотнение контекста…</div>');
  // Text only: no second animated indicator next to the thread's Waiting dots.
  assert.doesNotMatch(busy, /ag-cdot|<span|aria-hidden/);
  assert.equal(ringTexts.compacting, "Уплотнение контекста…");
});

test("in the panel the note sits under the form, before the context chip, and the field stays enabled", () => {
  const idle = panel({});
  assert.match(idle, /<div class="ag-compact small" role="status" aria-live="polite"><\/div>/);
  assert.doesNotMatch(idle, /Уплотнение/);

  const ready = reducePanel(initialPanelState, { type: "link", link: { link: "ready", readOnly: false } });
  const html = panel({ state: ready, compacting: true });
  const at = html.indexOf('class="ag-compact');
  assert.ok(at > html.indexOf("</form>") && at < html.indexOf('class="ag-ctx-wrap"'), "under the form");
  assert.ok(at < html.indexOf('class="ag-thread"'), "outside the scrolling thread");
  assert.match(html, /Уплотнение контекста…/);
  assert.match(html, /<textarea/);
  assert.doesNotMatch(html, /<textarea[^>]*disabled/);
});

test("while compacting the full-chat hint is hidden; it is back when the compaction ended and the fill is still full", () => {
  const bar = { openChat: null, sections: { active: [], archived: [] }, note: "", onOpen: noop, onNew: noop, onRename: async () => "", onArchive: noop, onRestore: noop };
  const busy = panel({ compacting: true, fill: fill(95, "full"), chat: bar });
  assert.match(busy, /Уплотнение контекста…/);
  assert.doesNotMatch(busy, /ag-fullhint|Начать новый чат/);
  const done = panel({ compacting: false, fill: fill(95, "full"), chat: bar });
  assert.doesNotMatch(done, /Уплотнение/);
  assert.match(done, /ag-fullhint/);
  assert.match(done, /Новый чат<\/button>/);
  // The ring stays while compacting.
  assert.match(busy, /ag-ring-full/);
});

test("ring after a compaction: the new, lower fill replaces a full one and the hint goes away", () => {
  const before = fillsFromUpdate({}, parseUpdate(wire({ fill: fill(95, "full") })));
  assert.equal(ringModel(fillOf(before, "a")).level, "full");
  assert.equal(ringModel(fillOf(before, "a")).hint, true);

  const during = fillsFromUpdate(before, start());
  assert.equal(during, before);

  const after = fillsFromUpdate(during, end(fill(12)));
  const m = ringModel(fillOf(after, "a"));
  assert.equal(m.percent, 12);
  assert.equal(m.level, "normal");
  assert.equal(m.hint, false);
  assert.equal(m.text, `${ringTexts.label(12)}. ${ringTexts.normal}`);

  const html = panel({ fill: fillOf(after, "a"), chat: { openChat: null, sections: { active: [], archived: [] }, note: "", onOpen: noop, onNew: noop, onRename: async () => "", onArchive: noop, onRestore: noop } });
  assert.doesNotMatch(html, /ag-fullhint/);
  assert.match(html, /ag-ring-normal/);
});

test("a failed compaction (no fill) leaves the ring as it was", () => {
  const before = { a: fill(95, "full") };
  assert.equal(fillsFromUpdate(before, end(null)), before);
  assert.equal(ringModel(before.a).level, "full");
});

test("overflow without a compaction has its own non-blaming sentence; the hint stays", () => {
  const m = ringModel(fill(100, "high", true));
  assert.equal(m.level, "full");
  assert.equal(m.hint, true);
  assert.equal(m.text, `${ringTexts.label(100)}. Начало разговора агент уже не помнит: новый чат ответит точнее.`);
  assert.doesNotMatch(m.text, /почти заполнена/);
  // The line under the field says the same for an overflow, and keeps its old sentence for a plain full chat.
  assert.equal(m.hintText, "Начать новый чат? Начало этого разговора агент уже не помнит; чат останется в списке.");
  assert.equal(m.hintText, ringTexts.overflowHint);
  const plain = ringModel(fill(93, "full"));
  assert.match(plain.text, /почти заполнена/);
  assert.equal(plain.hintText, ringTexts.hint);
  assert.equal(ringModel(fill(40, "normal")).hintText, "");
});

test("the hint line under the field shows the overflow sentence only for an overflow", () => {
  const bar = { openChat: null, sections: { active: [], archived: [] }, note: "", onOpen: noop, onNew: noop, onRename: async () => "", onArchive: noop, onRestore: noop };
  const over = panel({ fill: fill(100, "high", true), chat: bar });
  assert.match(over, /Начало этого разговора агент уже не помнит; чат останется в списке\./);
  assert.doesNotMatch(over, /почти заполнен и останется/);
  const full = panel({ fill: fill(93, "full"), chat: bar });
  assert.match(full, /Этот чат почти заполнен и останется в списке\./);
  assert.doesNotMatch(full, /Начало этого разговора/);
});

test("CSS: the note is in normal flow (no absolute, fixed or sticky), has no box and no animation, and shares the hint's slot", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
  const rules = css.split("\n").filter((l) => /^\s*(\.ag-in-bottom )?\.ag-compact\b/.test(l));
  assert.equal(rules.length, 4, rules.join("\n"));
  for (const r of rules) {
    assert.doesNotMatch(r, /position\s*:\s*(fixed|absolute|sticky)/, r);
    assert.doesNotMatch(r, /\b(border|outline|box-shadow|animation)\s*:|background(-color)?\s*:/, r);
  }
  assert.doesNotMatch(css, /ag-cdot/);
  assert.match(css, /^\.ag-compact \{[^}]*color: var\(--muted\)/m);
  // Same order and offsets as the hint in the bottom layout, so the two swap heights instead of adding up.
  assert.match(css, /^\.ag-in-bottom \.ag-compact \{ order: 4; \}/m);
  assert.match(css, /^\.ag-in-bottom \.ag-compact:not\(:empty\) \{ margin-top: -6px; padding-bottom: 12px; \}/m);
  assert.match(css, /^\.ag-in-bottom \.ag-fullhint \{ order: 4; margin-top: -6px; padding-bottom: 12px; \}/m);
});

test("the thread re-pins to the bottom when the note appears or goes (compacting is a dependency of the scroll effect)", () => {
  const src = readFileSync(new URL("../src/agent/AgentPanelView.tsx", import.meta.url), "utf8");
  assert.match(src, /\}, \[size, lastSig, props\.compacting\]\);/);
  assert.match(src, /\{!props\.compacting && <ContextHint /);
});
