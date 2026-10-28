// «Выполнено» in the assignments UI (TASK-316): the wording, the toast, the refusals and their ways out, what the list
// does with its rows, and the SSR of the row, the page pieces, the filter and the toast. Runs on the compiled modules.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  assignmentAfterDone,
  checkLabel,
  checkTitle,
  defaultState,
  doneOutcome,
  doneStamp,
  doneTexts,
  emptyTexts,
  issueButton,
  listAfterDone,
  listEmpty,
  pageDoneLabel,
  stateGroupLabel,
  stateOptions,
} from "../.test-build/assignmentDoneView.js";
import { doneMessages, parseDoneResult } from "../.test-build/bindings/homework.js";
import { AssignmentRowView, DoneButton, DoneIssueLine, DoneStamp } from "../.test-build/pages/AssignmentDoneParts.js";
import { AssignmentStateSwitch } from "../.test-build/pages/AssignmentFilter.js";
import { Toast, toastMs } from "../.test-build/ui/Toast.js";

const noop = () => undefined;
const DONE_AT = "2026-10-28T12:00:00Z";

const item = (over = {}) => ({
  id: "a1",
  lessonId: "l1",
  title: "Задачи 1–5",
  description: "",
  due: "",
  userEdited: false,
  version: 5,
  updatedAt: "2026-10-27T10:00:00Z",
  subject: "Матанализ",
  subjectSource: "schedule",
  subjectFromLesson: true,
  kind: "seminar",
  teacher: "",
  teacherSource: "",
  lessonDate: "",
  correctedSubject: "",
  correctedTeacher: "",
  done: false,
  doneAt: "",
  ...over,
});

const out = (id, code, done = false, version = 0) => ({ id, code, done, version });
const reply = (outcomes, prior = [], priorDone = false) =>
  parseDoneResult({ problem: "", outcomes, prior, priorDone });

test("the state switch: three options, «Открытые» first and the default", () => {
  assert.deepEqual(
    stateOptions.map((o) => [o.value, o.label]),
    [
      ["open", "Открытые"],
      ["done", "Выполненные"],
      ["all", "Все"],
    ],
  );
  assert.equal(defaultState, "open");
  assert.match(stateGroupLabel, /^Какие задания показывать/);

  const html = renderToStaticMarkup(h(AssignmentStateSwitch, { value: "done", onChange: noop }));
  assert.match(html, /class="seg hw-filter"/);
  assert.ok(html.includes(`aria-label="${stateGroupLabel}"`));
  assert.match(html, /aria-pressed="false">Открытые</);
  assert.match(html, /aria-pressed="true">Выполненные</);
  assert.match(html, /aria-pressed="false">Все</);
});

test("the toast: only when something changed and the reply can be put back", () => {
  const marked = reply([out("a1", "changed", true, 8)], [{ id: "a1", version: 8 }], false);
  const t = doneOutcome("mark", marked).toast;
  assert.equal(t.text, "Отмечено выполненным");
  // «Отменить» sends back exactly what the call returned: the same values, not a rebuilt list.
  assert.equal(t.undo.prior, marked.prior);
  assert.equal(t.undo.priorDone, marked.priorDone);
  assert.deepEqual(t.undo, { priorDone: false, prior: [{ id: "a1", version: 8 }] });

  const back = reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true);
  const u = doneOutcome("unmark", back).toast;
  assert.equal(u.text, "Возвращено в работу");
  assert.equal(u.undo.priorDone, true);

  const several = reply(
    [out("a1", "changed", true, 8), out("a2", "changed", true, 3), out("a3", "changed", true, 4), out("a4", "unchanged", true, 1)],
    [
      { id: "a1", version: 8 },
      { id: "a2", version: 3 },
      { id: "a3", version: 4 },
    ],
  );
  assert.equal(doneOutcome("mark", several).toast.text, "Отмечено выполненным: 3");
  assert.equal(doneOutcome("unmark", several).toast.text, "Возвращено в работу: 3");
});

test("no change or no undo means no toast; an undo gets none either", () => {
  // unchanged: nothing to put back
  assert.equal(doneOutcome("mark", reply([out("a1", "unchanged", true, 8)])).toast, null);
  // changed, but the reply carries no prior (a malformed one is dropped by the parser): never a toast without «Отменить»
  const noPrior = parseDoneResult({
    problem: "",
    outcomes: [out("a1", "changed", true, 8)],
    prior: [{ id: "zzz", version: 1 }],
    priorDone: false,
  });
  assert.deepEqual(noPrior.prior, []);
  assert.equal(doneOutcome("mark", noPrior).toast, null);
  // refusals and problems
  assert.equal(doneOutcome("mark", reply([out("a1", "conflict")])).toast, null);
  assert.equal(doneOutcome("mark", parseDoneResult({ problem: "unavailable" })).toast, null);
  // the result of «Отменить» itself is not offered to be undone again
  const undone = reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true);
  assert.equal(doneOutcome("undo", undone).toast, null);
});

test("each refusal says why and names its way out", () => {
  const issue = (action, outcomes) => doneOutcome(action, reply(outcomes)).issue;

  for (const code of ["not_found", "deleted"]) {
    const i = issue("mark", [out("a1", code)]);
    assert.equal(i.text, "Задание уже удалено");
    assert.equal(i.gone, true);
    assert.equal(i.way, "none");
    assert.equal(doneOutcome("mark", reply([out("a1", code)])).refresh, true, "the list is read again");
  }

  const c = issue("mark", [out("a1", "conflict")]);
  assert.equal(c.text, "Задание изменилось, попробуйте ещё раз");
  assert.equal(c.way, "retry");
  assert.deepEqual(issueButton(c, "list"), { kind: "retry", label: "Повторить" });
  assert.equal(issue("unmark", [out("a1", "conflict")]).way, "retry");

  // A retry of a conflicted undo conflicts again: the way out is to open the assignment.
  const cu = issue("undo", [out("a2", "conflict"), out("a1", "conflict")]);
  assert.equal(cu.text, doneMessages.conflict);
  assert.equal(cu.way, "open");
  assert.equal(cu.openId, "a2", "the first one opens");
  assert.deepEqual(issueButton(cu, "list"), { kind: "open", label: "Открыть задание" });
  assert.equal(issueButton(cu, "page"), null, "the page already shows the assignment");

  const f = issue("mark", [out("a1", "failed")]);
  assert.equal(f.text, doneMessages.failed);
  assert.equal(f.way, "retry");

  for (const problem of ["invalid", "unavailable"]) {
    const p = doneOutcome("mark", parseDoneResult({ problem }));
    assert.equal(p.issue.text, doneMessages[problem]);
    assert.equal(p.issue.way, "retry");
    assert.equal(p.refresh, false);
    assert.equal(p.toast, null);
  }

  // The deleted assignment on its own page: back to the list. «Повторить» elsewhere.
  assert.deepEqual(issueButton(issue("mark", [out("a1", "not_found")]), "page"), { kind: "back", label: "Все задания" });
  assert.equal(issueButton(issue("mark", [out("a1", "not_found")]), "list"), null);

  // The stale code name never shows.
  for (const i of [c, cu, f]) {
    assert.doesNotMatch(i.text, /busy|conflict|not_found|failed/);
  }
});

test("unchanged is no error: no line, no toast, the screen takes the stored state", () => {
  const res = reply([out("a1", "unchanged", true, 8)]);
  const o = doneOutcome("mark", res);
  assert.equal(o.issue, null);
  assert.equal(o.toast, null);
  assert.equal(o.refresh, true);
});

test("a mixed result: the changed ones get the toast, each distinct refusal one text", () => {
  const res = reply(
    [out("a1", "changed", true, 8), out("a2", "not_found"), out("a3", "deleted"), out("a4", "conflict"), out("a5", "failed")],
    [{ id: "a1", version: 8 }],
  );
  const o = doneOutcome("mark", res);
  assert.equal(o.toast.text, "Отмечено выполненным");
  // not_found and deleted share one text; the repeat is offered because a plain mark can be sent again
  assert.equal(o.issue.text, ["Задание уже удалено", doneTexts.conflict, doneMessages.failed].join(" "));
  assert.equal(o.issue.way, "retry");
  assert.equal(o.issue.gone, true);
  assert.equal(o.refresh, true);
});

test("the list after a call: «Открытые» lose a marked row, «Выполненные» a returned one, «Все» keep both", () => {
  const rows = [item({ id: "a1" }), item({ id: "a2" })];
  const marked = reply([out("a1", "changed", true, 8)], [{ id: "a1", version: 8 }]);

  const open = listAfterDone(rows, "open", marked, DONE_AT);
  assert.deepEqual(open.map((a) => a.id), ["a2"]);

  const all = listAfterDone(rows, "all", marked, DONE_AT);
  assert.deepEqual(all.map((a) => [a.id, a.done]), [["a1", true], ["a2", false]]);
  assert.equal(all[0].doneAt, DONE_AT);
  assert.equal(all[0].version, 8, "the stored version, so a later delete sends the right one");

  const doneRows = [item({ id: "a1", done: true, doneAt: DONE_AT, version: 8 }), item({ id: "a2", done: true, doneAt: DONE_AT })];
  const back = reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true);
  assert.deepEqual(listAfterDone(doneRows, "done", back, DONE_AT).map((a) => a.id), ["a2"]);
  const stays = listAfterDone(doneRows, "all", back, DONE_AT);
  assert.deepEqual(stays.map((a) => [a.id, a.done, a.doneAt]), [["a1", false, ""], ["a2", true, DONE_AT]]);
});

test("the list after a call: gone leaves, a refusal keeps, unchanged takes the stored state", () => {
  const rows = [item({ id: "a1" }), item({ id: "a2" }), item({ id: "a3" })];
  const res = reply([out("a1", "not_found"), out("a2", "conflict"), out("a3", "unchanged", true, 4)]);
  assert.deepEqual(listAfterDone(rows, "all", res, DONE_AT).map((a) => [a.id, a.done, a.version]), [
    ["a2", false, 5],
    ["a3", true, 4],
  ]);
  assert.deepEqual(listAfterDone(rows, "open", res, DONE_AT).map((a) => a.id), ["a2"]);
  // a problem changes nothing
  assert.deepEqual(listAfterDone(rows, "open", parseDoneResult({ problem: "unavailable" }), DONE_AT), rows);
  // an already done one keeps its date
  const was = item({ done: true, doneAt: "2026-10-20T12:00:00Z" });
  assert.equal(assignmentAfterDone(was, reply([out("a1", "unchanged", true, 6)]), DONE_AT).doneAt, "2026-10-20T12:00:00Z");
  // the input is not changed
  assert.equal(rows[0].done, false);
});

test("the date line: «Выполнено 28 окт.», nothing for an open one", () => {
  assert.equal(doneStamp(item({ done: true, doneAt: DONE_AT })), "Выполнено 28 окт.");
  assert.equal(doneStamp(item({ done: true, doneAt: "" })), "Выполнено");
  assert.equal(doneStamp(item()), "");
});

test("the names are the action, not the state", () => {
  assert.equal(checkLabel("Задача", false), "Отметить выполненным: Задача");
  assert.equal(checkLabel("Задача", true), "Вернуть в работу: Задача");
  assert.equal(checkTitle(false), "Отметить выполненным");
  assert.equal(checkTitle(true), "Вернуть в работу");
  assert.equal(pageDoneLabel(false), "Выполнено");
  assert.equal(pageDoneLabel(true), "Вернуть в работу");
});

test("empty lists: first run only for a student without any assignment", () => {
  assert.deepEqual(listEmpty("done", true), {
    kind: "note",
    text: "Пока ничего не отмечено",
    hint: "Отметить можно галочкой в строке задания.",
    action: null,
  });
  assert.equal(listEmpty("done", false).text, emptyTexts.done, "an empty «Выполненные» is never the first run");
  // everything done: the open list is empty but the student has assignments
  const open = listEmpty("open", true);
  assert.equal(open.kind, "note");
  assert.equal(open.text, "Открытых заданий нет");
  assert.deepEqual(open.action, { label: "Показать выполненные", state: "done" });
  assert.equal(listEmpty("open", null).kind, "note", "unknown is not the first run");
  assert.equal(listEmpty("open", false).kind, "first-run");
  assert.equal(listEmpty("all", false).kind, "first-run");
});

const row = (over = {}, props = {}) =>
  renderToStaticMarkup(
    h(AssignmentRowView, {
      assignment: item(over),
      busyText: "",
      onOpen: noop,
      onToggleDone: noop,
      onRemove: noop,
      ...props,
    }),
  );

test("an open row: a ring check, «Открыть» and «⋯» with the delete inside, no red button", () => {
  const html = row();
  assert.match(html, /<button type="button" class="done-check" aria-label="Отметить выполненным: Задачи 1–5" title="Отметить выполненным">/);
  assert.ok(html.includes("row-lead"), "the check is the lead of the row");
  assert.match(html, />Открыть</);
  assert.ok(html.includes('aria-label="Ещё действия: Задачи 1–5"'));
  assert.ok(html.includes("⋯"));
  assert.ok(!html.includes("danger"), "no red button in the row");
  assert.ok(!html.includes("Удалить"), "the delete is in the closed menu");
  assert.ok(!html.includes("hw-done-title"));
  assert.ok(!html.includes("Выполнено "));
  assert.ok(!html.includes("aria-busy"));
});

test("a done row: filled check with the way back, muted struck title, the date in the meta", () => {
  const html = row({ done: true, doneAt: DONE_AT });
  assert.match(html, /class="done-check on" aria-label="Вернуть в работу: Задачи 1–5" title="Вернуть в работу"/);
  assert.match(html, /<span class="hw-done-title">Задачи 1–5<\/span>/);
  assert.ok(html.includes("Выполнено 28 окт."), "not colour only");
  assert.match(html, />Открыть</);
  assert.ok(!html.includes("danger"));
});

test("a busy row says so in small text and marks the check busy; the title is text", () => {
  const html = row({ title: "<img src=x>" }, { busyText: doneTexts.saving });
  assert.match(html, /<span class="small muted hw-inline" role="status">Сохранение…<\/span>/);
  assert.match(html, /aria-busy="true"/);
  assert.ok(html.includes("&lt;img src=x&gt;"));
  assert.ok(!html.includes("<img"));
  assert.match(row({}, { busyText: "Удаление…" }), /Удаление…/);
});

test("the assignment page pieces in both states; edit and delete stay in the header", () => {
  const open = renderToStaticMarkup(h(DoneButton, { done: false, busy: false, onToggle: noop }));
  // The label shares a grid cell with «Сохранение…» (Button's busyLabel): the visible one is the first span.
  assert.match(open, /<button type="button" class="btn"[^>]*><svg[^>]*><use href="#i-ok"><\/use><\/svg><span class="btn-labels"><span>Выполнено<\/span>/);
  assert.ok(!open.includes("primary"), "not the accent button");
  assert.equal(renderToStaticMarkup(h(DoneStamp, { assignment: item() })), "");

  const done = renderToStaticMarkup(h(DoneButton, { done: true, busy: false, onToggle: noop }));
  assert.ok(done.includes("Вернуть в работу"));
  const stamp = renderToStaticMarkup(h(DoneStamp, { assignment: item({ done: true, doneAt: DONE_AT }) }));
  assert.match(stamp, /<p class="small muted hw-donedate">Выполнено 28 окт\.<\/p>/);

  const busy = renderToStaticMarkup(h(DoneButton, { done: false, busy: true, onToggle: noop }));
  assert.match(busy, /aria-busy="true"/);
  assert.ok(busy.includes("Сохранение…"));

  // The header of the page keeps «Изменить» and «Удалить» and puts «Выполнено» before them.
  const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
  const head = page.slice(page.indexOf("<DoneButton"), page.indexOf("<DoneStamp"));
  assert.ok(head.indexOf("changeTexts.edit") > 0, "«Изменить» follows");
  assert.ok(head.indexOf("changeTexts.remove") > head.indexOf("changeTexts.edit"), "«Удалить» follows");
  assert.match(head, /api\.deleteAssignment\(id, a\.version\)/);
  assert.match(page, /<DoneStamp assignment=\{a\} \/>/);
});

test("a refusal line: the text as an alert and one button out", () => {
  const issue = doneOutcome("mark", reply([out("a1", "conflict")])).issue;
  const html = renderToStaticMarkup(h(DoneIssueLine, { issue, where: "list", onRetry: noop, onOpen: noop }));
  assert.match(html, /role="alert"/);
  assert.ok(html.includes("Задание изменилось, попробуйте ещё раз"));
  assert.match(html, />Повторить</);

  const undo = doneOutcome("undo", reply([out("a1", "conflict")])).issue;
  assert.match(renderToStaticMarkup(h(DoneIssueLine, { issue: undo, where: "list", onRetry: noop, onOpen: noop })), />Открыть задание</);

  const gone = doneOutcome("mark", reply([out("a1", "deleted")])).issue;
  const page = renderToStaticMarkup(h(DoneIssueLine, { issue: gone, where: "page", onRetry: noop, onOpen: noop, onBack: noop }));
  assert.match(page, />Все задания</);
  assert.ok(!renderToStaticMarkup(h(DoneIssueLine, { issue: gone, where: "list", onRetry: noop, onOpen: noop })).includes("<button"));
});

test("the toast is a polite status with «Отменить» and a close button", () => {
  const html = renderToStaticMarkup(
    h(Toast, { toast: { id: 1, text: "Отмечено выполненным", actionLabel: doneTexts.undo }, onAction: noop, onClose: noop }),
  );
  assert.match(html, /role="status" aria-live="polite"/);
  assert.ok(html.includes("Отмечено выполненным"));
  assert.match(html, />Отменить</);
  assert.match(html, /aria-label="Закрыть"/);
  assert.equal(toastMs, 10_000);

  // The region stays for the announcement; without a toast it holds no button.
  const none = renderToStaticMarkup(h(Toast, { toast: null, onAction: noop, onClose: noop }));
  assert.match(none, /role="status" aria-live="polite"/);
  assert.ok(!none.includes("<button"));
});

test("the pages are wired as agreed: one runner per page, the undo sends back the returned values", () => {
  const read = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
  const runner = read("pages/useDoneRunner.ts");
  assert.ok(runner.includes("api.restoreDone(t.undo.priorDone, t.undo.prior)"));
  assert.ok(runner.includes("setToast(null)"), "the toast closes while the undo runs");

  const page = read("pages/HomeworkPage.tsx");
  assert.equal(page.split("useDoneRunner(api,").length - 1, 2, "list and assignment page");
  assert.equal(page.split("<Toast ").length - 1, 2);
  assert.ok(page.includes("requests.current.isNewest(token)"), "a stale answer of the list is dropped");
  assert.ok(page.includes("api.listAssignments(state)"));
  assert.match(page, /api\.deleteAssignment\(a\.id, a\.version\)/, "the delete flow stays");
  assert.ok(read("pages/TodayPage.tsx").includes("<Toast "));
});
