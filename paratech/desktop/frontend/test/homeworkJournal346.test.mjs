// «Задания» and «Журнал» (TASK-346): the grouped list, the first run, the menu in the head of the assignment page,
// «Сохранить ▾», the email row, and the journal's filters, reasons and the outcome in the row. Pure view: the pages
// are checked through the pieces they are made of, and their wiring in the source (like test/homeworkdeletewarn.test.mjs).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { doneTexts, focusKeepTarget, rowsChangingGroup } from "../.test-build/assignmentDoneView.js";
import {
  addTexts,
  dueGroupKey,
  draftButtonTexts,
  firstRunTexts,
  groupAssignments,
  groupTitles,
  weekDays,
} from "../.test-build/homeworkView.js";
import {
  AssignmentHeadMenu,
  DeliveryRow,
  GroupedRows,
  headMenuItems,
  headMenuTexts,
  HomeworkFirstRun,
  SaveMenu,
} from "../.test-build/pages/HomeworkMenus.js";
import {
  extraFilterCount,
  filtersNarrow,
  initialFilter,
  lineModel,
  noUndoText,
  outcomeHasRow,
  resetFilters,
  texts,
} from "../.test-build/journalView.js";
import { FilterBar, JournalList, LoadError } from "../.test-build/pages/JournalParts.js";

const noop = () => undefined;
const read = (rel) => readFileSync(new URL(`../src/${rel}`, import.meta.url), "utf8");

// ---- The grouped list ----

const now = new Date(2026, 10, 3, 12, 0, 0); // Tue 3 Nov 2026, noon, local time
const at = (y, m, d, hh = 0, mm = 0) => new Date(y, m, d, hh, mm).toISOString();

const item = (id, due, over = {}) => ({
  id,
  lessonId: "",
  title: `Задание ${id}`,
  description: "",
  due,
  userEdited: false,
  version: 1,
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
  done: false,
  doneAt: "",
  ...over,
});

const keys = (groups) => groups.map((g) => g.key);
const ids = (g) => g.items.map((a) => a.id);

test("the group of a deadline: passed, within the next 7 days (today + 6), later, none or unreadable", () => {
  assert.equal(weekDays, 7);
  assert.equal(dueGroupKey(at(2026, 10, 3, 11, 59), now), "overdue");
  assert.equal(dueGroupKey(at(2026, 10, 3, 12, 0), now), "overdue", "the moment of now has passed (like dueText)");
  assert.equal(dueGroupKey(at(2026, 10, 3, 12, 1), now), "week", "later today");
  assert.equal(dueGroupKey(at(2026, 10, 3, 23, 59), now), "week", "end of today");
  assert.equal(dueGroupKey(at(2026, 10, 9, 23, 59), now), "week", "day 6 (today + 6) is the last day of the week");
  assert.equal(dueGroupKey(at(2026, 10, 10, 0, 1), now), "later", "day 7 is already «Позже»");
  assert.equal(dueGroupKey(at(2026, 10, 10, 23, 59), now), "later", "and so is the end of day 7");
  assert.equal(dueGroupKey(at(2026, 11, 20, 9, 0), now), "later");
  assert.equal(dueGroupKey("", now), "nodue");
  assert.equal(dueGroupKey("не дата", now), "nodue");
});

test("the list is grouped «Просрочено · На этой неделе · Позже · Без срока», each sorted by deadline", () => {
  const items = [
    item("nodue1", ""),
    item("later", at(2026, 11, 20, 9)),
    item("week2", at(2026, 10, 8, 10)),
    item("bad", "не дата"),
    item("ovd2", at(2026, 10, 2, 9)),
    item("week1", at(2026, 10, 4, 10)),
    item("ovd1", at(2026, 9, 20, 9)),
    item("tie-b", at(2026, 10, 5, 10)),
    item("tie-a", at(2026, 10, 5, 10)),
  ];
  const groups = groupAssignments(items, now, "open");
  assert.deepEqual(keys(groups), ["overdue", "week", "later", "nodue"]);
  assert.deepEqual(
    groups.map((g) => g.title),
    ["Просрочено", "На этой неделе", "Позже", "Без срока"],
  );
  assert.deepEqual(ids(groups[0]), ["ovd1", "ovd2"], "earliest first");
  assert.deepEqual(ids(groups[1]), ["week1", "tie-b", "tie-a", "week2"], "ties keep the core's order");
  assert.deepEqual(ids(groups[2]), ["later"]);
  assert.deepEqual(ids(groups[3]), ["nodue1", "bad"], "no deadline: the core's order");
  assert.equal(items.length, groups.flatMap((g) => g.items).length, "no row is lost");
});

test("groups without rows are not drawn", () => {
  assert.deepEqual(keys(groupAssignments([item("a", ""), item("b", at(2026, 10, 3, 18))], now, "open")), ["week", "nodue"]);
  assert.deepEqual(groupAssignments([], now, "open"), []);
  assert.deepEqual(groupAssignments([], now, "all"), []);
  assert.deepEqual(groupAssignments([], now, "done"), []);
});

test("done ones: none in «Открытые», flat in «Выполненные», the last group «Выполнено» in «Все»", () => {
  const open = item("o1", at(2026, 10, 2, 9));
  const d1 = item("d1", at(2026, 10, 1, 9), { done: true, doneAt: "2026-11-02T10:00:00Z" });
  const d2 = item("d2", "", { done: true, doneAt: "2026-11-01T10:00:00Z" });

  assert.deepEqual(keys(groupAssignments([open], now, "open")), ["overdue"]);

  const flat = groupAssignments([d2, d1], now, "done");
  assert.deepEqual(keys(flat), ["flat"], "no deadline groups among the done ones");
  assert.equal(flat[0].title, "", "and no heading");
  assert.deepEqual(ids(flat[0]), ["d2", "d1"], "the core's order");

  const all = groupAssignments([d1, open, d2], now, "all");
  assert.deepEqual(keys(all), ["overdue", "done"]);
  assert.equal(all[1].title, groupTitles.done);
  assert.equal(all[1].title, "Выполнено");
  assert.deepEqual(ids(all[1]), ["d1", "d2"]);
  assert.deepEqual(ids(all[0]), ["o1"], "a done one is never «Просрочено»");
});

const row = (a) => h("div", { key: a.id, className: "row" }, a.id);
const grouped = (groups, over = {}) =>
  renderToStaticMarkup(h(GroupedRows, { groups, states: {}, onStateChange: noop, renderItem: row, ...over }));

test("the groups are h2 headings with the whole count, each in its own list", () => {
  const groups = groupAssignments(
    [item("o1", at(2026, 10, 2, 9)), item("o2", at(2026, 10, 2, 10)), item("w1", at(2026, 10, 4, 10)), item("n1", "")],
    now,
    "open",
  );
  const full = grouped(groups);
  assert.match(full, /<h2 class="hw-group" id="hw-h-overdue">Просрочено<span class="cnt num">2<\/span><\/h2>/);
  assert.match(full, /<h2 class="hw-group" id="hw-h-week">На этой неделе<span class="cnt num">1<\/span>/);
  assert.match(full, /<h2 class="hw-group" id="hw-h-nodue">Без срока/);
  assert.ok(!full.includes("hw-h-later"), "an empty group has no heading");
  assert.equal(full.split('role="list"').length - 1, 3, "a list per group");
  assert.ok(!full.includes("clist-more"), "short groups need no «Показать ещё»");

  const flat = grouped(groupAssignments([item("d1", "", { done: true })], now, "done"));
  assert.ok(!flat.includes("<h2"), "«Выполненные» stay flat");
  assert.equal(flat.split('role="list"').length - 1, 1);
});

test("every group has its own «Показать ещё» window: nine overdue rows never hide «На этой неделе»", () => {
  const overdue = Array.from({ length: 9 }, (_, i) => item(`o${i}`, at(2026, 10, 2, 8 + i)));
  const week = [item("w1", at(2026, 10, 4, 10)), item("w2", at(2026, 10, 5, 10))];
  const done = Array.from({ length: 10 }, (_, i) => item(`d${i}`, at(2026, 10, 1, 8 + i), { done: true }));
  const groups = groupAssignments([...overdue, ...week, ...done], now, "all");
  assert.deepEqual(keys(groups), ["overdue", "week", "done"]);

  const html = grouped(groups);
  // Every non-empty group is drawn with its heading and the whole count.
  assert.match(html, /id="hw-h-overdue">Просрочено<span class="cnt num">9<\/span>/);
  assert.match(html, /id="hw-h-week">На этой неделе<span class="cnt num">2<\/span>/);
  assert.match(html, /id="hw-h-done">Выполнено<span class="cnt num">10<\/span>/);
  // The limit (8) is applied per group: 8 of 9, both of the week, 8 of 10.
  assert.equal(html.split('class="row"').length - 1, 8 + 2 + 8);
  assert.ok(html.includes(">w1<") && html.includes(">w2<"), "the week is whole");
  assert.ok(!html.includes(">o8<") && !html.includes(">d9<"), "the long groups are cut");
  assert.ok(html.includes("Показать ещё 1") && html.includes("Показать ещё 2"), "each shows its own hidden count");
  assert.equal(html.split("clist-more").length - 1, 2);

  // The first button sits under the overdue card, before the heading of the next group.
  assert.ok(html.indexOf("Показать ещё 1") < html.indexOf("hw-h-week"));
  assert.ok(html.indexOf("hw-h-week") < html.indexOf("Показать ещё 2"));

  // One window opened by key does not open the others.
  const opened = grouped(groups, { states: { overdue: { collapsed: false, showAll: true } } });
  assert.ok(opened.includes(">o8<") && !opened.includes(">d9<"));
  assert.ok(!opened.includes("Показать ещё 1") && opened.includes("Показать ещё 2"));
  assert.ok(opened.includes("Свернуть"));
});

test("a group's window state is reported under the group's key", () => {
  const groups = groupAssignments([item("w1", at(2026, 10, 4, 10))], now, "open");
  const calls = [];
  const tree = GroupedRows({ groups, states: {}, onStateChange: (k, s) => calls.push([k, s]), renderItem: row });
  const list = tree.props.children[0].props.children.find((c) => c && c.props && c.props.onStateChange);
  list.props.onStateChange({ collapsed: false, showAll: true });
  assert.deepEqual(calls, [["week", { collapsed: false, showAll: true }]]);
});

test("the page keeps a list state per group and draws the groups through GroupedRows", () => {
  const page = read("pages/HomeworkPage.tsx");
  assert.match(page, /const groups = groupAssignments\(shown, new Date\(\), state\)/);
  assert.match(page, /const \[lists, setLists\] = useState<ListStates>\(\{\}\)/);
  assert.match(page, /<GroupedRows\s+groups=\{groups\}\s+states=\{lists\}\s+onStateChange=\{\(key, next\) => setLists\(\(cur\) => withState\(cur, key, next\)\)\}\s+renderItem=\{row\}/);
  assert.doesNotMatch(page, /<CollapsibleList|state=\{list\}|setList\(/, "no window shared by the groups");
  assert.equal(page.split("setLists({})").length - 1, 3, "another mark, a filter and the reset start the windows again");
  assert.match(read("pages/HomeworkMenus.tsx"), /state=\{stateOf\(props\.states, g\.key\)\}/);
  assert.doesNotMatch(read("homeworkView.ts"), /splitRows/);
});

test("marking a row done in «Все» puts the focus back on its check once the row sits in «Выполнено»", () => {
  const open = { id: "a1", done: false };
  const doneNow = { id: "a1", done: true };
  const other = { id: "a2", done: false };
  // Only a row that stays but changes its mark changes its group.
  assert.deepEqual(rowsChangingGroup([open, other], [doneNow, other]), ["a1"]);
  assert.deepEqual(rowsChangingGroup([open, other], [open, other]), []);
  assert.deepEqual(rowsChangingGroup([open, other], [other]), [], "a row that leaves is rowsLeaving's business");
  assert.deepEqual(rowsChangingGroup([doneNow], [open]), ["a1"], "and back to work");

  // The focus is kept only for the check of a moving row; any other control keeps its focus.
  assert.equal(focusKeepTarget({ kind: "check", id: "a1" }, ["a1"]), "a1");
  assert.equal(focusKeepTarget({ kind: "check", id: "a2" }, ["a1"]), null);
  assert.equal(focusKeepTarget({ kind: "other" }, ["a1"]), null);
  assert.equal(focusKeepTarget({ kind: "none" }, ["a1"]), null);
  assert.equal(focusKeepTarget({ kind: "check", id: "a1" }, []), null);

  // The page asks for it when the result is applied, and the hook finds the check by the same data attribute.
  const page = read("pages/HomeworkPage.tsx");
  assert.match(page, /focus\.rowsMoving\(rowsChangingGroup\(cur, next\)\)/);
  const hook = read("pages/useDoneFocus.ts");
  assert.match(hook, /rowsMoving\(moving: readonly string\[\]\)[\s\S]*focusKeepTarget\([\s\S]*pending\.current = target/);
  assert.match(hook, /\[data-done-id\]/);
  assert.match(read("pages/AssignmentDoneParts.tsx"), /data-done-id=\{props\.rowId\}/);
});

test("the rows stay in screen order for the focus: one data-done-id check per row, groups in order", () => {
  const groups = groupAssignments(
    [item("w1", at(2026, 10, 4, 10)), item("o1", at(2026, 10, 2, 9)), item("d1", "", { done: true })],
    now,
    "all",
  );
  const html = grouped(groups, {
    renderItem: (a) => h("div", { key: a.id, "data-done-id": a.id }),
  });
  const order = [...html.matchAll(/data-done-id="([^"]+)"/g)].map((m) => m[1]);
  assert.deepEqual(order, ["o1", "w1", "d1"], "the DOM order is the screen order");
});

// ---- The list head and the first run ----

test("«Добавить задание» is the primary button of the list head", () => {
  const page = read("pages/HomeworkPage.tsx");
  assert.match(page, /<Button variant="primary" icon="plus" onClick=\{\(\) => setAdding\(true\)\}>\s*\{addTexts\.button\}/);
  assert.equal(addTexts.button, "Добавить задание");
});

test("the first run says where assignments come from and offers «Добавить задание вручную»", () => {
  assert.equal(firstRunTexts.text, "Задания находятся сами в записи пары после конспекта.");
  assert.equal(firstRunTexts.manual, "Добавить задание вручную");
  const calls = [];
  const html = renderToStaticMarkup(h(HomeworkFirstRun, { onAdd: () => calls.push("add") }));
  assert.ok(html.includes("Задания находятся сами в записи пары после конспекта."));
  assert.match(html, /<button type="button" class="btn">Добавить задание вручную<\/button>/);
  assert.ok(!html.includes("btn primary"), "the one accent of the screen is the head's button");
  assert.ok(!html.includes("черновик"), "the draft and send promise is gone");
  assert.deepEqual(calls, []);
  // The page opens the same add dialog from it.
  assert.match(read("pages/HomeworkPage.tsx"), /<HomeworkFirstRun onAdd=\{\(\) => setAdding\(true\)\} \/>/);
});

// ---- The assignment page ----

const idleMenu = { changing: false, marking: false, checking: false, confirming: false, correcting: false };

test("the head's «⋯» menu: Изменить, Поправить предмет и преподавателя, Удалить last", () => {
  const items = headMenuItems(idleMenu);
  assert.deepEqual(
    items.map((i) => [i.key, i.label]),
    [
      ["edit", "Изменить"],
      ["correct", "Поправить предмет и преподавателя"],
      ["remove", "Удалить"],
    ],
  );
  assert.ok(items.every((i) => !i.disabled));
  assert.ok(!items.some((i) => i.label === "Выполнено" || i.label === doneTexts.pageMark), "«Выполнено» stays outside");
});

test("the menu items wait for a running call and say why; the delete question locks «Удалить» only", () => {
  const marking = headMenuItems({ ...idleMenu, marking: true });
  assert.ok(marking.every((i) => i.disabled && i.hint === "Идёт сохранение" && i.hint === doneTexts.busyHint));

  const deleting = headMenuItems({ ...idleMenu, changing: true });
  assert.ok(deleting.every((i) => i.disabled && i.hint === "Идёт удаление" && i.hint === headMenuTexts.deleting));

  const checking = headMenuItems({ ...idleMenu, checking: true });
  assert.ok(checking.every((i) => i.disabled && i.hint === doneTexts.removeChecking));

  const confirming = headMenuItems({ ...idleMenu, confirming: true });
  assert.equal(confirming[0].disabled, false);
  assert.equal(confirming[1].disabled, false);
  assert.equal(confirming[2].disabled, true);
  assert.equal(confirming[2].hint, doneTexts.removeAsked);
});

test("«Поправить предмет и преподавателя» is off with «Уже открыто» while its form is open", () => {
  assert.equal(headMenuTexts.formOpen, "Уже открыто");
  const open = headMenuItems({ ...idleMenu, correcting: true });
  assert.equal(open[0].disabled, false, "«Изменить» is not about that form");
  assert.equal(open[1].disabled, true);
  assert.equal(open[1].hint, "Уже открыто");
  assert.equal(open[2].disabled, false);
  // A running call is the more important reason.
  assert.equal(headMenuItems({ ...idleMenu, correcting: true, changing: true })[1].hint, "Идёт удаление");
  assert.match(read("pages/HomeworkPage.tsx"), /correcting=\{correcting\}/);
});

test("the «⋯» button of the head has the accessible name «Ещё действия: <title>» and the id the page finds it by", () => {
  const html = renderToStaticMarkup(
    h(AssignmentHeadMenu, { ...idleMenu, id: "a1", title: "Задачи 1–5", onPick: noop }),
  );
  assert.ok(html.includes('aria-label="Ещё действия: Задачи 1–5"'));
  assert.ok(html.includes('data-menu-id="a1"'));
  assert.ok(!html.includes("Удалить"), "the items are in the closed menu");
});

test("the head keeps «Выполнено» outside the menu, the lesson label stays, the ghost button is gone", () => {
  const page = read("pages/HomeworkPage.tsx");
  const head = page.slice(page.indexOf("<DoneButton"), page.indexOf("<DoneStamp"));
  assert.ok(head.indexOf("<DoneButton") === 0 && head.indexOf("<AssignmentHeadMenu") > 0, "«Выполнено», then «⋯»");
  assert.match(head, /id=\{id\}\s+title=\{a\.title\}/);
  assert.doesNotMatch(page, /variant="ghost" onClick=\{\(\) => setCorrecting\(true\)\}/);
  assert.match(page, /\{lesson !== "" && \(\s*<div className="hw-lesson-head">\s*<span className="hw-lesson">\{lesson\}<\/span>/);
  assert.match(page, /else if \(key === "correct"\) \{\s*setCorrecting\(true\);/);
  assert.match(page, /if \(key === "edit"\) \{\s*setEditing\(true\);/);
});

test("«Сохранить ▾» replaces the two save buttons: closed it is one button, open it has «Файл .txt» and «Word (.docx)»", () => {
  const closed = renderToStaticMarkup(h(SaveMenu, { busy: false, disabled: false, onFile: noop, onWord: noop }));
  assert.match(closed, /aria-haspopup="menu"/);
  assert.match(closed, /aria-expanded="false"/);
  assert.ok(closed.includes("Сохранить"));
  assert.ok(closed.includes("▾"));
  assert.ok(!closed.includes("Файл .txt"));

  const open = renderToStaticMarkup(h(SaveMenu, { busy: false, disabled: false, onFile: noop, onWord: noop, initialOpen: true }));
  assert.match(open, /aria-expanded="true"/);
  assert.match(open, /role="menu"/);
  const file = open.indexOf(">Файл .txt<");
  const word = open.indexOf(">Word (.docx)<");
  assert.ok(file > 0 && word > file, "Файл .txt, then Word");
  assert.equal(open.split('role="menuitem"').length - 1, 2);
  assert.ok(open.includes("mmenu-list"), "the menu look of the «⋯» menus");
});

test("«Сохранить ▾» while saving says «Сохранение…»; with an unsaved edit it is off and says why", () => {
  const busy = renderToStaticMarkup(h(SaveMenu, { busy: true, disabled: true, onFile: noop, onWord: noop }));
  assert.match(busy, /<span class="btn-off">Сохранить/);
  assert.match(busy, /<span>Сохранение…<\/span>/);

  const dirty = renderToStaticMarkup(
    h(SaveMenu, { busy: false, disabled: true, reason: "Сначала сохраните или отмените правку.", onFile: noop, onWord: noop }),
  );
  assert.match(dirty, /<button[^>]*disabled=""/);
  assert.ok(dirty.includes("Сначала сохраните или отмените правку."));
  assert.match(dirty, /aria-describedby=/);
  // A disabled menu cannot be open, even if it was.
  const was = renderToStaticMarkup(h(SaveMenu, { busy: false, disabled: true, onFile: noop, onWord: noop }));
  assert.ok(!was.includes('role="menu"'));
});

test("the page wires «Сохранить ▾» to the same handlers and the send button is «Отправить преподавателю…»", () => {
  const page = read("pages/HomeworkPage.tsx");
  assert.match(page, /<SaveMenu\s+busy=\{busy === "export" \|\| busy === "docx"\}\s+disabled=\{!idle \|\| dirty\}\s+reason=\{savedReason\}\s+onFile=\{exportFile\}\s+onWord=\{saveWord\}/);
  assert.equal(draftButtonTexts.send, "Отправить преподавателю…");
  assert.match(page, /\{draftButtonTexts\.send\}/);
  // A dirty edit switches the send off, and the button says why like its neighbour «Сохранить ▾».
  assert.match(page, /disabled=\{!idle \|\| dirty\}\s+disabledReason=\{savedReason\}\s+onClick=\{startSend\}/);
  for (const rel of ["pages/HomeworkPage.tsx", "pages/HomeworkMenus.tsx", "homeworkView.ts", "pages/HomeworkDialogs.tsx"]) {
    const text = read(rel);
    assert.doesNotMatch(text, /Отправить по почте/, rel);
    assert.doesNotMatch(text, /Сохранить в файл|Сохранить в Word/, rel);
  }
});

const delivery = (over = {}) => ({
  id: "d1",
  status: "UNKNOWN",
  recipient: "teacher@example.org",
  subject: "Тема",
  attempts: 1,
  lastError: "",
  confirmedAt: "2026-10-16T10:00:00Z",
  sentAt: "",
  createdAt: "2026-10-16T10:00:00Z",
  canMarkSent: false,
  canSendAgain: false,
  notice: "Нет подтверждения от почты.",
  ...over,
});

const emailRow = (d, over = {}) =>
  renderToStaticMarkup(h(DeliveryRow, { delivery: d, marking: false, idle: true, onMarkSent: noop, onSendAgain: noop, ...over }));

test("«Отправить ещё раз…» is an ordinary button, not the red one", () => {
  const html = emailRow(delivery({ canSendAgain: true }));
  assert.match(html, /<button type="button" class="btn">Отправить ещё раз…<\/button>/);
  assert.ok(!html.includes("danger"));
  assert.ok(!emailRow(delivery()).includes("Отправить ещё раз"), "only when the core allows it");
});

test("«Письмо дошло» comes with one line that says when to press it, tied to the button; the core's hint is not repeated", () => {
  const line =
    "Почта не подтвердила доставку. Если преподаватель письмо получил — отметьте «Письмо дошло». Повторная отправка может прийти вторым письмом.";
  assert.equal(draftButtonTexts.markSentNote, line);
  const html = emailRow(delivery({ canMarkSent: true }));
  assert.ok(html.includes(`<span id="mark-note-d1" class="small muted">${line}</span>`));
  assert.ok(!html.includes("Нет подтверждения от почты."), "the notice of the core would say the same twice");
  // Without the button the core's notice is the only line.
  assert.ok(emailRow(delivery()).includes("Нет подтверждения от почты."));
  assert.match(html, /<button type="button" class="btn" aria-describedby="mark-note-d1"><span class="btn-labels"><span>Письмо дошло<\/span>/);
  const none = emailRow(delivery());
  assert.ok(!none.includes(line) && !none.includes("Письмо дошло"), "only when the core allows it");
  // The email is text: the recipient is not markup.
  assert.ok(emailRow(delivery({ recipient: "<img src=x>" })).includes("&lt;img src=x&gt;"));
});

test("the email row is busy and off like the other calls of the page", () => {
  const html = emailRow(delivery({ canMarkSent: true, canSendAgain: true }), { marking: true, idle: false });
  assert.equal(html.split('disabled=""').length - 1, 2);
  assert.match(html, /<span class="btn-labels"><span class="btn-off">Письмо дошло<\/span><span>Сохранение…<\/span>/);
});

// ---- The journal ----

const line = (over = {}) => ({
  kind: "action", at: "2026-10-22T10:00:00Z", updatedAt: "2026-10-22T10:00:00Z", subjectId: "s1", actionId: "a1", threadId: "t1",
  turn: 1, tool: "create_subject", jobId: "", jobType: "", status: "DONE", undoable: true, cannotUndo: false, noUndo: "", ...over,
});
const names = new Map([["s1", "Матанализ"]]);
const list = (lines, extra = {}) =>
  renderToStaticMarkup(h(JournalList, { lines, subjects: names, busyAction: "", onUndo: noop, ...extra }));

test("the journal has no «Обновить» and no head actions", () => {
  assert.ok(!("refresh" in texts));
  for (const rel of ["pages/JournalPage.tsx", "pages/JournalParts.tsx", "journalView.ts"]) {
    assert.doesNotMatch(read(rel), /Обновить|texts\.refresh/, rel);
  }
  assert.doesNotMatch(read("pages/JournalPage.tsx"), /className="acts"/);
  assert.doesNotMatch(read("pages/JournalPage.tsx"), /<Button/);
});

test("a load error shows «Повторить» next to it, as an alert; «Загрузка…» stays a small inline text", () => {
  const html = renderToStaticMarkup(h(LoadError, { text: "Не удалось загрузить журнал.", onRetry: noop }));
  assert.match(html, /role="alert"/);
  assert.ok(html.includes("Не удалось загрузить журнал."));
  assert.match(html, />Повторить</);
  const page = read("pages/JournalPage.tsx");
  assert.match(page, /<LoadError text=\{error\} onRetry=\{retry\} \/>/);
  assert.match(page, /<p className="small muted">\{texts\.loading\}<\/p>/);
  // An empty answer after a failed read is no «ничего нет».
  assert.match(page, /!\(error !== "" && lines\.length === 0\)/);
});

test("«Повторить» keeps the lines on the screen and shows a small «Загрузка…» until the answer comes", () => {
  const page = read("pages/JournalPage.tsx");
  assert.match(
    page,
    /function retry\(\) \{\s*setError\(""\);\s*setRetrying\(true\);\s*if \(lines === null \|\| lines\.length === 0\) \{\s*setLines\(null\);\s*\}\s*load\(\);/,
    "lines are dropped only when there are none",
  );
  assert.match(page, /\(lines === null \? error === "" : retrying\) && <p className="small muted">\{texts\.loading\}<\/p>/);
  // The answer, good or bad, ends the retry.
  assert.equal(page.split("setRetrying(false)").length - 1, 2);
});

test("only «Период» and «Только отменяемые» are in sight; the rest is under «Ещё фильтры»", () => {
  const html = renderToStaticMarkup(h(FilterBar, { filter: initialFilter, subjects: [{ id: "s1", name: "Матанализ" }], onChange: noop }));
  assert.ok(html.includes("Период") && html.includes("Только отменяемые"));
  assert.ok(!html.includes("Только обратимые"));
  assert.match(html, /<button[^>]*aria-expanded="false"[^>]*>.*Ещё фильтры/);
  for (const hidden of ["Что показывать", "Предмет", "Состояние", "Матанализ"]) {
    assert.ok(!html.includes(hidden), hidden);
  }
  assert.ok(!html.includes('class="cnt num"'), "nothing is set: no count");
});

test("a set hidden filter opens «Ещё фильтры» and is counted, so it is never invisible", () => {
  assert.equal(extraFilterCount(initialFilter), 0);
  assert.equal(extraFilterCount({ ...initialFilter, days: 7, undoable: true }), 0, "the visible filters are not counted");
  assert.equal(extraFilterCount({ ...initialFilter, kind: "action" }), 1);
  assert.equal(extraFilterCount({ ...initialFilter, kind: "action", subjectId: "s1", status: "FAILED" }), 3);

  const html = renderToStaticMarkup(
    h(FilterBar, { filter: { ...initialFilter, status: "FAILED" }, subjects: [{ id: "s1", name: "Матанализ" }], onChange: noop }),
  );
  assert.match(html, /aria-expanded="true"/);
  assert.match(html, /Ещё фильтры<span class="cnt num">1<\/span>/);
  for (const shown of ["Что показывать", "Предмет", "Состояние", "Матанализ"]) {
    assert.ok(html.includes(shown), shown);
  }
});

test("a refusal reads «Нельзя отменить: <reason>»; a state that is no refusal stays plain", () => {
  assert.equal(noUndoText("sent", true), "Нельзя отменить: письмо уже отправлено");
  assert.equal(noUndoText("changed", true), "Нельзя отменить: после этого вносились другие изменения");
  assert.equal(noUndoText("result_ready", true), "Нельзя отменить: результат уже готов, прежний не сохранён");
  assert.equal(noUndoText("cannot_stop", true), "Нельзя отменить: обработку уже не остановить");
  assert.equal(noUndoText("irreversible", true), "Нельзя отменить: это предложение, решение за вами", "a refusal with the prefix");
  assert.equal(noUndoText("failed", false), "Не выполнено — отменять нечего");
  assert.equal(noUndoText("running", false), "Выполняется");
  assert.equal(noUndoText("unknown", false), "Итог неизвестен");
  assert.equal(noUndoText("undone", false), "Уже отменено");
  assert.equal(noUndoText("", false), "");
  // The core marks it as not undoable and names no reason (or one this app does not know): the general words.
  assert.equal(noUndoText("", true), "Нельзя отменить: такие действия не возвращаются");
  assert.equal(noUndoText("something new", true), "Нельзя отменить: такие действия не возвращаются");
  assert.equal(noUndoText("something new", false), "");
  assert.equal(noUndoText("constructor", true), "Нельзя отменить: такие действия не возвращаются", "no prototype keys");
});

test("the status is not said twice: a reason that repeats the status label is left out, the rest stay", () => {
  const reason = (noUndo, status) => lineModel(line({ undoable: false, noUndo, status }), 0, names).reason;
  assert.equal(reason("running", "RUNNING"), "");
  assert.equal(reason("running", "PENDING"), "");
  assert.equal(reason("unknown", "UNKNOWN"), "");
  assert.equal(reason("undone", "UNDONE"), "");
  assert.equal(reason("failed", "FAILED"), "Не выполнено — отменять нечего", "it adds a fact");
  assert.equal(reason("sent", "COMPLETED"), "Нельзя отменить: письмо уже отправлено");
  // A reason that the status does not repeat is still shown.
  assert.equal(reason("running", "DONE"), "Выполняется");
  assert.equal(reason("unknown", ""), "Итог неизвестен", "no status label: the reason is all there is");
  // On the screen: once.
  const out = list([line({ undoable: false, noUndo: "undone", status: "UNDONE" })]);
  assert.equal(out.split("Отменено").length - 1, 1);
  assert.ok(!out.includes("Уже отменено"));
});

test("a line that cannot be undone has the reason as text in place of a disabled button", () => {
  const sent = line({ kind: "processing", actionId: "", tool: "", jobId: "j1", jobType: "send_email", status: "COMPLETED", undoable: false, cannotUndo: true, noUndo: "sent" });
  const out = list([sent]);
  assert.match(out, /<span class="small muted">Нельзя отменить: письмо уже отправлено<\/span>/);
  assert.doesNotMatch(out, /<button/);
  assert.ok(!out.includes("Отменить нельзя"));

  const generic = list([line({ undoable: false, cannotUndo: true, noUndo: "" })]);
  assert.ok(generic.includes("Нельзя отменить: такие действия не возвращаются"));
  assert.doesNotMatch(generic, /<button/);

  const m = lineModel(sent, 0, names);
  assert.equal(m.cannotUndo, true);
  assert.equal(m.reason, "Нельзя отменить: письмо уже отправлено");
  assert.equal(lineModel(line(), 0, names).reason, "", "an undoable line says nothing");
});

test("the outcome of «Отменить» shows in the row of that action: a status when done, an alert when refused", () => {
  const lines = [line({ actionId: "a1" }), line({ actionId: "a2", tool: "create_folder" })];
  const done = list(lines, { outcome: { actionId: "a1", ok: true, text: "Действие отменено." } });
  assert.equal(done.split("Действие отменено.").length - 1, 1, "once, in one row");
  assert.match(done, /role="status"[^>]*><svg[^>]*><use href="#i-ok"><\/use><\/svg><span>Действие отменено\.<\/span>/);
  const row = done.slice(done.indexOf("row-below"));
  assert.ok(row.includes("Действие отменено."));
  assert.ok(done.indexOf("Создан предмет") < done.indexOf("Действие отменено.") && done.indexOf("Действие отменено.") < done.indexOf("Создана папка"), "under the row of a1, before a2");

  const refused = list(lines, { outcome: { actionId: "a2", ok: false, text: "Результат уже готов — отменить нельзя." } });
  assert.match(refused, /role="alert"[^>]*>.*Результат уже готов/);
  assert.ok(refused.indexOf("Создана папка") < refused.indexOf("Результат уже готов"));

  // An undone line has no «Отменить» any more, but its row still shows the outcome.
  const undone = list([line({ actionId: "a1", status: "UNDONE", undoable: false, noUndo: "undone" })], {
    outcome: { actionId: "a1", ok: true, text: "Действие отменено." },
  });
  assert.ok(undone.includes("Действие отменено.") && undone.includes("Отменено"), "the status is in the meta, once");
  assert.ok(!undone.includes("Уже отменено"), "and not repeated as a reason");

  assert.ok(!list(lines, { outcome: null }).includes("row-below"));
  assert.ok(!list(lines).includes("row-below"));
});

test("an outcome whose row a filter dropped is said on the page, not lost", () => {
  assert.equal(outcomeHasRow([line({ actionId: "a1" })], "a1"), true);
  assert.equal(outcomeHasRow([line({ actionId: "a1" })], "a9"), false);
  assert.equal(outcomeHasRow([line({ kind: "processing", actionId: "", jobId: "j1" })], ""), false, "a processing line has no action");
  assert.equal(outcomeHasRow([], "a1"), false);
  const page = read("pages/JournalPage.tsx");
  assert.match(page, /const strayOutcome = outcome !== null && lines !== null && !outcomeHasRow\(lines, outcome\.actionId\)/);
  assert.match(page, /setOutcome\(\{ actionId, \.\.\.undoOutcome\(await api\.undo\(actionId\)\) \}\)/);
  assert.match(page, /setOutcome\(null\);\s*setBusyAction|setBusyAction\(actionId\);\s*setOutcome\(null\)/, "cleared by the next undo");
  // It stands under the filters, above the list, where the student looks after pressing «Отменить».
  const stray = page.indexOf("{strayOutcome !== null && (");
  assert.ok(stray > page.indexOf("<FilterBar") && stray < page.indexOf("<JournalList"), "above the list, under the filters");
});

test("filters that hide everything say so and offer «Сбросить фильтры»; the period alone says the old words", () => {
  assert.equal(texts.emptyFiltered, "По выбранным фильтрам ничего нет.");
  assert.equal(texts.resetFilters, "Сбросить фильтры");
  assert.equal(texts.empty, "За выбранный период ничего нет.");

  assert.equal(filtersNarrow(initialFilter), false);
  assert.equal(filtersNarrow({ ...initialFilter, days: 7 }), false, "the period is not a filter that hides");
  assert.equal(filtersNarrow({ ...initialFilter, undoable: true }), true);
  assert.equal(filtersNarrow({ ...initialFilter, status: "FAILED" }), true);
  assert.equal(filtersNarrow({ ...initialFilter, kind: "action" }), true);
  assert.equal(filtersNarrow({ ...initialFilter, subjectId: "s1" }), true);

  const set = { kind: "action", subjectId: "s1", status: "FAILED", days: 90, undoable: true };
  assert.deepEqual(resetFilters(set), { ...initialFilter, days: 90 }, "all but the period");

  const calls = [];
  const filtered = renderToStaticMarkup(
    h(JournalList, { lines: [], subjects: names, busyAction: "", onUndo: noop, filtered: true, onResetFilters: () => calls.push("reset") }),
  );
  assert.ok(filtered.includes("По выбранным фильтрам ничего нет."));
  assert.match(filtered, /<button type="button" class="btn ghost">Сбросить фильтры<\/button>/);
  assert.ok(!filtered.includes("За выбранный период"));

  const plain = renderToStaticMarkup(h(JournalList, { lines: [], subjects: names, busyAction: "", onUndo: noop, filtered: false }));
  assert.ok(plain.includes("За выбранный период ничего нет."));
  assert.ok(!plain.includes("<button") && !plain.includes("Сбросить"));
  assert.deepEqual(calls, []);

  const page = read("pages/JournalPage.tsx");
  assert.match(page, /filtered=\{filtersNarrow\(filter\)\}\s+onResetFilters=\{\(\) => changeFilter\(resetFilters\(filter\)\)\}/);
});
