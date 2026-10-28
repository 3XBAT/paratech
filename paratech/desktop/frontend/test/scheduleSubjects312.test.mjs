// TASK-312: the view model of the review of subjects from the schedule (selection, counters, choices, the state machine
// and its outcomes) and the SSR of the panel, the empty list and the page header parts.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  buildChoices,
  createFlow,
  doneText,
  initialSelection,
  initialState,
  kindsText,
  lessonsWord,
  mainButton,
  mergeBatch,
  metaText,
  nameKey,
  offerChecking,
  offerGroups,
  omittedNote,
  planOf,
  reduce,
  refreshFlow,
  reviewTexts,
  setChecked,
  setMemberName,
  setName,
  subjectNameKeys,
  subjectsWord,
  toggleMember,
  undoFlow,
  groupItem,
  memberItem,
} from "../.test-build/scheduleSubjectsView.js";
import { EmptySubjectsActions, ReviewView, ScheduleSubjectsReview } from "../.test-build/pages/ScheduleSubjectsReview.js";
import { SubjectList } from "../.test-build/pages/SubjectsParts.js";

const html = (el) => renderToStaticMarkup(el);
const noop = () => undefined;

const kc = (kind, lessons) => ({ kind, lessons });
const member = (over = {}) => ({
  key: "matan", name: "Матан", spellings: ["Матан"], kinds: [kc("lecture", 4)], lessons: 4, recordings: 0, doubtful: false, link: "same", ...over,
});
const matan = (over = {}) => ({
  key: "matan", name: "Матан", aliases: ["Матанализ (лек.)"], kinds: [kc("lecture", 12), kc("seminar", 2)], lessons: 14, recordings: 0,
  doubtful: false, members: [member()], action: "create", subjectId: "", subjectName: "", ...over,
});
const fizika = (over = {}) => ({
  key: "fizika", name: "Физика", aliases: ["Физ-ка"], kinds: [kc("unknown", 5)], lessons: 5, recordings: 0, doubtful: true,
  members: [
    member({ key: "fizika", name: "Физика", spellings: ["Физика"], kinds: [kc("unknown", 3)], lessons: 3 }),
    member({ key: "fizka", name: "Физ-ка", spellings: ["Физ-ка"], kinds: [kc("unknown", 2)], lessons: 2, doubtful: true, link: "abbreviation" }),
  ],
  action: "create", subjectId: "", subjectName: "", ...over,
});
const seti = (over = {}) => ({
  key: "seti", name: "Компьютерные сети", aliases: ["Сети (лек.)", "Сети"], kinds: [kc("lecture", 3)], lessons: 3, recordings: 1, doubtful: false,
  members: [member({ key: "seti", name: "Сети", spellings: ["Сети (лек.)", "Сети"], kinds: [kc("lecture", 3)], lessons: 3, recordings: 1 })],
  action: "alias_of", subjectId: "s9", subjectName: "Компьютерные сети", ...over,
});
const reply = (over = {}) => ({ token: "tok1", groups: [matan(), fizika(), seti()], omitted: 0, problem: "", ...over });
const names = new Map([["s9", "Компьютерные сети"], ["s1", "Алгебра"]]);
const taken = subjectNameKeys(names);

const created = (id = "sched-1", key = "matan", name = "Матан") => ({ id, key, name });
const createReply = (over = {}) => ({ created: [created()], aliased: [], skipped: [], problem: "", ...over });
const undoReply = (over = {}) => ({ removed: [], kept: [], unaliased: [], keptAliases: [], problem: "", ...over });

// ---------------------------------------------------------------------------------------------------------------------
// words and numbers

test("plural helpers: subjects and lessons", () => {
  assert.deepEqual([1, 2, 4, 5, 11, 21, 22, 25].map(subjectsWord), [
    "1 предмет", "2 предмета", "4 предмета", "5 предметов", "11 предметов", "21 предмет", "22 предмета", "25 предметов",
  ]);
  assert.deepEqual([1, 2, 5, 14].map(lessonsWord), ["1 пара", "2 пары", "5 пар", "14 пар"]);
});

test("kinds in words with counts; unknown alone says nothing the lessons do not", () => {
  assert.equal(kindsText([kc("lecture", 12), kc("seminar", 6), kc("lab", 1)]), "лекции 12 · семинары 6 · лабораторные 1");
  assert.equal(kindsText([kc("unknown", 5)]), "");
  assert.equal(kindsText([kc("lecture", 2), kc("unknown", 1)]), "лекции 2 · пары 1");
  assert.equal(metaText(matan()), "14 пар · лекции 12 · семинары 2");
  assert.equal(metaText({ lessons: 0, recordings: 2, kinds: [] }), "2 записи");
});

test("the note about omitted rows agrees with the number", () => {
  assert.equal(omittedNote(1), "Ещё 1 редкая строка расписания не показана");
  assert.equal(omittedNote(3), "Ещё 3 редкие строки расписания не показаны");
  assert.equal(omittedNote(7), "Ещё 7 редких строк расписания не показаны");
});

// ---------------------------------------------------------------------------------------------------------------------
// selection and counters

test("everything is checked by default and doubtful members are merged", () => {
  const sel = initialSelection(reply().groups);
  assert.deepEqual([...sel.values()].map((r) => r.checked), [true, true, true]);
  assert.equal(sel.get("fizika").members.get("fizka").mode, "keep");
  const plan = planOf(reply().groups, sel, taken);
  assert.equal(plan.creates, 2); // Матан, Физика (merged)
  assert.equal(plan.additions, 2); // two spellings go to «Компьютерные сети»
  assert.equal(plan.problems.size, 0);
  const b = mainButton(plan);
  assert.deepEqual([b.label, b.disabled, b.reason, b.extra], ["Создать 2 предмета", false, "", "и добавить названия: 2"]);
});

test("unchecked rows are not counted", () => {
  const groups = reply().groups;
  let sel = setChecked(initialSelection(groups), "matan", false);
  assert.equal(planOf(groups, sel, taken).creates, 1);
  assert.equal(mainButton(planOf(groups, sel, taken)).label, "Создать 1 предмет");
  sel = setChecked(setChecked(sel, "fizika", false), "seti", false);
  const b = mainButton(planOf(groups, sel, taken));
  assert.deepEqual([b.label, b.disabled, b.reason], ["Создать предметы", true, "Отметьте хотя бы один предмет"]);
});

test("only additions: the button is about the names", () => {
  const groups = reply().groups;
  let sel = setChecked(setChecked(initialSelection(groups), "matan", false), "fizika", false);
  const b = mainButton(planOf(groups, sel, taken));
  assert.deepEqual([b.label, b.disabled, b.reason], ["Добавить названия: 2", false, ""]);
});

test("a member taken separately is one more subject; skipped, none; a second click returns it", () => {
  const groups = reply().groups;
  let sel = toggleMember(initialSelection(groups), "fizika", "fizka", "separate");
  assert.equal(sel.get("fizika").members.get("fizka").mode, "separate");
  assert.equal(planOf(groups, sel, taken).creates, 3);
  sel = toggleMember(sel, "fizika", "fizka", "skip");
  assert.equal(sel.get("fizika").members.get("fizka").mode, "skip");
  assert.equal(planOf(groups, sel, taken).creates, 2);
  sel = toggleMember(sel, "fizika", "fizka", "skip");
  assert.equal(sel.get("fizika").members.get("fizka").mode, "keep");
  // An unchecked group takes its separate members with it.
  sel = setChecked(toggleMember(sel, "fizika", "fizka", "separate"), "fizika", false);
  assert.equal(planOf(groups, sel, taken).creates, 1);
});

test("a group whose members all left adds nothing (an alias_of group with one doubtful member)", () => {
  const g = seti({ doubtful: true, members: [member({ key: "seti", name: "Сети", spellings: ["Сети"], doubtful: true, link: "typo" })] });
  let sel = initialSelection([g]);
  assert.equal(planOf([g], sel, taken).additions, 1);
  sel = toggleMember(sel, "seti", "seti", "separate");
  const plan = planOf([g], sel, taken);
  assert.deepEqual([plan.creates, plan.additions], [1, 0]);
});

test("untouched groups use the schedule's own selection state of a Map: keys like __proto__ are safe", () => {
  const g = matan({ key: "__proto__", members: [member({ key: "__proto__" })] });
  const sel = setName(initialSelection([g]), "__proto__", "Новое");
  assert.equal(sel.get("__proto__").name, "Новое");
  assert.equal(buildChoices([g], sel)[0].group, "__proto__");
});

// ---------------------------------------------------------------------------------------------------------------------
// names

test("name keys: trimmed, one space, case and ё folded", () => {
  assert.equal(nameKey("  Ёлка   Мат "), nameKey("елка мат"));
});

test("an empty name, a name equal to another new one or to an existing subject blocks the button with a reason", () => {
  const groups = reply().groups;
  let sel = setName(initialSelection(groups), "matan", "   ");
  let plan = planOf(groups, sel, taken);
  assert.equal(plan.problems.get(groupItem("matan")), "empty");
  let b = mainButton(plan);
  assert.deepEqual([b.label, b.disabled, b.reason], ["Создать 2 предмета", true, "Исправьте названия в отмеченных строках"]);

  sel = setName(initialSelection(groups), "matan", " ФИЗИКА ");
  plan = planOf(groups, sel, taken);
  assert.equal(plan.problems.get(groupItem("matan")), "duplicate");
  assert.equal(plan.problems.get(groupItem("fizika")), "duplicate");

  sel = setName(initialSelection(groups), "matan", "алгебра");
  assert.equal(planOf(groups, sel, taken).problems.get(groupItem("matan")), "exists");

  sel = setName(initialSelection(groups), "matan", "я".repeat(201));
  assert.equal(planOf(groups, sel, taken).problems.get(groupItem("matan")), "tooLong");
});

test("a problem of an unchecked row, or of a name not used, does not block", () => {
  const groups = reply().groups;
  const sel = setChecked(setName(initialSelection(groups), "matan", ""), "matan", false);
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
  // The name of a group whose members all left is not created.
  const only = fizika({ members: [member({ key: "fizka", name: "Физ-ка", doubtful: true, link: "typo" })] });
  const s2 = toggleMember(setName(initialSelection([only]), "fizika", ""), "fizika", "fizka", "skip");
  assert.equal(planOf([only], s2, taken).problems.size, 0);
});

test("a member taken separately is named and checked like a row", () => {
  const groups = reply().groups;
  let sel = toggleMember(initialSelection(groups), "fizika", "fizka", "separate");
  sel = setMemberName(sel, "fizika", "fizka", "физика");
  assert.equal(planOf(groups, sel, taken).problems.get(memberItem("fizika", "fizka")), "duplicate");
  sel = setMemberName(sel, "fizika", "fizka", "Физкультура");
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
});

// ---------------------------------------------------------------------------------------------------------------------
// choices

test("buildChoices: the exact shape for unchecked, renamed, separate and skipped", () => {
  const groups = reply().groups;
  let sel = setName(initialSelection(groups), "matan", "  Математический анализ ");
  sel = toggleMember(sel, "fizika", "fizka", "separate");
  sel = setChecked(sel, "seti", false);
  assert.deepEqual(buildChoices(groups, sel), [
    { group: "matan", action: "create", name: "Математический анализ", members: [] },
    { group: "fizika", action: "create", name: "", members: [{ key: "fizka", action: "separate", name: "" }] },
    { group: "seti", action: "skip", name: "", members: [] },
  ]);
});

test("buildChoices: untouched names are empty, alias_of keeps its action, skip and rename of a separate member", () => {
  const groups = reply().groups;
  assert.deepEqual(buildChoices(groups, initialSelection(groups)), [
    { group: "matan", action: "create", name: "", members: [] },
    { group: "fizika", action: "create", name: "", members: [] },
    { group: "seti", action: "alias_of", name: "", members: [] },
  ]);
  let sel = setName(initialSelection(groups), "seti", "не уйдёт"); // an alias_of group has no name to send
  sel = toggleMember(sel, "fizika", "fizka", "skip");
  assert.deepEqual(buildChoices(groups, sel)[2], { group: "seti", action: "alias_of", name: "", members: [] });
  assert.deepEqual(buildChoices(groups, sel)[1].members, [{ key: "fizka", action: "skip", name: "" }]);
  sel = setMemberName(toggleMember(initialSelection(groups), "fizika", "fizka", "separate"), "fizika", "fizka", " Физкультура ");
  assert.deepEqual(buildChoices(groups, sel)[1].members, [{ key: "fizka", action: "separate", name: "Физкультура" }]);
});

test("buildChoices never sends a non-doubtful member's choice", () => {
  const groups = [fizika()];
  const sel = initialSelection(groups);
  const lead = sel.get("fizika").members;
  const forged = new Map(sel).set("fizika", { ...sel.get("fizika"), members: new Map(lead).set("fizika", { mode: "skip", name: "" }) });
  assert.deepEqual(buildChoices(groups, forged)[0].members, []);
});

// ---------------------------------------------------------------------------------------------------------------------
// the proposal states

test("an empty proposal says everything is done, with only «Закрыть»", () => {
  const s = initialState(reply({ groups: [] }));
  assert.equal(s.view, "result");
  assert.deepEqual(s.notice, { tone: "ok", text: "Все предметы из расписания уже заведены", lines: [], steps: [] });
  const o = initialState(reply({ groups: [], omitted: 4 }));
  assert.deepEqual(o.notice.lines, ["Ещё 4 редкие строки расписания не показаны"]);
  assert.deepEqual(o.notice.steps, ["refresh"]);
});

test("an unavailable proposal offers «Повторить»-like refresh with plain words", () => {
  const s = initialState(reply({ groups: [], problem: "unavailable" }));
  assert.equal(s.view, "result");
  assert.equal(s.notice.tone, "fail");
  assert.deepEqual(s.notice.steps, ["refresh"]);
  assert.doesNotMatch(s.notice.text, /unavailable/);
});

test("the offer: checking, none, groups; a failed call or an unavailable core offers nothing", () => {
  assert.equal(offerGroups(offerChecking), null);
  assert.equal(offerGroups({ loaded: true, reply: null }), 0);
  assert.equal(offerGroups({ loaded: true, reply: reply({ groups: [], problem: "unavailable" }) }), 0);
  assert.equal(offerGroups({ loaded: true, reply: reply() }), 3);
});

// ---------------------------------------------------------------------------------------------------------------------
// create: states and outcomes

test("a call starts only from idle, and a second «saving» is ignored (no double send)", () => {
  const s0 = initialState(reply());
  const s1 = reduce(s0, { type: "saving" });
  assert.equal(s1.phase, "saving");
  assert.equal(reduce(s1, { type: "saving" }), s1);
  // Edits and calls are ignored while saving.
  assert.equal(reduce(s1, { type: "check", group: "matan", checked: false }), s1);
  assert.equal(reduce(s1, { type: "refreshing" }), s1);
  assert.equal(reduce(s1, { type: "undoing" }), s1);
});

test("success: the done line, «Отменить всё» only when something was done, the skipped rows explained", () => {
  const s = reduce(reduce(initialState(reply()), { type: "saving" }), {
    type: "created",
    reply: createReply({
      aliased: [{ subjectId: "s9", aliases: ["Сети"] }],
      skipped: [
        { key: "fizika", subjectId: "", reason: "name_taken" },
        { key: "", subjectId: "s9", reason: "too_many_aliases" },
        { key: "", subjectId: "s1", reason: "subject_gone" },
      ],
    }),
  });
  assert.deepEqual([s.phase, s.view], ["idle", "result"]);
  assert.equal(doneText(s.batches), "Заведено 1 предмет · Добавлено названий: 1");
  assert.deepEqual(s.notice.lines, [
    "„Физика“ не заведён: такое название уже занято",
    "У предмета уже много написаний — новые не добавлены",
    "Предмет был удалён — строка пропущена",
  ]);
  assert.deepEqual(s.notice.steps, ["refresh"]);
});

test("a skipped row is named as the student renamed it", () => {
  const renamed = reduce(initialState(reply()), { type: "rename", group: "matan", name: "Мой матан" });
  const s = reduce(reduce(renamed, { type: "saving" }), {
    type: "created",
    reply: createReply({ created: [], skipped: [{ key: "matan", subjectId: "", reason: "name_taken" }] }),
  });
  assert.equal(s.notice.text, "Ничего не заведено");
  assert.deepEqual(s.notice.lines, ["„Мой матан“ не заведён: такое название уже занято"]);
  assert.equal(doneText(s.batches), "");
  assert.equal(s.batches.length, 0);
});

test("a plain success with nothing skipped has no notice besides the done line", () => {
  const s = reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply() });
  assert.equal(s.notice, null);
  assert.equal(doneText(s.batches), "Заведено 1 предмет");
  assert.equal(doneText([mergeBatch([], "t", { created: [created("a"), created("b")], aliased: [] })[0]]), "Заведено 2 предмета");
});

const problems = [
  ["stale", "result", "Расписание или предметы изменились", ["refresh"]],
  ["invalid", "result", "Список уже нельзя применить — он устарел", ["refresh"]],
  ["name_taken", "list", "Такое название уже занято. Измените название в строке и повторите", []],
  ["busy", "list", "Предыдущее действие ещё выполняется", ["retryCreate"]],
  ["unavailable", "list", reviewTexts.unavailable, ["retryCreate"]],
];

for (const [problem, view, text, steps] of problems) {
  test(`create problem ${problem}: view, text and button; the edits and the token stay`, () => {
    let s = reduce(initialState(reply()), { type: "rename", group: "matan", name: "Мой матан" });
    s = reduce(reduce(s, { type: "saving" }), { type: "created", reply: createReply({ created: [], problem }) });
    assert.equal(s.phase, "idle");
    assert.equal(s.view, view);
    assert.deepEqual(s.notice, { tone: "fail", text, lines: [], steps });
    assert.equal(s.token, "tok1");
    assert.equal(s.selection.get("matan").name, "Мой матан");
    assert.equal(doneText(s.batches), ""); // nothing was done, so no «Отменить всё»
  });
}

test("create problems say nothing technical", () => {
  for (const [, , text] of problems) {
    assert.doesNotMatch(text, /stale|invalid|name_taken|busy|unavailable|token/i);
  }
  assert.doesNotMatch(reviewTexts.thrown, /error|reject|parse/i);
});

test("unavailable with a done part keeps «Отменить всё» (the done part comes with the problem)", () => {
  const s = reduce(reduce(initialState(reply()), { type: "saving" }), {
    type: "created",
    reply: createReply({ aliased: [{ subjectId: "s9", aliases: ["Сети"] }], problem: "unavailable" }),
  });
  assert.equal(s.view, "list");
  assert.deepEqual(s.notice.steps, ["retryCreate"]);
  assert.equal(doneText(s.batches), "Заведено 1 предмет · Добавлено названий: 1");
});

test("a retry merges into the batch of its token; a new token is a new batch", () => {
  let b = mergeBatch([], "t1", { created: [created("a")], aliased: [{ subjectId: "s9", aliases: ["Сети"] }] });
  b = mergeBatch(b, "t1", { created: [created("a"), created("b", "fizika", "Физика")], aliased: [{ subjectId: "s9", aliases: ["Сети", "Сети (лек.)"] }] });
  assert.equal(b.length, 1);
  assert.deepEqual(b[0].created.map((c) => c.id), ["a", "b"]);
  assert.deepEqual(b[0].aliased, [{ subjectId: "s9", aliases: ["Сети", "Сети (лек.)"] }]);
  b = mergeBatch(b, "t2", { created: [created("c")], aliased: [] });
  assert.deepEqual(b.map((x) => x.token), ["t1", "t2"]);
  assert.equal(mergeBatch(b, "t3", { created: [], aliased: [] }).length, 2);
});

test("an unreadable reply of create is treated like unavailable and keeps the earlier done part", () => {
  const partial = reduce(reduce(initialState(reply()), { type: "saving" }), {
    type: "created",
    reply: createReply({ problem: "busy" }),
  });
  const s = reduce(reduce(partial, { type: "saving" }), { type: "createThrown" });
  assert.equal(s.view, "list");
  assert.equal(s.notice.text, reviewTexts.thrown);
  assert.deepEqual(s.notice.steps, ["retryCreate"]);
  assert.equal(doneText(s.batches), "Заведено 1 предмет");
});

test("stale after a done part: the list is rebuilt from scratch, the done part and its undo stay", () => {
  let s = reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ problem: "stale" }) });
  assert.equal(s.view, "result");
  assert.equal(doneText(s.batches), "Заведено 1 предмет");
  s = reduce(reduce(s, { type: "refreshing" }), { type: "refreshed", reply: reply({ token: "tok2", groups: [seti()] }) });
  assert.deepEqual([s.view, s.token, s.groups.length, s.notice], ["list", "tok2", 1, null]);
  assert.equal(s.selection.get("seti").checked, true);
  assert.equal(doneText(s.batches), "Заведено 1 предмет");
});

test("a refresh that cannot read the answer offers the refresh again", () => {
  const s = reduce(reduce(initialState(reply()), { type: "refreshing" }), { type: "refreshThrown" });
  assert.equal(s.view, "result");
  assert.deepEqual(s.notice.steps, ["refresh"]);
});

test("createFlow sends the token and the built choices; a rejected call becomes an action", async () => {
  const calls = [];
  const s = reduce(initialState(reply()), { type: "check", group: "seti", checked: false });
  const ok = await createFlow({ create: async (t, c) => (calls.push([t, c]), createReply()) }, s);
  assert.equal(ok.type, "created");
  assert.equal(calls[0][0], "tok1");
  assert.deepEqual(calls[0][1], buildChoices(s.groups, s.selection));
  const bad = await createFlow({ create: async () => { throw new Error("parse: schedule create secret-name"); } }, s);
  assert.deepEqual(bad, { type: "createThrown" });
});

test("refreshFlow", async () => {
  assert.equal((await refreshFlow({ refresh: async () => reply() })).type, "refreshed");
  assert.equal((await refreshFlow({ refresh: async () => { throw new Error("x"); } })).type, "refreshThrown");
});

// ---------------------------------------------------------------------------------------------------------------------
// undo

const withBatch = () =>
  reduce(reduce(initialState(reply()), { type: "saving" }), {
    type: "created",
    reply: createReply({ aliased: [{ subjectId: "s9", aliases: ["Сети"] }] }),
  });

test("undo success: what was removed and what was kept, one line each", async () => {
  const s0 = withBatch();
  const action = await undoFlow(
    {
      undo: async () =>
        undoReply({
          removed: ["a", "b"],
          kept: [{ id: "c", name: "Матан", reason: "has_files" }, { id: "d", name: "Физика", reason: "has_notes" }],
          unaliased: [{ subjectId: "s9", aliases: ["Сети"] }],
          keptAliases: [
            { subjectId: "s9", aliases: ["x"], reason: "stale" },
            { subjectId: "gone", aliases: ["y"], reason: "stale" },
          ],
        }),
    },
    s0.batches,
    names,
  );
  const s = reduce(reduce(s0, { type: "undoing" }), action);
  assert.equal(s.phase, "idle");
  assert.equal(s.batches.length, 0);
  assert.equal(doneText(s.batches), ""); // no second «Отменить всё»
  assert.deepEqual(s.notice, {
    tone: "ok",
    text: "Отменено: убрано 2 предмета · убрано названий: 1",
    lines: [
      "В „Матан“ уже есть файл — оставлен",
      "В „Физика“ уже есть конспект — оставлен",
      "Названия у „Компьютерные сети“ оставлены: расписание изменилось",
      "Названия у предмета оставлены: расписание изменилось",
    ],
    steps: ["refresh"],
  });
});

test("undo with nothing removed says so", async () => {
  const s0 = withBatch();
  const action = await undoFlow({ undo: async () => undoReply({ kept: [{ id: "c", name: "Матан", reason: "has_files" }] }) }, s0.batches, names);
  assert.equal(reduce(s0, action).notice.text, "Ничего не убрано");
});

test("undo invalid: «Отменить» is gone for good, the way out is the list of subjects", async () => {
  const s0 = withBatch();
  const s = reduce(reduce(s0, { type: "undoing" }), await undoFlow({ undo: async () => undoReply({ problem: "invalid" }) }, s0.batches, names));
  assert.deepEqual(s.notice, {
    tone: "fail",
    text: "Отменить уже нельзя — приложение перезапускалось. Заведённые предметы остались в списке",
    lines: [],
    steps: ["toSubjects"],
  });
  assert.equal(s.batches.length, 0);
});

for (const [problem, text] of [["busy", "Ещё идёт предыдущее действие"], ["unavailable", reviewTexts.unavailable]]) {
  test(`undo ${problem}: the text, «Повторить», and the token is kept`, async () => {
    const s0 = withBatch();
    const s = reduce(reduce(s0, { type: "undoing" }), await undoFlow({ undo: async () => undoReply({ problem }) }, s0.batches, names));
    assert.deepEqual(s.notice, { tone: "fail", text, lines: [], steps: ["retryUndo"] });
    assert.deepEqual(s.batches, s0.batches);
    assert.equal(s.phase, "idle");
  });
}

test("an unreadable reply of undo keeps the token and «Отменить всё»", async () => {
  const s0 = withBatch();
  const s = reduce(reduce(s0, { type: "undoing" }), await undoFlow({ undo: async () => { throw new Error("x"); } }, s0.batches, names));
  assert.equal(s.notice.text, reviewTexts.thrown);
  assert.deepEqual(s.notice.steps, ["retryUndo"]);
  assert.deepEqual(s.batches, s0.batches);
});

test("undo of several batches goes in order and stops at the first problem, keeping the rest", async () => {
  const batches = [
    { token: "t1", created: [created("a")], aliased: [] },
    { token: "t2", created: [created("b")], aliased: [] },
    { token: "t3", created: [created("c")], aliased: [] },
  ];
  const calls = [];
  const action = await undoFlow(
    { undo: async (t) => (calls.push(t), t === "t2" ? undoReply({ problem: "busy" }) : undoReply({ removed: [t] })) },
    batches,
    names,
  );
  assert.deepEqual(calls, ["t1", "t2"]);
  assert.deepEqual(action.result.remaining.map((b) => b.token), ["t2", "t3"]);
  assert.equal(action.result.reply.problem, "busy");
});

test("undo without anything done is not started", () => {
  const s0 = initialState(reply());
  assert.equal(reduce(s0, { type: "undoing" }), s0);
});

// ---------------------------------------------------------------------------------------------------------------------
// SSR of the panel

const on = new Proxy({}, { get: () => noop });
const view = (state, extra = {}) => html(h(ReviewView, { state, names, on, ...extra }));
const count = (s, re) => (s.match(re) ?? []).length;

test("the panel: a row per group, all checked, names editable, facts and spellings small, one primary button", () => {
  const out = view(initialState(reply()));
  assert.equal(count(out, /class="sched-row"/g), 3);
  assert.equal(count(out, /type="checkbox"[^>]*checked=""/g) + count(out, /checked=""[^>]*type="checkbox"/g), 3);
  assert.match(out, /value="Матан"/);
  assert.match(out, /value="Физика"/);
  assert.match(out, /14 пар · лекции 12 · семинары 2/);
  assert.match(out, /Написания: Матанализ \(лек\.\)/);
  assert.match(out, /Создать 2 предмета/);
  assert.equal(count(out, /class="btn primary/g), 1);
  assert.match(out, /Закрыть/);
  assert.doesNotMatch(out, /<h[1-6]/); // no headings inside the panel
  assert.match(out, /aria-label="Предметы из расписания"/);
});

test("the panel: the doubtful group is marked and its member has two toggles", () => {
  const out = view(initialState(reply()));
  assert.match(out, /похоже на один предмет/);
  assert.match(out, /Физ-ка/);
  assert.equal(count(out, /aria-pressed="false"/g), 2);
  assert.match(out, />Отдельно</);
  assert.match(out, />Не заводить</);
  // A pressed «Отдельно» turns the member's name into a field.
  const sel = toggleMember(initialSelection(reply().groups), "fizika", "fizka", "separate");
  const out2 = view({ ...initialState(reply()), selection: sel });
  assert.match(out2, /aria-pressed="true"/);
  assert.match(out2, /aria-label="Название отдельного предмета"/);
  assert.match(out2, /Создать 3 предмета/);
});

test("the panel: a group of an existing subject says where it goes and has no name field", () => {
  const out = view(initialState(reply({ groups: [seti()] })));
  assert.match(out, /Добавить к „Компьютерные сети“/);
  assert.doesNotMatch(out, /aria-label="Название предмета"/);
  assert.match(out, /Сети \(лек\.\) · Сети/);
  assert.match(out, /Добавить названия: 2/);
  assert.match(out, /aria-label="Добавить названия к предмету"/);
});

test("the panel: nothing selected disables the button and says why in the text beside it", () => {
  const s = initialState(reply());
  const none = { ...s, selection: setChecked(setChecked(setChecked(s.selection, "matan", false), "fizika", false), "seti", false) };
  const out = view(none);
  assert.match(out, /<button[^>]*disabled=""[^>]*>[^]*?Создать предметы/);
  assert.match(out, /Отметьте хотя бы один предмет/);
  assert.equal(count(out, /class="btn primary/g), 1);
});

test("the panel: a name error stands beside the field with aria-invalid and aria-describedby, and blocks the button", () => {
  const s = initialState(reply());
  const out = view({ ...s, selection: setName(s.selection, "matan", "") });
  assert.match(out, /aria-invalid="true"/);
  assert.match(out, /aria-describedby="[^"]+"[^>]*>|aria-invalid="true"[^>]*aria-describedby="[^"]+"/);
  assert.match(out, /Введите название/);
  assert.match(out, /Исправьте названия в отмеченных строках/);
});

test("the panel: saving shows «Создаётся…», disables the controls and sets aria-busy", () => {
  const out = view(reduce(initialState(reply()), { type: "saving" }));
  assert.match(out, /aria-busy="true"/);
  assert.match(out, /Создаётся…/);
  assert.equal(count(out, /<input[^>]*disabled=""/g), 5); // 3 checkboxes and the 2 name fields
  assert.equal(count(out, /<button[^>]*disabled=""/g), 4); // the main button, «Закрыть» and the 2 member toggles
});

test("the panel: the outcome is announced in a polite status, failures in an alert, each with its button", () => {
  const ok = view(withBatch());
  assert.match(ok, /role="status" aria-live="polite"/);
  assert.match(ok, /Заведено 1 предмет · Добавлено названий: 1/);
  assert.match(ok, />Отменить всё</);
  assert.doesNotMatch(ok, /Создать \d/); // the list is gone, only «Закрыть» remains
  const stale = view(reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ created: [], problem: "stale" }) }));
  assert.match(stale, /role="alert"[^]*Расписание или предметы изменились[^]*Обновить список/);
  assert.doesNotMatch(stale, />Отменить всё</);
  const busy = view(reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ created: [], problem: "busy" }) }));
  assert.match(busy, /Предыдущее действие ещё выполняется[^]*Повторить/);
  assert.match(busy, /Создать 2 предмета/); // the choices stay
});

test("the panel: empty proposal and omitted rows", () => {
  const out = view(initialState(reply({ groups: [], omitted: 2 })));
  assert.match(out, /Все предметы из расписания уже заведены/);
  assert.match(out, /Ещё 2 редкие строки расписания не показаны/);
  assert.match(out, />Обновить список</);
  assert.doesNotMatch(out, /class="btn primary/);
  const withRows = view(initialState(reply({ omitted: 3 })));
  assert.match(withRows, /Ещё 3 редкие строки расписания не показаны — после создания нажмите „Обновить список“/);
});

test("the panel: untrusted schedule strings are text, never markup", () => {
  const evil = "<img src=x onerror=alert(1)>";
  const g = matan({
    name: evil, aliases: [evil, "<b>x</b>"],
    members: [member({ name: evil, spellings: [evil] })],
  });
  const d = fizika({
    name: evil,
    members: [member({ key: "fizika", name: evil }), member({ key: "fizka", name: evil, doubtful: true, link: "typo" })],
  });
  const s = seti({ subjectName: evil, aliases: [evil] });
  const out = view(initialState(reply({ groups: [g, d, s] })));
  assert.doesNotMatch(out, /<img|<b>x/);
  assert.match(out, /&lt;img src=x onerror=alert\(1\)&gt;/);
  // Kept subjects and skipped rows of the outcome are text too.
  const result = reduce(withBatch(), {
    type: "undone",
    result: { reply: undoReply({ removed: ["a"], kept: [{ id: "c", name: evil, reason: "has_files" }] }), remaining: [], thrown: false, names },
  });
  const out2 = view(result);
  assert.doesNotMatch(out2, /<img/);
  assert.match(out2, /В „&lt;img/);
});

test("the controller renders its first state without a DOM", () => {
  const out = html(h(ScheduleSubjectsReview, { reply: reply(), subjectNames: names, api: {}, onChanged: noop, onClose: noop }));
  assert.match(out, /Создать 2 предмета/);
});

// ---------------------------------------------------------------------------------------------------------------------
// the empty list and the page's offer

const offer = (state) => ({ state, onReview: noop, onImport: noop });
const actions = (state) => html(h(EmptySubjectsActions, { offer: offer(state), addLabel: "Предмет", onAdd: noop }));
const primaries = (s) => count(s, /class="btn primary/g);

test("empty list with groups: the schedule button with the count is the only primary", () => {
  const out = actions({ loaded: true, reply: reply({ groups: [matan(), fizika()] }) });
  assert.match(out, /Завести предметы из расписания/);
  assert.match(out, /<span class="sched-cnt num">2<\/span>/);
  assert.equal(primaries(out), 1);
  assert.ok(out.indexOf("Завести предметы") < out.indexOf("Предмет<")); // the primary first
  assert.match(out, /class="btn"[^>]*>[^]*Предмет/); // «Предмет» is secondary
});

test("empty list without a schedule: «Импортировать расписание» is the primary", () => {
  for (const o of [{ loaded: true, reply: reply({ groups: [] }) }, { loaded: true, reply: null }, { loaded: true, reply: reply({ groups: [], problem: "unavailable" }) }]) {
    const out = actions(o);
    assert.match(out, /Импортировать расписание/);
    assert.doesNotMatch(out, /Завести предметы/);
    assert.equal(primaries(out), 1);
  }
});

test("empty list while checking: a small indicator, no second primary", () => {
  const out = actions(offerChecking);
  assert.match(out, /Проверяется расписание…/);
  assert.equal(primaries(out), 0);
});

test("the empty state of «Предметы» through SubjectList: why it is empty and one primary", () => {
  const out = html(h(SubjectList, { subjects: [], onOpen: noop, onAdd: noop, offer: offer({ loaded: true, reply: reply() }) }));
  assert.match(out, /Предметов пока нет/);
  assert.match(out, /Предметы заводятся из расписания и записей, либо вручную/);
  assert.equal(primaries(out), 1);
  assert.match(out, /Завести предметы из расписания/);
  // Without an offer the list is as before.
  const plain = html(h(SubjectList, { subjects: [], onOpen: noop, onAdd: noop }));
  assert.equal(primaries(plain), 1);
  assert.doesNotMatch(plain, /Завести предметы/);
});

test("a list with subjects shows no schedule button of its own (the header has it)", () => {
  const s = { id: "s1", name: "Алгебра", semester: "2026-autumn", semesterLabel: "Осень 2026", aliases: [], folders: [], teachers: [], schedule: [], version: 1 };
  const out = html(h(SubjectList, { subjects: [s], onOpen: noop, onAdd: noop, offer: offer({ loaded: true, reply: reply() }) }));
  assert.doesNotMatch(out, /Завести предметы/);
});
