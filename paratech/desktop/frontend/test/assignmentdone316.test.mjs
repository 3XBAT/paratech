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
  focusSpot,
  focusTargetAfter,
  issueButton,
  listAfterDone,
  shouldMoveFocus,
  shouldRestoreFocus,
  listEmpty,
  pageDoneLabel,
  stateGroupLabel,
  stateOptions,
  toastTitle,
} from "../.test-build/assignmentDoneView.js";
import { doneMessages, parseDoneResult } from "../.test-build/bindings/homework.js";
import { DoneRunner, newestRunning } from "../.test-build/doneRunner.js";
import { AssignmentRowView, DoneButton, DoneIssueLine, DoneStamp } from "../.test-build/pages/AssignmentDoneParts.js";
import { AssignmentStateSwitch, filterReset } from "../.test-build/pages/AssignmentFilter.js";
import { isEditableTarget, isUndoKey, Toast, toastMs, undoKeyAllowed, windowAway } from "../.test-build/ui/Toast.js";

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
  assert.equal(t.text, "Выполнено", "no title known: the short text");
  // «Отменить» sends back exactly what the call returned: the same values, not a rebuilt list.
  assert.equal(t.undo.prior, marked.prior);
  assert.equal(t.undo.priorDone, marked.priorDone);
  assert.deepEqual(t.undo, { priorDone: false, prior: [{ id: "a1", version: 8 }] });

  // One assignment is named by the title of the row the student clicked.
  assert.equal(doneOutcome("mark", marked, { a1: "Реферат" }).toast.text, "Выполнено: «Реферат»");

  const back = reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true);
  const u = doneOutcome("unmark", back).toast;
  assert.equal(u.text, "Возвращено в работу");
  assert.equal(u.undo.priorDone, true);
  assert.equal(doneOutcome("unmark", back, { a1: "Реферат" }).toast.text, "Возвращено в работу: «Реферат»");

  const several = reply(
    [out("a1", "changed", true, 8), out("a2", "changed", true, 3), out("a3", "changed", true, 4), out("a4", "unchanged", true, 1)],
    [
      { id: "a1", version: 8 },
      { id: "a2", version: 3 },
      { id: "a3", version: 4 },
    ],
  );
  assert.equal(doneOutcome("mark", several).toast.text, "Отмечены выполненными: 3");
  assert.equal(doneOutcome("unmark", several).toast.text, "Возвращены в работу: 3");
  // several are counted, never named
  assert.equal(doneOutcome("mark", several, { a1: "Реферат" }).toast.text, "Отмечены выполненными: 3");
});

test("the title in the toast is one short line of plain text", () => {
  assert.equal(toastTitle("  Реферат\n по   физике "), "Реферат по физике");
  const long = "Подготовить доклад по истории математики за второй семестр обучения";
  const cut = toastTitle(long);
  assert.equal(Array.from(cut).length, 41, "40 characters and «…»");
  assert.ok(cut.endsWith("…"));
  assert.equal(toastTitle("а".repeat(40)), "а".repeat(40), "exactly 40 stays");
  // no cut inside a surrogate pair
  assert.equal(toastTitle("😀".repeat(41)), `${"😀".repeat(40)}…`);
  // a title with markup stays text (React escapes it; here it is only a string)
  const html = renderToStaticMarkup(
    h(Toast, { toast: { id: 1, text: `Выполнено: «${toastTitle("<img src=x>")}»`, actionLabel: "Отменить" }, onAction: noop, onClose: noop }),
  );
  assert.ok(html.includes("&lt;img src=x&gt;"));
  assert.ok(!html.includes("<img"));
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
  // The page does not tell the student to open what is open.
  assert.equal(
    cu.pageText,
    "Задание изменилось, поэтому отменить нельзя. Отметку можно поставить или снять кнопкой выше.",
  );
  assert.doesNotMatch(cu.pageText, /Откройте/);
  assert.equal(c.pageText, c.text, "the other refusals read the same everywhere");

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
  assert.equal(o.toast.text, "Выполнено");
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
  // The button of the page is an action; the status is the stamp «Выполнено 28 окт.».
  assert.equal(pageDoneLabel(false), "Отметить выполненным");
  assert.equal(pageDoneLabel(true), "Вернуть в работу");
});

test("empty lists: first run only for a student without any assignment", () => {
  assert.deepEqual(listEmpty("done", true), {
    kind: "note",
    text: "Пока ничего не отмечено.",
    hint: "Отметить задание можно кружком слева от названия.",
    action: { label: "Показать открытые", state: "open" },
  });
  assert.equal(listEmpty("done", false).text, emptyTexts.done, "an empty «Выполненные» is never the first run");
  // everything done: the open list is empty but the student has assignments
  const open = listEmpty("open", true);
  assert.equal(open.kind, "note");
  assert.equal(open.text, "Открытых заданий нет — всё выполнено.");
  assert.deepEqual(open.action, { label: "Показать выполненные", state: "done" });
  // not known whether any exist: the short line, still with the way to the done ones
  const unknown = listEmpty("open", null);
  assert.equal(unknown.kind, "note", "unknown is not the first run");
  assert.equal(unknown.text, "Открытых заданий нет");
  assert.deepEqual(unknown.action, { label: "Показать выполненные", state: "done" });
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
  assert.match(html, /<button type="button" class="done-check" aria-label="Отметить выполненным: Задачи 1–5" title="Отметить выполненным" data-done-id="a1">/);
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
  assert.match(open, /<button type="button" class="btn"[^>]*><svg[^>]*><use href="#i-ok"><\/use><\/svg><span class="btn-labels"><span>Отметить выполненным<\/span>/);
  assert.ok(!open.includes("primary"), "not the accent button");
  assert.equal(renderToStaticMarkup(h(DoneStamp, { assignment: item() })), "");

  const done = renderToStaticMarkup(h(DoneButton, { done: true, busy: false, onToggle: noop }));
  assert.ok(done.includes("Вернуть в работу"));
  assert.ok(!done.includes("#i-retry"), "no «try again» icon on the way back");
  assert.ok(!done.includes("#i-ok"), "the check icon is for marking only");
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
  // While the mark is saved the page does not edit or delete (a delete would go out with a stale version).
  assert.equal(head.split("changing || doneBusy").length - 1, 2, "«Изменить» and «Удалить» wait for the mark");
});

test("a refusal line: the text as an alert and one button out", () => {
  const issue = doneOutcome("mark", reply([out("a1", "conflict")])).issue;
  const html = renderToStaticMarkup(h(DoneIssueLine, { issue, where: "list", onRetry: noop, onOpen: noop }));
  assert.match(html, /role="alert"/);
  assert.ok(html.includes("Задание изменилось, попробуйте ещё раз"));
  assert.match(html, />Повторить</);

  const undo = doneOutcome("undo", reply([out("a1", "conflict")])).issue;
  const undoList = renderToStaticMarkup(h(DoneIssueLine, { issue: undo, where: "list", onRetry: noop, onOpen: noop }));
  assert.match(undoList, />Открыть задание</);
  assert.ok(undoList.includes("Откройте задание"));
  // On the assignment page the text does not send the student to what is open, and there is no such button.
  const undoPage = renderToStaticMarkup(h(DoneIssueLine, { issue: undo, where: "page", onRetry: noop, onOpen: noop }));
  assert.ok(undoPage.includes("Отметку можно поставить или снять кнопкой выше."));
  assert.ok(!undoPage.includes("Откройте"));
  assert.ok(!undoPage.includes("<button"));

  // A line left above the list after its row is gone can be closed.
  const closable = renderToStaticMarkup(
    h(DoneIssueLine, { issue: doneOutcome("mark", reply([out("a1", "deleted")])).issue, where: "list", onRetry: noop, onOpen: noop, onClose: noop }),
  );
  assert.match(closable, />Закрыть</);
  assert.ok(!renderToStaticMarkup(h(DoneIssueLine, { issue: undo, where: "list", onRetry: noop, onOpen: noop })).includes("Закрыть"));

  const gone = doneOutcome("mark", reply([out("a1", "deleted")])).issue;
  const page = renderToStaticMarkup(h(DoneIssueLine, { issue: gone, where: "page", onRetry: noop, onOpen: noop, onBack: noop }));
  assert.match(page, />Все задания</);
  assert.ok(!renderToStaticMarkup(h(DoneIssueLine, { issue: gone, where: "list", onRetry: noop, onOpen: noop })).includes("<button"));
});

test("the toast is a polite status with «Отменить» and a close button", () => {
  const html = renderToStaticMarkup(
    h(Toast, { toast: { id: 1, text: "Выполнено: «Реферат»", actionLabel: doneTexts.undo }, onAction: noop, onClose: noop }),
  );
  assert.match(html, /role="status" aria-live="polite"/);
  assert.ok(html.includes("Выполнено: «Реферат»"));
  assert.match(html, />Отменить</);
  assert.ok(html.includes('title="Отменить (Ctrl+Z)"'), "the key is told in the tooltip");
  assert.equal(doneTexts.undoTitle, "Отменить (Ctrl+Z)");
  assert.match(html, /aria-label="Закрыть"/);
  assert.equal(toastMs, 10_000);

  // The region stays for the announcement; without a toast it holds no button.
  const none = renderToStaticMarkup(h(Toast, { toast: null, onAction: noop, onClose: noop }));
  assert.match(none, /role="status" aria-live="polite"/);
  assert.ok(!none.includes("<button"));
});

test("Ctrl+Z undoes while the toast is shown, but never inside a text field", () => {
  const key = (over = {}) => ({ key: "z", code: "KeyZ", ctrlKey: true, metaKey: false, shiftKey: false, altKey: false, ...over });
  assert.equal(isUndoKey(key()), true);
  assert.equal(isUndoKey(key({ key: "я" })), true, "the Russian layout: the physical key");
  assert.equal(isUndoKey(key({ key: "Z", code: "KeyZ" })), true);
  assert.equal(isUndoKey(key({ ctrlKey: false })), false, "plain z is a letter");
  assert.equal(isUndoKey(key({ shiftKey: true })), false, "Ctrl+Shift+Z is redo");
  assert.equal(isUndoKey(key({ altKey: true })), false);
  assert.equal(isUndoKey(key({ repeat: true })), false, "a held key does not repeat the undo");
  assert.equal(isUndoKey(key({ key: "y", code: "KeyY" })), false);

  assert.equal(isEditableTarget({ tagName: "TEXTAREA" }), true);
  assert.equal(isEditableTarget({ tagName: "input" }), true);
  assert.equal(isEditableTarget({ tagName: "DIV", isContentEditable: true }), true);
  assert.equal(isEditableTarget({ tagName: "BUTTON" }), false);
  assert.equal(isEditableTarget(null), false);

  // The timer waits while the window is hidden or not the active one (Alt-Tab), and starts again on the return.
  const src = readFileSync(new URL("../src/ui/Toast.tsx", import.meta.url), "utf8");
  assert.ok(src.includes('"visibilitychange"') && src.includes('"blur"') && src.includes('"focus"'));
  assert.ok(src.includes("hover || focus || away"));
});

test("the toast sits in the content column on the inverse surface, and the check has a 32px hit area", () => {
  const css = readFileSync(new URL("../src/styles.css", import.meta.url), "utf8");
  const host = css.match(/\.toast-host \{([^}]*)\}/)[1];
  assert.match(host, /left: 64px/, "right of the rail");
  assert.match(host, /right: var\(--ag-w, 0px\)/, "left of the agent panel (the shell sets --ag-w, 0 when closed)");
  const toast = css.match(/\n\.toast \{([^}]*)\}/)[1];
  assert.match(toast, /background: var\(--prim\)/);
  assert.match(toast, /color: var\(--on-prim\)/);
  assert.match(css, /\.done-check::before \{[^}]*inset: -5px/, "22px ring + 2 x 5px = 32px");
  assert.match(css, /\.done-check \{[^}]*width: 22px; height: 22px/, "the visible ring stays 22px");
  const shell = readFileSync(new URL("../src/App.tsx", import.meta.url), "utf8");
  assert.ok(shell.includes('"--ag-w"'), "the variable the toast uses is the shell's own");
  const today = readFileSync(new URL("../src/pages/today.css", import.meta.url), "utf8");
  assert.match(today, /\.today-dlitem:hover \{ background: var\(--fill\)/, "the whole item lights up");
});

// A fake core: markDone and restoreDone wait for the test to answer, so the order of events is the test's.
function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
const tick = () => new Promise((r) => setTimeout(r, 0));

function harness(over = {}) {
  const calls = { mark: [], restore: [] };
  const waits = { mark: [], restore: [] };
  const api = {
    markDone: (ids, done) => {
      calls.mark.push([ids, done]);
      const d = deferred();
      waits.mark.push(d);
      return d.promise;
    },
    restoreDone: (priorDone, prior) => {
      calls.restore.push([priorDone, prior]);
      const d = deferred();
      waits.restore.push(d);
      return d.promise;
    },
  };
  const seen = { busy: [], issues: [], toasts: [], applied: [], refreshes: 0 };
  const state = { toast: null, issue: null, alive: true };
  const sink = {
    busy: (k) => seen.busy.push(k),
    issue: (i) => {
      state.issue = i;
      seen.issues.push(i);
    },
    toast: (t) => {
      state.toast = t;
      seen.toasts.push(t);
    },
    apply: (res) => seen.applied.push(res),
    refresh: () => {
      seen.refreshes++;
      return over.refresh?.(seen.refreshes);
    },
    alive: () => state.alive,
  };
  return { runner: new DoneRunner(api, sink), calls, waits, seen, state };
}

const marked = () => reply([out("a1", "changed", true, 8)], [{ id: "a1", version: 8 }], false);

test("the runner: a double click starts one call, and the outcome reaches the page once", async () => {
  const t = harness();
  const first = t.runner.mark("a1", true, "Реферат");
  const second = t.runner.mark("a1", true, "Реферат");
  assert.equal(t.calls.mark.length, 1, "the second click started nothing");
  assert.deepEqual(t.seen.busy, ["a1"]);

  t.waits.mark[0].resolve(marked());
  await Promise.all([first, second]);
  assert.equal(t.seen.applied.length, 1);
  assert.equal(t.seen.refreshes, 1);
  assert.equal(t.state.toast.text, "Выполнено: «Реферат»");
  assert.equal(t.state.issue, null);
  assert.equal(t.seen.busy.at(-1), "", "the busy mark is released");

  // After it is over a new click is a new call.
  void t.runner.mark("a1", false, "Реферат");
  assert.equal(t.calls.mark.length, 2);
});

test("the runner: «Отменить» sends back exactly the objects the call returned", async () => {
  const t = harness();
  const res = marked();
  const run = t.runner.mark("a1", true, "Реферат");
  t.waits.mark[0].resolve(res);
  await run;

  const undo = t.runner.undo();
  assert.equal(t.calls.restore.length, 1);
  assert.equal(t.calls.restore[0][0], res.priorDone);
  assert.equal(t.calls.restore[0][1], res.prior, "the same array, not a rebuilt list");
  assert.equal(t.calls.restore[0][1][0], res.prior[0], "and the same object in it");
  assert.equal(t.state.toast, null, "the toast closes while the undo runs");

  t.waits.restore[0].resolve(reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true));
  assert.equal(await undo, true);
  assert.equal(t.state.toast, null, "an undo gets no toast of its own");
  assert.equal(t.seen.applied.length, 2);
  assert.equal(await t.runner.undo(), false, "nothing left to undo");
});

test("the runner: a click on «Отменить» while the screen is re-read is not lost", async () => {
  const refresh = deferred();
  const t = harness({ refresh: (n) => (n === 1 ? refresh.promise : undefined) });
  const res = marked();
  const run = t.runner.mark("a1", true, "Реферат");
  t.waits.mark[0].resolve(res);
  await tick();
  assert.ok(t.state.toast !== null, "the toast is up while the re-read runs");
  assert.equal(t.seen.refreshes, 1);

  const undo = t.runner.undo();
  assert.equal(t.calls.restore.length, 1, "the undo started although the re-read is not over");
  assert.equal(t.calls.restore[0][1], res.prior);
  assert.equal(t.state.toast, null);

  t.waits.restore[0].resolve(reply([out("a1", "changed", false, 9)], [{ id: "a1", version: 9 }], true));
  assert.equal(await undo, true);
  refresh.resolve();
  await run;
});

test("the runner: when the undo cannot start the toast stays", async () => {
  const t = harness();
  const run = t.runner.mark("a1", true, "Реферат");
  t.waits.mark[0].resolve(marked());
  await run;
  const shown = t.state.toast;

  // The same assignment is being saved again (the student clicked its check): the undo has to wait.
  const again = t.runner.mark("a1", false, "Реферат");
  assert.equal(await t.runner.undo(), false);
  assert.equal(t.calls.restore.length, 0, "no call was made");
  assert.equal(t.state.toast, shown, "the toast is still there, with its «Отменить»");

  t.waits.mark[1].resolve(reply([out("a1", "unchanged", false, 9)]));
  await again;
});

test("the runner: a refusal shows its line, the repeat works, and a success clears the line", async () => {
  const t = harness();
  const run = t.runner.mark("a1", true, "Реферат");
  t.waits.mark[0].resolve(reply([out("a1", "conflict")]));
  await run;
  assert.equal(t.state.toast, null);
  assert.equal(t.state.issue.text, "Задание изменилось, попробуйте ещё раз");
  assert.deepEqual(t.state.issue.ids, ["a1"]);
  assert.equal(t.seen.busy.at(-1), "", "a refusal releases the row");
  assert.equal(t.seen.refreshes, 1, "the stored state is read");

  t.state.issue.retry();
  assert.equal(t.calls.mark.length, 2, "the same call again");
  assert.deepEqual(t.calls.mark[1], [["a1"], true]);
  assert.equal(t.state.issue, null, "the old line goes when the repeat starts");
  t.waits.mark[1].resolve(marked());
  await tick();
  assert.equal(t.state.issue, null);
  assert.equal(t.state.toast.text, "Выполнено: «Реферат»", "the title survives the repeat");

  t.runner.dismissIssue();
  assert.equal(t.state.issue, null);
});

test("the runner: the screen is read again only when the core answered per assignment", async () => {
  const t = harness();
  const run = t.runner.mark("a1", true);
  t.waits.mark[0].resolve(parseDoneResult({ problem: "unavailable" }));
  await run;
  assert.equal(t.seen.refreshes, 0);
  assert.equal(t.state.issue.way, "retry");
  assert.equal(t.state.toast, null);

  const ok = t.runner.mark("a1", true);
  t.waits.mark[1].resolve(marked());
  await ok;
  assert.equal(t.seen.refreshes, 1);
  assert.equal(t.state.toast.text, "Выполнено", "no title given: the short text");
});

test("the runner: a refusal of the undo keeps the page text, a failed re-read changes nothing, a closed page hears nothing", async () => {
  const t = harness({
    refresh: () => {
      throw new Error("offline");
    },
  });
  const run = t.runner.mark("a1", true, "Реферат");
  t.waits.mark[0].resolve(marked());
  await run; // the failed re-read did not throw out of the runner
  const undo = t.runner.undo();
  t.waits.restore[0].resolve(reply([out("a1", "conflict")]));
  await undo;
  assert.equal(t.state.issue.way, "open");
  assert.equal(t.state.issue.pageText, doneTexts.pageUndoConflict);

  const gone = harness();
  const p = gone.runner.mark("a1", true);
  gone.state.alive = false;
  gone.waits.mark[0].resolve(marked());
  await p;
  assert.equal(gone.seen.applied.length, 0);
  assert.equal(gone.state.toast, null);

  // A newer toast replaces the older one; the undo is of the newest.
  const two = harness();
  const a = two.runner.mark("a1", true, "А");
  two.waits.mark[0].resolve(marked());
  await a;
  const first = two.state.toast;
  const b = two.runner.mark("a2", true, "Б");
  two.waits.mark[1].resolve(reply([out("a2", "changed", true, 3)], [{ id: "a2", version: 3 }]));
  await b;
  assert.ok(two.state.toast.id > first.id);
  void two.runner.undo();
  assert.deepEqual(two.calls.restore[0][1], [{ id: "a2", version: 3 }]);
});

test("the focus goes to the next row's check, else the previous one, else the main region", () => {
  const order = ["a1", "a2", "a3", "a4"];
  assert.equal(focusTargetAfter(order, ["a2"]), "a3", "the next one");
  assert.equal(focusTargetAfter(order, ["a4"]), "a3", "the last row: the previous one");
  assert.equal(focusTargetAfter(order, ["a1"]), "a2");
  assert.equal(focusTargetAfter(["a1"], ["a1"]), "", "the list is empty now: the region");
  assert.equal(focusTargetAfter(order, ["a3", "a4"]), "a2", "several leave together");
  assert.equal(focusTargetAfter(order, ["a2", "a3"]), "a4", "the neighbour that stays");
  assert.equal(focusTargetAfter(order, ["zz"]), null, "no row of the screen leaves: nothing to move");
  assert.equal(focusTargetAfter(order, []), null);
});

test("the focus moves only from a leaving check or from nowhere; any other control keeps it", () => {
  const leaving = ["a2"];
  assert.equal(shouldMoveFocus({ kind: "check", id: "a2" }, leaving), true);
  assert.equal(shouldMoveFocus({ kind: "check", id: "a1" }, leaving), false, "another row's check");
  assert.equal(shouldMoveFocus({ kind: "other" }, leaving), false, "«Открыть», the filter, the previous toast's «Отменить»");
  assert.equal(shouldMoveFocus({ kind: "none" }, leaving), true, "the focused element is gone");
  assert.equal(shouldMoveFocus({ kind: "none" }, []), false, "nothing leaves");

  const body = { dataset: {} };
  assert.deepEqual(focusSpot(null, body), { kind: "none" });
  assert.deepEqual(focusSpot(body, body), { kind: "none" });
  assert.deepEqual(focusSpot({ dataset: { doneId: "a2" } }, body), { kind: "check", id: "a2" });
  assert.deepEqual(focusSpot({ dataset: {} }, body), { kind: "other" });
});

test("Ctrl+Z undoes only a toast that is visible, and not from behind a dialog", () => {
  const ok = { connected: true, rendered: true, insideHidden: false, inModal: false };
  assert.equal(undoKeyAllowed(ok), true);
  assert.equal(undoKeyAllowed({ ...ok, connected: false }), false, "not in the document");
  assert.equal(undoKeyAllowed({ ...ok, rendered: false }), false, "no box (display:none)");
  assert.equal(undoKeyAllowed({ ...ok, insideHidden: true }), false, "the page is hidden behind an overlay");
  assert.equal(undoKeyAllowed({ ...ok, inModal: true }), false, "a modal dialog is open");
});

test("a toast born in a blurred or hidden window does not start its timer", () => {
  assert.equal(windowAway("visible", true), false);
  assert.equal(windowAway("visible", false), true);
  assert.equal(windowAway("hidden", true), true);
});

test("after «Отменить» the focus returns only if it is still there or lost", () => {
  assert.equal(shouldRestoreFocus(true, false), true, "still on the pressed control");
  assert.equal(shouldRestoreFocus(false, true), true, "the button is gone: body");
  assert.equal(shouldRestoreFocus(false, false), false, "the student moved on");
});

test("the toast action names its key and the focus regions have a name", () => {
  const html = renderToStaticMarkup(h(Toast, { toast: { id: 1, text: "Выполнено", actionLabel: "Отменить" }, onAction: noop, onClose: noop }));
  assert.match(html, /aria-keyshortcuts="Control\+Z"/);
  const read = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
  const hw = read("pages/HomeworkPage.tsx");
  assert.ok(hw.includes('aria-labelledby="hw-list-title"') && hw.includes('id="hw-list-title"'));
  assert.ok(hw.includes('aria-labelledby="hw-detail-title"') && hw.includes('id="hw-detail-title"'));
  const today = read("pages/TodayPage.tsx");
  assert.ok(today.includes('aria-labelledby="today-title"') && today.includes('id="today-title"'));
});

test("the runner: a mark in flight stays busy when an undo of other ids ends first", async () => {
  assert.equal(newestRunning(new Set()), "");
  assert.equal(newestRunning(new Set(["a", "b"])), "b");

  const t = harness();
  const first = t.runner.mark("a2", true, "Б");
  t.waits.mark[0].resolve(reply([out("a2", "changed", true, 3)], [{ id: "a2", version: 3 }]));
  await first;

  const mark = t.runner.mark("a1", true, "А");
  assert.equal(t.seen.busy.at(-1), "a1");
  const undo = t.runner.undo();
  assert.equal(t.seen.busy.at(-1), "a2", "the newest call");

  t.waits.restore[0].resolve(reply([out("a2", "changed", false, 4)], [{ id: "a2", version: 4 }], true));
  await undo;
  assert.equal(t.seen.busy.at(-1), "a1", "the mark in flight keeps its «Сохранение…»");

  t.waits.mark[1].resolve(marked());
  await mark;
  assert.equal(t.seen.busy.at(-1), "", "released at the end");
});

test("the pages are wired as agreed: one runner per page, focus kept, the re-read keeps the shown version", () => {
  const read = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");
  const hook = read("pages/useDoneRunner.ts");
  assert.ok(hook.includes("new DoneRunner("), "the hook only maps the tested runner to state");
  assert.ok(!hook.includes("api.restoreDone"), "the sequencing is not in the hook");

  const page = read("pages/HomeworkPage.tsx");
  assert.equal(page.split("useDoneRunner(api,").length - 1, 2, "list and assignment page");
  assert.equal(page.split("<Toast ").length - 1, 2);
  assert.equal(page.split("doneToastProps(doneRun, focus)").length - 1, 2, "Отменить and the close restore the focus");
  assert.ok(page.includes("requests.current.isNewest(token)"), "a stale answer of the list is dropped");
  assert.ok(page.includes("api.listAssignments(state)"));
  assert.match(page, /api\.deleteAssignment\(a\.id, a\.version\)/, "the delete flow stays");
  // The page keeps the version being viewed when the mark is saved.
  assert.ok(page.includes("refresh: () => reload(true, true).catch"));
  assert.ok(page.includes("onOpen={() => reload(true, true).catch"));
  // The list: the re-read also reads the chips, the email filter starts again with a new mark.
  assert.equal(page.split("fetchDeliveries(api, as)").length - 1, 2, "the load and the re-read both read the chips");
  assert.match(page, /function changeState[\s\S]*setFilter\("all"\)[\s\S]*doneRun\.dismissIssue\(\)/);
  assert.ok(page.includes('state !== "done" && ('), "no email filter among the done ones");
  assert.ok(page.includes("filterReset"), "the empty group has a way back");
  assert.ok(page.includes("onClose={doneRun.dismissIssue}"), "a stray refusal can be closed");
  assert.equal(filterReset, "Сбросить фильтр");

  const today = read("pages/TodayPage.tsx");
  assert.ok(today.includes("doneToastProps(doneRun, focus)"));
  assert.ok(today.indexOf("<Toast ") > today.indexOf("</section>"), "outside .today, a size container");
  assert.ok(today.includes("onMark: (id, title) =>"));
});
