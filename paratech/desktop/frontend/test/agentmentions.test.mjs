// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-246: the «@» search binding.
import { test } from "node:test";
import assert from "node:assert/strict";
import { agentApi, methods, parseMentions } from "../.test-build/bindings/agent.js";

const cand = (kind, id) => ({ kind, id, label: `Имя ${id}`, hint: "Физика · 27.10.2026" });

test("each kind parses with its hint", () => {
  const list = ["subject", "lesson", "material", "assignment"].map((k, i) => cand(k, `id${i}`));
  assert.deepEqual(parseMentions({ problem: "", candidates: list }), { problem: "", candidates: list });
});

test("bad entries are dropped; a missing hint reads as empty; markup stays plain text", () => {
  const html = { kind: "subject", id: "s", label: "<b>x</b>" };
  const v = parseMentions({
    problem: "",
    candidates: [
      null,
      "x",
      5,
      [],
      { kind: "folder", id: "a", label: "a", hint: "" },
      { kind: "subject", label: "a", hint: "" },
      { kind: "subject", id: "", label: "a" },
      { kind: "subject", id: "a" },
      { kind: "subject", id: "a", label: "" },
      { kind: 1, id: "a", label: "a" },
      html,
    ],
  });
  assert.deepEqual(v, { problem: "", candidates: [{ ...html, hint: "" }] });
});

test("at most 20 candidates are kept", () => {
  const list = Array.from({ length: 30 }, (_, i) => cand("material", `m${i}`));
  const v = parseMentions({ problem: "", candidates: list });
  assert.equal(v.candidates.length, 20);
  assert.equal(v.candidates[19].id, "m19");
});

test("problems: known codes pass, unknown or malformed read as failed, non-array candidates are empty", () => {
  assert.deepEqual(parseMentions({ problem: "unavailable", candidates: [] }), { problem: "unavailable", candidates: [] });
  assert.deepEqual(parseMentions({ problem: "failed", candidates: [cand("lesson", "l")] }), { problem: "failed", candidates: [] });
  assert.equal(parseMentions({ problem: "weird", candidates: [] }).problem, "failed");
  assert.equal(parseMentions({ candidates: [] }).problem, "failed");
  assert.deepEqual(parseMentions({ problem: "", candidates: null }), { problem: "", candidates: [] });
  assert.deepEqual(parseMentions({ problem: "", candidates: "x" }), { problem: "", candidates: [] });
  for (const bad of [null, undefined, "x", 5, []]) {
    assert.deepEqual(parseMentions(bad), { problem: "failed", candidates: [] });
  }
});

test("the call routes to MentionCandidates with the query", async () => {
  const calls = [];
  const api = agentApi(async (method, ...args) => {
    calls.push([method, ...args]);
    return { problem: "", candidates: [cand("subject", "s1")] };
  });
  const v = await api.mentionCandidates("физ");
  assert.deepEqual(calls, [[methods.mentionCandidates, "физ"]]);
  assert.ok(methods.mentionCandidates.endsWith("wails.AgentService.MentionCandidates"));
  assert.equal(v.candidates[0].id, "s1");
});
