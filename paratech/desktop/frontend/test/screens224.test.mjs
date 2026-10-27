// TASK-224: «Журнал» by groups, left menu of «Настройки», counters of the filters of «Задания», the status line of «Сегодня».
import { test } from "node:test";
import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { groupLines } from "../.test-build/journalView.js";
import { JournalList } from "../.test-build/pages/JournalParts.js";
import { SettingsHead, SettingsTabs } from "../.test-build/pages/SettingsTabs.js";
import { assignmentCounts, filterAssignments, isSentAssignment } from "../.test-build/homeworkView.js";
import { FilterCounters } from "../.test-build/pages/AssignmentFilter.js";
import { nearestDeadlineText } from "../.test-build/todayView.js";

const noop = () => undefined;
const now = new Date(2026, 9, 27, 15, 0, 0);

function line(over = {}) {
  return {
    kind: "action", at: new Date(2026, 9, 27, 10, 0).toISOString(), updatedAt: "", subjectId: "", actionId: "a1", threadId: "t1",
    turn: 1, tool: "create_subject", jobId: "", jobType: "", status: "DONE", undoable: false, cannotUndo: false, noUndo: "", ...over,
  };
}

test("lines group into Сейчас, Сегодня and dates, keeping the order", () => {
  const lines = [
    line({ actionId: "run", kind: "processing", jobId: "j1", jobType: "stt", status: "RUNNING" }),
    line({ actionId: "t1" }),
    line({ actionId: "t2", at: new Date(2026, 9, 27, 8, 0).toISOString() }),
    line({ actionId: "y1", at: new Date(2026, 9, 26, 18, 0).toISOString() }),
    line({ actionId: "y2", at: new Date(2026, 9, 26, 9, 0).toISOString() }),
    line({ actionId: "old", at: new Date(2025, 11, 31, 9, 0).toISOString() }),
    line({ actionId: "bad", at: "" }),
  ];
  const groups = groupLines(lines, now);
  assert.deepEqual(groups.map((g) => g.title), ["Сейчас", "Сегодня", "26 октября", "31 декабря 2025", "Без даты"]);
  assert.deepEqual(groups.map((g) => g.items.map((i) => i.line.actionId)), [["run"], ["t1", "t2"], ["y1", "y2"], ["old"], ["bad"]]);
  assert.deepEqual(groups[1].items.map((i) => i.index), [1, 2]);
});

test("an unfinished action is in Сейчас even when it started on an earlier day", () => {
  const g = groupLines([line({ status: "PENDING", at: new Date(2026, 9, 1).toISOString() })], now);
  assert.deepEqual(g.map((x) => x.title), ["Сейчас"]);
});

test("the journal renders a heading with a count per group and nothing for an empty list", () => {
  const out = renderToStaticMarkup(h(JournalList, { lines: [line(), line({ actionId: "a2", at: new Date(2026, 9, 20).toISOString() })], subjects: new Map(), busyAction: "", onUndo: noop, now }));
  assert.match(out, /<h2 class="jr-group" id="[^"]+">Сегодня<span class="cnt num">1<\/span><\/h2>/);
  assert.match(out, /<h2 class="jr-group" id="[^"]+">20 октября<span class="cnt num">1<\/span><\/h2>/);
  const empty = renderToStaticMarkup(h(JournalList, { lines: [], subjects: new Map(), busyAction: "", onUndo: noop, now }));
  assert.ok(!empty.includes("jr-group"));
});

test("«Настройки» has a shared head and a menu of all sections with the current one marked", () => {
  const head = renderToStaticMarkup(h(SettingsHead));
  assert.ok(head.includes("Изменения сохраняются сразу") && head.includes("<h1>Настройки</h1>"));
  const nav = renderToStaticMarkup(h(SettingsTabs, { current: "models", onOpen: noop }));
  assert.match(nav, /class="set-nav"/);
  for (const t of ["Основные", "Модели речи", "Подключения", "Диагностика", "Профиль", "Оформление"]) {
    assert.ok(nav.includes(t), t);
  }
  assert.equal(nav.match(/aria-current="page"/g).length, 1);
  assert.match(nav, /aria-current="page"[^>]*>(<svg[^>]*>.*?<\/svg>)Модели речи/);
});

const items = [{ id: "a" }, { id: "b" }, { id: "c" }];
const deliveries = { a: [{ status: "SENT" }], b: [{ status: "FAILED" }, { status: "SENT" }], c: [] };

test("a sent assignment is one whose newest email is SENT", () => {
  assert.equal(isSentAssignment(deliveries.a), true);
  assert.equal(isSentAssignment(deliveries.b), false);
  assert.equal(isSentAssignment(deliveries.c), false);
  assert.equal(isSentAssignment(undefined), false);
});

test("counters of the filters: unknown split before the emails are loaded", () => {
  assert.deepEqual(assignmentCounts(items, {}, false), { all: 3, active: null, sent: null });
  assert.deepEqual(assignmentCounts(items, deliveries, true), { all: 3, active: 2, sent: 1 });
});

test("the filter shows its part; before the emails load every filter shows all", () => {
  assert.deepEqual(filterAssignments(items, "sent", deliveries, true).map((a) => a.id), ["a"]);
  assert.deepEqual(filterAssignments(items, "active", deliveries, true).map((a) => a.id), ["b", "c"]);
  assert.equal(filterAssignments(items, "sent", {}, false).length, 3);
});

test("the filter buttons carry the counters", () => {
  const out = renderToStaticMarkup(h(FilterCounters, { counts: { all: 3, active: 2, sent: 1 }, value: "active", onChange: noop }));
  assert.match(out, /Все<span class="cnt">3<\/span>/);
  assert.match(out, /aria-pressed="true"[^>]*>Активные<span class="cnt">2<\/span>/);
  assert.match(out, /Отправленные<span class="cnt">1<\/span>/);
  const loading = renderToStaticMarkup(h(FilterCounters, { counts: { all: 3, active: null, sent: null }, value: "all", onChange: noop }));
  assert.ok(!loading.includes("Активные<span"));
});

test("every filter but «Все» is disabled while the split is unknown", () => {
  const loading = renderToStaticMarkup(h(FilterCounters, { counts: { all: 3, active: null, sent: null }, value: "all", onChange: noop }));
  assert.match(loading, /<button type="button" aria-pressed="false" disabled="">Активные<\/button>/);
  assert.match(loading, /<button type="button" aria-pressed="true">Все/);
  const ready = renderToStaticMarkup(h(FilterCounters, { counts: { all: 3, active: 2, sent: 1 }, value: "all", onChange: noop }));
  assert.ok(!ready.includes("disabled"));
});

test("dated groups show the time only, Сейчас and Без даты the full date", () => {
  const lines = [line({ actionId: "run", kind: "processing", jobId: "j1", jobType: "stt", status: "RUNNING" }), line(), line({ actionId: "bad", at: "" })];
  const out = renderToStaticMarkup(h(JournalList, { lines, subjects: new Map(), busyAction: "", onUndo: noop, now }));
  // Only the line in «Сейчас» keeps the date; the one in «Сегодня» has the time, «Без даты» has a dash.
  assert.equal(out.match(/27\.10\.2026/g).length, 1);
  assert.ok(out.includes(">10:00<") || out.includes("10:00 ·") || out.includes("10:00"));
});

test("a group section is named by its heading once", () => {
  const out = renderToStaticMarkup(h(JournalList, { lines: [line()], subjects: new Map(), busyAction: "", onUndo: noop, now }));
  assert.match(out, /<section class="jr-sec" aria-labelledby="jr-h-today">/);
  assert.ok(!out.includes('aria-label="Сегодня"'));
});

test("the sections of «Настройки» have no h1 of their own (the shared head is the only one)", () => {
  // The pages need the Wails bindings to render, so their source is checked; SettingsHead is rendered.
  assert.equal(renderToStaticMarkup(h(SettingsHead)).match(/<h1/g).length, 1);
  for (const f of ["ModelsPage", "ConnectionsPage", "DiagnosticsPage", "SettingsPage", "AppleCalendarHelpPage"]) {
    const src = readFileSync(new URL(`../src/pages/${f}.tsx`, import.meta.url), "utf8");
    assert.ok(!src.includes("<h1"), f);
  }
});

test("the status line names the nearest deadline of the week", () => {
  const d = (y, m, day) => ({ due: new Date(y, m, day, 12).toISOString() });
  assert.equal(nearestDeadlineText([]), "");
  assert.equal(nearestDeadlineText([{ due: "" }]), "");
  assert.equal(nearestDeadlineText([d(2026, 9, 30), d(2026, 9, 28)]), "Ближайший срок — ср, 28.10");
});
