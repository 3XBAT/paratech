// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import {
  allCollapsed,
  defaultLimit,
  initialListState,
  listTexts,
  listWindow,
  setCollapsed,
  stateOf,
  visibleRows,
  withState,
} from "../.test-build/listView.js";
import { CollapsibleList } from "../.test-build/CollapsibleList.js";
import { PreviewView } from "../.test-build/pages/SchedulePage.js";
import { previewGroups, summaryChips } from "../.test-build/scheduleView.js";
import { isFinishedJob, isFinishedRecording, splitFinished } from "../.test-build/activityView.js";

const { renderToStaticMarkup } = server;
const sizes = [0, 1, 8, 9, 500, 5000];
const range = (n) => Array.from({ length: n }, (_, i) => i);
const count = (markup, s) => markup.split(s).length - 1;
const folded = { collapsed: true, showAll: false };
const all = { collapsed: false, showAll: true };

function list(n, state, props = {}) {
  return renderToStaticMarkup(
    h(CollapsibleList, {
      items: range(n),
      itemKey: (i) => i,
      renderItem: (i) => h("li", null, `row ${i}`),
      state,
      onStateChange: () => undefined,
      title: "Группа",
      ...props,
    }),
  );
}

test("the default limit is 8 rows", () => {
  assert.equal(defaultLimit, 8);
  assert.deepEqual(initialListState, { collapsed: false, showAll: false });
});

test("a list shows the first rows, all of them, or none", () => {
  for (const n of sizes) {
    const shown = Math.min(n, defaultLimit);
    assert.deepEqual(listWindow(n, initialListState), { shown, hidden: n - shown, more: n > 8, less: false }, `${n}`);
    assert.deepEqual(listWindow(n, all), { shown: n, hidden: 0, more: false, less: n > 8 }, `${n} all`);
    assert.deepEqual(listWindow(n, folded), { shown: 0, hidden: n, more: false, less: false }, `${n} folded`);
    assert.deepEqual(listWindow(n, { collapsed: true, showAll: true }).shown, 0, `${n} folded after show all`);
    assert.equal(visibleRows(range(n), initialListState).length, shown);
    assert.equal(visibleRows(range(n), all).length, n);
    assert.equal(visibleRows(range(n), folded).length, 0);
  }
  assert.equal(listTexts.more(listWindow(9, initialListState).hidden), "Показать ещё 1");
  assert.equal(listTexts.more(listWindow(5000, initialListState).hidden), "Показать ещё 4992");
  assert.equal(listWindow(5, initialListState, 3).hidden, 2, "a custom limit");
  assert.equal(listWindow(5, initialListState, 0).shown, 1, "a limit below 1 is 1");
});

test("a list renders at most the limit of rows and the matching buttons", () => {
  for (const n of sizes) {
    const shown = Math.min(n, 8);
    const markup = list(n, initialListState);
    assert.equal(count(markup, "<li>"), shown, `${n} rows`);
    assert.equal(markup.includes(`Показать ещё ${n - 8}`), n > 8, `${n} more`);
    assert.ok(!markup.includes(">Свернуть<"), `${n} no less`);
    assert.match(markup, new RegExp(`aria-expanded="true".*\\(${n}\\)`));

    const opened = list(n, all);
    assert.equal(count(opened, "<li>"), n);
    assert.equal(opened.includes(">Свернуть<"), n > 8);

    const closed = list(n, folded);
    assert.equal(count(closed, "<li>"), 0);
    assert.match(closed, /aria-expanded="false"/);
    assert.ok(!closed.includes("Показать") && !closed.includes("Свернуть"));
  }
  assert.match(list(9, initialListState, { labels: { more: () => listTexts.allErrors } }), />Показать все ошибки</);
  assert.ok(!list(3, initialListState, { title: undefined }).includes("<button"), "no title, no header");
});

test("a collapsed list of 5000 rows renders quickly", () => {
  const items = range(5000);
  const started = performance.now();
  for (let i = 0; i < 20; i++) {
    visibleRows(items, initialListState);
    list(5000, initialListState);
  }
  list(5000, all);
  assert.ok(performance.now() - started < 2000, "rendering took too long");
});

test("list states are kept by key and folded together", () => {
  let s = {};
  assert.deepEqual(stateOf(s, "added"), initialListState);
  assert.deepEqual(stateOf(s, "toString"), initialListState, "only own keys");
  s = withState(s, "added", all);
  assert.deepEqual(stateOf(s, "added"), all);
  assert.equal(allCollapsed(s, ["added", "changed"]), false);
  assert.equal(allCollapsed(s, []), false);
  s = setCollapsed(s, ["added", "changed"], true);
  assert.equal(allCollapsed(s, ["added", "changed"]), true);
  assert.deepEqual(stateOf(s, "added"), { collapsed: true, showAll: true }, "show all is kept");
  s = setCollapsed(s, ["added", "changed"], false);
  assert.deepEqual(stateOf(s, "added"), all);
});

const lesson = (i, title = `Урок ${i}`) => ({ title, start: "14.10.2026 09:00", end: "14.10.2026 10:30", zone: "Europe/Moscow" });

function preview(over = {}) {
  return {
    cancelled: false, selectionId: "1", fileName: "big.ics", source: "ics", errors: [],
    added: [], changed: [], removed: [], unchangedCount: 3, conflicts: [], conflictsTruncated: false,
    window: null, canCommit: true, ...over,
  };
}

function page(p, lists = {}) {
  return renderToStaticMarkup(
    h(PreviewView, { preview: p, busy: false, lists, onLists: () => undefined, onApply: () => {}, onCancel: () => {} }),
  );
}

test("the preview shows counters, groups and the pinned Apply", () => {
  const big = preview({
    added: range(5000).map((i) => ({ line: i + 2, ...lesson(i) })),
    removed: range(9).map((i) => ({ line: 0, ...lesson(i) })),
  });
  assert.deepEqual(summaryChips(big).map((c) => c.text), [
    "Добавится: 5000", "Изменится: 0", "Удалится: 9", "Без изменений: 3", "Ошибки: 0",
  ]);
  assert.deepEqual(previewGroups(big), ["added", "removed"]);
  const markup = page(big);
  assert.equal(count(markup, "<li"), 5 + 8 + 8, "chips and the first rows of each group");
  assert.match(markup, /Добавятся <span class="clist-count">\(5000\)/);
  assert.match(markup, />Показать ещё 4992</);
  assert.match(markup, />Показать ещё 1</);
  assert.match(markup, />Свернуть всё</);
  assert.match(markup, /class="actions sticky-actions"><button type="button">Применить</);

  const closed = page(big, { added: folded, removed: folded });
  assert.equal(count(closed, "<li"), 5, "only the chips");
  assert.match(closed, />Развернуть всё</);
  // The state is keyed by group: another preview object shows the same folding.
  assert.equal(count(page({ ...big, selectionId: "2" }, { added: folded }), "<li"), 5 + 8);
});

test("row errors fold the same way, with «Показать все ошибки» and no Apply", () => {
  const p = preview({ canCommit: false, errors: range(5000).map((i) => ({ line: i + 2, field: "date", reason: "bad" })) });
  assert.deepEqual(previewGroups(p), ["errors"]);
  const markup = page(p);
  assert.equal(count(markup, "<li"), 5 + 8);
  assert.match(markup, /<li class="error">Ошибки: 5000<\/li>/);
  assert.match(markup, />Показать все ошибки</);
  assert.ok(!markup.includes("Применить"));
  assert.equal(count(page(p, { errors: all }), "<li"), 5 + 5000);
});

test("file text is rendered as text, not HTML", () => {
  const evil = "<b>x</b>";
  const markup = page(
    preview({
      fileName: evil,
      added: [{ line: 2, ...lesson(1, evil) }],
      conflicts: [{ line: 2, lesson: lesson(1, evil), withLine: 0, with: lesson(2) }],
    }),
  );
  assert.ok(!markup.includes(evil), "raw HTML from the file");
  assert.match(markup, /<bdi>&lt;b&gt;x&lt;\/b&gt;<\/bdi>/);
  const errors = page(preview({ fileName: evil, canCommit: false, errors: [{ line: 3, field: evil, reason: evil }] }));
  assert.ok(!errors.includes(evil));
  assert.equal(count(errors, "<bdi>&lt;b&gt;x&lt;/b&gt;</bdi>"), 3, "file name, field and reason");

  for (const file of ["../src/CollapsibleList.tsx", "../src/listView.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML"]) {
      assert.ok(!src.includes(banned), `${file} uses ${banned}`);
    }
  }
});

test("finished recordings and jobs go to the folded list; rows with an action stay in view", () => {
  assert.equal(isFinishedRecording({ status: "COMPLETED", stoppable: false }), true);
  assert.equal(isFinishedRecording({ status: "FAILED", stoppable: false }), true);
  assert.equal(isFinishedRecording({ status: "RECORDING", stoppable: true }), false);
  assert.equal(isFinishedRecording({ status: "RECOVERY_REQUIRED", stoppable: false }), false);
  assert.equal(isFinishedRecording({ status: "COMPLETED", stoppable: true }), false, "the core offers Stop");
  for (const s of ["COMPLETED", "FAILED", "CANCELLED"]) {
    assert.equal(isFinishedJob({ status: s, runNowOffered: false }), true, s);
  }
  for (const s of ["QUEUED", "RUNNING", "RETRY_WAIT", "WAITING_FOR_POWER", "SOMETHING_NEW"]) {
    assert.equal(isFinishedJob({ status: s, runNowOffered: false }), false, s);
  }
  assert.equal(isFinishedJob({ status: "FAILED", runNowOffered: true }), false, "the core offers Run now");

  const items = [1, 2, 3, 4, 5];
  assert.deepEqual(splitFinished(items, (i) => i % 2 === 0), { active: [1, 3, 5], finished: [2, 4] });
  assert.deepEqual(splitFinished([], () => true), { active: [], finished: [] });
});
