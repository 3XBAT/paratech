// TASK-207: chat list, update routing by threadId, problem texts, title check, earlier requests (text only).
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { initialPanelState, reducePanel } from "../.test-build/agentState.js";
import {
  chatBusyText,
  chatGoneText,
  chatProblemText,
  chatSections,
  checkChatTitle,
  earlierItems,
  openAfterArchive,
  reconcileOpenChat,
  refreshesChats,
  visibleEntries,
} from "../.test-build/agentView.js";
import { lineModel } from "../.test-build/journalView.js";
import { ChatBar } from "../.test-build/agent/ChatBar.js";
import { AgentPanelView } from "../.test-build/agent/AgentPanelView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

const chat = (over) => ({ threadId: "a", title: "Чат", archived: false, lastActive: "2026-10-27T10:00:00Z", running: false, ...over });
const update = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
  stopReason: "", problem: "", progress: false, cards: [], ...over,
});
const apply = (s, ...us) => us.reduce((acc, u) => reducePanel(acc, { type: "update", update: u }), s);
const scope = (over) => ({ text: "x", subjectId: "", lessonId: "", assignmentId: "", screen: "today", ...over });

test("the list: unarchived by last activity, the archive below, equal times keep the core's order", () => {
  const s = chatSections(
    [
      chat({ threadId: "old", lastActive: "2026-10-25T10:00:00Z" }),
      chat({ threadId: "arch", archived: true, lastActive: "2026-10-20T10:00:00Z" }),
      chat({ threadId: "new", lastActive: "2026-10-27T10:00:00Z" }),
      chat({ threadId: "arch2", archived: true, lastActive: "2026-10-26T10:00:00Z" }),
      chat({ threadId: "tie1", lastActive: "2026-10-26T10:00:00Z" }),
      chat({ threadId: "tie2", lastActive: "2026-10-26T10:00:00Z" }),
      chat({ threadId: "bad", lastActive: "" }),
    ],
    [],
  );
  assert.deepEqual(s.active.map((r) => r.threadId), ["new", "tie1", "tie2", "old", "bad"]);
  assert.deepEqual(s.archived.map((r) => r.threadId), ["arch2", "arch"]);
});

test("an empty title reads as «Без названия»", () => {
  assert.equal(chatSections([chat({ title: "  " })], []).active[0].title, "Без названия");
});

test("the running mark: the core's flag, or a request the panel sees running or just sent in that chat", () => {
  const entries = apply(initialPanelState, update({ threadId: "b", status: "EXECUTING" })).entries;
  const sent = reducePanel(initialPanelState, { type: "asked", id: 1, text: "q", scope: scope({ threadId: "c" }) }).entries;
  const rows = chatSections([chat({ threadId: "a", running: true }), chat({ threadId: "b" }), chat({ threadId: "c" }), chat({ threadId: "d" })], [...entries, ...sent]).active;
  const run = Object.fromEntries(rows.map((r) => [r.threadId, r.running]));
  assert.deepEqual(run, { a: true, b: true, c: true, d: false });
  const done = apply(initialPanelState, update({ threadId: "b", status: "DONE" })).entries;
  assert.equal(chatSections([chat({ threadId: "b" })], done).active[0].running, false);
});

test("updates land on their own chat: another chat's request is neither drawn nor overwritten", () => {
  const card = { kind: "reply", text: "ответ A" };
  let s = apply(initialPanelState, update({ threadId: "a", turn: 1, cards: [card] }));
  s = apply(s, update({ threadId: "b", turn: 1, status: "EXECUTING" }), update({ threadId: "b", turn: 1, status: "DONE", cards: [{ kind: "reply", text: "ответ B" }] }));
  assert.equal(s.entries.length, 2);
  const a = visibleEntries(s.entries, "a");
  assert.deepEqual(a.map((e) => e.threadId), ["a"]);
  assert.equal(a[0].update.cards[0].text, "ответ A");
  assert.equal(visibleEntries(s.entries, "b")[0].update.cards[0].text, "ответ B");
  assert.deepEqual(visibleEntries(s.entries, "none"), []);
});

test("a sent request belongs to the chat it was asked in until the core names the thread", () => {
  let s = reducePanel(initialPanelState, { type: "asked", id: 1, text: "q", scope: scope({ threadId: "a" }) });
  assert.equal(visibleEntries(s.entries, "a").length, 1);
  assert.equal(visibleEntries(s.entries, "b").length, 0);
  // An update of another chat with the same scope must not take the request of chat a.
  s = apply(s, update({ threadId: "b", status: "EXECUTING" }));
  assert.equal(s.entries.find((e) => e.key === "local:1").threadId, "");
  assert.equal(visibleEntries(s.entries, "b").length, 1);
  s = apply(s, update({ threadId: "a", status: "EXECUTING" }));
  assert.equal(s.entries.find((e) => e.key === "local:1"), undefined);
  assert.equal(visibleEntries(s.entries, "a")[0].text, "q");
});

test("an Ask without a chat is matched by scope; a failed Ask (threadId \"\") lands on its request", () => {
  let s = reducePanel(initialPanelState, { type: "asked", id: 1, text: "q", scope: scope({ threadId: "" }) });
  s = apply(s, update({ threadId: "", status: "FAILED", problem: "failed" }));
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].update.problem, "failed");
  assert.equal(visibleEntries(s.entries, "").length, 1);
  // An orphaned failure with no request of its own is shown in any chat.
  const o = apply(initialPanelState, update({ threadId: "", status: "FAILED", problem: "failed" }));
  assert.equal(visibleEntries(o.entries, "a").length, 1);
});

test("problem texts: busy on rename/archive has its own, not_found refreshes, the rest is generic", () => {
  assert.equal(chatProblemText("", "rename"), "");
  assert.equal(chatProblemText("busy", "rename"), chatBusyText);
  assert.equal(chatProblemText("busy", "archive"), chatBusyText);
  assert.equal(chatBusyText, "Чат занят: дождитесь конца просьбы или остановите её.");
  assert.notEqual(chatProblemText("busy", "restore"), chatBusyText);
  assert.equal(chatProblemText("not_found", "open"), chatGoneText);
  assert.equal(refreshesChats("not_found"), true);
  for (const p of ["invalid", "failed", "weird"]) {
    assert.equal(refreshesChats(p), false);
    for (const op of ["list", "open", "new", "rename", "archive", "restore"]) {
      assert.match(chatProblemText(p, op), /^Не удалось /);
    }
  }
  assert.equal(chatProblemText("invalid", "rename"), chatProblemText("failed", "rename"));
});

test("title check: trimmed, not empty, at most 80 characters (not UTF-16 units)", () => {
  assert.deepEqual(checkChatTitle("  Физика  "), { ok: true, title: "Физика" });
  assert.equal(checkChatTitle("   ").ok, false);
  assert.equal(checkChatTitle("а".repeat(80)).ok, true);
  assert.equal(checkChatTitle("а".repeat(81)).ok, false);
  // 80 emoji are 160 UTF-16 units but 80 characters.
  assert.equal(checkChatTitle("😀".repeat(80)).ok, true);
  assert.equal(checkChatTitle("😀".repeat(81)).ok, false);
});

test("the open chat after a list read and after archiving", () => {
  const list = [chat({ threadId: "a", lastActive: "2026-10-27T10:00:00Z" }), chat({ threadId: "b", lastActive: "2026-10-26T10:00:00Z" }), chat({ threadId: "z", archived: true })];
  assert.equal(reconcileOpenChat(list, "b"), "b");
  assert.equal(reconcileOpenChat(list, "z"), "z", "an archived chat can stay open");
  assert.equal(reconcileOpenChat(list, "gone"), "a");
  assert.equal(reconcileOpenChat(list, ""), "a");
  assert.equal(reconcileOpenChat([], "a"), "");
  assert.equal(openAfterArchive(list, "a", "b"), "a");
  assert.equal(openAfterArchive(list, "a", "a"), "b");
  assert.equal(openAfterArchive([chat({ threadId: "a" })], "a", "a"), "");
});

test("earlier requests: text, mentions with the stored label, live requests left out", () => {
  const req = (turn, over) => ({ turn, text: `q${turn}`, mentions: [], reply: `r${turn}`, current: false, ...over });
  const live = apply(initialPanelState, update({ threadId: "a", turn: 3 })).entries;
  const items = earlierItems(
    [
      req(3, { current: true }),
      req(2, { mentions: [{ kind: "subject", id: "s1", label: "Физика" }, { kind: "lesson", id: "l1", label: "" }, { kind: "x", id: "i", label: " " }] }),
      req(1, { reply: "" }),
    ],
    live,
    "a",
  );
  assert.deepEqual(items.map((i) => i.turn), [1, 2]);
  assert.deepEqual(items[1].mentions.map((m) => m.label), ["Физика", "Занятие", "Объект"]);
  assert.equal(items[0].reply, "");
  // Entries of another chat do not hide the same turn here.
  assert.equal(earlierItems([req(3)], live, "b").length, 1);
});

test("the journal line shows the chat title as plain text; none for processing lines", () => {
  const line = { kind: "action", at: "", updatedAt: "", subjectId: "", actionId: "x", threadId: "a", chatTitle: " <b>Физика</b> ", turn: 1, tool: "create_assignment", jobId: "", jobType: "", status: "DONE", undoable: false, cannotUndo: false, noUndo: "" };
  assert.equal(lineModel(line, 0, new Map()).chat, "<b>Физика</b>");
  assert.equal(lineModel({ ...line, chatTitle: "" }, 0, new Map()).chat, "");
  assert.equal(lineModel({ ...line, kind: "processing", jobId: "j", jobType: "stt" }, 0, new Map()).chat, "");
});

const sections = chatSections(
  [chat({ threadId: "a", title: "<img src=x onerror=1>", running: true }), chat({ threadId: "b", title: "Второй" }), chat({ threadId: "c", title: "Старый", archived: true })],
  [],
);
const barProps = (over) => ({
  openChat: sections.active[0],
  sections,
  note: "",
  onOpen: noop,
  onNew: noop,
  onRename: async () => "",
  onArchive: noop,
  onRestore: noop,
  ...over,
});

test("the chat bar renders titles as text, marks the running chat, the Archive is collapsed", () => {
  const html = renderToStaticMarkup(h(ChatBar, barProps()));
  assert.ok(!html.includes("<img"), "title is escaped");
  assert.ok(html.includes("&lt;img src=x onerror=1&gt;"));
  assert.ok(html.includes("идёт просьба"));
  assert.ok(html.includes("Новый чат"));
  assert.ok(html.includes("В архив"));
  assert.ok(html.includes('aria-expanded="false"') && html.includes('aria-haspopup="menu"'));
  assert.ok(html.includes('aria-live="polite"'));
});

test("an archived open chat offers «Вернуть»; with no chat the title is «Новый чат» and rename is off", () => {
  const archived = renderToStaticMarkup(h(ChatBar, barProps({ openChat: sections.archived[0] })));
  assert.ok(archived.includes("Вернуть") && archived.includes("в архиве"));
  const none = renderToStaticMarkup(h(ChatBar, barProps({ openChat: null, sections: chatSections([], []) })));
  assert.ok(none.includes("Новый чат"));
  assert.ok(/<button[^>]*disabled=""[^>]*>Переименовать/.test(none));
  assert.ok(!none.includes("В архив"));
});

test("the panel draws earlier requests as text with mention pills and the reply line, no buttons", () => {
  const earlier = earlierItems(
    [{ turn: 1, text: "что задали <b>@Физика</b>", mentions: [{ kind: "subject", id: "s", label: "Физика" }], reply: "Задали <i>задачи</i>", current: false }],
    [],
    "a",
  );
  const html = renderToStaticMarkup(
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
      chat: barProps(),
      earlier,
    }),
  );
  assert.ok(html.includes("что задали &lt;b&gt;@Физика&lt;/b&gt;"));
  assert.ok(html.includes("Задали &lt;i&gt;задачи&lt;/i&gt;"));
  assert.ok(html.includes('class="ag-pill small"'));
  assert.ok(!html.includes("<b>@") && !html.includes("<i>"));
  assert.ok(!html.includes("Можно задать вопрос"), "the empty hint goes when earlier requests are shown");
  assert.ok(html.includes("ag-in-bottom"));
});
