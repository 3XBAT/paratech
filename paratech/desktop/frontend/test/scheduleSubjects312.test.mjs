// TASK-312: the view model of the review of subjects from the schedule (selection, counters, choices, the state machine
// and its outcomes, the undo) and the SSR of the panel, the line after it, the empty list and the page hints.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  addSubjectVariant,
  buildChoices,
  carrySelection,
  closedState,
  createFlow,
  createGate,
  doneText,
  escCloses,
  groupItem,
  initialSelection,
  initialState,
  kindsText,
  lessonsWord,
  lineSteps,
  mainButton,
  memberItem,
  mergeBatch,
  metaText,
  keepScheduleReview,
  nameKey,
  nextMode,
  offerChecking,
  offerGroups,
  offerStatus,
  omittedAfter,
  omittedLeft,
  omittedNote,
  planOf,
  reduce,
  refreshFlow,
  reviewTexts,
  setChecked,
  setMemberMode,
  setMemberName,
  setName,
  showCheckFailed,
  subjectsChanged,
  subjectsWord,
  takenOf,
  undoFlow,
} from "../.test-build/scheduleSubjectsView.js";
import {
  EmptySubjectsActions,
  KeptUndoLine,
  ReviewView,
  ScheduleCheckFailedLine,
  ScheduleOfferLine,
  ScheduleSubjectsReview,
} from "../.test-build/pages/ScheduleSubjectsReview.js";
import { ScheduleNextHint } from "../.test-build/pages/SchedulePage.js";
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
const subjects = [
  { id: "s9", name: "Компьютерные сети", aliases: [] },
  { id: "s1", name: "Алгебра", aliases: ["Алг."] },
];
const names = new Map(subjects.map((s) => [s.id, s.name]));
const noState = { token: "tok1", batches: [], attempt: [] };
const taken = takenOf(subjects, noState);

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

test("kinds in words with counts, the unknown kind last and called «без типа»", () => {
  assert.equal(kindsText([kc("lecture", 12), kc("seminar", 6), kc("lab", 1)]), "лекции 12 · семинары 6 · лабораторные 1");
  assert.equal(kindsText([kc("unknown", 5)]), "");
  assert.equal(kindsText([kc("lecture", 2), kc("unknown", 1)]), "лекции 2 · без типа 1");
  assert.equal(kindsText([kc("unknown", 2), kc("seminar", 6), kc("lecture", 8)]), "лекции 8 · семинары 6 · без типа 2");
  assert.equal(metaText(matan()), "14 пар · лекции 12 · семинары 2");
  assert.equal(metaText({ lessons: 14, recordings: 0, kinds: [kc("unknown", 2), kc("seminar", 6), kc("lecture", 8)] }), "14 пар · лекции 8 · семинары 6 · без типа 2");
  assert.equal(metaText({ lessons: 0, recordings: 2, kinds: [] }), "2 записи");
});

test("the notes about omitted names agree with the number and avoid «редкие строки»", () => {
  assert.equal(omittedNote(1), "Ещё 1 название из расписания появится в списке после создания этих предметов");
  assert.equal(omittedNote(3), "Ещё 3 названия из расписания появятся в списке после создания этих предметов");
  assert.equal(omittedNote(7), "Ещё 7 названий из расписания появятся в списке после создания этих предметов");
  assert.equal(omittedLeft(1), "Ещё 1 название из расписания не показано");
  assert.equal(omittedLeft(2), "Ещё 2 названия из расписания не показаны");
  for (const n of [1, 2, 5]) {
    assert.doesNotMatch(omittedNote(n) + omittedLeft(n) + omittedAfter(n), /редк|„|“/);
  }
});

test("after a create the omitted names wait for a choice (no «появятся» promise)", () => {
  assert.equal(omittedAfter(1), "Ещё 1 название из расписания ждёт выбора");
  assert.equal(omittedAfter(3), "Ещё 3 названия из расписания ждут выбора");
  assert.equal(omittedAfter(7), "Ещё 7 названий из расписания ждут выбора");
});

test("the glossary of the panel says «создать» (no «завести» in the choices and outcomes)", () => {
  assert.equal(reviewTexts.skip, "Не создавать");
  assert.equal(reviewTexts.skipped, "Не будет создан");
  assert.equal(reviewTexts.checkboxAria("Х"), "Создать предмет «Х»");
  assert.equal(reviewTexts.nothingCreated, "Ничего не создано");
  assert.equal(reviewTexts.open, "Завести предметы из расписания"); // named by the task
  assert.equal(reviewTexts.nextHint(5), reviewTexts.offerLine(5));
  assert.equal(reviewTexts.emptyWithGroups(3), "В расписании найдено предметов: 3. Их можно создать за один шаг");
});

test("no German quotes and no storage jargon in any text of the review", () => {
  const all = Object.values(reviewTexts).map((t) => (typeof t === "function" ? t(3) : t));
  all.push(reviewTexts.checkboxAria("Х"), reviewTexts.checkboxAddAria("Х"));
  for (const t of all) {
    assert.doesNotMatch(t, /„|“|локальн/i);
  }
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
  assert.deepEqual([b.label, b.disabled, b.reason, b.extra], ["Создать 2 предмета", false, "", "и добавить другие названия: 2"]);
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

test("only additions: the button is about the other names", () => {
  const groups = reply().groups;
  const sel = setChecked(setChecked(initialSelection(groups), "matan", false), "fizika", false);
  const b = mainButton(planOf(groups, sel, taken));
  assert.deepEqual([b.label, b.disabled, b.reason], ["Добавить другие названия: 2", false, ""]);
});

test("a member taken separately is one more subject; skipped, none; «Вместе» returns it", () => {
  const groups = reply().groups;
  let sel = setMemberMode(initialSelection(groups), "fizika", "fizka", "separate");
  assert.equal(sel.get("fizika").members.get("fizka").mode, "separate");
  assert.equal(planOf(groups, sel, taken).creates, 3);
  sel = setMemberMode(sel, "fizika", "fizka", "skip");
  assert.equal(sel.get("fizika").members.get("fizka").mode, "skip");
  assert.equal(planOf(groups, sel, taken).creates, 2);
  sel = setMemberMode(sel, "fizika", "fizka", "keep");
  assert.equal(sel.get("fizika").members.get("fizka").mode, "keep");
  // An unchecked group takes its separate members with it.
  sel = setChecked(setMemberMode(sel, "fizika", "fizka", "separate"), "fizika", false);
  assert.equal(planOf(groups, sel, taken).creates, 1);
});

test("a group whose members all left adds nothing (an alias_of group with one doubtful member)", () => {
  const g = seti({ doubtful: true, members: [member({ key: "seti", name: "Сети", spellings: ["Сети"], doubtful: true, link: "typo" })] });
  let sel = initialSelection([g]);
  assert.equal(planOf([g], sel, taken).additions, 1);
  sel = setMemberMode(sel, "seti", "seti", "separate");
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
  // The field has no length cap, so the plan counts characters: 200 are fine.
  sel = setName(initialSelection(groups), "matan", "я".repeat(200));
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
});

test("the other names of an existing subject are taken too (the core refuses a new name equal to one)", () => {
  assert.ok(taken.has(nameKey("Алг.")));
  const groups = reply().groups;
  const sel = setName(initialSelection(groups), "matan", " алг. ");
  assert.equal(planOf(groups, sel, taken).problems.get(groupItem("matan")), "exists");
});

test("a new name equal to a spelling that goes to another row is a duplicate", () => {
  const groups = reply().groups;
  // «Физ-ка» is merged into the row «Физика»: the core would refuse a second subject with that name.
  let sel = setName(initialSelection(groups), "matan", "физ-ка");
  assert.equal(planOf(groups, sel, taken).problems.get(groupItem("matan")), "duplicate");
  // A spelling added to an existing subject counts as well.
  sel = setName(initialSelection(groups), "matan", "Сети");
  assert.equal(planOf(groups, sel, taken).problems.get(groupItem("matan")), "duplicate");
  // The row's own spellings are not a clash with its own name.
  sel = setName(initialSelection(groups), "fizika", "Физ-ка");
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
  // Once the member goes its own way, its spelling goes with it.
  sel = setMemberName(setMemberMode(initialSelection(groups), "fizika", "fizka", "separate"), "fizika", "fizka", "Физ-ка");
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
});

test("a problem of an unchecked row, or of a name not used, does not block", () => {
  const groups = reply().groups;
  const sel = setChecked(setName(initialSelection(groups), "matan", ""), "matan", false);
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
  // The name of a group whose members all left is not created.
  const only = fizika({ members: [member({ key: "fizka", name: "Физ-ка", doubtful: true, link: "typo" })] });
  const s2 = setMemberMode(setName(initialSelection([only]), "fizika", ""), "fizika", "fizka", "skip");
  assert.equal(planOf([only], s2, taken).problems.size, 0);
});

test("a member taken separately is named and checked like a row", () => {
  const groups = reply().groups;
  let sel = setMemberMode(initialSelection(groups), "fizika", "fizka", "separate");
  sel = setMemberName(sel, "fizika", "fizka", "физика");
  assert.equal(planOf(groups, sel, taken).problems.get(memberItem("fizika", "fizka")), "duplicate");
  sel = setMemberName(sel, "fizika", "fizka", "Физкультура");
  assert.equal(planOf(groups, sel, taken).problems.size, 0);
});

// ---------------------------------------------------------------------------------------------------------------------
// the subjects this very token made are not «already existing» (a retry after a partial create)

test("takenOf leaves out the subjects the reply listed for this token, and only for this token", () => {
  const list = [...subjects, { id: "sched-1", name: "Матан", aliases: ["Матанализ"] }];
  const batches = [{ token: "tok1", created: [created("sched-1")], aliased: [] }];
  const own = takenOf(list, { token: "tok1", batches, attempt: [] });
  assert.ok(!own.has("матан") && !own.has("матанализ"));
  assert.ok(own.has("алгебра"));
  // Another token's batch is a real subject now.
  assert.ok(takenOf(list, { token: "tok2", batches, attempt: [] }).has("матан"));
});

test("takenOf: after an unreadable reply the derived ids of the names just sent are left out; other ids are not", () => {
  const list = [...subjects, { id: "sched-77", name: "Физика", aliases: [] }, { id: "s5", name: "Химия", aliases: [] }];
  const t = takenOf(list, { token: "tok1", batches: [], attempt: ["физика", "химия"] });
  assert.ok(!t.has("физика"));
  assert.ok(t.has("химия")); // not a subject of the schedule: a real clash
  assert.ok(takenOf(list, noState).has("физика")); // nothing pending: it exists
});

test("retry after a partial create (unavailable, 1 of 2 created) still sends the call after the subjects reload", async () => {
  let s = reduce(reduce(initialState(reply({ groups: [matan(), fizika()] })), { type: "saving" }), {
    type: "created",
    reply: createReply({ created: [created("sched-1", "matan", "Матан")], problem: "unavailable" }),
  });
  assert.equal(s.view, "list");
  const reloaded = [...subjects, { id: "sched-1", name: "Матан", aliases: [] }];
  const gate = createGate(s, reloaded);
  assert.equal(gate.plan.problems.size, 0);
  assert.equal(gate.plan.creates, 2);
  assert.equal(gate.ok, true);
  assert.equal(mainButton(gate.plan).disabled, false);
  // The same list without the exclusion would block the button: the exclusion is what lets the retry through.
  assert.equal(planOf(s.groups, s.selection, takenOf(reloaded, { ...noState, token: "other" })).problems.get(groupItem("matan")), "exists");
  const calls = [];
  const action = await createFlow({ create: async (t, c) => (calls.push([t, c]), createReply()) }, s);
  assert.equal(action.type, "created");
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], "tok1"); // the same token: the core finds its own work
  // The panel shows an enabled button, not the «exists» error.
  const out = html(h(ReviewView, { state: s, subjects: reloaded, on, autoFocus: false }));
  assert.doesNotMatch(out, /Предмет с таким названием уже есть/);
  assert.doesNotMatch(out, /<button[^>]*disabled=""[^>]*>[^]*?Создать 2 предмета/);
  // A clean finish forgets what was pending.
  s = reduce(reduce(s, { type: "saving" }), { type: "created", reply: createReply({ created: [created("sched-2", "fizika", "Физика")] }) });
  assert.deepEqual(s.attempt, []);
});

test("retry after an unreadable reply: the subjects the core made under derived ids do not block it", () => {
  const s = reduce(reduce(initialState(reply({ groups: [matan(), fizika()] })), { type: "saving" }), { type: "createThrown" });
  const reloaded = [...subjects, { id: "sched-aa", name: "Матан", aliases: [] }, { id: "sched-bb", name: "Физика", aliases: [] }];
  const gate = createGate(s, reloaded);
  assert.deepEqual([gate.ok, gate.plan.problems.size], [true, 0]);
  // A subject of the same name that the schedule did not make still blocks.
  assert.equal(createGate(s, [...subjects, { id: "s5", name: "Матан", aliases: [] }]).ok, false);
});

test("the gate: no call while saving, outside the list, with a problem or with nothing chosen", () => {
  const s = initialState(reply());
  assert.equal(createGate(s, subjects).ok, true);
  assert.equal(createGate(reduce(s, { type: "saving" }), subjects).ok, false);
  assert.equal(createGate({ ...s, view: "result" }, subjects).ok, false);
  assert.equal(createGate(reduce(s, { type: "rename", group: "matan", name: "" }), subjects).ok, false);
  const none = { ...s, selection: setChecked(setChecked(setChecked(s.selection, "matan", false), "fizika", false), "seti", false) };
  assert.equal(createGate(none, subjects).ok, false);
});

// ---------------------------------------------------------------------------------------------------------------------
// choices

test("buildChoices: the exact shape for unchecked, renamed, separate and skipped", () => {
  const groups = reply().groups;
  let sel = setName(initialSelection(groups), "matan", "  Математический анализ ");
  sel = setMemberMode(sel, "fizika", "fizka", "separate");
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
  sel = setMemberMode(sel, "fizika", "fizka", "skip");
  assert.deepEqual(buildChoices(groups, sel)[2], { group: "seti", action: "alias_of", name: "", members: [] });
  assert.deepEqual(buildChoices(groups, sel)[1].members, [{ key: "fizka", action: "skip", name: "" }]);
  sel = setMemberName(setMemberMode(initialSelection(groups), "fizika", "fizka", "separate"), "fizika", "fizka", " Физкультура ");
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
  assert.deepEqual(o.notice.lines, ["Ещё 4 названия из расписания не показаны"]);
  assert.deepEqual(o.notice.steps, ["rest"]);
});

test("an unavailable proposal offers «Повторить»-like refresh with plain words", () => {
  const s = initialState(reply({ groups: [], problem: "unavailable" }));
  assert.equal(s.view, "result");
  assert.equal(s.notice.tone, "fail");
  assert.deepEqual(s.notice.steps, ["refresh"]);
  assert.doesNotMatch(s.notice.text, /unavailable/);
});

test("the offer: checking, none, groups, failed; a failed call or an unavailable core is not «no schedule»", () => {
  assert.equal(offerStatus(offerChecking), "checking");
  assert.equal(offerStatus({ loaded: true, reply: null }), "failed");
  assert.equal(offerStatus({ loaded: true, reply: reply({ groups: [], problem: "unavailable" }) }), "failed");
  assert.equal(offerStatus({ loaded: true, reply: reply({ groups: [] }) }), "none");
  assert.equal(offerStatus({ loaded: true, reply: reply() }), "groups");
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
  assert.equal(doneText(s.batches), "Создано предметов: 1 · Добавлено названий: 1");
  assert.deepEqual(s.notice.lines, [
    "«Физика» не создан: такое название уже занято",
    "У предмета уже много других названий — новые не добавлены",
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
  assert.equal(s.notice.text, "Ничего не создано");
  assert.deepEqual(s.notice.lines, ["«Мой матан» не создан: такое название уже занято"]);
  assert.equal(doneText(s.batches), "");
  assert.equal(s.batches.length, 0);
});

test("a plain success with nothing skipped has no notice besides the done line; counters, not «Заведено 1 предмет»", () => {
  const s = reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply() });
  assert.equal(s.notice, null);
  assert.equal(doneText(s.batches), "Создано предметов: 1");
  assert.equal(doneText([mergeBatch([], "t", { created: [created("a"), created("b")], aliased: [] })[0]]), "Создано предметов: 2");
  assert.equal(doneText([{ token: "t", created: [], aliased: [{ subjectId: "s9", aliases: ["a", "b"] }] }]), "Добавлено названий: 2");
});

test("a clean success with omitted names carries «Показать остальные» and the new note", () => {
  const s = reduce(reduce(initialState(reply({ omitted: 3 })), { type: "saving" }), { type: "created", reply: createReply() });
  assert.deepEqual(s.notice, { tone: "ok", text: "", lines: [omittedAfter(3)], steps: ["rest"] });
  const withSkip = reduce(reduce(initialState(reply({ omitted: 1 })), { type: "saving" }), {
    type: "created",
    reply: createReply({ skipped: [{ key: "", subjectId: "s1", reason: "subject_gone" }] }),
  });
  assert.deepEqual(withSkip.notice.steps, ["rest"]);
  assert.equal(withSkip.notice.lines.length, 2);
  // Nothing omitted: «Обновить список» as before, and nothing at all for a plain success.
  const none = reduce(reduce(initialState(reply()), { type: "saving" }), {
    type: "created",
    reply: createReply({ skipped: [{ key: "", subjectId: "s1", reason: "subject_gone" }] }),
  });
  assert.deepEqual(none.notice.steps, ["refresh"]);
});

const problems = [
  ["stale", "result", "Расписание или предметы изменились", ["refresh"]],
  ["invalid", "result", "Список уже нельзя применить — он устарел", ["refresh"]],
  ["name_taken", "list", "Пока шёл выбор, появился предмет с таким же названием. Исправьте название в отмеченной строке", []],
  ["busy", "list", "Ещё выполняется предыдущее действие — повторите через пару секунд", ["retryCreate"]],
  ["unavailable", "list", "Не удалось сохранить. Выбор сохранён — можно повторить", ["retryCreate"]],
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

test("a reply-level name_taken has no «Повторить»: the clashing row is marked after the reload, the main button is the way on", () => {
  const s = reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ created: [], problem: "name_taken" }) });
  const before = html(h(ReviewView, { state: s, subjects, on }));
  assert.match(before, /role="alert"[^]*Пока шёл выбор, появился предмет с таким же названием/);
  assert.doesNotMatch(before, /Повторить/);
  assert.match(before, /Создать 2 предмета/);
  // The subjects reload: a subject «Матан» now exists, so the row is marked and the button waits for a new name.
  const reloaded = [...subjects, { id: "s5", name: "Матан", aliases: [] }];
  assert.equal(planOf(s.groups, s.selection, takenOf(reloaded, s)).problems.get(groupItem("matan")), "exists");
  const after = html(h(ReviewView, { state: s, subjects: reloaded, on }));
  assert.match(after, /Предмет с таким названием уже есть/);
  assert.match(after, /Исправьте названия в отмеченных строках/);
});

test("subjects are reloaded after a create that did something, an unreadable one, an undo and a reply-level name_taken", () => {
  const created1 = { type: "created", reply: createReply() };
  assert.equal(subjectsChanged(created1), true);
  assert.equal(subjectsChanged({ type: "created", reply: createReply({ created: [], problem: "name_taken" }) }), true);
  assert.equal(subjectsChanged({ type: "created", reply: createReply({ created: [], problem: "stale" }) }), false);
  assert.equal(subjectsChanged({ type: "created", reply: createReply({ created: [], problem: "busy" }) }), false);
  assert.equal(subjectsChanged({ type: "createThrown" }), true);
  assert.equal(subjectsChanged({ type: "refreshed", reply: reply() }), false);
});

test("create problems say nothing technical and have one text each", () => {
  for (const [, , text] of problems) {
    assert.doesNotMatch(text, /stale|invalid|name_taken|busy|unavailable|token/i);
  }
  assert.doesNotMatch(reviewTexts.unavailable, /error|reject|parse|хранилищ/i);
  assert.doesNotMatch(reviewTexts.undoUnavailable, /error|reject|parse|хранилищ/i);
  assert.equal(reviewTexts.undoUnavailable, "Не удалось отменить. Можно повторить");
  assert.equal(reviewTexts.creating, "Создание…");
});

test("unavailable with a done part keeps «Отменить всё» (the done part comes with the problem)", () => {
  const s = reduce(reduce(initialState(reply()), { type: "saving" }), {
    type: "created",
    reply: createReply({ aliased: [{ subjectId: "s9", aliases: ["Сети"] }], problem: "unavailable" }),
  });
  assert.equal(s.view, "list");
  assert.deepEqual(s.notice.steps, ["retryCreate"]);
  assert.equal(doneText(s.batches), "Создано предметов: 1 · Добавлено названий: 1");
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
  assert.equal(s.notice.text, reviewTexts.unavailable);
  assert.deepEqual(s.notice.steps, ["retryCreate"]);
  assert.equal(doneText(s.batches), "Создано предметов: 1");
});

test("stale after a done part: the list is rebuilt from scratch, the done part and its undo stay", () => {
  let s = reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ problem: "stale" }) });
  assert.equal(s.view, "result");
  assert.equal(doneText(s.batches), "Создано предметов: 1");
  s = reduce(reduce(s, { type: "refreshing" }), { type: "refreshed", reply: reply({ token: "tok2", groups: [seti()] }) });
  assert.deepEqual([s.view, s.token, s.groups.length, s.notice], ["list", "tok2", 1, null]);
  assert.equal(s.selection.get("seti").checked, true);
  assert.equal(doneText(s.batches), "Создано предметов: 1");
});

test("«Обновить список» keeps the student's edits for the groups that are still proposed", () => {
  let s = initialState(reply());
  s = reduce(s, { type: "rename", group: "matan", name: "Мой матан" });
  s = reduce(s, { type: "check", group: "seti", checked: false });
  s = reduce(s, { type: "member", group: "fizika", member: "fizka", mode: "separate" });
  s = reduce(s, { type: "renameMember", group: "fizika", member: "fizka", name: "Физкультура" });
  const him = matan({ key: "him", name: "Химия", members: [member({ key: "him", name: "Химия" })] });
  const next = reduce(reduce(s, { type: "refreshing" }), { type: "refreshed", reply: reply({ token: "tok2", groups: [matan(), fizika(), seti(), him] }) });
  assert.equal(next.token, "tok2");
  assert.equal(next.selection.get("matan").name, "Мой матан");
  assert.equal(next.selection.get("seti").checked, false);
  assert.deepEqual(next.selection.get("fizika").members.get("fizka"), { mode: "separate", name: "Физкультура" });
  // A new group gets the defaults.
  assert.deepEqual([next.selection.get("him").checked, next.selection.get("him").name], [true, "Химия"]);
  // A group that is gone is gone; the choices follow the rest.
  const fewer = reduce(next, { type: "refreshed", reply: reply({ token: "tok3", groups: [matan()] }) });
  assert.deepEqual([...fewer.selection.keys()], ["matan"]);
  assert.equal(fewer.selection.get("matan").name, "Мой матан");
});

test("carrySelection: a name nobody edited follows the new proposal; an edited one stays", () => {
  const old = [matan()];
  const fresh = [matan({ name: "Математика" })];
  assert.equal(carrySelection(old, initialSelection(old), fresh).get("matan").name, "Математика");
  assert.equal(carrySelection(old, setName(initialSelection(old), "matan", "Мой"), fresh).get("matan").name, "Мой");
  // The same for a separate member.
  const f0 = [fizika()];
  const sel = setMemberMode(initialSelection(f0), "fizika", "fizka", "separate");
  const f1 = [fizika({ members: [f0[0].members[0], { ...f0[0].members[1], name: "Физ-ра" }] })];
  assert.deepEqual(carrySelection(f0, sel, f1).get("fizika").members.get("fizka"), { mode: "separate", name: "Физ-ра" });
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

const threeBatches = () => [
  { token: "t1", created: [created("a")], aliased: [] },
  { token: "t2", created: [created("b")], aliased: [] },
  { token: "t3", created: [created("c")], aliased: [] },
];
const withBatches = (batches) => ({ ...initialState(reply()), batches });

test("undo success: what was removed and what was kept, counters and one line each", async () => {
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
    text: "Отменено: предметов 2 · названий 1",
    lines: [
      "В «Матан» уже есть файл — оставлен",
      "В «Физика» уже есть конспект — оставлен",
      "Другие названия у «Компьютерные сети» оставлены: расписание изменилось",
      "Другие названия у предмета оставлены: расписание изменилось",
    ],
    steps: ["refresh"],
  });
});

test("undo with nothing removed says so", async () => {
  const s0 = withBatch();
  const action = await undoFlow({ undo: async () => undoReply({ kept: [{ id: "c", name: "Матан", reason: "has_files" }] }) }, s0.batches, names);
  assert.equal(reduce(s0, action).notice.text, "Ничего не убрано");
});

test("undo invalid for every batch: «Отменить» is gone for good, only «Закрыть» (no «К предметам»)", async () => {
  const s0 = withBatch();
  const s = reduce(reduce(s0, { type: "undoing" }), await undoFlow({ undo: async () => undoReply({ problem: "invalid" }) }, s0.batches, names));
  assert.deepEqual(s.notice, {
    tone: "fail",
    text: "Отменить уже нельзя: приложение перезапускалось. Заведённые предметы остались в списке",
    lines: [],
    steps: [],
  });
  assert.equal(s.batches.length, 0);
  assert.equal(s.view, "result");
  const out = html(h(ReviewView, { state: s, subjects, on }));
  assert.doesNotMatch(out, /К предметам/);
  assert.match(out, />Закрыть</);
});

for (const [problem, text] of [["busy", reviewTexts.busy], ["unavailable", reviewTexts.undoUnavailable]]) {
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
  assert.equal(s.notice.text, reviewTexts.undoUnavailable);
  assert.deepEqual(s.notice.steps, ["retryUndo"]);
  assert.deepEqual(s.batches, s0.batches);
});

test("undo of several batches goes newest first and stops at the first problem, keeping that batch and the older ones", async () => {
  const calls = [];
  const action = await undoFlow(
    { undo: async (t) => (calls.push(t), t === "t2" ? undoReply({ problem: "busy" }) : undoReply({ removed: [t] })) },
    threeBatches(),
    names,
  );
  assert.deepEqual(calls, ["t3", "t2"]);
  assert.deepEqual(action.result.remaining.map((b) => b.token), ["t1", "t2"]);
  assert.equal(action.result.reply.problem, "busy");
  // All the way: newest first, then nothing is left.
  const all = [];
  const done = await undoFlow({ undo: async (t) => (all.push(t), undoReply({ removed: [t] })) }, threeBatches(), names);
  assert.deepEqual(all, ["t3", "t2", "t1"]);
  assert.deepEqual(done.result.remaining, []);
  assert.deepEqual(done.result.reply.removed, ["t3", "t2", "t1"]);
});

test("a problem in a later batch keeps and shows what was undone before it, and what its own reply reports", async () => {
  const s0 = withBatches(threeBatches());
  const action = await undoFlow(
    {
      undo: async (t) =>
        t === "t3"
          ? undoReply({ removed: ["c"], kept: [{ id: "k", name: "Матан", reason: "has_files" }] })
          : undoReply({ removed: ["b"], unaliased: [{ subjectId: "s9", aliases: ["Сети"] }], problem: "busy" }),
    },
    s0.batches,
    names,
  );
  const s = reduce(reduce(s0, { type: "undoing" }), action);
  assert.deepEqual(s.batches.map((b) => b.token), ["t1", "t2"]);
  assert.deepEqual(s.notice, {
    tone: "fail",
    text: reviewTexts.busy,
    lines: ["Отменено: предметов 2 · названий 1", "В «Матан» уже есть файл — оставлен"],
    steps: ["retryUndo"],
  });
  assert.equal(doneText(s.batches), "Создано предметов: 2");
  // The same through an unreadable reply of the second call.
  const thrown = await undoFlow({ undo: async (t) => { if (t === "t2") { throw new Error("x"); } return undoReply({ removed: [t] }); } }, s0.batches, names);
  const s2 = reduce(s0, thrown);
  assert.equal(s2.notice.text, reviewTexts.undoUnavailable);
  assert.deepEqual(s2.notice.lines, ["Отменено: предметов 1"]);
  assert.deepEqual(s2.batches.map((b) => b.token), ["t1", "t2"]);
});

test("an invalid batch is dropped alone: the rest is undone and the notice says what could not be", async () => {
  const calls = [];
  const s0 = withBatches(threeBatches());
  const action = await undoFlow(
    { undo: async (t) => (calls.push(t), t === "t2" ? undoReply({ problem: "invalid" }) : undoReply({ removed: [t] })) },
    s0.batches,
    names,
  );
  assert.deepEqual(calls, ["t3", "t2", "t1"]);
  const s = reduce(reduce(s0, { type: "undoing" }), action);
  assert.deepEqual(s.batches, []);
  assert.equal(s.view, "result");
  assert.deepEqual(s.notice, {
    tone: "ok",
    text: "Отменено: предметов 2",
    lines: [reviewTexts.undoPartInvalid],
    steps: ["refresh"],
  });
  // Invalid first, then a real problem: the invalid one is gone, the failing one stays with the older ones.
  const mixed = await undoFlow({ undo: async (t) => (t === "t3" ? undoReply({ problem: "invalid" }) : undoReply({ problem: "unavailable" })) }, s0.batches, names);
  const m = reduce(s0, mixed);
  assert.deepEqual(m.batches.map((b) => b.token), ["t1", "t2"]);
  assert.equal(m.notice.text, reviewTexts.undoUnavailable);
  assert.ok(m.notice.lines.includes(reviewTexts.undoPartInvalid));
});

test("undo without anything done is not started", () => {
  const s0 = initialState(reply());
  assert.equal(reduce(s0, { type: "undoing" }), s0);
});

// ---------------------------------------------------------------------------------------------------------------------
// «Отменить всё» after the panel closed

const keptBatch = { token: "t1", created: [created("a"), created("b", "fizika", "Физика")], aliased: [{ subjectId: "s9", aliases: ["Сети"] }] };

test("the closed state: the batches come back from the panel and go back to it", () => {
  const s = closedState([keptBatch]);
  assert.equal(doneText(s.batches), "Создано предметов: 2 · Добавлено названий: 1");
  assert.equal(reduce(s, { type: "adopt", batches: [] }).batches.length, 0);
  assert.deepEqual(reduce(s, { type: "adopt", batches: [keptBatch, { ...keptBatch, token: "t2" }] }).batches.map((b) => b.token), ["t1", "t2"]);
  // A panel opened with batches shows their line and keeps the order: the older come first.
  const opened = initialState(reply(), [keptBatch]);
  assert.equal(doneText(opened.batches), "Создано предметов: 2 · Добавлено названий: 1");
  const more = reduce(reduce(opened, { type: "saving" }), { type: "created", reply: createReply({ created: [created("z")] }) });
  assert.deepEqual(more.batches.map((b) => b.token), ["t1", "tok1"]);
});

test("the line on the page: «Создано предметов: N · Отменить всё», nothing when there is nothing", () => {
  const out = html(h(KeptUndoLine, { state: closedState([keptBatch]), onUndo: noop, onDismiss: noop }));
  assert.match(out, /Создано предметов: 2 · Добавлено названий: 1/);
  assert.match(out, />Отменить всё</);
  assert.match(out, /role="status"/);
  assert.equal(html(h(KeptUndoLine, { state: closedState([]), onUndo: noop, onDismiss: noop })), "");
  const undoing = html(h(KeptUndoLine, { state: reduce(closedState([keptBatch]), { type: "undoing" }), onUndo: noop, onDismiss: noop }));
  assert.match(undoing, /Отменяется…/);
  assert.match(undoing, /<button[^>]*disabled=""/);
});

test("the line on the page: the outcome of the undo with kept lines and a way out", async () => {
  const s0 = closedState([keptBatch]);
  const ok = reduce(
    reduce(s0, { type: "undoing" }),
    await undoFlow({ undo: async () => undoReply({ removed: ["a"], kept: [{ id: "b", name: "Физика", reason: "has_files" }] }) }, s0.batches, names),
  );
  const out = html(h(KeptUndoLine, { state: ok, onUndo: noop, onDismiss: noop }));
  assert.doesNotMatch(out, />Отменить всё</);
  assert.match(out, /Отменено: предметов 1/);
  assert.match(out, /В «Физика» уже есть файл — оставлен/);
  assert.match(out, />Закрыть</); // there is no proposal here to refresh
  assert.doesNotMatch(out, /Обновить список|Показать остальные/);
  // A problem keeps «Отменить всё» and offers «Повторить».
  const busy = reduce(
    reduce(s0, { type: "undoing" }),
    await undoFlow({ undo: async () => undoReply({ problem: "busy" }) }, s0.batches, names),
  );
  const bo = html(h(KeptUndoLine, { state: busy, onUndo: noop, onDismiss: noop }));
  assert.match(bo, /role="alert"[^]*Ещё выполняется предыдущее действие[^]*Повторить/);
  assert.match(bo, />Отменить всё</);
  // The app was restarted: only «Закрыть».
  const gone = reduce(reduce(s0, { type: "undoing" }), await undoFlow({ undo: async () => undoReply({ problem: "invalid" }) }, s0.batches, names));
  const go = html(h(KeptUndoLine, { state: gone, onUndo: noop, onDismiss: noop }));
  assert.match(go, /Отменить уже нельзя: приложение перезапускалось/);
  assert.match(go, />Закрыть</);
  assert.doesNotMatch(go, /Повторить|К предметам/);
  // «Закрыть» drops the notice.
  assert.equal(reduce(ok, { type: "dismiss" }).notice, null);
  assert.equal(html(h(KeptUndoLine, { state: reduce(ok, { type: "dismiss" }), onUndo: noop, onDismiss: noop })), "");
});

test("lineSteps: the page's line never offers a refresh of a proposal it does not hold", () => {
  assert.deepEqual(lineSteps({ tone: "ok", text: "", lines: [], steps: ["refresh"] }), ["close"]);
  assert.deepEqual(lineSteps({ tone: "ok", text: "", lines: [], steps: ["rest"] }), ["close"]);
  assert.deepEqual(lineSteps({ tone: "fail", text: "", lines: [], steps: [] }), ["close"]);
  assert.deepEqual(lineSteps({ tone: "fail", text: "", lines: [], steps: ["retryUndo"] }), ["retryUndo"]);
});

// ---------------------------------------------------------------------------------------------------------------------
// Esc

test("Esc closes the panel except inside a text field or while a call runs", () => {
  assert.equal(escCloses({ tagName: "SECTION" }, false), true);
  assert.equal(escCloses({ tagName: "BUTTON" }, false), true);
  assert.equal(escCloses({ tagName: "INPUT", type: "checkbox" }, false), true);
  assert.equal(escCloses({ tagName: "INPUT", type: "text" }, false), false);
  assert.equal(escCloses({ tagName: "INPUT" }, false), false);
  assert.equal(escCloses({ tagName: "SECTION" }, true), false);
});

// ---------------------------------------------------------------------------------------------------------------------
// SSR of the panel

const on = new Proxy({}, { get: () => noop });
const view = (state, extra = {}) => html(h(ReviewView, { state, subjects, on, ...extra }));
const count = (s, re) => (s.match(re) ?? []).length;

test("the panel: a row per group, all checked, names editable, facts and spellings small, one primary button", () => {
  const out = view(initialState(reply()));
  assert.equal(count(out, /class="sched-row"/g), 3);
  assert.equal(count(out, /type="checkbox"[^>]*checked=""/g) + count(out, /checked=""[^>]*type="checkbox"/g), 3);
  assert.match(out, /value="Матан"/);
  assert.match(out, /value="Физика"/);
  assert.match(out, /14 пар · лекции 12 · семинары 2/);
  assert.match(out, /Другие названия: Матанализ \(лек\.\)/);
  assert.doesNotMatch(out, /Написания/);
  assert.match(out, /Создать 2 предмета/);
  assert.equal(count(out, /class="btn primary/g), 1);
  assert.match(out, /Закрыть/);
  assert.doesNotMatch(out, /<h[1-6]/); // no headings inside the panel
  assert.match(out, /aria-label="Предметы из расписания"/);
});

test("the panel: no length cap in the name field (the plan counts characters)", () => {
  assert.doesNotMatch(view(initialState(reply())), /maxlength/i);
});

test("the panel: the checkbox and the name field are named, and the checkbox sits in a label with the facts", () => {
  const out = view(initialState(reply()));
  assert.match(out, /aria-label="Создать предмет «Матан»"/);
  assert.match(out, /aria-label="Название предмета «Матан»"/);
  assert.match(out, /aria-label="Добавить названия к «Компьютерные сети»"/);
  assert.equal(count(out, /<label class="sched-chk-hit"><input[^>]*type="checkbox"/g), 3);
  // The facts are labels of the checkbox: a click on them toggles it.
  assert.match(out, /<label for="[^"]+" id="[^"]+" class="small muted">14 пар/);
});

test("the panel: the doubtful group is marked and its member has one three-way choice", () => {
  const out = view(initialState(reply()));
  assert.match(out, /Похоже, это один предмет\. Если нет — выберите «Отдельно»/);
  assert.match(out, /Физ-ка/);
  assert.equal(count(out, /role="radiogroup"/g), 1);
  assert.equal(count(out, /role="radio"/g), 3);
  assert.match(out, /aria-checked="true"[^>]*>Вместе</);
  assert.match(out, /aria-checked="false"[^>]*>Отдельно</);
  assert.match(out, /aria-checked="false"[^>]*>Не создавать</);
  assert.equal(count(out, /aria-checked="true"/g), 1);
  assert.doesNotMatch(out, /aria-pressed/);
  // Roving tabindex: one Tab stop, the checked radio.
  assert.equal(count(out, /role="radio"[^>]*tabindex="0"|tabindex="0"[^>]*role="radio"/g), 1);
  assert.equal(count(out, /role="radio"[^>]*tabindex="-1"|tabindex="-1"[^>]*role="radio"/g), 2);
  // «Отдельно» turns the member's name into a field.
  const sel = setMemberMode(initialSelection(reply().groups), "fizika", "fizka", "separate");
  const out2 = view({ ...initialState(reply()), selection: sel });
  assert.match(out2, /aria-checked="true"[^>]*>Отдельно</);
  assert.match(out2, /aria-label="Название отдельного предмета «Физ-ка»"/);
  assert.match(out2, /Создать 3 предмета/);
  // «Не создавать» strikes the name and says so.
  const out3 = view({ ...initialState(reply()), selection: setMemberMode(initialSelection(reply().groups), "fizika", "fizka", "skip") });
  assert.match(out3, /aria-checked="true"[^>]*>Не создавать</);
  assert.match(out3, /Не будет создан/);
  assert.doesNotMatch(out3, /заведен|Не заводить/);
});

test("the panel: a group of an existing subject says where it goes and has no name field", () => {
  const out = view(initialState(reply({ groups: [seti()] })));
  assert.match(out, /Добавить к «Компьютерные сети»/);
  assert.doesNotMatch(out, /aria-label="Название предмета/);
  assert.match(out, /Другие названия: Сети \(лек\.\) · Сети/);
  assert.match(out, /Добавить другие названия: 2/);
  assert.match(out, /aria-label="Добавить названия к «Компьютерные сети»"/);
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

test("the panel: an other name of an existing subject is an error beside the field", () => {
  const s = initialState(reply());
  const out = view({ ...s, selection: setName(s.selection, "matan", "Алг.") });
  assert.match(out, /Предмет с таким названием уже есть/);
});

test("the panel: saving shows «Создание…», disables the controls and sets aria-busy", () => {
  const out = view(reduce(initialState(reply()), { type: "saving" }));
  assert.match(out, /aria-busy="true"/);
  assert.match(out, /Создание…/);
  assert.equal(count(out, /<input[^>]*disabled=""/g), 5); // 3 checkboxes and the 2 name fields
  assert.equal(count(out, /<button[^>]*disabled=""/g), 5); // the main button, «Закрыть» and the 3 choices of the member
});

test("the panel: the outcome is announced in a polite status, failures in an alert, each with its button", () => {
  const ok = view(withBatch());
  assert.match(ok, /role="status" aria-live="polite"/);
  assert.match(ok, /Создано предметов: 1 · Добавлено названий: 1/);
  assert.match(ok, />Отменить всё</);
  assert.doesNotMatch(ok, /Создать \d/); // the list is gone, only «Закрыть» remains
  const stale = view(reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ created: [], problem: "stale" }) }));
  assert.match(stale, /role="alert"[^]*Расписание или предметы изменились[^]*Обновить список/);
  assert.doesNotMatch(stale, />Отменить всё</);
  const busy = view(reduce(reduce(initialState(reply()), { type: "saving" }), { type: "created", reply: createReply({ created: [], problem: "busy" }) }));
  assert.match(busy, /Ещё выполняется предыдущее действие — повторите через пару секунд[^]*Повторить/);
  assert.match(busy, /Создать 2 предмета/); // the choices stay
});

test("the panel: empty proposal and omitted names", () => {
  const out = view(initialState(reply({ groups: [], omitted: 2 })));
  assert.match(out, /Все предметы из расписания уже заведены/);
  assert.match(out, /Ещё 2 названия из расписания не показаны/);
  assert.match(out, />Показать остальные</);
  assert.doesNotMatch(out, /class="btn primary/);
  const withRows = view(initialState(reply({ omitted: 3 })));
  assert.match(withRows, /Ещё 3 названия из расписания появятся в списке после создания этих предметов/);
  assert.doesNotMatch(withRows, /„|“/);
  // After a clean create the button to show the rest is there as well.
  const after = view(reduce(reduce(initialState(reply({ omitted: 3 })), { type: "saving" }), { type: "created", reply: createReply() }));
  assert.match(after, /Создано предметов: 1/);
  assert.match(after, /Ещё 3 названия из расписания ждут выбора/);
  assert.doesNotMatch(after, /появятся/);
  assert.match(after, />Показать остальные</);
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
    result: { reply: undoReply({ removed: ["a"], kept: [{ id: "c", name: evil, reason: "has_files" }] }), remaining: [], thrown: false, names, invalid: 0, tried: 1 },
  });
  const out2 = view(result);
  assert.doesNotMatch(out2, /<img/);
  assert.match(out2, /В «&lt;img/);
});

test("the controller renders its first state without a DOM", () => {
  const out = html(h(ScheduleSubjectsReview, { reply: reply(), subjects, api: {}, onChanged: noop, onClose: noop }));
  assert.match(out, /Создать 2 предмета/);
  const again = html(h(ScheduleSubjectsReview, { reply: reply(), subjects, initialBatches: [keptBatch], api: {}, onChanged: noop, onClose: noop }));
  assert.match(again, /Создано предметов: 2 · Добавлено названий: 1/);
  assert.match(again, />Отменить всё</);
});

// ---------------------------------------------------------------------------------------------------------------------
// the empty list, the line above the list and the page hints

const offer = (state) => ({ state, onReview: noop, onImport: noop, onRetry: noop });
const actions = (state) => html(h(EmptySubjectsActions, { offer: offer(state), addLabel: "Добавить предмет", onAdd: noop }));
const primaries = (s) => count(s, /class="btn primary/g);

test("empty list with groups: the schedule button with the count is the only primary", () => {
  const out = actions({ loaded: true, reply: reply({ groups: [matan(), fizika()] }) });
  assert.match(out, /Завести предметы из расписания/);
  assert.match(out, /<span class="sched-cnt num">2<\/span>/);
  assert.equal(primaries(out), 1);
  assert.ok(out.indexOf("Завести предметы") < out.indexOf("Добавить предмет<")); // the primary first
  assert.match(out, /class="btn"[^>]*>[^]*Добавить предмет/); // «Добавить предмет» is secondary
});

test("empty list when the call succeeded with no groups: «Импортировать расписание» is the primary", () => {
  const out = actions({ loaded: true, reply: reply({ groups: [] }) });
  assert.match(out, /Импортировать расписание/);
  assert.doesNotMatch(out, /Завести предметы|Не удалось проверить/);
  assert.equal(primaries(out), 1);
  assert.match(out, /class="btn primary"[^>]*>[^]*?Импортировать расписание/);
});

test("empty list when the check failed: «Не удалось проверить расписание», «Повторить», «Добавить предмет» is the only primary", () => {
  for (const o of [{ loaded: true, reply: null }, { loaded: true, reply: reply({ groups: [], problem: "unavailable" }) }]) {
    const out = actions(o);
    assert.match(out, /Не удалось проверить расписание/);
    assert.match(out, />Повторить</);
    assert.doesNotMatch(out, /Импортировать расписание|Завести предметы/);
    assert.equal(primaries(out), 1);
    assert.match(out, /class="btn primary"[^>]*>[^]*?Добавить предмет</);
  }
});

test("empty list while checking: a small indicator, no schedule button, no second primary", () => {
  const out = actions(offerChecking);
  assert.match(out, /Проверяется расписание…/);
  assert.doesNotMatch(out, /Импортировать расписание|Завести предметы/);
  assert.equal(primaries(out), 0);
});

test("the empty state of «Предметы» through SubjectList: why it is empty and one primary", () => {
  const out = html(h(SubjectList, { subjects: [], onOpen: noop, onAdd: noop, offer: offer({ loaded: true, reply: reply() }) }));
  assert.match(out, /Предметов пока нет/);
  assert.match(out, /В расписании найдено предметов: 3\. Их можно создать за один шаг/);
  assert.equal(primaries(out), 1);
  assert.match(out, /Завести предметы из расписания/);
  assert.match(out, />Добавить предмет</);
  for (const o of [offerChecking, { loaded: true, reply: reply({ groups: [] }) }, { loaded: true, reply: null }]) {
    const plainText = html(h(SubjectList, { subjects: [], onOpen: noop, onAdd: noop, offer: offer(o) }));
    assert.match(plainText, /Предметы создаются из расписания или вручную/);
  }
  // Without an offer the list is as before.
  const plain = html(h(SubjectList, { subjects: [], onOpen: noop, onAdd: noop }));
  assert.equal(primaries(plain), 1);
  assert.doesNotMatch(plain, /Завести предметы/);
});

test("a list with subjects has no schedule button in SubjectList itself (the page shows one quiet line)", () => {
  const s = { id: "s1", name: "Алгебра", semester: "2026-autumn", semesterLabel: "Осень 2026", aliases: [], folders: [], teachers: [], schedule: [], version: 1 };
  const out = html(h(SubjectList, { subjects: [s], onOpen: noop, onAdd: noop, offer: offer({ loaded: true, reply: reply() }) }));
  assert.doesNotMatch(out, /Завести предметы/);
});

test("the quiet line above the list: the count and a secondary button", () => {
  const out = html(h(ScheduleOfferLine, { groups: 4, onClick: noop }));
  assert.match(out, /Из расписания можно создать предметы: 4/);
  assert.match(out, /Завести предметы из расписания/);
  assert.equal(primaries(out), 0);
  assert.match(out, /id="subjects-schedule-offer"/);
  assert.doesNotMatch(out, /sched-cnt/);
});

test("the hint after an import: the count of subjects without a card and a secondary button", () => {
  const out = html(h(ScheduleNextHint, { groups: 5, onClick: noop }));
  assert.match(out, /Из расписания можно создать предметы: 5/);
  assert.match(out, />Завести предметы из расписания</);
  assert.equal(primaries(out), 0);
  assert.doesNotMatch(out, /Следующий шаг/);
});

// ---------------------------------------------------------------------------------------------------------------------
// round 2: races, failed checks, one-shot flag, keyboard

test("adopt is ignored while the page's own undo runs, so the panel cannot take the batches under it", () => {
  const undoing = reduce(closedState([keptBatch]), { type: "undoing" });
  assert.equal(undoing.phase, "undoing");
  assert.equal(reduce(undoing, { type: "adopt", batches: [] }), undoing);
  assert.equal(reduce(undoing, { type: "adopt", batches: [keptBatch, { ...keptBatch, token: "t2" }] }).batches.length, 1);
  assert.equal(reduce(closedState([keptBatch]), { type: "adopt", batches: [] }).batches.length, 0); // idle: as before
});

test("the offer buttons are disabled while the page's undo runs", () => {
  const line = html(h(ScheduleOfferLine, { groups: 2, disabled: true, onClick: noop }));
  assert.match(line, /<button[^>]*disabled=""[^>]*>[^]*Завести предметы из расписания/);
  assert.doesNotMatch(html(h(ScheduleOfferLine, { groups: 2, onClick: noop })), /disabled/);
  const empty = html(h(EmptySubjectsActions, { offer: { ...offer({ loaded: true, reply: reply() }), disabled: true }, addLabel: "Добавить предмет", onAdd: noop }));
  assert.match(empty, /<button[^>]*disabled=""[^>]*>[^]*Завести предметы из расписания/);
});

test("a refresh answered with a problem keeps the groups, the edits and the token, like an unreadable answer", () => {
  let s = initialState(reply());
  s = reduce(s, { type: "rename", group: "matan", name: "Мой матан" });
  s = reduce(s, { type: "check", group: "seti", checked: false });
  const bad = reduce(reduce(s, { type: "refreshing" }), { type: "refreshed", reply: reply({ token: "", groups: [], omitted: 0, problem: "unavailable" }) });
  assert.equal(bad.phase, "idle");
  assert.equal(bad.view, "result");
  assert.equal(bad.token, "tok1");
  assert.equal(bad.groups, s.groups);
  assert.equal(bad.selection, s.selection);
  assert.deepEqual(bad.notice, { tone: "fail", text: reviewTexts.listUnavailable, lines: [], steps: ["refresh"] });
  assert.deepEqual(bad.notice, reduce(reduce(s, { type: "refreshing" }), { type: "refreshThrown" }).notice);
  // The next refresh that works carries the edit over.
  const ok = reduce(reduce(bad, { type: "refreshing" }), { type: "refreshed", reply: reply({ token: "tok2" }) });
  assert.equal(ok.view, "list");
  assert.equal(ok.token, "tok2");
  assert.equal(ok.selection.get("matan").name, "Мой матан");
  assert.equal(ok.selection.get("seti").checked, false);
});

test("the check failed with subjects on the page: one quiet line with «Повторить»", () => {
  const failed = [{ loaded: true, reply: null }, { loaded: true, reply: reply({ groups: [], problem: "unavailable" }) }];
  for (const o of failed) {
    assert.equal(showCheckFailed(o, 3), true);
    assert.equal(showCheckFailed(o, 0), false); // the empty state shows it itself
  }
  for (const o of [offerChecking, { loaded: true, reply: reply() }, { loaded: true, reply: reply({ groups: [] }) }]) {
    assert.equal(showCheckFailed(o, 3), false);
  }
  let retried = 0;
  const out = html(h(ScheduleCheckFailedLine, { onRetry: () => retried++ }));
  assert.match(out, /Не удалось проверить расписание/);
  assert.match(out, /class="btn ghost sm"[^>]*>Повторить</);
  assert.equal(primaries(out), 0);
  assert.equal(retried, 0);
});

test("the one-shot request of the review is kept only on the list of «Предметы»", () => {
  assert.equal(keepScheduleReview(true, { page: "subjects", subject: "" }), true);
  assert.equal(keepScheduleReview(true, { page: "subjects", subject: "s1" }), false); // an open subject drops it
  assert.equal(keepScheduleReview(true, { page: "schedule", subject: "" }), false);
  assert.equal(keepScheduleReview(false, { page: "subjects", subject: "" }), false);
});

test("the page line «Создано предметов» has a ghost «Закрыть» that drops the kept state", () => {
  const out = html(h(KeptUndoLine, { state: closedState([keptBatch]), onUndo: noop, onDismiss: noop, onForget: noop }));
  assert.match(out, />Отменить всё</);
  assert.match(out, /class="btn ghost sm"[^>]*>Закрыть</);
  const forgotten = reduce(closedState([keptBatch]), { type: "forget" });
  assert.deepEqual([forgotten.batches, forgotten.notice], [[], null]);
  assert.equal(html(h(KeptUndoLine, { state: forgotten, onUndo: noop, onDismiss: noop, onForget: noop })), "");
  // Not while the undo runs.
  const undoing = reduce(closedState([keptBatch]), { type: "undoing" });
  assert.equal(reduce(undoing, { type: "forget" }), undoing);
  // The panel's own done line has no second «Закрыть» of that kind (the panel has one in its footer).
  assert.equal(count(view(withBatch()), />Закрыть</g), 1);
});

test("arrow keys move the choice of a doubtful member and wrap; other keys do nothing", () => {
  assert.equal(nextMode("keep", "ArrowRight"), "separate");
  assert.equal(nextMode("keep", "ArrowDown"), "separate");
  assert.equal(nextMode("separate", "ArrowRight"), "skip");
  assert.equal(nextMode("skip", "ArrowRight"), "keep");
  assert.equal(nextMode("keep", "ArrowLeft"), "skip");
  assert.equal(nextMode("keep", "ArrowUp"), "skip");
  assert.equal(nextMode("skip", "ArrowLeft"), "separate");
  assert.equal(nextMode("separate", "ArrowUp"), "keep");
  for (const key of ["Tab", "Enter", " ", "a", "Home"]) {
    assert.equal(nextMode("keep", key), null);
  }
});

test("the header «Предмет» is primary only when neither the empty state nor the open review has the accent", () => {
  assert.equal(addSubjectVariant(null, false), "primary"); // still loading
  assert.equal(addSubjectVariant(3, false), "primary");
  assert.equal(addSubjectVariant(0, false), "secondary");
  assert.equal(addSubjectVariant(3, true), "secondary"); // the panel's «Создать N предметов» is the one primary
  assert.equal(addSubjectVariant(0, true), "secondary");
  assert.equal(primaries(html(h(ScheduleCheckFailedLine, { onRetry: noop }))), 0);
});
