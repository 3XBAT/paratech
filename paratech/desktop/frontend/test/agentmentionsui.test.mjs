// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-247: «@» mentions in the agent input and the mention pills of the thread (pure helpers, SSR render).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  activeFragment,
  addMention,
  askMentions,
  backspaceRemovesPill,
  clampActive,
  createMentionSearch,
  idleSearch,
  maxMentions,
  mentionDebounceMs,
  mentionKindCaption,
  mentionOpening,
  mentionPills,
  mentionTexts,
  moveActive,
  pickerKey,
  popupModel,
  removeFragment,
} from "../.test-build/agentMentions.js";
import { askInput, contextFor, entryModel, restoredMentions } from "../.test-build/agentView.js";
import { initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";
import { AgentRequest } from "../.test-build/agent/AgentCards.js";
import { MessagePills } from "../.test-build/agent/MentionParts.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const flush = () => new Promise((r) => setImmediate(r));

const frag = (text, caret = text.length) => activeFragment(text, caret);

// ---- the fragment under the caret ----

test("fragment: «@» at the start and after a space or a newline opens; the query is the text up to the caret", () => {
  assert.deepEqual(frag("@"), { start: 0, end: 1, query: "" });
  assert.deepEqual(frag("@физ"), { start: 0, end: 4, query: "физ" });
  assert.deepEqual(frag("привет @физ"), { start: 7, end: 11, query: "физ" });
  assert.deepEqual(frag("привет\n@фи"), { start: 7, end: 10, query: "фи" });
  assert.deepEqual(frag("a\t@x"), { start: 2, end: 4, query: "x" });
});

test("fragment: «@» inside a word, a space after the query, or no «@» is no fragment", () => {
  assert.equal(frag("a@b"), null);
  assert.equal(frag("почта me@mail.ru"), null);
  assert.equal(frag("@физ "), null);
  assert.equal(frag("@физ ика"), null);
  assert.equal(frag("без собаки"), null);
  assert.equal(frag(""), null);
});

test("fragment: the caret decides (middle of the text), the text after it is not part of the query", () => {
  assert.deepEqual(frag("что в @физика сегодня", 9), { start: 6, end: 9, query: "фи" });
  assert.equal(frag("что в @физика сегодня", 5), null, "caret before the «@»");
  assert.equal(frag("что в @физика сегодня", 15), null, "caret after the space that ends the word");
  assert.deepEqual(frag("@a", 99), { start: 0, end: 2, query: "a" }, "a caret past the end is clamped");
  assert.equal(frag("@a", -3), null);
});

test("fragment: with several «@» the one before the caret counts", () => {
  assert.deepEqual(frag("@физ и @хим"), { start: 7, end: 11, query: "хим" });
  assert.deepEqual(frag("@физ и @хим", 4), { start: 0, end: 4, query: "физ" });
  assert.equal(frag("@a@b"), null, "the second «@» sits in a word");
});

test("picking removes the fragment and keeps the rest; no double space is left", () => {
  const cut = (text, caret) => removeFragment(text, frag(text, caret));
  assert.deepEqual(cut("@физ", 4), { text: "", caret: 0 });
  assert.deepEqual(cut("привет @физ", 11), { text: "привет ", caret: 7 });
  assert.deepEqual(cut("что в @физ сегодня", 10), { text: "что в сегодня", caret: 6 });
  assert.deepEqual(cut("@физ сегодня", 4), { text: "сегодня", caret: 0 });
  assert.deepEqual(cut("a\n@x\nb", 4), { text: "a\n\nb", caret: 2 }, "a line break is never eaten");
  // The text after the caret stays as typed.
  assert.deepEqual(cut("@физика", 3), { text: "зика", caret: 0 });
});

// ---- the pills ----

test("pills: at most 8, the same object is not added twice, only kind, id and label are kept", () => {
  let pills = [];
  for (let i = 0; i < maxMentions; i++) {
    const r = addMention(pills, { kind: "subject", id: `s${i}`, label: `П${i}`, hint: "x", extra: 1 });
    assert.equal(r.outcome, "added");
    pills = r.pills;
  }
  assert.equal(pills.length, 8);
  assert.deepEqual(pills[0], { kind: "subject", id: "s0", label: "П0" });
  assert.equal(addMention(pills, { kind: "lesson", id: "l", label: "Л" }).outcome, "full");
  assert.equal(addMention(pills, { kind: "subject", id: "s3", label: "П3" }).outcome, "duplicate", "a repeat is a repeat even when full");
  assert.equal(addMention(pills, { kind: "subject", id: "s3", label: "П3" }).pills.length, 8);
  // The same id of another kind is another object.
  assert.equal(addMention([{ kind: "subject", id: "x", label: "a" }], { kind: "material", id: "x", label: "b" }).outcome, "added");
});

test("Backspace removes the last pill only with the caret at the very start", () => {
  assert.equal(backspaceRemovesPill({ start: 0, end: 0 }, 2), true);
  assert.equal(backspaceRemovesPill({ start: 0, end: 0 }, 0), false);
  assert.equal(backspaceRemovesPill({ start: 3, end: 3 }, 2), false);
  assert.equal(backspaceRemovesPill({ start: 0, end: 4 }, 2), false, "a selection is deleted as text");
});

test("a request carries kind, id and label only; no key at all without mentions", () => {
  const ctx = contextFor("today", "", false);
  const withOnes = askInput(ctx, "что задали", [{ kind: "subject", id: "s1", label: "Физика", hint: "x", junk: 1 }]);
  assert.deepEqual(withOnes.mentions, [{ kind: "subject", id: "s1", label: "Физика" }]);
  assert.deepEqual(Object.keys(withOnes.mentions[0]).sort(), ["id", "kind", "label"]);
  assert.equal("mentions" in askInput(ctx, "x"), false);
  assert.equal("mentions" in askInput(ctx, "x", []), false);
  assert.equal(askMentions([]), undefined);
});

// ---- keys of the open list ----

const key = (k, over = {}) => ({ key: k, ...over });

test("keys: arrows move, Enter and Tab pick, Esc closes", () => {
  assert.equal(pickerKey(key("ArrowDown"), 3), "next");
  assert.equal(pickerKey(key("ArrowUp"), 3), "prev");
  assert.equal(pickerKey(key("Enter"), 3), "pick");
  assert.equal(pickerKey(key("Tab"), 3), "pick");
  assert.equal(pickerKey(key("Escape"), 3), "close");
  assert.equal(pickerKey(key("Escape"), 0), "close");
  assert.equal(pickerKey(key("a"), 3), "none");
});

test("keys: Enter never sends while the list is open; Tab and arrows leave an empty list alone", () => {
  assert.equal(pickerKey(key("Enter"), 0), "swallow");
  assert.equal(pickerKey(key("Tab"), 0), "none");
  assert.equal(pickerKey(key("ArrowDown"), 0), "none");
  assert.equal(pickerKey(key("Enter", { shiftKey: true }), 3), "none", "Shift+Enter breaks the line");
  assert.equal(pickerKey(key("Tab", { shiftKey: true }), 3), "none");
});

test("keys: nothing is intercepted while an IME composes or with Ctrl/Alt/Meta", () => {
  for (const k of ["Enter", "Tab", "ArrowDown", "ArrowUp", "Escape"]) {
    assert.equal(pickerKey(key(k, { isComposing: true }), 3), "none", k);
    assert.equal(pickerKey(key(k, { keyCode: 229 }), 3), "none", k);
    assert.equal(pickerKey(key(k, { ctrlKey: true }), 3), "none", k);
  }
});

test("the active option wraps at both ends and stays in range", () => {
  assert.equal(moveActive(0, 3, "next"), 1);
  assert.equal(moveActive(2, 3, "next"), 0);
  assert.equal(moveActive(0, 3, "prev"), 2);
  assert.equal(moveActive(1, 3, "prev"), 0);
  assert.equal(moveActive(0, 0, "next"), 0);
  assert.equal(clampActive(5, 3), 2);
  assert.equal(clampActive(-1, 3), 0);
  assert.equal(clampActive(4, 0), 0);
});

// ---- the list ----

const cand = (kind, id, label = `Имя ${id}`, hint = "") => ({ kind, id, label, hint });

test("list: options carry the kind caption (Пара for a lesson) and mark what is already picked", () => {
  const search = { status: "ready", query: "", candidates: [cand("subject", "s"), cand("lesson", "l", "Лекция", "Физика · 27.10.2026"), cand("material", "m"), cand("assignment", "a")] };
  const m = popupModel(search, [{ kind: "subject", id: "s", label: "Имя s" }]);
  assert.equal(m.kind, "options");
  assert.deepEqual(m.options.map((o) => o.caption), ["Предмет", "Пара", "Материал", "Задание"]);
  assert.deepEqual(m.options.map((o) => o.picked), [true, false, false, false]);
  assert.equal(m.options[1].hint, "Физика · 27.10.2026");
  assert.equal(m.text, "");
});

test("list: every state says what is going on; a failure offers «Повторить»", () => {
  const some = [cand("subject", "s")];
  assert.deepEqual([popupModel(idleSearch, []).kind, popupModel(idleSearch, []).text], ["loading", "Ищу…"]);
  assert.equal(popupModel({ status: "loading", query: "ф", candidates: [] }, []).text, mentionTexts.loading);
  assert.equal(popupModel({ status: "ready", query: "ф", candidates: [] }, []).text, "Ничего не найдено");
  const off = popupModel({ status: "unavailable", query: "", candidates: [] }, []);
  assert.deepEqual([off.kind, off.text, off.retry], ["unavailable", "Поиск недоступен: помощник не подключён", false]);
  const bad = popupModel({ status: "failed", query: "", candidates: [] }, []);
  assert.deepEqual([bad.kind, bad.text, bad.retry], ["failed", "Не удалось выполнить поиск", true]);
  // The previous answer stays while the next one is fetched.
  assert.equal(popupModel({ status: "loading", query: "фи", candidates: some }, []).kind, "options");
});

test("list: at 8 pills the list is one non-selectable line instead of options", () => {
  const eight = Array.from({ length: 8 }, (_, i) => ({ kind: "subject", id: `s${i}`, label: `П${i}` }));
  const m = popupModel({ status: "ready", query: "", candidates: [cand("subject", "new")] }, eight);
  assert.deepEqual([m.kind, m.text, m.options.length, m.retry], ["full", "Не больше 8 упоминаний в одной просьбе", 0, false]);
});

test("list: a candidate that repeats in the answer is shown once", () => {
  const m = popupModel({ status: "ready", query: "", candidates: [cand("subject", "s"), cand("subject", "s")] }, []);
  assert.equal(m.options.length, 1);
});

// ---- the debounced search ----

function harness() {
  const timers = new Map();
  let next = 0;
  const calls = [];
  const pending = [];
  const states = [];
  const search = createMentionSearch({
    call: (q) => {
      calls.push(q);
      return new Promise((resolve, reject) => pending.push({ q, resolve, reject }));
    },
    setTimer: (fn, ms) => {
      timers.set(++next, { fn, ms });
      return next;
    },
    clearTimer: (hd) => timers.delete(hd),
    onState: (s) => states.push(s),
  });
  return {
    search,
    calls,
    states,
    timers,
    last: () => states.at(-1),
    fire: () => {
      const [k, t] = [...timers][0];
      timers.delete(k);
      t.fn();
    },
    answer: async (i, candidates = [], problem = "") => {
      pending[i].resolve({ problem, candidates });
      await flush();
    },
    fail: async (i) => {
      pending[i].reject(new Error("x"));
      await flush();
    },
  };
}

test("search: the first request after opening goes at once, the next ones wait for the pause in typing", () => {
  const t = harness();
  t.search.request("");
  assert.deepEqual(t.calls, [""], "no delay on the first open");
  assert.equal(t.timers.size, 0);
  t.search.request("ф");
  t.search.request("фи");
  t.search.request("физ");
  assert.deepEqual(t.calls, [""], "typing makes no call");
  assert.equal(t.timers.size, 1, "one timer: each key restarts it");
  assert.equal([...t.timers.values()][0].ms, mentionDebounceMs);
  assert.equal(mentionDebounceMs, 250);
});

test("search: after the pause one call goes out with the newest query", async () => {
  const t = harness();
  t.search.request("");
  await t.answer(0, [cand("subject", "a")]);
  t.search.request("ф");
  t.search.request("физ");
  t.fire();
  assert.deepEqual(t.calls, ["", "физ"]);
  await t.answer(1, [cand("subject", "p")]);
  assert.deepEqual([t.last().status, t.last().query, t.last().candidates.length], ["ready", "физ", 1]);
});

test("search: never two calls at once; the query that fires meanwhile runs right after, with the newest text", async () => {
  const t = harness();
  t.search.request("");
  t.search.request("a");
  t.fire();
  t.search.request("ab");
  t.fire();
  assert.deepEqual(t.calls, [""], "the first call is still in flight");
  await t.answer(0, [cand("subject", "old")]);
  assert.deepEqual(t.calls, ["", "ab"], "the newest query, not «a»");
  assert.ok(!t.states.some((s) => s.status === "ready" && s.query === ""), "the stale answer is dropped");
  await t.answer(1, [cand("subject", "new")]);
  assert.deepEqual([t.last().status, t.last().query, t.last().candidates[0].id], ["ready", "ab", "new"]);
});

test("search: an answer for an older query is dropped and the list keeps saying it searches", async () => {
  const t = harness();
  t.search.request("a");
  t.search.request("ab");
  await t.answer(0, [cand("subject", "x")]);
  assert.deepEqual([t.last().status, t.last().query], ["loading", "ab"]);
  t.fire();
  assert.deepEqual(t.calls, ["a", "ab"]);
  await t.answer(1, [cand("subject", "y")]);
  assert.equal(t.last().candidates[0].id, "y");
});

test("search: the same query again is a no-op; going back to an answered query shows it without a call", async () => {
  const t = harness();
  t.search.request("a");
  t.search.request("a");
  assert.deepEqual(t.calls, ["a"]);
  await t.answer(0, [cand("subject", "x")]);
  const n = t.states.length;
  t.search.request("a");
  assert.equal(t.states.length, n, "nothing re-emitted");
  t.search.request("ab");
  assert.equal(t.timers.size, 1);
  t.search.request("a");
  assert.equal(t.timers.size, 0, "the pending call is dropped");
  assert.deepEqual([t.last().status, t.last().query, t.last().candidates[0].id], ["ready", "a", "x"]);
  assert.deepEqual(t.calls, ["a"]);
});

test("search: closing stops everything; an answer that comes later is not delivered", async () => {
  const t = harness();
  t.search.request("");
  t.search.request("a");
  t.search.cancel();
  assert.equal(t.timers.size, 0);
  assert.equal(t.last(), idleSearch);
  const n = t.states.length;
  await t.answer(0, [cand("subject", "x")]);
  assert.equal(t.states.length, n);
});

test("search: opening again while the old call still runs waits for it and then asks once", async () => {
  const t = harness();
  t.search.request("");
  t.search.cancel();
  t.search.request("");
  assert.deepEqual(t.calls, [""], "one call in flight, not two");
  await t.answer(0, [cand("subject", "x")]);
  assert.deepEqual(t.calls, [""]);
  assert.deepEqual([t.last().status, t.last().candidates.length], ["ready", 1], "the answer of the same query serves");
  t.search.cancel();
  t.search.request("z");
  assert.deepEqual(t.calls, ["", "z"]);
});

test("search: problems become unavailable or failed, «Повторить» asks again for the same query", async () => {
  const t = harness();
  t.search.request("ф");
  await t.answer(0, [], "unavailable");
  assert.equal(t.last().status, "unavailable");
  t.search.cancel();
  t.search.request("ф");
  await t.answer(1, [], "failed");
  assert.equal(t.last().status, "failed");
  t.search.retry();
  assert.equal(t.last().status, "loading");
  assert.deepEqual(t.calls, ["ф", "ф", "ф"]);
  await t.answer(2, [cand("subject", "x")]);
  assert.deepEqual([t.last().status, t.last().candidates.length], ["ready", 1]);
});

test("search: a rejected or throwing call reads as failed", async () => {
  const t = harness();
  t.search.request("a");
  await t.fail(0);
  assert.equal(t.last().status, "failed");
  const states = [];
  const s = createMentionSearch({
    call: () => {
      throw new Error("sync");
    },
    setTimer: () => 1,
    clearTimer: noop,
    onState: (x) => states.push(x),
  });
  s.request("");
  await flush();
  assert.equal(states.at(-1).status, "failed");
});

// ---- pills of the thread ----

test("pills: the stored name is shown as it is; an empty name reads as the kind", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const p = mentionPills([
    { kind: "subject", id: "s1", label: evil },
    { kind: "lesson", id: "l1", label: "" },
    { kind: "material", id: "m1", label: "   " },
    { kind: "assignment", id: "a1", label: "Задачи 1-7" },
    { kind: "weird", id: "w", label: "" },
    { kind: "constructor", id: "c", label: "" },
  ]);
  assert.equal(p[0].label, evil, "markup stays data");
  assert.deepEqual(p.map((x) => x.label), [evil, "Пара", "Материал", "Задачи 1-7", "Объект", "Объект"]);
  assert.equal(p[0].aria, `Открыть: Предмет ${evil}`);
  assert.equal(p[3].aria, "Открыть: Задание Задачи 1-7");
  assert.equal(new Set(p.map((x) => x.key)).size, p.length);
  assert.deepEqual(mentionPills(undefined), []);
  assert.equal(mentionKindCaption("lesson"), "Пара");
});

test("pills open the object through the routes the window has", () => {
  assert.deepEqual(mentionOpening("subject", "s1"), { type: "subject", id: "s1" });
  assert.deepEqual(mentionOpening("assignment", "a1"), { type: "assignment", id: "a1" });
  assert.deepEqual(mentionOpening("lesson", "l1"), { type: "page", page: "knowledge" });
  assert.deepEqual(mentionOpening("material", "m1"), { type: "page", page: "subjects" });
  assert.equal(mentionOpening("weird", "x"), null);
  assert.equal(mentionOpening("subject", ""), null);
});

const scope = (over) => ({ text: "x", subjectId: "", lessonId: "", assignmentId: "", screen: "today", ...over });
const live = (over) => ({ key: "local:1", seq: 1, threadId: "", turn: 0, text: "что задали", scope: scope({}), callProblem: "", update: null, ...over });

test("a live request shows the mentions of its scope; a restored one takes them from the history", () => {
  const m = [{ kind: "subject", id: "s1", label: "Физика" }];
  assert.deepEqual(entryModel(live({ scope: scope({ mentions: m }) })).mentions.map((x) => x.label), ["Физика"]);
  assert.deepEqual(entryModel(live()).mentions, []);
  // After a restart the snapshot entry has no scope: the history of the same turn supplies the mentions.
  const snap = live({ key: "thread:t:3", threadId: "t", turn: 3, scope: null });
  const hist = [{ turn: 2, text: "", mentions: [], reply: "", current: false }, { turn: 3, text: "q", mentions: m, reply: "", current: true }];
  assert.deepEqual(restoredMentions(snap, hist, "t"), m);
  assert.equal(restoredMentions(snap, hist, "other"), undefined, "another chat's history is never used");
  assert.equal(restoredMentions({ ...snap, scope: scope({}) }, hist, "t"), undefined, "the scope's own mentions win");
  assert.equal(restoredMentions({ ...snap, turn: 9 }, hist, "t"), undefined);
  assert.deepEqual(entryModel(snap, { last: true, blocked: false, mentions: restoredMentions(snap, hist, "t") }).mentions.map((x) => x.label), ["Физика"]);
  // A rerun keeps the scope, so it keeps the mentions.
  const s = reducePanel(initialPanelState, { type: "rerunAsked", from: { threadId: "t", turn: 1, text: "q", scope: scope({ mentions: m }) } });
  assert.deepEqual(entryModel(s.entries[0]).mentions.map((x) => x.label), ["Физика"]);
});

// ---- render ----

const ready = reducePanel(initialPanelState, { type: "link", link: { link: "ready", readOnly: false } });
const offline = reducePanel(initialPanelState, { type: "link", link: { link: "no_hub", readOnly: true } });
const ctx = { label: "Сегодня", removable: false, ask: { subjectId: "", lessonId: "", assignmentId: "", screen: "today" } };
const composer = (over = {}) => ({
  pills: [],
  popup: null,
  active: 0,
  listId: ":r1:",
  activeId: undefined,
  onChange: noop,
  onKeyDown: () => false,
  onCaret: noop,
  onFocus: noop,
  onBlur: noop,
  pick: noop,
  hover: noop,
  remove: noop,
  retry: noop,
  clear: noop,
  ...over,
});
const panel = (over) =>
  renderToStaticMarkup(
    h(AgentPanelView, {
      state: ready,
      ctx,
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

test("render: the field is a combobox; closed it says so, open it points at the listbox and the active option", () => {
  const closed = panel({ mentions: composer() });
  assert.ok(closed.includes('role="combobox"') && closed.includes('aria-expanded="false"') && closed.includes('aria-autocomplete="list"'));
  assert.ok(!closed.includes("aria-controls") && !closed.includes('role="listbox"'));
  const popup = popupModel(
    { status: "ready", query: "", candidates: [cand("subject", "s", "Физика", "Предметы"), cand("lesson", "l", "<b>Лекция</b>", "Физика · 27.10.2026")] },
    [],
  );
  const open = panel({ mentions: composer({ popup, active: 1, activeId: ":r1:-1" }) });
  assert.ok(open.includes('aria-expanded="true"') && open.includes('aria-controls=":r1:"') && open.includes('aria-activedescendant=":r1:-1"'));
  assert.ok(open.includes('role="listbox"') && open.includes('id=":r1:"'));
  assert.equal(open.split('role="option"').length - 1, 2);
  assert.equal(open.split('aria-selected="true"').length - 1, 1);
  assert.ok(open.includes('id=":r1:-1"'));
  assert.ok(open.includes("&lt;b&gt;Лекция&lt;/b&gt;") && !open.includes("<b>Лекция"), "the name is text");
  assert.ok(open.includes("Пара") && open.includes("Физика · 27.10.2026"));
});

test("render: status lines sit inside the popup; «Повторить» only after a failure", () => {
  const withState = (search, pills = []) => panel({ mentions: composer({ popup: popupModel(search, pills), pills }) });
  assert.ok(withState({ status: "loading", query: "", candidates: [] }).includes("Ищу…"));
  assert.ok(withState({ status: "ready", query: "я", candidates: [] }).includes("Ничего не найдено"));
  const off = withState({ status: "unavailable", query: "", candidates: [] });
  assert.ok(off.includes("Поиск недоступен: помощник не подключён") && !off.includes("Повторить"));
  const bad = withState({ status: "failed", query: "", candidates: [] });
  assert.ok(bad.includes("Не удалось выполнить поиск") && bad.includes("Повторить"));
  const eight = Array.from({ length: 8 }, (_, i) => ({ kind: "subject", id: `s${i}`, label: `П${i}` }));
  const full = withState({ status: "ready", query: "", candidates: [cand("subject", "x")] }, eight);
  assert.ok(full.includes("Не больше 8 упоминаний в одной просьбе") && !full.includes('role="option"'));
  assert.ok(full.includes('role="status"'));
});

test("render: the chosen mentions are a labelled row of removable pills; the name is escaped", () => {
  const evil = "<img src=x onerror=1>";
  const html = panel({ mentions: composer({ pills: [{ kind: "subject", id: "s", label: evil }, { kind: "lesson", id: "l", label: "Лекция" }] }) });
  assert.ok(html.includes('aria-label="Упоминания"'));
  assert.ok(html.includes(`aria-label="Убрать упоминание ${evil.replace("<", "&lt;").replace(">", "&gt;")}"`));
  assert.ok(html.includes('aria-label="Убрать упоминание Лекция"'));
  assert.ok(!html.includes("<img"));
  assert.ok(!panel({ mentions: composer() }).includes("Убрать упоминание"), "no row without pills");
});

test("render: the hint about «@» shows in an empty chat only, with the field on", () => {
  assert.ok(panel({ mentions: composer() }).includes(mentionTexts.hint));
  assert.equal(mentionTexts.hint, "Напишите @, чтобы указать предмет, пару, материал или задание");
  assert.ok(!panel({ mentions: composer(), state: offline }).includes(mentionTexts.hint));
  assert.ok(!panel({}).includes(mentionTexts.hint), "no hint without mentions");
  const asked = reducePanel(ready, { type: "asked", id: 1, text: "q", scope: scope({}) });
  assert.ok(!panel({ mentions: composer(), state: asked }).includes(mentionTexts.hint));
});

test("render: the pills of a request are buttons with the stored name and open the object", () => {
  const stored = [{ kind: "subject", id: "s1", label: "Старое имя <i>x</i>" }, { kind: "assignment", id: "a1", label: "Задачи" }];
  const model = entryModel(live({ scope: scope({ mentions: stored }) }));
  const opened = [];
  const html = renderToStaticMarkup(
    h(AgentRequest, { model, actions: { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined, openMention: (k, i) => opened.push([k, i]) } }),
  );
  assert.ok(html.includes('<button type="button" class="ag-pill small"'));
  assert.ok(html.includes("Старое имя &lt;i&gt;x&lt;/i&gt;") && !html.includes("<i>x"));
  assert.ok(html.includes('aria-label="Открыть: Предмет Старое имя &lt;i&gt;x&lt;/i&gt;"'));
  assert.ok(!html.includes("удалено"));
  // The user's text comes first, the pills under it.
  assert.ok(html.indexOf("что задали") < html.indexOf("ag-pill"));
  const el = MessagePills({ pills: model.mentions, onOpen: (k, i) => opened.push([k, i]) });
  const buttons = [];
  const walk = (n) => {
    if (Array.isArray(n)) return n.forEach(walk);
    if (n && typeof n === "object" && n.props) {
      if (n.type === "button") buttons.push(n);
      walk(n.props.children);
    }
  };
  walk(el);
  assert.equal(buttons.length, 2);
  buttons[1].props.onClick();
  assert.deepEqual(opened, [["assignment", "a1"]]);
});

test("render: earlier requests show their pills as buttons when the panel can open them", () => {
  const earlier = [{ key: "e1", turn: 1, text: "q", mentions: mentionPills([{ kind: "material", id: "m1", label: "Конспект.pdf" }]), reply: "" }];
  const html = panel({ earlier, onOpenMention: noop });
  assert.ok(html.includes('<button type="button" class="ag-pill small"') && html.includes('aria-label="Открыть: Материал Конспект.pdf"'));
  assert.ok(html.includes('title="Материал: Конспект.pdf"'));
  const plain = panel({ earlier });
  assert.ok(!plain.includes("<button type=\"button\" class=\"ag-pill"), "no dead button without a handler");
  assert.ok(plain.includes("Конспект.pdf"));
});

// ---- the page code never writes markup from names ----

test("no source of the agent panel writes HTML", () => {
  const dir = new URL("../src/agent/", import.meta.url);
  const files = readdirSync(dir).filter((f) => f.endsWith(".tsx") || f.endsWith(".ts"));
  assert.ok(files.includes("MentionParts.tsx") && files.includes("useMentionComposer.ts"));
  for (const f of files) {
    assert.doesNotMatch(readFileSync(new URL(f, dir), "utf8"), /dangerouslySetInnerHTML|innerHTML|outerHTML|insertAdjacentHTML/, f);
  }
  for (const f of ["../src/agentMentions.ts", "../src/agentView.ts"]) {
    assert.doesNotMatch(readFileSync(new URL(f, import.meta.url), "utf8"), /dangerouslySetInnerHTML|innerHTML/, f);
  }
});
