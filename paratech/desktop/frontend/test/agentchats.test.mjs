// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-206: chat list, chat calls, earlier requests, Ask with threadId and mentions, journal chatTitle.
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  agentApi,
  methods,
  parseChatResult,
  parseChats,
  parseEarlier,
  parseJournalLine,
  parseUpdate,
} from "../.test-build/bindings/agent.js";

const chat = { threadId: "th1", title: "Новый чат", archived: false, lastActive: "2026-10-27T10:00:00Z", running: true };

test("the chat list parses; malformed data reads as failed", () => {
  const v = parseChats({ problem: "", chats: [chat, { ...chat, threadId: "th2", archived: true, running: false }] });
  assert.equal(v.problem, "");
  assert.deepEqual(v.chats[0], chat);
  assert.equal(v.chats[1].archived, true);
  assert.deepEqual(parseChats({ problem: "", chats: null }), { problem: "", chats: [] });
  assert.deepEqual(parseChats({ problem: "failed", chats: [] }), { problem: "failed", chats: [] });
  for (const bad of [null, "x", 5, [], { problem: "", chats: "x" }, { problem: "", chats: [{ title: "x" }] }]) {
    assert.deepEqual(parseChats(bad), { problem: "failed", chats: [] });
  }
});

test("a chat result carries the chat only on success; problems are known codes or failed", () => {
  assert.deepEqual(parseChatResult({ problem: "", chat }), { problem: "", chat });
  for (const p of ["invalid", "busy", "failed", "not_found"]) {
    assert.deepEqual(parseChatResult({ problem: p, chat: { threadId: "", title: "", archived: false, lastActive: "", running: false } }), {
      problem: p,
      chat: null,
    });
  }
  assert.equal(parseChatResult({ problem: "weird" }).problem, "failed");
  assert.equal(parseChatResult({ chat }).problem, "failed");
  for (const bad of [null, 1, "x", [], { problem: "" }, { problem: "", chat: 3 }, { problem: "", chat: {} }]) {
    assert.deepEqual(parseChatResult(bad), { problem: "failed", chat: null });
  }
});

test("earlier requests parse with mentions; malformed data reads as failed", () => {
  const req = {
    turn: 2,
    text: "что задали @Физика",
    mentions: [{ kind: "subject", id: "s1", label: "Физика" }],
    reply: "Задали задачи",
    current: true,
  };
  const v = parseEarlier({ problem: "", requests: [{ ...req, mentions: null, reply: "", current: false }, req] });
  assert.equal(v.problem, "");
  assert.deepEqual(v.requests[0].mentions, []);
  assert.equal(v.requests[0].current, false);
  assert.deepEqual(v.requests[1], req);
  assert.deepEqual(parseEarlier({ problem: "not_found", requests: [] }), { problem: "not_found", requests: [] });
  assert.equal(parseEarlier({ problem: "nope" }).problem, "failed");
  for (const bad of [null, "x", { problem: "", requests: [{ text: "x" }] }, { problem: "", requests: [{ turn: 1, text: "x", mentions: [{ kind: 1 }] }] }]) {
    assert.deepEqual(parseEarlier(bad), { problem: "failed", requests: [] });
  }
});

test("the api calls the methods by name with the arguments", async () => {
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    if (method === methods.chats) return { problem: "", chats: [chat] };
    if (method === methods.chatEarlier) return { problem: "", requests: [] };
    if (method === methods.ask) return { problem: "" };
    return { problem: "", chat };
  });
  assert.equal((await api.chats()).chats.length, 1);
  assert.deepEqual((await api.newChat()).chat, chat);
  assert.equal((await api.renameChat("th1", "Физика")).problem, "");
  assert.equal((await api.archiveChat("th1")).problem, "");
  assert.equal((await api.restoreChat("th1")).problem, "");
  assert.deepEqual(await api.chatEarlier("th1"), { problem: "", requests: [] });
  const ask = { text: "x", subjectId: "", lessonId: "", assignmentId: "", screen: "", threadId: "th1", mentions: [{ kind: "subject", id: "s1", label: "Физика" }] };
  assert.equal(await api.ask(ask), "");
  assert.deepEqual(calls.map((c) => c[0]), [
    "paratech/desktop/internal/input/wails.AgentService.Chats",
    "paratech/desktop/internal/input/wails.AgentService.NewChat",
    "paratech/desktop/internal/input/wails.AgentService.RenameChat",
    "paratech/desktop/internal/input/wails.AgentService.ArchiveChat",
    "paratech/desktop/internal/input/wails.AgentService.RestoreChat",
    "paratech/desktop/internal/input/wails.AgentService.ChatEarlier",
    "paratech/desktop/internal/input/wails.AgentService.Ask",
  ]);
  assert.deepEqual(calls[2].slice(1), ["th1", "Физика"]);
  assert.deepEqual(calls[3].slice(1), ["th1"]);
  assert.deepEqual(calls[6][1], ask);
  // A malformed answer of the core is a failed call, never a throw.
  const broken = agentApi(async () => 42);
  assert.deepEqual(await broken.chats(), { problem: "failed", chats: [] });
  assert.deepEqual(await broken.newChat(), { problem: "failed", chat: null });
  assert.deepEqual(await broken.chatEarlier("th1"), { problem: "failed", requests: [] });
});

test("a journal line carries chatTitle; an older core reads as empty", () => {
  assert.equal(parseJournalLine({ kind: "action", threadId: "th1", chatTitle: "Физика" }).chatTitle, "Физика");
  assert.equal(parseJournalLine({ kind: "action", threadId: "th1" }).chatTitle, "");
  assert.throws(() => parseJournalLine({ kind: "action", chatTitle: 5 }));
});

test("an agent update exposes its threadId", () => {
  const u = parseUpdate({
    threadId: "th9", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "", status: "DONE", stopReason: "",
    problem: "", progress: false, cards: [],
  });
  assert.equal(u.threadId, "th9");
});
