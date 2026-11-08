// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
// TASK-400 (D5): the open «Предметы» reads again when an agent action line appears or is undone, not on every update.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { becameUndone, newActionLines } from "../.test-build/agentView.js";
import { resolveView, shownSubjectId } from "../.test-build/subjectsHome.js";

const line = (actionId) => ({ kind: "action_line", actionId });
const other = (kind) => ({ kind, actionId: "" });
const subject = (id, name) => ({ id, name, semester: "", aliases: [], folders: [], teachers: [] });

// Plays the updates of the panel against the decision AgentPanel makes; returns how many times the library was told.
function play(updates) {
  let seen = new Set();
  let reloads = 0;
  for (const cards of updates) {
    const r = newActionLines(seen, cards);
    seen = r.seen;
    if (r.fresh) {
      reloads++;
    }
  }
  return reloads;
}

test("a new action line (create_subject done) asks for a re-read once", () => {
  const r = newActionLines(new Set(), [other("reply"), line("a1")]);
  assert.equal(r.fresh, true);
  assert.deepEqual([...r.seen], ["a1"]);
});

test("a plain update, or the same line again, does not ask", () => {
  const seen = new Set(["a1"]);
  const same = newActionLines(seen, [line("a1"), other("progress")]);
  assert.equal(same.fresh, false);
  assert.equal(same.seen, seen, "nothing new: the same set is kept");
  assert.equal(newActionLines(seen, []).fresh, false);
  assert.equal(newActionLines(seen, [other("reply"), other("plan")]).fresh, false);
});

test("a line without an action id is no change", () => {
  assert.equal(newActionLines(new Set(), [line("")]).fresh, false);
});

test("only the lines that appear count: frequent updates of one request read once per new line", () => {
  const updates = [[], [other("progress")], [line("a1")], [line("a1"), other("reply")], [line("a1"), other("reply")], [line("a1"), line("a2"), other("reply")], [line("a1"), line("a2")]];
  assert.equal(play(updates), 2);
  assert.equal(play([[other("progress")], [other("reply")], [other("reply")]]), 0);
});

test("«Отменить» that comes back ok is a change; busy, a refusal and a second ok are not", () => {
  const busy = { busy: true, done: false, text: "" };
  const ok = { busy: false, done: true, text: "" };
  const refused = { busy: false, done: false, text: "Не удалось отменить." };
  assert.equal(becameUndone(undefined, busy), false);
  assert.equal(becameUndone(busy, refused), false);
  assert.equal(becameUndone(busy, ok), true);
  assert.equal(becameUndone(undefined, ok), true);
  assert.equal(becameUndone(ok, ok), false);
});

test("after the undo of create_subject the re-read list no longer holds the subject; its page gives way", () => {
  const before = [subject("s1", "Физика"), subject("s2", "Химия")];
  const after = before.filter((s) => s.id !== "s2");
  // The open page of the undone subject shows the home subject instead of a dead page.
  assert.deepEqual(resolveView("s2", before, ""), { kind: "subject", id: "s2" });
  const gone = resolveView("s2", after, "");
  assert.equal(gone.kind, "subject");
  assert.equal(shownSubjectId(gone), "s1");
  // The last subject undone: the tiles, not an empty page.
  assert.equal(resolveView("s1", [], "").kind, "empty");
});

// The wiring is React state; there is no DOM here, so the source is read (as other tests of this suite do).
const panel = readFileSync(new URL("../src/agent/AgentPanel.tsx", import.meta.url), "utf8");
const page = readFileSync(new URL("../src/pages/SubjectsPage.tsx", import.meta.url), "utf8");

test("AgentPanel tells the library on a new action line and on an undone one, not on every update", () => {
  assert.match(panel, /newActionLines\(seenActions\.current, update\.cards\)/);
  assert.match(panel, /if \(lines\.fresh\) \{\s*libraryChangedRef\.current\?\.\(\);/);
  assert.match(panel, /if \(becameUndone\(prev, note\)\) \{\s*libraryChangedRef\.current\?\.\(\);/);
  assert.equal(panel.match(/libraryChangedRef\.current\?\.\(\)/g).length, 2);
  assert.match(panel, /if \(!update\.progress\) \{\s*const lines = newActionLines/);
});

test("SubjectsPage reads the list, the files and the overview of the open subject again when reload changes", () => {
  assert.match(page, /useEffect\(load, \[load, props\.reload\]\)/);
  assert.match(page, /\[api, subject\?\.id, props\.reload, changes\]/);
  assert.match(page, /reloadSeen\.current !== props\.reload/);
  assert.match(page, /overviewFlow\.current\?\.refresh\(shownId\)/);
});
