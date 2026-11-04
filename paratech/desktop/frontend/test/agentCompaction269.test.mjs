// TASK-269: the compaction flags of agent:update are parsed and never reach the request list; nothing new is drawn.
import { test } from "node:test";
import assert from "node:assert/strict";
import { initialPanelState, reducePanel } from "../.test-build/agentState.js";
import { fillsFromUpdate } from "../.test-build/agentView.js";
import { parseUpdate } from "../.test-build/bindings/agent.js";

const wire = (over) => ({
  threadId: "a", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE",
  stopReason: "", problem: "", progress: false, cards: [], fill: null, ...over,
});
const fill = (percent, level = "normal", overflow = false) => ({ percent, level, overflow });
const compactionStart = { compaction: true, compacting: true, turn: 0, status: "", fill: null };
const compactionEnd = (f) => ({ compaction: true, compacting: false, turn: 0, status: "", fill: f });

test("parseUpdate reads compaction and compacting; missing reads false", () => {
  const start = parseUpdate(wire(compactionStart));
  assert.equal(start.compaction, true);
  assert.equal(start.compacting, true);
  assert.equal(start.fill, null);

  const end = parseUpdate(wire(compactionEnd(fill(12))));
  assert.equal(end.compaction, true);
  assert.equal(end.compacting, false);
  assert.deepEqual(end.fill, fill(12));

  const old = parseUpdate(wire({}));
  assert.equal(old.compaction, false);
  assert.equal(old.compacting, false);

  const plain = parseUpdate(wire({ compaction: false, compacting: false }));
  assert.equal(plain.compaction, false);
  assert.equal(plain.compacting, false);
  // Only a real boolean counts.
  assert.equal(parseUpdate(wire({ compaction: "true", compacting: 1 })).compaction, false);
});

test("reducePanel: a compaction update changes nothing, whether it starts or ends one", () => {
  const live = reducePanel(initialPanelState, { type: "update", update: parseUpdate(wire({})) });
  assert.equal(live.entries.length, 1);

  for (const raw of [wire(compactionStart), wire(compactionEnd(fill(12)))]) {
    const update = parseUpdate(raw);
    assert.equal(reducePanel(initialPanelState, { type: "update", update }), initialPanelState);
    assert.equal(reducePanel(live, { type: "update", update }), live);
  }
});

test("reducePanel: a normal update still works", () => {
  const s = reducePanel(initialPanelState, { type: "update", update: parseUpdate(wire({ status: "PLANNING" })) });
  assert.equal(s.entries.length, 1);
  assert.equal(s.entries[0].threadId, "a");
  assert.equal(s.entries[0].turn, 1);
  assert.equal(s.entries[0].update.status, "PLANNING");
});

test("regression: a failed-Ask-shaped update (turn 0, no status, compaction false) still makes its orphan entry", () => {
  const update = parseUpdate(wire({ turn: 0, status: "", problem: "failed" }));
  assert.equal(update.compaction, false);
  const s = reducePanel(initialPanelState, { type: "update", update });
  assert.equal(s.entries.length, 1);
  assert.match(s.entries[0].key, /^orphan:/);
  assert.equal(s.entries[0].update.problem, "failed");
});

test("fillsFromUpdate: the end of a compaction sets the new fill, the start leaves the fills as they are", () => {
  const prev = { a: fill(90, "high"), b: fill(5) };
  assert.equal(fillsFromUpdate(prev, parseUpdate(wire(compactionStart))), prev);

  const next = fillsFromUpdate(prev, parseUpdate(wire(compactionEnd(fill(12)))));
  assert.deepEqual(next, { a: fill(12), b: fill(5) });
  assert.notEqual(next, prev);

  // A failed compaction ends with no fill: the ring stays.
  assert.equal(fillsFromUpdate(prev, parseUpdate(wire(compactionEnd(null)))), prev);
});
