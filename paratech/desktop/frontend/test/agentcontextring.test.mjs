// TASK-208: the context ring by the send button and the «Новый чат» hint of a full chat.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { fillOf, fillsFromList, fillsFromUpdate, ringModel, ringTexts } from "../.test-build/agentView.js";
import { parseFill } from "../.test-build/bindings/agent.js";
import { ContextHint, ContextRing } from "../.test-build/agent/ContextRing.js";
import { Button } from "../.test-build/ui/Button.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

const fill = (percent, level, overflow = false) => ({ percent, level, overflow });
const chat = (over) => ({ threadId: "a", title: "Чат", archived: false, lastActive: "2026-10-27T10:00:00Z", running: false, fill: null, ...over });
const update = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
  stopReason: "", problem: "", progress: false, cards: [], fill: null, ...over,
});

const ring = (f) => renderToStaticMarkup(h(ContextRing, { fill: f }));
const hint = (f, onNew = noop) => renderToStaticMarkup(h(ContextHint, { fill: f, onNew }));

test("levels: normal is muted, high and full are the accent; the class comes from the core's level", () => {
  assert.match(ring(fill(10, "normal")), /class="ag-ring ag-ring-normal"/);
  assert.match(ring(fill(75, "high")), /class="ag-ring ag-ring-high"/);
  assert.match(ring(fill(95, "full")), /class="ag-ring ag-ring-full"/);
  // The level decides, not the percent: a high level at 5 % stays high.
  assert.match(ring(fill(5, "high")), /ag-ring-high/);
  assert.equal(ringModel(fill(95, "normal")).level, "normal");
});

test("overflow is shown exactly as full, with the hint", () => {
  const m = ringModel(fill(100, "high", true));
  assert.equal(m.level, "full");
  assert.equal(m.hint, true);
  assert.match(ring(fill(100, "normal", true)), /ag-ring-full/);
  assert.match(hint(fill(100, "normal", true)), /Начать новый чат/);
});

test("an unknown fill draws nothing: no ring, no hint", () => {
  assert.equal(ringModel(null), null);
  assert.equal(ring(null), "");
  assert.equal(hint(null), "");
  assert.equal(renderToStaticMarkup(h(ContextRing, { fill: null })), "");
});

test("0 % is a quiet empty ring with its text", () => {
  const html = ring(fill(0, "normal"));
  assert.match(html, /stroke-dasharray="0 100"/);
  assert.match(html, /aria-label="Контекст чата: 0 %\./);
});

test("the arc is filled to the percent and never past 100", () => {
  assert.match(ring(fill(42, "normal")), /stroke-dasharray="42 100"/);
  assert.match(ring(fill(100, "full")), /stroke-dasharray="100 100"/);
  assert.equal(ringModel(fill(250, "full")).percent, 100);
  assert.equal(ringModel(fill(-3, "normal")).percent, 0);
  assert.equal(ringModel(fill(41.9, "normal")).percent, 41);
  assert.equal(ringModel(fill(Number.NaN, "normal")).percent, 0);
  assert.match(ring(fill(250, "full")), /Контекст чата: 100 %/);
});

test("tooltip and accessible name start with «Контекст чата: N %»; the sentence follows at every level", () => {
  for (const [f, sentence] of [
    [fill(12, "normal"), ringTexts.normal],
    [fill(72, "high"), ringTexts.high],
    [fill(93, "full"), ringTexts.full],
  ]) {
    const m = ringModel(f);
    assert.equal(m.text, `Контекст чата: ${f.percent} %. ${sentence}`);
    const html = ring(f);
    assert.ok(html.includes(`role="img"`));
    assert.ok(html.includes(`aria-label="${m.text}"`));
    assert.ok(html.includes(`title="${m.text}"`));
  }
  assert.equal(ringTexts.label(7), "Контекст чата: 7 %");
});

test("the hidden text sits inside the role=img element, once (children of an img are presentational)", () => {
  const html = ring(fill(80, "high"));
  const text = ringModel(fill(80, "high")).text;
  assert.ok(html.includes(`<span class="vh">${text}</span></span>`));
  assert.equal(html.split(`<span class="vh">`).length - 1, 1);
  // The drawing itself is hidden from readers.
  assert.match(html, /<svg[^>]*aria-hidden="true"/);
});

test("the sentences are short, impersonal and name no colour", () => {
  for (const s of [ringTexts.normal, ringTexts.high, ringTexts.full, ringTexts.hint, ringTexts.hintButton]) {
    assert.ok(s.length <= 70, s);
    assert.doesNotMatch(s, /(^|[^а-яё])(я|мы|вам|вы)([^а-яё]|$)/i);
    assert.doesNotMatch(s, /красн|оранж|жёлт|цвет/i);
  }
  assert.equal(ringTexts.hint, "Начать новый чат");
  assert.equal(ringTexts.hintButton, "Новый чат");
});

test("the hint appears only at full; normal and high have none", () => {
  assert.equal(hint(fill(20, "normal")), "");
  assert.equal(hint(fill(80, "high")), "");
  const html = hint(fill(95, "full"));
  assert.match(html, /class="ag-fullhint small"/);
  assert.match(html, /Начать новый чат/);
  assert.match(html, /<button[^>]*type="button"[^>]*>Новый чат<\/button>/);
  // No chat controls (no onNew): no dead button.
  assert.equal(renderToStaticMarkup(h(ContextHint, { fill: fill(95, "full") })), "");
});

test("the «Новый чат» button of the hint calls onNew", () => {
  let calls = 0;
  const el = ContextHint({ fill: fill(95, "full"), onNew: () => calls++ });
  const find = (node) => {
    if (node === null || typeof node !== "object") {
      return null;
    }
    if (node.type === Button) {
      return node;
    }
    const kids = node.props?.children;
    for (const k of Array.isArray(kids) ? kids : [kids]) {
      const r = find(k);
      if (r) {
        return r;
      }
    }
    return null;
  };
  const button = find(el);
  assert.ok(button);
  assert.equal(button.props.children, "Новый чат");
  button.props.onClick();
  assert.equal(calls, 1);
});

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
const bar = { openChat: null, sections: { active: [], archived: [] }, note: "", onOpen: noop, onNew: noop, onRename: async () => "", onArchive: noop, onRestore: noop };

test("in the panel the ring is right before the send button and the hint row follows the form", () => {
  const html = panel({ fill: fill(95, "full"), chat: bar });
  const ringAt = html.indexOf('class="ag-ring ');
  const sendAt = html.indexOf('class="ag-send"');
  const formEnd = html.indexOf("</form>");
  const hintAt = html.indexOf('class="ag-fullhint');
  assert.ok(ringAt > html.indexOf("<textarea") && ringAt < sendAt && sendAt < formEnd, "ring between the field and the send button");
  assert.ok(hintAt > formEnd && hintAt < html.indexOf('class="ag-ctx-wrap"'), "hint under the form");
});

test("in the panel: no fill, no ring and no hint; high has a ring and no hint", () => {
  for (const f of [undefined, null]) {
    const html = panel({ fill: f, chat: bar });
    assert.doesNotMatch(html, /ag-ring|ag-fullhint/);
  }
  const high = panel({ fill: fill(80, "high"), chat: bar });
  assert.match(high, /ag-ring-high/);
  assert.doesNotMatch(high, /ag-fullhint/);
});

// ---- Which fill the ring shows ----

test("the list sets the fill per chat; switching chats shows that chat's own", () => {
  const fills = fillsFromList({}, [chat({ threadId: "a", fill: fill(10, "normal") }), chat({ threadId: "b", fill: fill(95, "full") })], []);
  assert.equal(fillOf(fills, "a").percent, 10);
  assert.equal(fillOf(fills, "b").level, "full");
  assert.equal(fillOf(fills, "none"), null);
  assert.equal(fillOf(fills, ""), null);
});

test("an unreadable fill in the list shows nothing; a chat gone from the list is dropped", () => {
  const first = fillsFromList({}, [chat({ threadId: "a", fill: fill(50, "normal") }), chat({ threadId: "b", fill: fill(5, "normal") })], []);
  const next = fillsFromList(first, [chat({ threadId: "a", fill: null })], []);
  assert.equal(fillOf(next, "a"), null);
  assert.equal(fillOf(next, "b"), null);
  // A chat of an older core has no fill field at all.
  const old = fillsFromList({}, [{ threadId: "a", title: "x", archived: false, lastActive: "", running: false }], []);
  assert.equal(fillOf(old, "a"), null);
});

test("an update with a fill sets its own chat's; an update with fill null leaves the ring as it is", () => {
  const prev = { a: fill(40, "normal"), b: fill(10, "normal") };
  const set = fillsFromUpdate(prev, update({ threadId: "a", fill: fill(75, "high") }));
  assert.equal(fillOf(set, "a").level, "high");
  assert.equal(fillOf(set, "b").percent, 10);
  assert.equal(fillsFromUpdate(prev, update({ threadId: "a", fill: null })), prev);
  assert.equal(fillsFromUpdate(prev, update({ threadId: "", fill: fill(99, "full") })), prev);
  // A new chat gets its first fill from its update.
  assert.equal(fillOf(fillsFromUpdate({}, update({ threadId: "n", fill: fill(3, "normal") })), "n").percent, 3);
});

test("after a compaction the new, lower fill replaces the old one", () => {
  const full = fillsFromUpdate({}, update({ fill: fill(100, "full") }));
  const lower = fillsFromUpdate(full, update({ turn: 2, fill: fill(35, "normal") }));
  assert.equal(fillOf(lower, "a").percent, 35);
  assert.equal(ringModel(fillOf(lower, "a")).hint, false);
});

test("during a step the last fill stays: a chat with a request in flight ignores the list", () => {
  const prev = { a: fill(40, "normal") };
  const running = (over) => reducePanel(initialPanelState, { type: "update", update: update({ status: "EXECUTING", ...over }) }).entries;
  // The core says the chat runs.
  const byCore = fillsFromList(prev, [chat({ running: true, fill: fill(41, "normal") })], []);
  assert.equal(fillOf(byCore, "a").percent, 40);
  // The panel sees the request run before the list does.
  const byPanel = fillsFromList(prev, [chat({ running: false, fill: fill(41, "normal") })], running());
  assert.equal(fillOf(byPanel, "a").percent, 40);
  // A running chat with no known fill takes the list's rather than showing nothing.
  assert.equal(fillOf(fillsFromList({}, [chat({ running: true, fill: fill(41, "normal") })], []), "a").percent, 41);
  // Other chats still follow the list.
  const other = fillsFromList(prev, [chat({ running: true, fill: fill(41, "normal") }), chat({ threadId: "b", fill: fill(8, "normal") })], []);
  assert.equal(fillOf(other, "b").percent, 8);
  // Once the request has ended the list is trusted again.
  const done = reducePanel(initialPanelState, { type: "update", update: update({ status: "DONE" }) }).entries;
  assert.equal(fillOf(fillsFromList(prev, [chat({ fill: fill(55, "normal") })], done), "a").percent, 55);
});

test("unchanged fills keep the same object, so the panel does not redraw", () => {
  const prev = fillsFromList({}, [chat({ fill: fill(10, "normal") })], []);
  assert.equal(fillsFromList(prev, [chat({ fill: fill(10, "normal") })], []), prev);
  assert.equal(fillsFromUpdate(prev, update({ fill: fill(10, "normal") })), prev);
});

test("parseFill output feeds the ring; malformed reads as no ring", () => {
  assert.equal(ringModel(parseFill({ percent: 91, level: "full", overflow: false })).level, "full");
  assert.equal(ringModel(parseFill({ percent: 91, level: "red", overflow: false })), null);
});

// ---- Source checks ----

test("no 70/90 threshold in the TS that draws the ring: the level is the core's", () => {
  const view = readFileSync(new URL("../src/agentView.ts", import.meta.url), "utf8");
  const start = view.indexOf("// ---- The context ring");
  assert.ok(start > 0);
  const section = view.slice(start);
  const component = readFileSync(new URL("../src/agent/ContextRing.tsx", import.meta.url), "utf8");
  for (const src of [section, component]) {
    // Comments may mention the thresholds; code may not use them.
    const code = src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
    assert.doesNotMatch(code, /\b(69|70|89|90)\b/);
    assert.doesNotMatch(code, /percent\s*[<>]=?\s*\d/);
  }
});

test("the CSS has no transition of the arc under prefers-reduced-motion and no red", () => {
  const css = readFileSync(new URL("../src/agent/agent.css", import.meta.url), "utf8");
  assert.match(css, /@media \(prefers-reduced-motion: reduce\) \{ \.ag-ring-arc \{ transition: none; \} \}/);
  assert.match(css, /\.ag-ring-arc \{[^}]*transition: stroke-dasharray/);
  const block = css.slice(css.indexOf("/* The context ring"));
  assert.doesNotMatch(block, /\bred\b|#f00|--rec|--err|--danger/i);
  assert.match(block, /\.ag-ring-high, \.ag-ring-full \{ color: var\(--accent\); \}/);
});
