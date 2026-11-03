// «Предметы», the tree: every subject expands and folds by its own toggle, independent of the subject on screen.
// Before, only the selected subject was ever expanded, so the student could not fold all of the subjects of a semester.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { readFileSync } from "node:fs";
import { expandSubject, initialOpen, isSubjectOpen, seedOpen, toggleLabel, toggleSubject } from "../.test-build/subjectTreeState.js";
import { SubjectTree } from "../.test-build/pages/SubjectsParts.js";

const noop = () => undefined;
const folder = (id, name) => ({ id, name, kind: "lectures", order: 0 });
const subject = (id, name) => ({
  id,
  name,
  semester: "2026-autumn",
  semesterLabel: "Осень 2026",
  folders: [folder(`${id}-f`, "Лекции")],
});
const ids = ["math", "ai", "net"];
const openOf = (state) => ids.filter((id) => isSubjectOpen(state, id));

test("the selected subject is expanded on arrival, once; a selection by the route (not a name click) expands and folds nothing", () => {
  let s = initialOpen("math");
  assert.deepEqual(openOf(s), ["math"]);
  // The selection moves: the new one is not expanded, the old one is not folded.
  s = seedOpen(s, "ai");
  assert.deepEqual(openOf(s), ["math"]);
  assert.deepEqual(openOf(initialOpen("")), []);
});

test("the subject on screen is not known at the first render: it is expanded when it first appears, and only then", () => {
  let s = initialOpen("");
  s = seedOpen(s, "");
  assert.deepEqual(openOf(s), []);
  s = seedOpen(s, "ai");
  assert.deepEqual(openOf(s), ["ai"]);
  s = toggleSubject(s, "ai");
  s = seedOpen(s, "ai");
  assert.deepEqual(openOf(s), [], "a folded subject stays folded while it is selected");
});

test("collapse all, expand two, collapse the selected one: each toggle moves only its own subject", () => {
  let s = initialOpen("math"); // math is selected and expanded
  // Collapse all: the selected one folds like the others.
  s = toggleSubject(s, "math");
  assert.deepEqual(openOf(s), []);
  // The selection moves while everything is folded: nothing opens by itself.
  s = seedOpen(s, "ai");
  assert.deepEqual(openOf(s), []);
  // Expand two.
  s = toggleSubject(s, "math");
  s = toggleSubject(s, "net");
  assert.deepEqual(openOf(s), ["math", "net"]);
  // Fold the selected one ("ai" is selected but folded; "math" is not selected): select math, then fold it.
  s = seedOpen(s, "math");
  s = toggleSubject(s, "math");
  assert.deepEqual(openOf(s), ["net"]);
  s = toggleSubject(s, "net");
  assert.deepEqual(openOf(s), []);
});

test("a click on a subject's name expands it, never folds another, and the own toggle still folds it, also when it is open", () => {
  let s = toggleSubject(initialOpen("math"), "math"); // everything folded, math still selected
  assert.deepEqual(openOf(s), []);
  s = expandSubject(s, "ai"); // name click on a collapsed subject
  assert.deepEqual(openOf(s), ["ai"]);
  s = expandSubject(s, "net");
  assert.deepEqual(openOf(s), ["ai", "net"], "the others stay as they are");
  s = expandSubject(s, "net");
  assert.deepEqual(openOf(s), ["ai", "net"], "a click on an expanded one does not fold it");
  s = toggleSubject(s, "net"); // net is the open one: its own toggle folds it
  assert.deepEqual(openOf(s), ["ai"]);
  // The name click wins over a late seed: the clicked subject is the one expanded.
  assert.deepEqual(openOf(expandSubject(initialOpen(""), "ai")), ["ai"]);
  assert.deepEqual(openOf(seedOpen(expandSubject(initialOpen(""), "ai"), "net")), ["ai"], "no second seed after a click");
});

test("the name button of the tree expands and opens the subject (source wiring)", () => {
  const src = readFileSync(new URL("../src/pages/SubjectsParts.tsx", import.meta.url), "utf8");
  assert.match(src, /setOpened\(\(o\) => expandSubject\(seedOpen\(o, props\.currentId\), s\.id\)\);\s*props\.onOpen\(s\.id\);/);
});

test("a toggle does not mutate the state it was given", () => {
  const a = initialOpen("math");
  const b = toggleSubject(a, "ai");
  assert.deepEqual([...a.open], ["math"]);
  assert.deepEqual([...b.open].sort(), ["ai", "math"]);
});

test("the toggle has a name with the subject in it", () => {
  assert.equal(toggleLabel("Сети", true), "Свернуть: Сети");
  assert.equal(toggleLabel("Сети", false), "Развернуть: Сети");
});

const tree = (currentId) =>
  renderToStaticMarkup(
    h(SubjectTree, {
      subjects: [subject("math", "Математика"), subject("ai", "Методы ИИ"), subject("net", "Сети")],
      currentId,
      counts: new Map(),
      busy: false,
      onOpen: noop,
      onAddSubject: noop,
      onAddFolder: noop,
    }),
  );

test("the tree: one toggle per subject; only the expanded ones list folders; the others show no counts", () => {
  const out = tree("ai");
  assert.equal((out.match(/class="tog"/g) ?? []).length, 3, "a toggle per subject");
  assert.match(out, /aria-expanded="true" aria-label="Свернуть: Методы ИИ"/);
  assert.match(out, /aria-expanded="false" aria-label="Развернуть: Математика"/);
  assert.equal((out.match(/class="ti fold"/g) ?? []).length, 1, "only the selected subject starts expanded");
  // Without a selection nothing is expanded.
  const none = tree("");
  assert.equal((none.match(/class="ti fold"/g) ?? []).length, 0);
  assert.equal((none.match(/aria-expanded="false" aria-label="Развернуть/g) ?? []).length, 3);
});
