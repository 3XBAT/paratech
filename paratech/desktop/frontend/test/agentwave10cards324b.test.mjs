// TASK-324 (part B): the schedule_subjects card and the offer_button card of the agent panel. Parsing of the card and its
// calls with their exact arguments, the card in every state (waiting with a doubtful group, done with «Вернуть», stopped
// midway, stale, undone, declined, a refusal, loading, an error), the flow of its buttons, and the mapping of every offer
// to the page's own button. Fixtures only: the catalog is not served to the model yet.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { isOff, isOn } from "./iconbtn.mjs";
import { offerActions, parseCard, parseUpdate } from "../.test-build/bindings/agent.js";
import {
  agentConfirmApi,
  cardKinds as confirmKinds,
  cardStates,
  confirmErrorText,
  errorTexts,
  genericError,
  methods as confirmMethods,
  parseConfirmCard,
  scheduleBusyText,
  scheduleNameText,
  scheduleUndoAgainText,
  scheduleUndoGoneText,
} from "../.test-build/bindings/agentConfirm.js";
import {
  choicesOf,
  doneSummary,
  initialScheduleFlow,
  refusalOpensSubjects,
  ScheduleCardFlow,
  scheduleModel,
  scheduleTexts,
  undoSummary,
} from "../.test-build/agentScheduleView.js";
import {
  actionWording,
  entryModel,
  latestUndoableAction,
  latestUndoableCard,
  offerModel,
  offerSpecs,
  offerUnavailableText,
  texts as agentTexts,
} from "../.test-build/agentView.js";
import { offerReasons, offerStep, runOffer } from "../.test-build/agentOffer.js";
import { AgentRequest, OfferButton, OfferControlView } from "../.test-build/agent/AgentCards.js";
import { ScheduleCardPending, ScheduleCardView, ScheduleSubjectsCard } from "../.test-build/agent/ScheduleCard.js";
import { doneText, initialState, mainButton, planOf, reduce, reviewTexts, subjectsWord, takenOf } from "../.test-build/scheduleSubjectsView.js";
import { removalTexts, runRestore } from "../.test-build/subjectRemovalView.js";
import { calendarTexts } from "../.test-build/calendarView.js";
import { actionLabels as rowActionLabels, rowItemLabels } from "../.test-build/recordingRowView.js";
import { texts as diagnosticsTexts } from "../.test-build/diagnosticsView.js";
import { fileTexts } from "../.test-build/libraryInboxView.js";
import { manualStartTexts } from "../.test-build/manualStartView.js";
import { googleTexts } from "../.test-build/googleView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const evil = "<img src=x onerror=alert(1)>";
const TOKEN = "a".repeat(64);

// ---- Fixtures ----

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
const groups = () => [matan(), fizika(), seti()];
const existing = { s9: "Компьютерные сети", s1: "Алгебра" };
const subjectRefs = Object.entries(existing).map(([id, name]) => ({ id, name, aliases: [] }));

const created = (id, key, name) => ({ id, key, name });
const createResult = (over = {}) => ({
  created: [created("sched-1", "matan", "Матан"), created("sched-2", "fizika", "Физика")],
  aliased: [{ subjectId: "s9", aliases: ["Сети (лек.)"] }],
  skipped: [],
  problem: "",
  ...over,
});
const undoResult = (over = {}) => ({ removed: ["sched-1", "sched-2"], kept: [], unaliased: [{ subjectId: "s9", aliases: ["Сети (лек.)"] }], keptAliases: [], problem: "", ...over });

/** The card as agentconfirm_schedule.go sends it. */
function sched(state, schedule = {}, over = {}) {
  return {
    kind: "schedule_subjects", targetKind: "schedule_subjects", targetId: TOKEN, state, message: "", assignmentId: "", title: "", subject: "",
    due: "", recipient: "", recipientName: "", role: "", reason: "", subjectId: "", options: null, emailSubject: "", attachmentName: "",
    attachmentSize: 0, attachmentFormat: "", draftVersion: 0, notice: "", deletion: null,
    schedule: { groups: [], omitted: 0, result: null, undo: null, ...schedule }, ...over,
  };
}

const waiting = (over = {}) => sched("waiting", { groups: groups(), ...over });
const done = (result = createResult(), over = {}) => sched("done", { result }, over);
const stopped = (result = createResult()) => sched("confirmed", { result }, { message: "Создано не всё. Нажмите «Да» ещё раз — уже созданное не повторится, или «Отменить всё»." });
const stale = () => sched("stale", {}, { message: "Предложение устарело: расписание или предметы изменились. Проверьте список на странице «Предметы» или попросите агента предложить предметы ещё раз." });
const undone = (u = undoResult()) => sched("undone", { undo: u, result: null });
const declinedCard = () => sched("declined");

/** The review state of a waiting card (the rows being chosen) or the names the student saw. */
const reviewOf = (g = groups(), omitted = 0) => initialState({ token: TOKEN, groups: g, omitted, problem: "" });
const emptyReview = () => reviewOf([], 0);

const flowOf = (raw, over = {}) => ({ card: parseConfirmCard(raw), busy: "", error: "", ...over });
const handlers = (over = {}) => ({
  check: noop, rename: noop, member: noop, renameMember: noop, yes: noop, no: noop, undo: noop, reload: noop, openSubjects: noop, ...over,
});
const view = (raw, o = {}) =>
  renderToStaticMarkup(
    h(ScheduleCardView, {
      flow: o.flow ?? flowOf(raw, o.flowOver),
      review: o.review ?? (raw !== null && raw.state === "waiting" ? reviewOf(raw.schedule.groups, raw.schedule.omitted) : emptyReview()),
      subjects: subjectRefs,
      on: handlers(o.on),
    }),
  );

/** Every element of a rendered (not mounted) tree for which `pred` holds. */
function findAll(node, pred, out = []) {
  if (node === null || node === undefined || typeof node !== "object") {
    return out;
  }
  if (Array.isArray(node)) {
    node.forEach((n) => findAll(n, pred, out));
    return out;
  }
  if (pred(node)) {
    out.push(node);
  }
  return findAll(node.props?.children, pred, out);
}

// ---- Parsing and calls ----

test("the schedule_subjects confirm card: kind, states and the content parse as the Go card", () => {
  assert.ok(confirmKinds.includes("schedule_subjects"));
  assert.ok(cardStates.includes("undone"));
  const w = parseConfirmCard(waiting({ omitted: 3 }));
  assert.equal(w.kind, "schedule_subjects");
  assert.equal(w.targetKind, "schedule_subjects");
  assert.equal(w.targetId, TOKEN);
  assert.equal(w.schedule.groups.length, 3);
  assert.equal(w.schedule.omitted, 3);
  assert.equal(w.schedule.groups[1].members[1].doubtful, true);
  assert.equal(w.schedule.result, null);
  const d = parseConfirmCard(
    done(createResult({ skipped: [{ key: "x", subjectId: "", reason: "name_taken", removedSubjectId: "gone1", removedSubjectVersion: 4 }] })),
  );
  assert.equal(d.schedule.result.created.length, 2);
  assert.equal(d.schedule.result.skipped[0].removedSubjectId, "gone1");
  assert.equal(d.schedule.result.skipped[0].removedSubjectVersion, 4);
  const u = parseConfirmCard(undone());
  assert.equal(u.state, "undone");
  assert.deepEqual(u.schedule.undo.removed, ["sched-1", "sched-2"]);
});

test("a schedule_subjects card without its content is an error; other kinds carry none", () => {
  assert.throws(() => parseConfirmCard(sched("waiting", {}, { schedule: null })));
  assert.throws(() => parseConfirmCard(sched("waiting", { groups: null })));
  assert.throws(() => parseConfirmCard(sched("done", { result: { created: null } })));
  const other = parseConfirmCard(sched("waiting", {}, { kind: "delete_assignment", schedule: null }));
  assert.equal(other.schedule, null);
});

test("the refusals of the schedule card are known texts; anything else is the generic one", () => {
  for (const t of [scheduleBusyText, scheduleNameText, scheduleUndoGoneText, scheduleUndoAgainText]) {
    assert.ok(errorTexts.includes(t), t);
    assert.equal(confirmErrorText(new Error(t)), t);
  }
  assert.equal(confirmErrorText(new Error("sql: no rows")), genericError);
  assert.equal(scheduleBusyText, "Предметы из расписания сейчас создаются или отменяются. Подождите немного и нажмите ещё раз.");
  assert.equal(scheduleNameText, "Это название уже занято другим предметом или строкой списка. Выберите другое и нажмите «Да».");
  assert.equal(scheduleUndoGoneText, "Отменить отсюда уже нельзя. Лишние предметы можно убрать на странице «Предметы».");
  assert.equal(scheduleUndoAgainText, "Отменилось не всё. Нажмите «Отменить всё» ещё раз — повтор безопасен.");
});

test("the calls go to the bound methods with the exact arguments", async () => {
  const seen = [];
  const reply = { card: waiting(), confirm: done(), undo: undone(), decline: declinedCard() };
  const call = async (name, ...args) => {
    seen.push([name, ...args]);
    if (name === confirmMethods.card) return reply.card;
    if (name === confirmMethods.confirmSchedule) return reply.confirm;
    if (name === confirmMethods.undoSchedule) return reply.undo;
    return reply.decline;
  };
  const api = agentConfirmApi(call);
  const choices = [{ group: "matan", action: "create", name: "", members: [] }, { group: "seti", action: "skip", name: "", members: [] }];
  assert.equal((await api.card({ kind: "schedule_subjects", id: TOKEN })).kind, "schedule_subjects");
  assert.equal((await api.confirmSchedule(TOKEN, choices)).state, "done");
  assert.equal((await api.undoSchedule(TOKEN)).state, "undone");
  assert.equal((await api.decline({ kind: "schedule_subjects", id: TOKEN })).state, "declined");
  const service = "paratech/desktop/internal/input/wails.AgentConfirmService";
  assert.deepEqual(seen, [
    [`${service}.Card`, "schedule_subjects", TOKEN],
    [`${service}.ConfirmSchedule`, TOKEN, choices],
    [`${service}.UndoSchedule`, TOKEN],
    [`${service}.Decline`, "schedule_subjects", TOKEN],
  ]);
});

// ---- The card in the thread ----

const raw = (over) => ({
  kind: "action_line", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "", answerId: "",
  cannotDo: null, text: "", assignments: null, assignment: null, stopReason: "", workId: "", outcome: "", ...over,
});

function modelOf(cards) {
  const u = parseUpdate({
    threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "",
    problem: "", progress: false, cards,
  });
  return entryModel({ key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "", update: u });
}

const baseActions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const render = (cards, extra = {}) => renderToStaticMarkup(h(AgentRequest, { model: modelOf(cards), actions: { ...baseActions, ...extra } }));
const scheduleCard = () => raw({ kind: "schedule_subjects", target: { kind: "schedule_subjects", id: TOKEN } });
const offerCard = (offer) => raw({ kind: "offer_button", offer });

test("cardModel: a schedule_subjects card names its token; one without it is dropped", () => {
  assert.deepEqual(modelOf([scheduleCard()]).cards, [{ type: "schedule", token: TOKEN }]);
  assert.deepEqual(modelOf([raw({ kind: "schedule_subjects" })]).cards, []);
  assert.deepEqual(modelOf([raw({ kind: "schedule_subjects", target: { kind: "proposal", id: TOKEN } })]).cards, []);
});

test("in the thread: the schedule card starts as a small loading line; without the core calls it says it is pending", () => {
  const api = { card: async () => parseConfirmCard(waiting()), confirmSchedule: async () => parseConfirmCard(done()) };
  const out = render([scheduleCard()], { confirm: { api, deleteApi: {} } });
  assert.ok(out.includes(scheduleTexts.loading));
  assert.doesNotMatch(out, /<h3|<button/);
  const bare = render([scheduleCard()]);
  assert.ok(bare.includes(agentTexts.confirmPending));
});

test("cardModel: an offer_button card is one button; an unknown action or a missing id is dropped", () => {
  const m = modelOf([offerCard({ action: "open_summary", target: { kind: "recording", id: "r1" } })]).cards;
  assert.equal(m.length, 1);
  assert.equal(m[0].type, "offer");
  assert.equal(m[0].offer.label, "Открыть конспект");
  assert.deepEqual(modelOf([offerCard({ action: "format_disk", target: null })]).cards, []);
  assert.deepEqual(modelOf([offerCard({ action: "open_summary", target: null })]).cards, []);
  assert.deepEqual(modelOf([offerCard({ action: "open_summary", target: { kind: "material", id: "m1" } })]).cards, []);
  assert.deepEqual(modelOf([offerCard(null)]).cards, []);
});

// ---- The card: waiting ----

/** The buttons under the rows (the rows' own «Не создавать» radio is not one of them). */
const actionBtn = (label, extra = "") => new RegExp(`class="ag-actions"><button${extra}[^>]*>${label}<`);

test("waiting: the review rows inside the card, no heading, «Не создавать» then «Создать N предметов», nothing focused", () => {
  const out = view(waiting());
  assert.doesNotMatch(out, /<h[1-6]/);
  assert.ok(out.includes(reviewTexts.intro));
  for (const t of ["Матан", "Физика", "Компьютерные сети", reviewTexts.doubtful, reviewTexts.together, reviewTexts.separate, reviewTexts.skip]) {
    assert.ok(out.includes(t), t);
  }
  assert.ok(out.includes(`${reviewTexts.addTo} «Компьютерные сети»`));
  // Matan and Physics are created (the doubtful member is merged), the schedule group adds spellings to an existing subject.
  const main = mainButton(planOf(groups(), reviewOf().selection, takenOf(subjectRefs, { token: TOKEN, batches: [], attempt: [] })));
  assert.equal(main.label, "Создать 2 предмета");
  assert.ok(out.includes("Создать 2 предмета"));
  assert.ok(out.lastIndexOf(">Не создавать<") > 0 && out.lastIndexOf(">Не создавать<") < out.indexOf("Создать 2 предмета"));
  assert.equal(scheduleTexts.no, "Не создавать");
  assert.doesNotMatch(out, />Нет<|>Да</);
  assert.doesNotMatch(out, /autofocus|autoFocus/i);
  assert.match(out, actionBtn("Не создавать", "(?![^>]*disabled)"));
  assert.match(out, /<button(?![^>]*disabled)[^>]*><span class="btn-labels"><span>Создать 2 предмета</);
  // The doubtful members start merged: «Вместе» is the checked radio of each.
  assert.match(out, /aria-checked="true"[^>]*>Вместе</);
  assert.doesNotMatch(out, /aria-checked="true"[^>]*>Отдельно</);
});

test("waiting: the plural of «предмет» follows N", () => {
  const one = [matan()];
  const out = view(waiting({ groups: one }));
  assert.ok(out.includes("Создать 1 предмет"));
  assert.ok(!out.includes("Создать 1 предмета"));
  const five = ["a", "b", "c", "d", "e"].map((k) => matan({ key: k, name: `П${k}`, members: [member({ key: k, name: `П${k}`, spellings: [`П${k}`] })], aliases: [] }));
  assert.ok(view(waiting({ groups: five })).includes(`Создать ${subjectsWord(5)}`));
  assert.equal(subjectsWord(5), "5 предметов");
});

test("waiting: nothing to create — «Да» is off and the reason stands beside it", () => {
  let review = reviewOf();
  for (const g of groups()) {
    review = reduce(review, { type: "check", group: g.key, checked: false });
  }
  const out = view(waiting(), { review });
  assert.match(out, /<button[^>]*disabled[^>]*><span class="btn-labels"><span>Создать предметы</);
  assert.ok(out.includes(reviewTexts.nothingSelected));
  assert.match(out, actionBtn("Не создавать", "(?![^>]*disabled)"));
});

test("waiting: a name problem blocks «Да» with the page's reason; the problem stands beside the field", () => {
  const review = reduce(reviewOf(), { type: "rename", group: "matan", name: "  " });
  const out = view(waiting(), { review });
  assert.ok(out.includes(reviewTexts.fixNames));
  assert.ok(out.includes(reviewTexts.emptyName));
  assert.match(out, /<button[^>]*disabled[^>]*><span class="btn-labels"><span>Создать 2 предмета</);
  // A name that an existing subject already has.
  const taken = reduce(reviewOf(), { type: "rename", group: "matan", name: "алгебра" });
  assert.ok(view(waiting(), { review: taken }).includes(reviewTexts.exists));
});

test("waiting: the choices «Да» sends are the page's: merged members send nothing, «Отдельно» and «Не создавать» are explicit", () => {
  const review = reviewOf();
  assert.deepEqual(choicesOf(review), [
    { group: "matan", action: "create", name: "", members: [] },
    { group: "fizika", action: "create", name: "", members: [] },
    { group: "seti", action: "alias_of", name: "", members: [] },
  ]);
  let r = reduce(review, { type: "member", group: "fizika", member: "fizka", mode: "separate" });
  r = reduce(r, { type: "renameMember", group: "fizika", member: "fizka", name: "Физкультура" });
  r = reduce(r, { type: "check", group: "seti", checked: false });
  r = reduce(r, { type: "rename", group: "matan", name: "Мат. анализ" });
  assert.deepEqual(choicesOf(r), [
    { group: "matan", action: "create", name: "Мат. анализ", members: [] },
    { group: "fizika", action: "create", name: "", members: [{ key: "fizka", action: "separate", name: "Физкультура" }] },
    { group: "seti", action: "skip", name: "", members: [] },
  ]);
  const skip = reduce(review, { type: "member", group: "fizika", member: "fizka", mode: "skip" });
  assert.deepEqual(choicesOf(skip)[1].members, [{ key: "fizka", action: "skip", name: "" }]);
  assert.deepEqual(choicesOf(emptyReview()), []);
  // The rows of the card send their edits to the owner.
  const tree = ScheduleCardView({ flow: flowOf(waiting()), review, subjects: subjectRefs, on: handlers() });
  assert.ok(findAll(tree, (n) => n.props?.group !== undefined && n.props?.row !== undefined).length === 3);
});

test("waiting: stored text is text, never markup", () => {
  const out = view(waiting({ groups: [matan({ name: evil })] }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

test("waiting: a call runs — every control is off and «Да» says it works", () => {
  const out = view(waiting(), { flowOver: { busy: "confirm" } });
  assert.match(out, /aria-busy="true"/);
  assert.match(out, actionBtn("Не создавать", "[^>]*disabled"));
  assert.ok(out.includes('<span class="btn-off">Создать 2 предмета</span><span>Создание…</span>'));
  assert.doesNotMatch(out, /<button(?![^>]*disabled)/);
  assert.doesNotMatch(out, /<input(?![^>]*disabled)/);
});

test("waiting: no groups left — a plain line, no buttons that would do nothing", () => {
  const out = view(waiting({ groups: [] }), { review: emptyReview() });
  assert.ok(out.includes(reviewTexts.allDone));
  assert.doesNotMatch(out, /<button/);
});

test("waiting: the names the proposal left out are counted under the rows", () => {
  const out = view(waiting({ omitted: 2 }), { review: reviewOf(groups(), 2) });
  assert.ok(out.includes("Ещё 2 названия из расписания появятся в списке после создания этих предметов"));
});

// ---- The card: the other states ----

test("done: «Создано предметов: N · Отменить всё», the added spellings and the lines of the skipped rows", () => {
  const result = createResult({
    skipped: [
      { key: "kurs", subjectId: "s1", reason: "too_many_aliases" },
      { key: "fizika", subjectId: "", reason: "name_taken", removedSubjectId: "gone1", removedSubjectVersion: 4 },
    ],
  });
  const out = view(done(result), { review: reviewOf(), on: { restore: async () => ({ state: { phase: "done" }, subject: null }) } });
  assert.ok(out.includes("Создано предметов: 2 · Добавлено названий: 1"));
  assert.doesNotMatch(out, /Заведено/);
  assert.match(out, />Отменить всё</);
  assert.ok(out.includes(reviewTexts.tooManyAliases));
  // The skipped row a removed subject holds is called by the name the student saw, with «Вернуть».
  assert.ok(out.includes("«Физика»"));
  assert.ok(out.includes(removalTexts.nameRemoved));
  assert.match(out, />Вернуть</);
  assert.doesNotMatch(out, /<h[1-6]|>Нет<|>Да</);
});

test("done: a card read afresh calls the row by the subject it can tell, or says it plainly", () => {
  const result = createResult({ skipped: [{ key: "x", subjectId: "s9", reason: "name_taken" }, { key: "y", subjectId: "", reason: "name_taken" }] });
  const out = view(done(result), { review: emptyReview() });
  assert.ok(out.includes("Названия для «Компьютерные сети» не добавлены: одно из них уже занято"));
  assert.ok(out.includes("Одно название не добавлено: оно уже занято"));
});

test("done: nothing was created — «Ничего не создано», no «Отменить всё»", () => {
  const result = { created: [], aliased: [], skipped: [{ key: "x", subjectId: "", reason: "name_taken" }], problem: "" };
  const out = view(done(result), { review: emptyReview() });
  assert.ok(out.includes(reviewTexts.nothingCreated));
  assert.doesNotMatch(out, />Отменить всё</);
});

test("done: the names left out wait for a choice, and «Открыть «Предметы»» is the way to them", () => {
  const out = view(done(), { review: reviewOf(groups(), 2) });
  assert.ok(out.includes("Ещё 2 названия из расписания ждут выбора"));
  assert.match(out, />Открыть «Предметы»</);
  assert.doesNotMatch(view(done(), { review: reviewOf(groups(), 0) }), /Открыть «Предметы»/);
});

test("done: «Отменить всё» works as a button; while the undo runs it is off and says so", () => {
  const busy = view(done(), { review: reviewOf(), flowOver: { busy: "undo" } });
  assert.ok(busy.includes(reviewTexts.undoing));
  assert.match(busy, /<button[^>]*disabled[^>]*>/);
  const tree = ScheduleCardView({ flow: flowOf(done()), review: reviewOf(), subjects: subjectRefs, on: handlers({ undo: () => "undo" }) });
  const doneRow = findAll(tree, (n) => n.props?.onUndo !== undefined)[0];
  assert.equal(doneRow.props.onUndo(), "undo");
});

test("stopped midway: the card's own words (not the core's «Нажмите «Да»»), «Отменить всё» and primary «Повторить»; what was created is said", () => {
  const out = view(stopped(), { review: reviewOf() });
  assert.ok(out.includes("Создано не всё. «Повторить» досоздаст остальное без дублей, «Отменить всё» уберёт созданное."));
  assert.equal(scheduleTexts.partialFallback, "Создано не всё. «Повторить» досоздаст остальное без дублей, «Отменить всё» уберёт созданное.");
  // The core's sentence names a «Да» that is not a button here: it is never shown.
  assert.doesNotMatch(out, /Нажмите «Да»/);
  assert.ok(out.includes("Создано предметов: 2"));
  assert.ok(out.indexOf(">Отменить всё<") > 0 && out.indexOf(">Отменить всё<") < out.lastIndexOf("<span>Повторить</span>"));
  assert.match(out, /<button(?![^>]*disabled)[^>]*><span class="btn-labels"><span>Повторить</);
  assert.match(out, /class="btn primary sm"[^>]*><span class="btn-labels"><span>Повторить</);
  assert.doesNotMatch(out, />Нет<|>Да<|<span>Да</);
  assert.doesNotMatch(out, /autofocus/i);
  // An older core with no message says the same.
  const bare = view(sched("confirmed", { result: createResult() }), { review: reviewOf() });
  assert.ok(bare.includes(scheduleTexts.partialFallback));
  assert.deepEqual(scheduleModel(parseConfirmCard(stopped()), emptyReview(), new Map()).message, scheduleTexts.partialFallback);
});

test("stale: the core's message and «Открыть «Предметы»» as the way out; no «Да»", () => {
  const out = view(stale());
  assert.ok(out.includes("Предложение устарело: расписание или предметы изменились."));
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Открыть «Предметы»</);
  assert.doesNotMatch(out, />Да<|>Нет<|<h[1-6]/);
  const bare = view(sched("stale"));
  assert.ok(bare.includes(scheduleTexts.staleFallback));
  assert.doesNotMatch(view(stale(), { on: { openSubjects: undefined } }), /<button/);
});

test("undone: «Отменено: предметов N · названий K» (the page's line) and what stayed, in plain words", () => {
  const u = undoResult({
    removed: ["sched-1"],
    kept: [{ id: "sched-2", name: "Физика", reason: "has_files" }, { id: "sched-3", name: "Химия", reason: "has_notes" }],
    unaliased: [],
    keptAliases: [{ subjectId: "s9", aliases: ["Сети"], reason: "stale" }],
  });
  const out = view(undone(u), { review: reviewOf() });
  assert.ok(out.includes("Отменено: предметов 1"));
  assert.ok(out.includes("В «Физика» уже есть файл — оставлен"));
  assert.ok(out.includes("В «Химия» уже есть конспект — оставлен"));
  assert.ok(out.includes("Другие названия у «Компьютерные сети» оставлены: расписание изменилось"));
  // The way on is «Предметы»: the student may check the list.
  assert.equal((out.match(/<button/g) ?? []).length, 1);
  assert.match(out, />Открыть «Предметы»</);
  assert.ok(view(undone()).includes("Отменено: предметов 2 · названий 1"));
  assert.doesNotMatch(view(undone()), /убрано/);
  assert.equal(undoSummary(0, 0), "Ничего не убрано");
  assert.equal(undoSummary(5, 2), "Отменено: предметов 5 · названий 2");
  assert.equal(doneSummary(1, 0), "Создано предметов: 1");
  assert.equal(doneSummary(0, 3), "Добавлено названий: 3");
  // The lines are the page's own functions.
  assert.equal(doneSummary(3, 2), doneText([{ token: "t", created: [created("a", "a", "A"), created("b", "b", "B"), created("c", "c", "C")], aliased: [{ subjectId: "s9", aliases: ["x", "y"] }] }]));
});

test("declined: «Оставлено без изменений» and «Открыть «Предметы»» as the way on", () => {
  const out = view(declinedCard());
  assert.ok(out.includes("Оставлено без изменений"));
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Открыть «Предметы»</);
  assert.equal((out.match(/<button/g) ?? []).length, 1);
  assert.doesNotMatch(out, /<h[1-6]/);
  assert.doesNotMatch(view(declinedCard(), { on: { openSubjects: undefined } }), /<button/);
});

test("a refusal of the core: its own words, the buttons stay active; the undo that cannot go on names «Предметы»", () => {
  const w = view(waiting(), { flowOver: { error: scheduleNameText } });
  assert.ok(w.includes(scheduleNameText));
  assert.match(w, /role="alert"/);
  assert.match(w, actionBtn("Не создавать", "(?![^>]*disabled)"));
  assert.match(w, /<button(?![^>]*disabled)[^>]*><span class="btn-labels"><span>Создать 2 предмета</);
  const busy = view(waiting(), { flowOver: { error: scheduleBusyText } });
  assert.ok(busy.includes(scheduleBusyText));
  const gone = view(done(), { review: reviewOf(), flowOver: { error: scheduleUndoGoneText } });
  assert.ok(gone.includes(scheduleUndoGoneText));
  assert.match(gone, />Открыть «Предметы»</);
  assert.match(gone, /<button(?![^>]*disabled)[^>]*><span class="btn-labels"><span>Отменить всё</);
  assert.equal(refusalOpensSubjects(scheduleUndoGoneText), true);
  assert.equal(refusalOpensSubjects(scheduleUndoAgainText), false);
  const again = view(done(), { review: reviewOf(), flowOver: { error: scheduleUndoAgainText } });
  assert.ok(again.includes(scheduleUndoAgainText));
  assert.doesNotMatch(again, /Открыть «Предметы»/);
});

test("loading is a small line; an error has the reason and «Повторить»", () => {
  const loading = renderToStaticMarkup(h(ScheduleCardPending, { error: "" }));
  assert.ok(loading.includes("Загрузка предметов…"));
  assert.doesNotMatch(loading, /<button|role="alert"|<section/);
  const failed = renderToStaticMarkup(h(ScheduleCardPending, { error: genericError, onReload: noop }));
  assert.ok(failed.includes(genericError));
  assert.match(failed, /role="alert"/);
  assert.match(failed, />Повторить</);
  assert.doesNotMatch(renderToStaticMarkup(h(ScheduleCardPending, { error: genericError })), /<button/);
  const viaView = view(null, { flow: { card: null, busy: "", error: "" } });
  assert.ok(viaView.includes("Загрузка предметов…"));
  const real = renderToStaticMarkup(h(ScheduleSubjectsCard, { api: {}, token: TOKEN }));
  assert.ok(real.includes("Загрузка предметов…"));
});

test("«Вернуть» of a skipped row is the page's call: the row's id and version, through the restore the panel gives", async () => {
  const result = createResult({ skipped: [{ key: "fizika", subjectId: "", reason: "name_taken", removedSubjectId: "gone1", removedSubjectVersion: 4 }] });
  const calls = [];
  const restore = async (id, version) => (calls.push([id, version]), { state: { phase: "done" }, subject: null });
  let reloaded = 0;
  const tree = ScheduleCardView({ flow: flowOf(done(result)), review: reviewOf(), subjects: subjectRefs, on: handlers({ restore, reload: () => reloaded++ }) });
  const rows = findAll(tree, (n) => Array.isArray(n.props?.rows) && n.props?.restore !== undefined)[0];
  assert.equal(rows.props.rows.length, 1);
  assert.deepEqual(rows.props.rows[0], { key: "fizika", name: "Физика", subjectId: "gone1", version: 4 });
  await rows.props.restore(rows.props.rows[0].subjectId, rows.props.rows[0].version);
  assert.deepEqual(calls, [["gone1", 4]]);
  rows.props.onChanged();
  assert.equal(reloaded, 1);
  // The panel's restore is runRestore over the library binding, exactly as the page does it.
  const seen = [];
  const library = { restoreSubject: async (id, v) => (seen.push([id, v]), { problem: "", subject: null }) };
  const call = await runRestore(library, "gone1", 4);
  assert.deepEqual(seen, [["gone1", 4]]);
  assert.equal(call.state.phase, "done");
  // Without the restore a skipped row stays a line.
  const plain = view(done(result), { review: reviewOf(), on: { restore: undefined } });
  assert.doesNotMatch(plain, />Вернуть</);
});

// ---- The flow of the buttons ----

function fakeApi(over = {}) {
  const seen = [];
  const api = {
    card: async (t) => (seen.push(["card", t]), parseConfirmCard(waiting())),
    confirmSchedule: async (token, choices) => (seen.push(["confirm", token, choices]), parseConfirmCard(done())),
    undoSchedule: async (token) => (seen.push(["undo", token]), parseConfirmCard(undone())),
    decline: async (t) => (seen.push(["decline", t]), parseConfirmCard(declinedCard())),
    ...over,
  };
  return { api, seen };
}

test("flow: load, «Да» with the choices, «Отменить всё» — each makes one call with its exact arguments", async () => {
  const { api, seen } = fakeApi();
  const states = [];
  const flow = new ScheduleCardFlow(api, TOKEN, (s) => states.push(s));
  assert.equal(flow.state, initialScheduleFlow);
  await flow.load();
  assert.equal(flow.state.card.state, "waiting");
  const choices = choicesOf(reviewOf());
  await flow.yes(choices);
  assert.equal(flow.state.card.state, "done");
  assert.equal(flow.state.busy, "");
  await flow.undo();
  assert.equal(flow.state.card.state, "undone");
  assert.deepEqual(seen, [
    ["card", { kind: "schedule_subjects", id: TOKEN }],
    ["confirm", TOKEN, choices],
    ["undo", TOKEN],
  ]);
  assert.ok(states.some((s) => s.busy === "confirm"));
  assert.ok(states.some((s) => s.busy === "undo"));
});

test("flow: «Нет» declines the token; a second click while it runs does nothing", async () => {
  let release;
  const gate = new Promise((r) => (release = r));
  const { api, seen } = fakeApi({ decline: async (t) => (seen.push(["decline", t]), await gate, parseConfirmCard(declinedCard())) });
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  await flow.load();
  const first = flow.no();
  const second = flow.no();
  const yes = flow.yes([]);
  release();
  await Promise.all([first, second, yes]);
  assert.equal(flow.state.card.state, "declined");
  assert.deepEqual(seen.filter((s) => s[0] !== "card"), [["decline", { kind: "schedule_subjects", id: TOKEN }]]);
});

test("flow: a double «Да» is one call; after it «Да» and «Нет» on a done card do nothing", async () => {
  let release;
  const gate = new Promise((r) => (release = r));
  const { api, seen } = fakeApi({ confirmSchedule: async (t, c) => (seen.push(["confirm", t, c]), await gate, parseConfirmCard(done())) });
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  await flow.load();
  const a = flow.yes([]);
  const b = flow.yes([]);
  release();
  await Promise.all([a, b]);
  await flow.yes([]);
  await flow.no();
  assert.equal(seen.filter((s) => s[0] === "confirm").length, 1);
  assert.equal(seen.filter((s) => s[0] === "decline").length, 0);
});

test("flow: «Отменить всё» goes only after something was created; a waiting card has nothing to undo", async () => {
  const { api, seen } = fakeApi();
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  await flow.load();
  await flow.undo();
  assert.equal(seen.filter((s) => s[0] === "undo").length, 0);
});

test("flow: a stop midway keeps «Да» for the rest; the repeat goes with no choices when the card shows no rows", async () => {
  const { api, seen } = fakeApi({
    card: async () => parseConfirmCard(stopped()),
    confirmSchedule: async (t, c) => (seen.push(["confirm", t, c]), parseConfirmCard(done())),
  });
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  await flow.load();
  assert.equal(flow.state.card.state, "confirmed");
  await flow.yes(choicesOf(emptyReview()));
  assert.deepEqual(seen, [["confirm", TOKEN, []]]);
  assert.equal(flow.state.card.state, "done");
});

test("flow: a refusal shows the core's text, keeps the card, frees the buttons; an unknown error is the generic text", async () => {
  for (const [thrown, text] of [
    [new Error(scheduleBusyText), scheduleBusyText],
    [new Error(scheduleNameText), scheduleNameText],
    [new Error("sql: no rows"), genericError],
    [scheduleNameText, scheduleNameText],
  ]) {
    const { api } = fakeApi({
      confirmSchedule: async () => {
        throw thrown;
      },
    });
    const flow = new ScheduleCardFlow(api, TOKEN, noop);
    await flow.load();
    await flow.yes(choicesOf(reviewOf()));
    assert.equal(flow.state.error, text);
    assert.equal(flow.state.card.state, "waiting");
    assert.equal(flow.state.busy, "");
    // The next click is a new try, and a good one clears the text.
    api.confirmSchedule = async () => parseConfirmCard(done());
    await flow.yes(choicesOf(reviewOf()));
    assert.equal(flow.state.error, "");
    assert.equal(flow.state.card.state, "done");
  }
  const { api } = fakeApi({
    undoSchedule: async () => {
      throw new Error(scheduleUndoGoneText);
    },
  });
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  api.card = async () => parseConfirmCard(done());
  await flow.load();
  await flow.undo();
  assert.equal(flow.state.error, scheduleUndoGoneText);
  assert.equal(flow.state.card.state, "done");
});

test("flow: a stale answer is a card, not an error", async () => {
  const { api } = fakeApi({ confirmSchedule: async () => parseConfirmCard(stale()) });
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  await flow.load();
  await flow.yes([]);
  assert.equal(flow.state.card.state, "stale");
  assert.equal(flow.state.error, "");
});

test("flow: the pages read the subjects again only after a call that returned a changed card, not after a refusal", async () => {
  // «Создать» and «Отменить всё» resolve true for done / confirmed / undone and false for a refusal, a stale answer or a no-op.
  const refused = fakeApi({ confirmSchedule: async () => { throw new Error(scheduleNameText); }, undoSchedule: async () => { throw new Error(scheduleUndoGoneText); } });
  const f1 = new ScheduleCardFlow(refused.api, TOKEN, noop);
  await f1.load();
  assert.equal(await f1.yes(choicesOf(reviewOf())), false);
  assert.equal(f1.state.error, scheduleNameText);
  refused.api.confirmSchedule = async () => parseConfirmCard(done());
  assert.equal(await f1.yes(choicesOf(reviewOf())), true);
  assert.equal(await f1.undo(), false);
  assert.equal(f1.state.error, scheduleUndoGoneText);
  const f2 = new ScheduleCardFlow(fakeApi({ confirmSchedule: async () => parseConfirmCard(stale()) }).api, TOKEN, noop);
  await f2.load();
  assert.equal(await f2.yes([]), false);
  const f3 = new ScheduleCardFlow(fakeApi({ confirmSchedule: async () => parseConfirmCard(stopped()) }).api, TOKEN, noop);
  await f3.load();
  assert.equal(await f3.yes([]), true);
  assert.equal(await f3.undo(), true);
  // A call that never starts (a waiting card has nothing to undo) changed nothing.
  const f4 = new ScheduleCardFlow(fakeApi().api, TOKEN, noop);
  await f4.load();
  assert.equal(await f4.undo(), false);
});

test("flow: a card that cannot be read keeps the error; «Повторить» reads it again; a card of another kind is not shown", async () => {
  let fail = true;
  const { api } = fakeApi({
    card: async () => {
      if (fail) throw new Error("boom");
      return parseConfirmCard(waiting());
    },
  });
  const flow = new ScheduleCardFlow(api, TOKEN, noop);
  await flow.load();
  assert.equal(flow.state.error, genericError);
  assert.equal(flow.state.card, null);
  fail = false;
  await flow.load();
  assert.equal(flow.state.error, "");
  assert.equal(flow.state.card.kind, "schedule_subjects");
  const other = fakeApi({ card: async () => parseConfirmCard(sched("waiting", {}, { kind: "delete_assignment", schedule: null })) });
  const f2 = new ScheduleCardFlow(other.api, TOKEN, noop);
  await f2.load();
  assert.equal(f2.state.card, null);
  assert.equal(f2.state.error, genericError);
});

test("flow: a disposed flow tells nobody; the model reads every state", () => {
  const seen = [];
  const flow = new ScheduleCardFlow(fakeApi().api, TOKEN, (s) => seen.push(s));
  flow.dispose();
  return flow.load().then(() => {
    assert.equal(seen.length, 0);
    for (const [card, kind] of [[waiting(), "waiting"], [done(), "done"], [stopped(), "confirmed"], [stale(), "stale"], [undone(), "undone"], [declinedCard(), "declined"]]) {
      assert.equal(scheduleModel(parseConfirmCard(card), emptyReview(), new Map()).view, kind);
    }
    assert.equal(scheduleModel(parseConfirmCard(sched("rejected", {}, { message: "Нельзя" })), emptyReview(), new Map()).message, "Нельзя");
  });
});

// ---- offer_button ----

/** What a page's own button is called, per action: the constants of the pages, so a rename there fails here. */
const pageLabels = {
  open_material: fileTexts.open,
  open_summary: rowItemLabels.summary,
  open_lecture_text: rowItemLabels.lectureText,
  open_transcript: rowItemLabels.transcript,
  google_sync: calendarTexts.sync,
  download_model: rowActionLabels.openModels,
  sound_check: diagnosticsTexts.checkSound,
  start_recording: manualStartTexts.open,
};

const target = (kind, id) => ({ kind, id });
const needs = { open_material: "material", open_call_link: "lesson", copy_call_link: "lesson", open_summary: "recording", open_lecture_text: "recording", open_transcript: "recording", save_as: "work" };
const offerOf = (action) => ({ action, target: needs[action] ? target(needs[action], `${needs[action]}1`) : null });

function fakeDeps(over = {}) {
  const log = [];
  const deps = {
    library: { openMaterial: async (id) => (log.push(["openMaterial", id]), { status: "opened", message: "Файл открывается в программе по умолчанию." }) },
    turnkey: { work: async (id) => (log.push(["work", id]), { id, workType: "paper", writing: { step: "none", result: "done", materialId: "mat7" } }) },
    calendar: { sync: async () => (log.push(["sync"]), { ok: true, message: "Синхронизация запущена.", state: {} }) },
    nav: {
      openLessonSummary: (id, section) => log.push(["openSummary", id, section]),
      openLessonTranscript: (id) => log.push(["openTranscript", id]),
      openPage: (page) => log.push(["openPage", page]),
      startRecording: () => log.push(["startRecording"]),
    },
    undoLatest: async () => (log.push(["undoLatest"]), { ok: true, text: "Действие отменено" }),
    ...over,
  };
  return { deps, log };
}

test("the offers are the closed enum of the core, each with a label and the kind of id it needs", () => {
  assert.deepEqual(Object.keys(offerSpecs), [...offerActions]);
  assert.equal(offerActions.length, 14);
});

test("the labels are the pages' own buttons", () => {
  for (const [action, label] of Object.entries(pageLabels)) {
    assert.equal(offerSpecs[action].label, label, action);
    assert.equal(offerModel(offerOf(action)).label, label, action);
  }
  assert.equal(offerSpecs.undo_action.label, "Отменить");
  assert.equal(offerSpecs.save_as.label, "Открыть работу");
  assert.equal(offerSpecs.open_call_link.label, "Открыть ссылку на пару");
  assert.equal(offerSpecs.copy_call_link.label, "Скопировать ссылку на пару");
});

test("a button that only navigates is labelled by what happens: «Открыть «Подключения»» (ADR-0033 §3: the page's sign-in needs the page's fields)", () => {
  for (const action of ["google_sign_in", "open_help"]) {
    assert.equal(offerSpecs[action].label, "Открыть «Подключения»", action);
    assert.equal(offerModel({ action, target: null }).label, "Открыть «Подключения»", action);
    assert.deepEqual(offerStep({ action, target: null }), { type: "page", page: "connections" }, action);
  }
  assert.notEqual(offerSpecs.google_sign_in.label, googleTexts.signIn);
  // The ones that do the page's own thing keep its words.
  assert.equal(offerSpecs.sound_check.label, diagnosticsTexts.checkSound);
  assert.equal(offerSpecs.download_model.label, rowActionLabels.openModels);
  assert.equal(offerSpecs.start_recording.label, manualStartTexts.open);
});

test("the mapping table: every action opens the page's button, calls its binding or goes to its page", async () => {
  const expected = {
    open_material: [{ type: "openMaterial", id: "material1" }, [["openMaterial", "material1"]]],
    open_summary: [{ type: "openSummary", id: "recording1" }, [["openSummary", "recording1", undefined]]],
    open_lecture_text: [{ type: "openSummary", id: "recording1", section: "lecture" }, [["openSummary", "recording1", "lecture"]]],
    open_transcript: [{ type: "openTranscript", id: "recording1" }, [["openTranscript", "recording1"]]],
    save_as: [{ type: "openWork", id: "work1" }, [["work", "work1"], ["openMaterial", "mat7"]]],
    google_sign_in: [{ type: "page", page: "connections" }, [["openPage", "connections"]]],
    google_sync: [{ type: "calendarSync" }, [["sync"]]],
    download_model: [{ type: "page", page: "models" }, [["openPage", "models"]]],
    sound_check: [{ type: "page", page: "diagnostics" }, [["openPage", "diagnostics"]]],
    open_help: [{ type: "page", page: "connections" }, [["openPage", "connections"]]],
    undo_action: [{ type: "undoLatest" }, [["undoLatest"]]],
    start_recording: [{ type: "startRecording" }, [["startRecording"]]],
    open_call_link: [{ type: "unavailable" }, []],
    copy_call_link: [{ type: "unavailable" }, []],
  };
  assert.deepEqual(Object.keys(expected).sort(), [...offerActions].sort());
  for (const action of offerActions) {
    const [step, calls] = expected[action];
    const offer = offerOf(action);
    assert.deepEqual(offerStep(offer), step, action);
    const { deps, log } = fakeDeps();
    const r = await runOffer(offer, deps);
    assert.deepEqual(log, calls, action);
    assert.equal(r.ok, action !== "open_call_link" && action !== "copy_call_link", action);
  }
});

test("the call link has no page yet: no card is drawn (not a disabled button), and a press that arrives anyway calls nothing", async () => {
  for (const action of ["open_call_link", "copy_call_link"]) {
    assert.equal(offerModel(offerOf(action)), null);
    assert.deepEqual(modelOf([offerCard(offerOf(action))]).cards, []);
    const out = render([offerCard(offerOf(action))], { offer: noop });
    assert.doesNotMatch(out, /<button|disabled|Недоступно/);
    assert.ok(!out.includes("ag-offer"));
    assert.equal(offerUnavailableText, "Недоступно в этой версии");
    const { deps, log } = fakeDeps();
    assert.deepEqual(await runOffer(offerOf(action), deps), { ok: false, text: "Недоступно в этой версии" });
    assert.deepEqual(log, []);
  }
});

test("an offer without its id, with the id of another kind, or of an unknown action presses nothing", async () => {
  for (const offer of [
    { action: "open_material", target: null },
    { action: "open_material", target: target("recording", "r1") },
    { action: "open_summary", target: target("recording", "") },
    { action: "format_disk", target: null },
  ]) {
    const { deps, log } = fakeDeps();
    assert.equal(offerModel(offer), null);
    assert.deepEqual(await runOffer(offer, deps), { ok: false, text: offerReasons.noTarget });
    assert.deepEqual(log, []);
  }
});

test("open_material: the page's call; a file that is gone shows the core's text and stays usable", async () => {
  const { deps } = fakeDeps({ library: { openMaterial: async () => ({ status: "not_found", message: "Файл не найден: возможно, его удалили." }) } });
  assert.deepEqual(await runOffer(offerOf("open_material"), deps), { ok: false, text: "Файл не найден: возможно, его удалили." });
  const thrown = fakeDeps({ library: { openMaterial: async () => { throw new Error("boom"); } } });
  assert.deepEqual(await runOffer(offerOf("open_material"), thrown.deps), { ok: false, text: offerReasons.failed });
});

test("save_as: the work's file opens; a work with no file yet or an unreadable work says why", async () => {
  const notReady = fakeDeps({ turnkey: { work: async (id) => ({ id, workType: "paper", writing: { step: "outline", result: "", materialId: "" } }) } });
  assert.deepEqual(await runOffer(offerOf("save_as"), notReady.deps), { ok: false, text: offerReasons.workNotReady });
  assert.deepEqual(notReady.log, []);
  const gone = fakeDeps({ turnkey: { work: async () => { throw new Error("no work"); } } });
  assert.deepEqual(await runOffer(offerOf("save_as"), gone.deps), { ok: false, text: offerReasons.workFailed });
  const refused = fakeDeps({ library: { openMaterial: async () => ({ status: "refused", message: "Этот файл нельзя открыть отсюда." }) } });
  assert.deepEqual(await runOffer(offerOf("save_as"), refused.deps), { ok: false, text: "Этот файл нельзя открыть отсюда." });
});

test("google_sync: Go's own message says how it went; a failed call has a plain text", async () => {
  const { deps } = fakeDeps({ calendar: { sync: async () => ({ ok: false, message: "Вход в Google не выполнен.", state: {} }) } });
  assert.deepEqual(await runOffer(offerOf("google_sync"), deps), { ok: false, text: "Вход в Google не выполнен." });
  const thrown = fakeDeps({ calendar: { sync: async () => { throw new Error("x"); } } });
  assert.deepEqual(await runOffer(offerOf("google_sync"), thrown.deps), { ok: false, text: calendarTexts.actionFailed });
});

test("undo_action: undoes the latest action once; with nothing to undo it says so", async () => {
  const { deps } = fakeDeps();
  assert.deepEqual(await runOffer(offerOf("undo_action"), deps), { ok: true, text: "", once: true });
  // The success says what went back.
  const what = fakeDeps({ undoLatest: async () => ({ ok: true, text: "", what: "Отмечены выполненными: 3 задания" }) });
  assert.deepEqual(await runOffer(offerOf("undo_action"), what.deps), { ok: true, text: "Отменено: Отмечены выполненными: 3 задания", once: true });
  const none = fakeDeps({ undoLatest: async () => null });
  assert.deepEqual(await runOffer(offerOf("undo_action"), none.deps), { ok: false, text: offerReasons.nothingToUndo });
  const refused = fakeDeps({ undoLatest: async () => ({ ok: false, text: "Нельзя отменить: действие уже отменено." }) });
  assert.deepEqual(await runOffer(offerOf("undo_action"), refused.deps), { ok: false, text: "Нельзя отменить: действие уже отменено." });
});

test("the latest action that can still be undone: newest first, undone and running ones are skipped", () => {
  const act = (id, undoable = true) => ({ type: "action", actionId: id, undo: { shown: undoable, enabled: true } });
  const requests = [[act("a1"), act("a2")], [act("a3"), act("a4", false)], [{ type: "reply" }]];
  assert.equal(latestUndoableAction(requests, {}), "a3");
  assert.equal(latestUndoableAction(requests, { a3: { done: true, busy: false, text: "" } }), "a2");
  assert.equal(latestUndoableAction(requests, { a3: { done: true }, a2: { busy: true }, a1: { done: false } }), "a1");
  assert.equal(latestUndoableAction(requests, { a3: { done: true }, a2: { done: true }, a1: { done: true } }), null);
  assert.equal(latestUndoableAction([], {}), null);
});

test("the offer_button card: one button with the page's label, no heading; nothing at all until the panel wires it", () => {
  const offer = { action: "open_transcript", target: target("recording", "r1") };
  const out = render([offerCard(offer)], { offer: noop });
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Открыть расшифровку</);
  assert.doesNotMatch(out, /<h[1-6]|title=|aria-describedby/);
  assert.equal((out.match(/<button/g) ?? []).length, 1);
  // No disabled dead button: the card renders nothing.
  const bare = render([offerCard(offer)]);
  assert.doesNotMatch(bare, /<button|Открыть расшифровку|ag-offer/);
});

test("undo_action: «Отменить» shows beside it what it would undo, and says what went back", () => {
  const model = offerModel({ action: "undo_action", target: null });
  assert.equal(model.label, "Отменить");
  const view = (actions, result = null) =>
    renderToStaticMarkup(h(OfferControlView, { offer: model, actions: { ...baseActions, offer: noop, ...actions }, busy: false, result }));
  const out = view({ undoTarget: () => "Отмечены выполненными: 3 задания" });
  // TASK-368: the undo offer is a small secondary button: an icon with a tooltip, named by the aria-label.
  assert.ok(isOn(out, "Отменить"));
  assert.match(out, /<use href="#i-undo"><\/use>/);
  assert.ok(out.includes("Отмечены выполненными: 3 задания"));
  // Nothing to undo: the existing text stays (no target line); other offers never show one.
  assert.doesNotMatch(view({ undoTarget: () => "" }), /ag-act-note/);
  assert.doesNotMatch(view({}), /ag-act-note/);
  const other = renderToStaticMarkup(
    h(OfferControlView, { offer: offerModel({ action: "open_material", target: target("material", "m1") }), actions: { ...baseActions, offer: noop, undoTarget: () => "Что-то" }, busy: false, result: null }),
  );
  assert.ok(!other.includes("Что-то"));
  // After the undo the line says «Отменено: …» instead of the target, and the button is off.
  const done = view({ undoTarget: () => "Отмечены выполненными: 3 задания" }, { ok: true, text: "Отменено: Отмечены выполненными: 3 задания", once: true });
  assert.equal((done.match(/Отмечены выполненными: 3 задания/g) ?? []).length, 1);
  assert.ok(isOff(done, "Отменить"));
  assert.ok(done.includes("Отменено: Отмечены выполненными: 3 задания"));
  // Not wired: nothing is drawn, not even the target line.
  assert.equal(renderToStaticMarkup(h(OfferControlView, { offer: model, actions: { ...baseActions, undoTarget: () => "x" }, busy: false, result: null })), "");
});

test("the action line an undo_action offer would undo: the latest one, in its own words", () => {
  const cards = modelOf([
    raw({ actionId: "a1", result: { kind: "marked_done", count: 3, setting: "", value: "", excerpt: [] } }),
    raw({ actionId: "a2", result: { kind: "note_text", count: 0, setting: "", value: "", excerpt: ["строка"] } }),
  ]).cards;
  const last = latestUndoableCard([cards], {});
  assert.equal(last.actionId, "a2");
  assert.equal(actionWording(last, {}, new Date()), "Записано в «Мой конспект»");
  assert.equal(latestUndoableCard([cards], { a2: { done: true, busy: false, text: "" } }).actionId, "a1");
  assert.equal(actionWording(latestUndoableCard([cards], { a2: { done: true } }), {}, new Date()), "Отмечены выполненными: 3 задания");
  assert.equal(latestUndoableCard([cards], { a1: { done: true }, a2: { done: true } }), null);
  assert.equal(latestUndoableCard([], {}), null);
  // An added assignment reads with its subject, as the line does.
  const added = modelOf([raw({ actionId: "a3", assignment: { id: "x1", title: "Задачи", subjectId: "s1", due: "2026-10-24", time: "18:30" } })]).cards;
  const now = new Date(2026, 9, 22, 10, 0);
  assert.ok(actionWording(latestUndoableCard([added], {}), { s1: "Алгебра" }, now).startsWith("Задание добавлено: Задачи · Алгебра"));
});

test("the press: the button is off while it runs, shows the reason after a refusal and stays usable; an undo done goes off", () => {
  const model = offerModel({ action: "open_material", target: target("material", "m1") });
  const view = (busy, result, actions = { ...baseActions, offer: noop }) => renderToStaticMarkup(h(OfferControlView, { offer: model, actions, busy, result }));
  assert.match(view(false, null), /<button(?![^>]*disabled)[^>]*>Открыть</);
  assert.match(view(true, null), /<button[^>]*disabled[^>]*>Открыть</);
  const refused = view(false, { ok: false, text: "Файл не найден: возможно, его удалили." });
  assert.ok(refused.includes("Файл не найден: возможно, его удалили."));
  assert.match(refused, /<button(?![^>]*disabled)[^>]*>Открыть</);
  assert.match(refused, /class="ag-act-note small muted"/);
  assert.doesNotMatch(view(false, { ok: true, text: "" }), /ag-act-note/);
  assert.match(view(false, { ok: true, text: "", once: true }), /<button[^>]*disabled[^>]*>Открыть</);
  // The panel has not wired the press: no dead button.
  assert.equal(view(false, null, baseActions), "");
});

test("the offer on an action line is the same control (start_recording under a stopped recording)", () => {
  const line = raw({
    actionId: "act1",
    result: { kind: "recording_stopped", count: 0, setting: "", value: "", excerpt: [] },
    offer: { action: "start_recording", target: null },
  });
  const out = render([line], { offer: noop });
  assert.ok(out.includes("Запись остановлена"));
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Начать запись</);
  assert.doesNotMatch(out, />Отменить</);
});

test("the words of the new cards follow the glossary", () => {
  const texts = [
    ...Object.values(scheduleTexts),
    ...Object.values(offerReasons),
    ...Object.values(offerSpecs).map((s) => s.label),
    offerUnavailableText,
    view(waiting()),
    view(done(), { review: reviewOf() }),
    view(undone()),
    view(stale()),
  ].join("\n");
  assert.doesNotMatch(texts, /Домашн|Распознаван|ДЗ|Закрыть/);
});
