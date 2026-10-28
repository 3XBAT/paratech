// «Удалить» of an assignment whose work is running (TASK-338): the inline warning, «Работа остановлена» in the removed
// line, the unsettled-email refusal with «Открыть отправку». The page has no DOM test harness: its logic sits in the
// helpers and components tested here, and the wiring is checked in the page source (like test/homeworkedit.test.mjs).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { changeOutcome, changeProblemText, changeTexts, deleteNeedsWarning } from "../.test-build/homeworkView.js";
import { ChangeRefusal, DeleteWarning, RemovedLine } from "../.test-build/pages/HomeworkDialogs.js";

const { renderToStaticMarkup } = server;

const item = () => ({
  id: "a1",
  lessonId: "",
  title: "Задача",
  description: "",
  due: "",
  userEdited: true,
  version: 3,
  updatedAt: "2026-10-27T10:00:00Z",
  subject: "Физика",
  subjectSource: "",
  subjectFromLesson: false,
  kind: "",
  teacher: "",
  teacherSource: "",
  lessonDate: "",
  correctedSubject: "",
  correctedTeacher: "",
});

const page = readFileSync(new URL("../src/pages/HomeworkPage.tsx", import.meta.url), "utf8");

test("the texts are the agreed Russian ones", () => {
  assert.equal(
    changeTexts.stopWarning,
    "Работа над заданием будет остановлена. Вернуть задание можно в течение 3 дней, работа сама не продолжится.",
  );
  assert.equal(changeTexts.stopped, "Работа остановлена");
  assert.equal(changeTexts.cancel, "Отмена");
  assert.equal(changeTexts.openSend, "Открыть отправку");
  assert.equal(changeProblemText("in_use"), "Письмо преподавателю ещё отправляется — удалить можно после");
});

test("a running work asks first; anything else, a failed check included, does not", async () => {
  assert.equal(await deleteNeedsWarning(async () => ({ working: true })), true);
  assert.equal(await deleteNeedsWarning(async () => ({ working: false })), false);
  assert.equal(await deleteNeedsWarning(async () => ({})), false);
  assert.equal(await deleteNeedsWarning(async () => ({ working: "true" })), false);
  assert.equal(await deleteNeedsWarning(async () => Promise.reject(new Error("db locked"))), false);
  assert.equal(
    await deleteNeedsWarning(() => {
      throw new Error("sync");
    }),
    false,
  );
});

test("a done delete carries stopped; a refusal never does; in_use opens the send", () => {
  const stopped = changeOutcome({ problem: "", assignment: item(), stopped: true });
  assert.equal(stopped.stopped, true);
  assert.equal(stopped.openSend, false);
  assert.equal(changeOutcome({ problem: "", assignment: item(), stopped: false }).stopped, false);

  const inUse = changeOutcome({ problem: "in_use", assignment: null, stopped: false });
  assert.equal(inUse.assignment, null);
  assert.equal(inUse.openSend, true);
  assert.equal(inUse.stopped, false);
  assert.equal(inUse.reload, false);
  assert.equal(inUse.text, changeProblemText("in_use"));

  for (const problem of ["changed", "not_found", "failed", "expired"]) {
    assert.equal(changeOutcome({ problem, assignment: null, stopped: false }).openSend, false, problem);
  }
});

test("the removed line says «Работа остановлена» only after a stopping delete; «Вернуть» stays", () => {
  const plain = renderToStaticMarkup(h(RemovedLine, { busy: false, onRestore: () => undefined }));
  assert.match(plain, /role="status"[^>]*>Задание удалено</);
  assert.ok(!plain.includes("Работа остановлена"));

  const stopped = renderToStaticMarkup(h(RemovedLine, { busy: false, stopped: true, onRestore: () => undefined }));
  assert.ok(stopped.includes("Задание удалено · Работа остановлена"));
  assert.match(stopped, />Вернуть</);
});

test("the warning names the stop and the 3 days; «Удалить» is the only danger button, «Отмена» has the focus", () => {
  const html = renderToStaticMarkup(h(DeleteWarning, { onConfirm: () => undefined, onCancel: () => undefined }));
  assert.ok(html.includes(changeTexts.stopWarning));
  assert.match(html, /role="status"/);
  assert.match(html, /danger[^>]*>Удалить</);
  assert.match(html, />Отмена</);
  assert.equal(html.split("danger").length - 1, 1);
});

test("the in_use refusal is an alert with «Открыть отправку»; others have no button", () => {
  const withButton = renderToStaticMarkup(
    h(ChangeRefusal, { text: changeProblemText("in_use"), onOpenSend: () => undefined }),
  );
  assert.match(withButton, /role="alert"/);
  assert.ok(withButton.includes(changeProblemText("in_use")));
  assert.match(withButton, />Открыть отправку</);

  const plain = renderToStaticMarkup(h(ChangeRefusal, { text: changeProblemText("failed") }));
  assert.match(plain, /role="alert"/);
  assert.ok(!plain.includes("Открыть отправку"));
});

test("both delete buttons ask first and delete only through the warning or when nothing is running", () => {
  // The buttons call the asking functions, never the delete directly.
  assert.match(page, /onClick=\{\(\) => void askRemove\(a\)\}/);
  assert.match(page, /onClick=\{\(\) => void askRemoveThis\(\)\}/);
  // Asking: the check comes first; a running work shows the warning, otherwise the delete follows at once.
  assert.match(page, /deleteNeedsWarning\(\(\) => api\.deleteImpact\(a\.id\)\)/);
  assert.match(page, /deleteNeedsWarning\(\(\) => api\.deleteImpact\(id\)\)/);
  assert.match(page, /setConfirmId\(a\.id\)/);
  assert.match(page, /setConfirming\(true\)/);
  // «Отмена» only closes the warning; the confirm button deletes.
  assert.match(page, /onConfirm=\{\(\) => void remove\(a\)\}/);
  assert.match(page, /onConfirm=\{\(\) => void removeThis\(\)\}/);
  assert.match(page, /setConfirmId\(""\);\s*setCancelledId\(a\.id\);/);
  assert.match(page, /setConfirming\(false\);\s*setCancelled\(true\);/);
  // A double click is one question and one delete (the same guard the page already uses).
  assert.match(page, /impactGuard\.current\.has\(a\.id\)/);
  assert.match(page, /impactGuard\.current\.has\(id\)/);
});

test("the check shows no «Удаление…»; «Отмена» gives the focus back to «Удалить»", () => {
  // The check only disables the button (checkingId / checking); the busy label is the real delete's (busyId / changing).
  assert.match(page, /disabled=\{busyId === a\.id \|\| checkingId === a\.id \|\| confirmId === a\.id\}/);
  assert.match(page, /disabled=\{changing \|\| checking \|\| confirming\}/);
  assert.match(page, /busy=\{busyId === a\.id\}/);
  assert.match(page, /busy=\{changing\}\s*busyLabel=\{changeTexts\.removing\}/);
  assert.doesNotMatch(page, /setBusyId\(a\.id\);\s*try \{\s*await impactGuard/);
  assert.doesNotMatch(page, /setChanging\(true\);\s*try \{\s*await impactGuard/);
  // After «Отмена» the button is mounted again (new key) with autoFocus, as after «Вернуть».
  assert.match(page, /key=\{cancelledId === a\.id \? "remove-cancelled" : "remove"\}/);
  assert.match(page, /autoFocus=\{restoredId === a\.id \|\| cancelledId === a\.id\}/);
  assert.match(page, /key=\{cancelled \? "remove-cancelled" : "remove"\}/);
  assert.match(page, /autoFocus=\{restored \|\| cancelled\}/);
});

test("the refusal button opens the assignment from the list and the emails on the detail page", () => {
  assert.match(page, /onOpenSend=\{problem\.openSend \? \(\) => props\.onOpen\(a\.id, "mail"\) : undefined\}/);
  // The list's button lands on the emails: the page remembers the target, the editor shows the emails once loaded.
  assert.match(page, /setMailFor\(target === "mail" \? id : ""\)/);
  assert.match(page, /showMail=\{mailFor === openId\}/);
  assert.match(page, /if \(showMail === true && loaded\) \{\s*showDeliveries\(\);\s*onMailShown\?\.\(\);/);
  assert.match(page, /onOpenSend=\{changeOpenSend \? showDeliveries : undefined\}/);
  assert.match(page, /ref=\{mailRef\}/);
  assert.match(page, /mailRef\.current\?\.scrollIntoView/);
  assert.match(page, /stopped=\{stoppedIds\[a\.id\] === true\}/);
  assert.match(page, /stopped=\{goneStopped\}/);
});
