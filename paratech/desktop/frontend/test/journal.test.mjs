// «Журнал» (TASK-124): filters, «Отменить» and its refusals, «Отменить нельзя», and text-as-text.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { initialFilter, lineModel, statusOptions, problemText, refusalText, toFilter, undoOutcome } from "../.test-build/journalView.js";
import { FilterBar, JournalList } from "../.test-build/pages/JournalParts.js";

const noop = () => undefined;

function line(over = {}) {
  return {
    kind: "action", at: "2026-10-22T10:00:00Z", updatedAt: "2026-10-22T10:00:00Z", subjectId: "s1", actionId: "a1", threadId: "t1",
    turn: 1, tool: "create_subject", jobId: "", jobType: "", status: "DONE", undoable: true, cannotUndo: false, noUndo: "", ...over,
  };
}

const names = new Map([["s1", "Матанализ"]]);

function list(lines, busyAction = "", subjects = names) {
  return renderToStaticMarkup(h(JournalList, { lines, subjects, busyAction, onUndo: noop }));
}

test("the filter form maps to the binding's filter: period 30 days by default", () => {
  assert.deepEqual(toFilter(initialFilter), { kind: "", subjectId: "", status: "", days: 30, undoable: false });
  assert.deepEqual(
    toFilter({ kind: "processing", subjectId: "s1", status: "FAILED", days: 7, undoable: true }),
    { kind: "processing", subjectId: "s1", status: "FAILED", days: 7, undoable: true },
  );
});

test("the filter bar offers kind, subject, status, 7/30/90 days and only-undoable", () => {
  const out = renderToStaticMarkup(
    h(FilterBar, { filter: { ...initialFilter, days: 7, undoable: true, subjectId: "s1" }, subjects: [{ id: "s1", name: "Матанализ" }], onChange: noop }),
  );
  for (const t of ["Всё", "Действия", "Обработка", "Предмет", "Состояние", "Период", "Только обратимые"]) {
    assert.ok(out.includes(t), t);
  }
  assert.match(out, /<option value="s1" selected="">Матанализ<\/option>/);
  for (const d of ["7 дней", "30 дней", "90 дней"]) {
    assert.ok(out.includes(d), d);
  }
  assert.match(out, /<option value="7" selected="">7 дней/);
  assert.match(out, /type="checkbox" checked=""/);
});

test("an undoable action line offers «Отменить»; the busy one is labelled and all buttons wait", () => {
  const out = list([line()]);
  assert.match(out, /Создан предмет/);
  assert.match(out, /Матанализ/);
  assert.match(out, /Выполнено/);
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Отменить</);
  const busy = list([line(), line({ actionId: "a2" })], "a1");
  assert.match(busy, />Отмена…</);
  assert.equal((busy.match(/<button[^>]*disabled/g) ?? []).length, 2);
});

test("a sent email says «Отменить нельзя» with the reason and no «Отменить»", () => {
  const sent = line({ kind: "processing", actionId: "", tool: "", jobId: "j1", jobType: "send_email", status: "COMPLETED", undoable: false, cannotUndo: true, noUndo: "sent", subjectId: "" });
  const out = list([sent]);
  assert.match(out, /Отправка письма/);
  assert.match(out, /Отменить нельзя/);
  assert.match(out, /Письмо отправлено/);
  assert.doesNotMatch(out, />Отменить</);
  const m = lineModel(sent, 0, names);
  assert.equal(m.undoActionId, "");
  assert.equal(m.cannotUndo, true);
});

test("an irreversible proposal says «Отменить нельзя»; reasons explain lines without «Отменить»", () => {
  const proposal = line({ tool: "propose_send", status: "PROPOSED", undoable: false, cannotUndo: true, noUndo: "irreversible" });
  const out = list([proposal]);
  assert.match(out, /Предложена отправка письма/);
  assert.match(out, /Отменить нельзя/);
  assert.match(out, /Решение остаётся за вами/);
  const reasons = { running: "Выполняется", failed: "Не выполнено", undone: "Уже отменено", changed: "Изменено после — отменить нельзя", result_ready: "Результат готов", cannot_stop: "Уже не остановить", unknown: "Итог неизвестен" };
  for (const [noUndo, text] of Object.entries(reasons)) {
    const o = list([line({ undoable: false, noUndo, status: "UNDONE" })]);
    assert.ok(o.includes(text), noUndo);
    assert.doesNotMatch(o, />Отменить</);
  }
});

test("the refusals of Undo are worded in Russian, a problem too, and an unknown one is generic", () => {
  const refuse = (refusal) => undoOutcome({ problem: "", actionId: "a1", status: "UNDO_REFUSED", refusal });
  assert.deepEqual(refuse("changed"), { ok: false, text: "Изменено после — отменить нельзя." });
  assert.match(refuse("result_ready").text, /Результат уже готов/);
  assert.match(refuse("cannot_stop").text, /не остановить/);
  assert.match(refuse("name_taken").text, /уже есть/);
  assert.match(refuse("not_found").text, /не найдена/);
  assert.match(refuse("not_undoable").text, /отменить нельзя/);
  assert.equal(refuse("something new").text, "Не удалось отменить действие.");
  assert.equal(refusalText("changed"), "Изменено после — отменить нельзя.");
  assert.deepEqual(undoOutcome({ problem: "", actionId: "a1", status: "UNDONE", refusal: "" }), { ok: true, text: "Действие отменено." });
  assert.equal(undoOutcome({ problem: "unavailable", actionId: "a1", status: "", refusal: "" }).ok, false);
  assert.equal(problemText("closed"), "Приложение закрывается.");
  assert.equal(problemText(""), "");
});

test("a journal line shows fixed phrases and typed names as text, never markup, and never raw ids", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const out = list(
    [line({ subjectId: "s9", tool: evil, actionId: "a-secret-id" }), line({ kind: "processing", jobType: evil, jobId: "j-secret", subjectId: "", undoable: false })],
    "",
    new Map([["s9", evil]]),
  );
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(!out.includes("a-secret-id"));
  assert.ok(!out.includes("j-secret"));
  assert.match(out, /Действие/);
  assert.match(out, /Обработка/);
  const bar = renderToStaticMarkup(h(FilterBar, { filter: initialFilter, subjects: [{ id: "s9", name: evil }], onChange: noop }));
  assert.ok(!bar.includes("<img"));
});

test("an empty period says so", () => {
  assert.match(list([]), /За выбранный период ничего нет/);
});

test("the journal code builds no markup from text", () => {
  for (const f of ["../src/pages/JournalParts.tsx", "../src/pages/JournalPage.tsx", "../src/journalView.ts"]) {
    assert.doesNotMatch(readFileSync(new URL(f, import.meta.url), "utf8"), /dangerouslySetInnerHTML|innerHTML|insertAdjacentHTML/, f);
  }
});

test("busy is worded for Undo refusals and call problems", () => {
  const text = "Действие уже меняется, попробуйте ещё раз.";
  assert.equal(undoOutcome({ problem: "", actionId: "a1", status: "", refusal: "busy" }).text, text);
  assert.equal(undoOutcome({ problem: "busy", actionId: "a1", status: "", refusal: "" }).text, text);
  assert.equal(problemText("busy"), text);
  assert.equal(refusalText("busy"), text);
});

test("the RUNNING status filter says it means processing, not pending actions", () => {
  const running = statusOptions.find((o) => o.value === "RUNNING");
  assert.equal(running.label, "Обработка идёт");
  assert.equal(statusOptions.filter((o) => o.label === "Выполняется").length, 0);
});
