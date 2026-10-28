// «Сегодня» → «Горит на 7 дней» with «Выполнено» (TASK-316): the check in every row beside the open button, the row
// that leaves at once, the post line of a lesson whose homework is all done.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { burningAfterDone, doneOutcome } from "../.test-build/assignmentDoneView.js";
import { parseDoneResult } from "../.test-build/bindings/homework.js";
import { homeworkText, lessonLine, postLine } from "../.test-build/todayView.js";
import { BurningSection } from "../.test-build/pages/TodayParts.js";

const noop = () => undefined;
const at = (d) => new Date(2026, 9, d, 12, 0).toISOString();

const deadline = (id, title = "Реферат", over = {}) => ({
  assignmentId: id,
  title,
  subject: "",
  due: at(30),
  daysLeft: 2,
  urgent: false,
  ...over,
});

const reply = (outcomes, prior = [], priorDone = false) => parseDoneResult({ problem: "", outcomes, prior, priorDone });
const out = (id, code, done = false, version = 0) => ({ id, code, done, version });

test("every burning row has a check beside the open button, never inside it", () => {
  const html = renderToStaticMarkup(
    h(BurningSection, {
      burning: [deadline("a1", "Реферат"), deadline("a2", "ЛР")],
      onOpenAssignment: noop,
      done: { busyId: "", issue: null, onMark: noop },
    }),
  );
  assert.equal(html.match(/class="done-check"/g).length, 2);
  assert.ok(html.includes('aria-label="Отметить выполненным: Реферат"'));
  assert.ok(html.includes('aria-label="Отметить выполненным: ЛР"'));
  assert.equal(html.match(/<button[^>]*today-dl/g).length, 2);
  // No interactive element inside another: the open button closes before the check of the next row starts.
  for (const m of html.matchAll(/<button[^>]*today-dl[^>]*>([\s\S]*?)<\/button>/g)) {
    assert.ok(!m[1].includes("<button") && !m[1].includes("done-check"));
  }
});

test("without the done handler the rows are as before (no check)", () => {
  const html = renderToStaticMarkup(h(BurningSection, { burning: [deadline("a1")], onOpenAssignment: noop }));
  assert.ok(!html.includes("done-check"));
  assert.ok(!html.includes("today-dlitem"));
});

test("a busy row says so in small text; a refusal shows under its row with the way out", () => {
  const busy = renderToStaticMarkup(
    h(BurningSection, {
      burning: [deadline("a1")],
      onOpenAssignment: noop,
      done: { busyId: "a1", issue: null, onMark: noop },
    }),
  );
  assert.match(busy, /role="status">Сохранение…</);

  const issue = { ...doneOutcome("mark", reply([out("a1", "failed")])).issue, ids: ["a1"], retry: noop };
  const html = renderToStaticMarkup(
    h(BurningSection, {
      burning: [deadline("a1")],
      onOpenAssignment: noop,
      done: { busyId: "", issue, onMark: noop },
    }),
  );
  assert.match(html, /role="alert"/);
  assert.match(html, />Повторить</);
  assert.ok(html.indexOf("today-dlissue") > html.indexOf("today-dl\""), "under its row");
});

test("a refusal about a row that is gone stays above the rows", () => {
  const gone = { ...doneOutcome("mark", reply([out("zz", "not_found")])).issue, ids: ["zz"], retry: noop };
  const html = renderToStaticMarkup(
    h(BurningSection, {
      burning: [deadline("a1")],
      onOpenAssignment: noop,
      done: { busyId: "", issue: gone, onMark: noop },
    }),
  );
  assert.ok(html.includes("Задание уже удалено"));
  assert.ok(html.indexOf("Задание уже удалено") < html.indexOf("today-dlitem"));
});

test("the row leaves at once when it is marked or gone; a refusal and others stay", () => {
  const rows = [deadline("a1"), deadline("a2"), deadline("a3"), deadline("a4")];
  const res = reply([out("a1", "changed", true, 8), out("a2", "conflict"), out("a3", "not_found")], [{ id: "a1", version: 8 }]);
  assert.deepEqual(burningAfterDone(rows, res).map((b) => b.assignmentId), ["a2", "a4"]);
  // already done elsewhere: it leaves too
  assert.deepEqual(burningAfterDone(rows, reply([out("a4", "unchanged", true, 2)])).map((b) => b.assignmentId), ["a1", "a2", "a3"]);
  // a problem without outcomes changes nothing
  assert.equal(burningAfterDone(rows, parseDoneResult({ problem: "unavailable" })).length, 4);
  // the undo (all back to open) does not take rows away; the screen is read again to bring them back
  const undo = reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true);
  assert.equal(burningAfterDone(rows, undo).length, 4);
  assert.equal(doneOutcome("undo", undo).refresh, true);
});

test("the toast of a mark in «Сегодня» comes from the same model and carries the undo", () => {
  const res = reply([out("a1", "changed", true, 8)], [{ id: "a1", version: 8 }]);
  const o = doneOutcome("mark", res);
  assert.equal(o.toast.text, "Отмечено выполненным");
  assert.equal(o.toast.undo.prior, res.prior);
  assert.equal(o.refresh, true, "the screen is read again");
});

test("a lesson whose homework is all done says so instead of the deadline", () => {
  assert.equal(homeworkText(1, at(30), 1), "1 задание · выполнено");
  assert.equal(homeworkText(3, at(30), 3), "3 задания · выполнены");
  assert.equal(homeworkText(5, "", 5), "5 заданий · выполнено");
  assert.equal(homeworkText(3, "", 2), "3 задания", "partly done keeps the old text");
  assert.equal(homeworkText(0, "", 0), "");
  assert.equal(homeworkText(2, at(30)).startsWith("2 задания · до"), true);

  const p = {
    recordingId: "r1",
    transcriptId: "t1",
    lesson: { lessonId: "l1", title: "Матанализ", subject: "", subjectId: "", subjectName: "", kind: "lecture", start: at(28) },
    headline: "Конспект готов",
    summaryReady: true,
    homework: 2,
    homeworkDone: 2,
    earliestDue: at(30),
    calendar: [],
    shownAt: at(28),
  };
  assert.equal(postLine(p).homework, "2 задания · выполнены");
  assert.equal(lessonLine({ id: "l1", title: "Матанализ", subject: "", kind: "lecture", teacher: "", start: at(28), end: at(28), post: p }, []).post.homework, "2 задания · выполнены");
  assert.equal(postLine({ ...p, homeworkDone: 1 }).homework.startsWith("2 задания · до"), true);
});

test("the Today page wires the runner, the local removal and the toast", () => {
  const page = readFileSync(new URL("../src/pages/TodayPage.tsx", import.meta.url), "utf8");
  assert.ok(page.includes("useDoneRunner(homework,"));
  assert.ok(page.includes("burningAfterDone(s.burning, res)"));
  assert.match(page, /refresh: load/);
  assert.ok(page.includes("<Toast "));
});
