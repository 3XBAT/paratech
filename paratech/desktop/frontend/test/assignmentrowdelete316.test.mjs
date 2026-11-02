// The list row after the merge of «Выполнено» (TASK-316) and the delete warning (TASK-338): «Удалить» is an item of the
// row's «⋯» menu and starts the delete check; the warning, the unsettled-email refusal and a refusal of the mark sit
// together under the row, each with its own button.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { doneOutcome, doneTexts, focusRowMenu, removeItem } from "../.test-build/assignmentDoneView.js";
import { parseDoneResult } from "../.test-build/bindings/homework.js";
import { changeProblemText, changeTexts } from "../.test-build/homeworkView.js";
import { AssignmentRowView, DoneIssueLine } from "../.test-build/pages/AssignmentDoneParts.js";
import { ChangeRefusal, DeleteWarning } from "../.test-build/pages/HomeworkDialogs.js";

const noop = () => undefined;

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

const row = (props = {}) =>
  renderToStaticMarkup(
    h(AssignmentRowView, {
      assignment: item(),
      busyText: "",
      onOpen: noop,
      onToggleDone: noop,
      onRemove: noop,
      ...props,
    }),
  );

const doneIssue = () => doneOutcome("mark", parseDoneResult({ problem: "", outcomes: [{ id: "a1", code: "conflict", done: false, version: 0 }], prior: [], priorDone: false })).issue;

test("the menu item «Удалить» is off while a call runs, while the check runs and while the question is open", () => {
  assert.deepEqual(removeItem(false, ""), { disabled: false, hint: "" });
  assert.deepEqual(removeItem(true, ""), { disabled: true, hint: doneTexts.busyHint });
  assert.deepEqual(removeItem(false, "checking"), { disabled: true, hint: "Проверяется…" });
  assert.deepEqual(removeItem(false, "confirm"), { disabled: true, hint: doneTexts.removeAsked });
  // A running call says why first.
  assert.equal(removeItem(true, "checking").hint, doneTexts.busyHint);
});

test("the row keeps its menu while the delete check runs: no red button, no «Удаление…», the button is found by the row", () => {
  const html = row({ removeLock: "checking" });
  assert.ok(html.includes('aria-label="Ещё действия: Задачи 1–5"'));
  assert.ok(html.includes('data-menu-id="a1"'));
  assert.ok(!html.includes("danger"));
  assert.ok(!html.includes("Удаление…"), "the check is not the delete");
  assert.ok(!html.includes("aria-busy"), "the check does not mark the row busy");
});

test("the delete warning under the row: the stop is named, «Отмена» has the focus, one danger button", () => {
  const html = row({ removeLock: "confirm", below: h(DeleteWarning, { onConfirm: noop, onCancel: noop }) });
  assert.ok(html.includes(changeTexts.stopWarning));
  assert.match(html, />Отмена</);
  assert.match(html, /danger[^>]*>Удалить</);
  assert.equal(html.split("danger").length - 1, 1, "only the warning's «Удалить» is red");
  assert.ok(html.includes("Открыть"), "the row keeps «Открыть»");
});

test("the in_use refusal under the row comes with «Открыть отправку»", () => {
  const html = row({ below: h(ChangeRefusal, { text: changeProblemText("in_use"), onOpenSend: noop }) });
  assert.match(html, /role="alert"/);
  assert.ok(html.includes(changeProblemText("in_use")));
  assert.match(html, />Открыть отправку</);
});

test("a refusal of the mark and a refusal of the delete show together, each with its button", () => {
  const issue = doneIssue();
  const html = row({
    below: h(
      "div",
      null,
      h(ChangeRefusal, { text: changeProblemText("in_use"), onOpenSend: noop }),
      h(DeleteWarning, { onConfirm: noop, onCancel: noop }),
      h(DoneIssueLine, { issue, where: "list", onRetry: noop, onOpen: noop }),
    ),
  });
  assert.ok(html.includes(issue.text), "the mark's refusal");
  assert.match(html, />Повторить</);
  assert.ok(html.includes(changeProblemText("in_use")), "the delete's refusal");
  assert.match(html, />Открыть отправку</);
  assert.ok(html.includes(changeTexts.stopWarning), "the question");
  assert.match(html, />Отмена</);
});

test("focusRowMenu focuses the «⋯» button of that row only", () => {
  const focused = [];
  const button = (id) => ({ dataset: { menuId: id }, focus: () => focused.push(id) });
  const root = { querySelectorAll: () => [button("a1"), button("a2")] };
  assert.equal(focusRowMenu(root, "a2"), true);
  assert.deepEqual(focused, ["a2"]);
  assert.equal(focusRowMenu(root, "zz"), false);
  assert.equal(focusRowMenu(root, ""), false);
  assert.equal(focusRowMenu(null, "a1"), false);
});

test("the page wires the row: the delete check from the menu, the warning and the refusal under the row", () => {
  const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");
  assert.match(page, /onRemove=\{\(\) => void askRemove\(a\)\}/);
  assert.match(page, /removeLock=\{checkingId === a\.id \? "checking" : confirmId === a\.id \? "confirm" : ""\}/);
  const below = page.slice(page.indexOf("below={\n          <>"), page.indexOf("/>\n    );\n  };"));
  assert.match(below, /\{problemLine\}/);
  assert.match(below, /<DoneIssueLine issue=\{doneIssue\} where="list"/);
  assert.match(page, /<DeleteWarning\s+onConfirm=\{\(\) => void remove\(a\)\}/);
  assert.match(page, /<ChangeRefusal text=\{problem\.text\} onOpenSend=\{problem\.openSend \? \(\) => props\.onOpen\(a\.id, "mail"\) : undefined\} \/>/);
});
