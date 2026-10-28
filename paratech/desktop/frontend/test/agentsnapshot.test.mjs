// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-273: the current request of a chat opened after a restart comes back as a live card (chatCurrent snapshot).
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { agentApi, methods, parseCurrent } from "../.test-build/bindings/agent.js";
import { hasLiveRequest, initialPanelState, loadSnapshot, reducePanel, snapshotText } from "../.test-build/agentState.js";
import { chatProblemText, entryModel, refreshesChats, visibleEntries } from "../.test-build/agentView.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

const planCard = {
  kind: "plan",
  plan: { summary: "Добавить срок <b>сдачи</b>", steps: [{ tool: "propose_deadline", args: [], deferred: [] }], notes: [] },
  questions: null,
  jobId: "",
  artifact: "",
  target: null,
  actionId: "",
  answerId: "",
  cannotDo: null,
  text: "",
  assignments: [],
  assignment: null,
  stopReason: "",
  workId: "",
  outcome: "",
  freeWork: null,
};

const update = (over) => ({
  threadId: "a", turn: 2, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "AWAITING_PLAN",
  stopReason: "", problem: "", progress: false, cards: [planCard], ...over,
});
const live = (s, u) => reducePanel(s, { type: "update", update: u });
const snap = (s, u, text = "") => reducePanel(s, { type: "snapshot", update: u, text });
const scope = (over) => ({ text: "x", subjectId: "", lessonId: "", assignmentId: "", screen: "today", ...over });
const ids = (e) => entryModel(e).cards.flatMap((c) => ("buttons" in c ? c.buttons.map((b) => b.id) : []));

test("a plan waiting for «Да» after a restart becomes a card with the confirm buttons", () => {
  const s = snap(initialPanelState, update(), "Поставь срок на пятницу");
  const e = s.entries[0];
  assert.equal(e.key, "thread:a:2");
  assert.equal(e.threadId, "a");
  assert.equal(e.turn, 2);
  assert.equal(e.text, "Поставь срок на пятницу");
  assert.deepEqual(ids(e), ["approve", "revise", "cancel"]);
  assert.equal(visibleEntries(s.entries, "a").length, 1);
  assert.equal(visibleEntries(s.entries, "b").length, 0);

  const seen = [];
  const html = renderToStaticMarkup(
    h(AgentPanelView, {
      state: { ...s, link: { link: "ok", readOnly: false } },
      ctx: { label: "Сегодня", removable: false, ask: { subjectId: "", lessonId: "", assignmentId: "", screen: "today" } },
      draft: "",
      notice: "",
      inputRef: { current: null },
      onDraft: noop,
      onSubmit: noop,
      onRemoveContext: noop,
      actionsFor: (entry) => {
        seen.push(entry);
        return { busy: false, press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
      },
    }),
  );
  assert.ok(html.includes("Поставь срок на пятницу"));
  assert.ok(html.includes("Да") && html.includes("Изменить") && html.includes("Отмена"));
  assert.ok(!/<button[^>]*disabled=""[^>]*>\s*(<[^>]*>)*Да/.test(html), "«Да» is enabled");
  // Model text is escaped, never HTML.
  assert.ok(html.includes("&lt;b&gt;сдачи&lt;/b&gt;") && !html.includes("<b>сдачи"));
  assert.ok(!html.includes("Подтверждение пока недоступно"));
});

test("«Да» of the snapshot card calls approvePlan with the request's thread and turn", async () => {
  const s = snap(initialPanelState, update());
  const e = s.entries[0];
  // The panel builds `ref` from the entry; a named entry (threadId set) is what lets the buttons act.
  assert.notEqual(e.threadId, "");
  const calls = [];
  const api = agentApi(async (method, arg) => {
    calls.push([method, arg]);
    return { problem: "" };
  });
  await api.approvePlan({ threadId: e.threadId, turn: e.turn });
  assert.deepEqual(calls, [[methods.approvePlan, { threadId: "a", turn: 2 }]]);
});

test("a stopped request restored from the snapshot offers «Продолжить»; a running one only the status", () => {
  const stopped = snap(initialPanelState, update({ status: "STOPPED", stopReason: "user", cards: [] })).entries[0];
  assert.deepEqual(ids(stopped), ["continue", "cancel"]);
  const running = snap(initialPanelState, update({ status: "EXECUTING", cards: [] })).entries[0];
  assert.deepEqual(ids(running), ["stop"]);
});

test("an update after the snapshot replaces the card, it does not add a second one", () => {
  let s = snap(initialPanelState, update(), "q");
  s = live(s, update({ status: "EXECUTING", cards: [] }));
  s = live(s, update({ status: "DONE", cards: [{ ...planCard, kind: "reply", plan: null, text: "Готово" }] }));
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].text, "q", "the text from the history stays");
  assert.equal(s.entries[0].update.status, "DONE");
  // The next turn is a new entry next to it.
  s = live(s, update({ turn: 3, status: "EXECUTING", cards: [] }));
  assert.deepEqual(s.entries.map((x) => x.key), ["thread:a:2", "thread:a:3"]);
});

test("a live update wins over a late snapshot of the same chat", () => {
  let s = live(initialPanelState, update({ status: "EXECUTING", cards: [] }));
  const before = s;
  s = snap(s, update());
  assert.equal(s, before);
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].update.status, "EXECUTING");
  // Also a newer turn, and a request just sent into the chat that waits for its first update.
  assert.equal(snap(live(initialPanelState, update({ turn: 3 })), update({ turn: 2 })).entries.length, 1);
  const sent = reducePanel(initialPanelState, { type: "asked", id: 1, text: "q", scope: scope({ threadId: "a" }) });
  assert.equal(snap(sent, update()).entries.length, 1);
  // A snapshot of another chat is not blocked by that.
  assert.equal(snap(sent, update({ threadId: "b" })).entries.length, 2);
});

test("a snapshot of a failed Ask shape or an unnamed chat is ignored", () => {
  assert.equal(snap(initialPanelState, update({ threadId: "" })).entries.length, 0);
  assert.equal(snap(initialPanelState, update({ turn: 0, status: "", cards: [] })).entries.length, 0);
});

test("hasLiveRequest: only this chat counts", () => {
  const s = live(initialPanelState, update({ threadId: "b" }));
  assert.equal(hasLiveRequest(s.entries, "b"), true);
  assert.equal(hasLiveRequest(s.entries, "a"), false);
  assert.equal(hasLiveRequest([], ""), false);
});

test("snapshotText takes the student's words of the same turn, if the history has them", () => {
  const reqs = [{ turn: 1, text: "q1" }, { turn: 2, text: "q2" }];
  assert.equal(snapshotText(reqs, 2), "q2");
  assert.equal(snapshotText(reqs, 5), "");
  assert.equal(snapshotText([], 1), "");
});

// ---- loadSnapshot ----

function harness(over = {}) {
  let state = initialPanelState;
  let open = true;
  const problems = [];
  const calls = [];
  const deps = {
    threadId: "a",
    entries: () => state.entries,
    stillOpen: () => open,
    current: async () => {
      calls.push("current");
      return { problem: "", update: update() };
    },
    earlier: Promise.resolve([{ turn: 2, text: "q2", mentions: [], reply: "", current: true }]),
    dispatch: (a) => {
      state = reducePanel(state, a);
    },
    onProblem: (p) => problems.push(p),
    ...over,
  };
  return { deps, get state() { return state; }, problems, calls, close: () => (open = false), set: (s) => (state = s) };
}

test("loadSnapshot shows the card with the student's text from the history", async () => {
  const t = harness();
  await loadSnapshot(t.deps);
  assert.equal(t.state.entries.length, 1);
  assert.equal(t.state.entries[0].text, "q2");
  assert.deepEqual(ids(t.state.entries[0]), ["approve", "revise", "cancel"]);
});

test("loadSnapshot does not even ask when the session shows the chat live", async () => {
  const t = harness();
  t.set(live(initialPanelState, update({ status: "EXECUTING", cards: [] })));
  await loadSnapshot(t.deps);
  assert.deepEqual(t.calls, []);
  assert.equal(t.state.entries[0].update.status, "EXECUTING");
});

test("a snapshot that arrives after the chat was switched is dropped", async () => {
  let release;
  const t = harness({
    current: () => new Promise((r) => (release = () => r({ problem: "", update: update() }))),
  });
  const done = loadSnapshot(t.deps);
  t.close();
  release();
  await done;
  assert.equal(t.state.entries.length, 0);

  // Also when the switch happens while the history is still being read.
  let gotEarlier;
  const u = harness({ earlier: new Promise((r) => (gotEarlier = r)) });
  const d2 = loadSnapshot(u.deps);
  await Promise.resolve();
  u.close();
  gotEarlier([]);
  await d2;
  assert.equal(u.state.entries.length, 0);
});

test("a live update that arrives while the snapshot is loading wins", async () => {
  let release;
  const t = harness({
    current: () => new Promise((r) => (release = () => r({ problem: "", update: update() }))),
  });
  const done = loadSnapshot(t.deps);
  t.set(live(t.state, update({ status: "EXECUTING", cards: [] })));
  release();
  await done;
  assert.equal(t.state.entries.length, 1);
  assert.equal(t.state.entries[0].update.status, "EXECUTING");
});

test("null keeps the text line: no entry, no problem", async () => {
  const t = harness({ current: async () => ({ problem: "", update: null }) });
  await loadSnapshot(t.deps);
  assert.equal(t.state.entries.length, 0);
  assert.deepEqual(t.problems, []);
});

test("an update of another chat is never taken", async () => {
  const t = harness({ current: async () => ({ problem: "", update: update({ threadId: "b" }) }) });
  await loadSnapshot(t.deps);
  assert.equal(t.state.entries.length, 0);
});

test("problems are reported and leave the panel as it was; a thrown call reads as failed", async () => {
  for (const p of ["not_found", "invalid", "failed", "busy"]) {
    const t = harness({ current: async () => ({ problem: p, update: null }) });
    await loadSnapshot(t.deps);
    assert.deepEqual(t.problems, [p]);
    assert.equal(t.state.entries.length, 0);
  }
  const t = harness({
    current: async () => {
      throw new Error("x");
    },
  });
  await loadSnapshot(t.deps);
  assert.deepEqual(t.problems, ["failed"]);
  // Malformed data of the binding is a failed call.
  assert.deepEqual(parseCurrent({ problem: "", update: { threadId: 1 } }), { problem: "failed", update: null });
  // A closed chat reports nothing.
  const c = harness({ current: async () => ({ problem: "failed", update: null }) });
  const done = loadSnapshot(c.deps);
  c.close();
  await done;
  assert.deepEqual(c.problems, []);
});

test("the problem texts: a missing chat re-reads the list, the rest is a Russian line", () => {
  assert.equal(refreshesChats("not_found"), true);
  assert.equal(chatProblemText("not_found", "current"), "Чат не найден: список обновлён.");
  for (const p of ["invalid", "failed"]) {
    assert.equal(refreshesChats(p), false);
    assert.equal(chatProblemText(p, "current"), "Не удалось загрузить текущую просьбу чата.");
  }
});

test("the key of an orphan includes the thread: failures of two chats in one scope stay apart", () => {
  const fail = (threadId) => update({ threadId, turn: 0, status: "", problem: "failed", cards: [] });
  let s = live(initialPanelState, fail("a"));
  s = live(s, fail("b"));
  assert.equal(s.entries.length, 2);
  assert.ok(s.entries.every((e) => e.key.startsWith("orphan:")));
  assert.equal(new Set(s.entries.map((e) => e.key)).size, 2);
  assert.ok(s.entries[0].key.includes("a") && s.entries[1].key.includes("b"));
  // The same chat and scope again updates the same entry.
  s = live(s, fail("a"));
  assert.equal(s.entries.length, 2);
  // No thread at all stays one orphan per scope.
  let o = live(initialPanelState, update({ threadId: "", turn: 0, status: "", problem: "failed", cards: [] }));
  o = live(o, update({ threadId: "", turn: 0, status: "", problem: "failed", cards: [] }));
  assert.equal(o.entries.length, 1);
});
