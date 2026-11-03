// TASK-324 (part A): the wave-10 cards of the agent panel. Parsing of the new card kinds and fields, the stub for an unknown
// kind, the wording of the result lines of the U actions, the delete_assignment confirm card in each state and the journal
// words. Fixtures only: the catalog is not served to the model yet.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createElement as h } from "react";
import server from "react-dom/server";
import { cardKinds, parseCard, parseLineResult, parseOffer, parseUpdate } from "../.test-build/bindings/agent.js";
import { entryModel, offerModel, resultTexts, resultWording, stepText, undoableActions } from "../.test-build/agentView.js";
import { AgentRequest, OfferButton } from "../.test-build/agent/AgentCards.js";
import { ConfirmCard, ConfirmCardPending, ConfirmCardView } from "../.test-build/agent/ConfirmCard.js";
import {
  assignmentInUseText,
  cardKinds as confirmKinds,
  confirmErrorText,
  errorTexts,
  genericError,
  parseConfirmCard,
} from "../.test-build/bindings/agentConfirm.js";
import { ConfirmFlow, confirmCardModel, confirmTexts, deleteAssignmentQuestion } from "../.test-build/agentConfirmView.js";
import { lineModel, recordingStopNoUndo } from "../.test-build/journalView.js";
import { doneTexts } from "../.test-build/assignmentDoneView.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;
const actions = { press: noop, revise: noop, answer: noop, undo: noop, undoAll: noop, undoNote: () => undefined };
const evil = "<img src=x onerror=alert(1)>";

// ---- Fixtures ----

/** A card as the Go core sends it (every field; offer and result are omitted when empty). */
function raw(over) {
  return {
    kind: "action_line", plan: null, questions: null, jobId: "", artifact: "", target: null, actionId: "act1", answerId: "",
    cannotDo: null, text: "", assignments: null, assignment: null, stopReason: "", workId: "", outcome: "", ...over,
  };
}

const result = (kind, over = {}) => ({ kind, count: 0, setting: "", value: "", excerpt: [], ...over });
const line = (r, over = {}) => raw({ result: r, ...over });

function modelOf(cards) {
  const u = parseUpdate({
    threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "",
    problem: "", progress: false, cards,
  });
  return entryModel({ key: "k", seq: 1, threadId: "t", turn: 1, text: "", scope: null, callProblem: "", update: u });
}

const render = (cards, extra = {}) => renderToStaticMarkup(h(AgentRequest, { model: modelOf(cards), actions: { ...actions, ...extra } }));

/** The text of the first action card, without markup. */
const textOf = (r, over) => modelOf([line(r, over)]).cards[0].text;

// ---- Parsing ----

test("the new card kinds are known and keep their fields", () => {
  for (const k of ["schedule_subjects", "delete_assignment", "offer_button"]) {
    assert.ok(cardKinds.includes(k), k);
  }
  const del = parseCard(raw({ kind: "delete_assignment", target: { kind: "assignment", id: "a1" }, actionId: "" }));
  assert.equal(del.kind, "delete_assignment");
  assert.deepEqual(del.target, { kind: "assignment", id: "a1" });
  const sched = parseCard(raw({ kind: "schedule_subjects", target: { kind: "schedule_subjects", id: "tok" }, actionId: "" }));
  assert.equal(sched.kind, "schedule_subjects");
  assert.deepEqual(sched.target, { kind: "schedule_subjects", id: "tok" });
  const offer = parseCard(raw({ kind: "offer_button", actionId: "", offer: { action: "open_summary", target: { kind: "recording", id: "r1" } } }));
  assert.deepEqual(offer.offer, { action: "open_summary", target: { kind: "recording", id: "r1" } });
});

test("offer and result are optional: an older core and every plain card read them as null", () => {
  const c = parseCard(raw({}));
  assert.equal(c.offer, null);
  assert.equal(c.result, null);
  assert.equal(parseCard(raw({ offer: null, result: null })).result, null);
});

test("the stop-recording line carries an offer without a target", () => {
  const c = parseCard(line(result("recording_stopped"), { offer: { action: "start_recording", target: null } }));
  assert.deepEqual(c.offer, { action: "start_recording", target: null });
  assert.equal(c.result.kind, "recording_stopped");
});

test("the result keeps every field; a field of the wrong type reads as empty, never throws", () => {
  const r = parseCard(line(result("note_text", { excerpt: ["раз", "два"] }))).result;
  assert.deepEqual(r, { kind: "note_text", count: 0, setting: "", value: "", excerpt: ["раз", "два"] });
  assert.deepEqual(parseLineResult({ kind: "setting", count: "3", setting: 5, value: null, excerpt: ["ok", 7, null] }), {
    kind: "setting", count: 0, setting: "", value: "", excerpt: ["ok"],
  });
  assert.deepEqual(parseLineResult({ kind: "marked_done", count: 3 }).excerpt, []);
  for (const bad of [null, undefined, "x", [], {}, { kind: "" }, { kind: 4 }]) {
    assert.equal(parseLineResult(bad), null, JSON.stringify(bad));
  }
});

test("a malformed offer or result is null and the line keeps its «Отменить»", () => {
  for (const bad of [{ action: 1 }, { action: "" }, "start_recording", { action: "open_help", target: { kind: 1, id: "x" } }]) {
    assert.equal(parseOffer(bad), null, JSON.stringify(bad));
  }
  const c = parseCard(raw({ offer: { action: 5 }, result: "boom" }));
  assert.equal(c.kind, "action_line");
  assert.equal(c.offer, null);
  assert.equal(c.result, null);
  assert.match(render([raw({ offer: { action: 5 }, result: "boom" })]), />Отменить</);
});

test("an unknown card kind becomes an «unknown» card instead of being dropped", () => {
  const c = parseCard({ kind: "hologram", whatever: [1, 2] });
  assert.equal(c.kind, "unknown");
  assert.equal(c.actionId, "");
  assert.equal(c.offer, null);
  assert.equal(c.result, null);
  const u = parseUpdate({
    threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "",
    problem: "", cards: [raw({ kind: "reply", text: "ok" }), { kind: "hologram" }],
  });
  assert.deepEqual(u.cards.map((x) => x.kind), ["reply", "unknown"]);
});

test("strictness stays: a known kind that is malformed, or a kind that is no string, is still dropped", () => {
  assert.throws(() => parseCard(raw({ kind: "reply", text: 5 })));
  assert.throws(() => parseCard(raw({ kind: 7 })));
  assert.throws(() => parseCard(null));
  const u = parseUpdate({
    threadId: "t", turn: 1, subjectId: "", lessonId: "", assignmentId: "", screen: "today", status: "DONE", stopReason: "",
    problem: "", cards: [raw({ kind: "reply", text: 5 }), raw({ kind: "reply", text: "ok" }), { kind: 7 }],
  });
  assert.equal(u.cards.length, 1);
  // The retired free_work card stays dropped: an update would not make it known.
  assert.equal(parseUpdate({ ...u, cards: [{ kind: "free_work" }] }).cards.length, 0);
});

// ---- The stub of an unknown kind ----

test("the stub says to update the app, quietly, with nothing to press", () => {
  const out = render([{ kind: "hologram" }]);
  assert.ok(out.includes("Эту часть ответа нельзя показать в этой версии paratech. Обновите приложение."));
  assert.equal(resultTexts.unknownCard, "Эту часть ответа нельзя показать в этой версии paratech. Обновите приложение.");
  assert.doesNotMatch(out, /<button|<h3|<input/);
  assert.match(out, /role="status"/);
});

// ---- Result lines ----

test("marked_done: «Отмечено выполненным: 1 задание» for one, «Отмечены выполненными: N» for several, with the plural of «задание»", () => {
  assert.equal(textOf(result("marked_done", { count: 1 })), "Отмечено выполненным: 1 задание");
  const cases = [[2, "задания"], [3, "задания"], [4, "задания"], [5, "заданий"], [11, "заданий"], [14, "заданий"], [21, "задание"], [22, "задания"], [50, "заданий"]];
  for (const [n, word] of cases) {
    assert.equal(textOf(result("marked_done", { count: n })), `Отмечены выполненными: ${n} ${word}`, String(n));
  }
  const out = render([line(result("marked_done", { count: 3 }))]);
  assert.ok(out.includes("Отмечены выполненными: 3 задания"));
  assert.match(out, />Отменить</);
  assert.doesNotMatch(out, /Действие выполнено/);
  // The words are the assignment toast's (assignmentDoneView).
  assert.equal(resultTexts.markedDoneMany, doneTexts.markedMany);
  assert.equal(resultTexts.markedUndone, doneTexts.unmarked);
  assert.equal(resultTexts.markedUndoneMany, doneTexts.unmarkedMany);
});

test("marked_undone: «Возвращено в работу: 1 задание», «Возвращены в работу: N заданий»", () => {
  assert.equal(textOf(result("marked_undone", { count: 1 })), "Возвращено в работу: 1 задание");
  assert.equal(textOf(result("marked_undone", { count: 7 })), "Возвращены в работу: 7 заданий");
  assert.match(render([line(result("marked_undone", { count: 2 }))]), />Отменить</);
});

test("a count outside 1.. reads as the generic line", () => {
  for (const count of [0, -2, 2.5]) {
    assert.equal(textOf(result("marked_done", { count })), "Действие выполнено", String(count));
  }
});

test("note_text: the sentence, then the excerpt as plain muted lines (no heading), then «Отменить»", () => {
  const out = render([line(result("note_text", { excerpt: ["Первая строка", "Вторая строка", "Третья"] }))]);
  assert.ok(out.includes("Записано в «Мой конспект»:"));
  for (const t of ["Первая строка", "Вторая строка", "Третья"]) {
    assert.match(out, new RegExp(`<span class="ag-act-ex small muted">${t}</span>`));
  }
  assert.ok(out.indexOf("Записано в «Мой конспект»:") < out.indexOf("Первая строка"));
  assert.ok(out.indexOf("Третья") < out.indexOf(">Отменить<"));
  assert.doesNotMatch(out, /<h[1-6]/);
});

test("note_text: no excerpt is the sentence alone; blank lines are left out; at most three lines", () => {
  const none = render([line(result("note_text"))]);
  assert.ok(none.includes("Записано в «Мой конспект»"));
  assert.ok(!none.includes("Записано в «Мой конспект»:"));
  assert.doesNotMatch(none, /ag-act-ex/);
  assert.deepEqual(resultWording(result("note_text", { excerpt: ["", "  ", "a"] })), { text: "Записано в «Мой конспект»:", detail: ["a"] });
  assert.equal(resultWording(result("note_text", { excerpt: ["1", "2", "3", "4", "5"] })).detail.length, 3);
});

test("note_text: the saved text is text, never markup", () => {
  const out = render([line(result("note_text", { excerpt: [evil] }))]);
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

test("the other note lines and their words", () => {
  const words = {
    note_upload: "Файл добавлен в «Мой конспект»",
    note_move: "«Мой конспект» перенесён",
    note_rename: "«Мой конспект» переименован",
    note_remove: "«Мой конспект» удалён",
  };
  for (const [kind, text] of Object.entries(words)) {
    assert.equal(textOf(result(kind)), text, kind);
    const out = render([line(result(kind))]);
    assert.ok(out.includes(text), kind);
    assert.match(out, />Отменить</, kind);
  }
});

test("recording_started: «Запись началась» and the undo is «Остановить»", () => {
  const out = render([line(result("recording_started"))]);
  assert.ok(out.includes("Запись началась"));
  assert.match(out, />Остановить</);
  assert.doesNotMatch(out, />Отменить</);
  // The press is the existing undo of the action.
  const undone = [];
  const el = AgentRequest({ model: modelOf([line(result("recording_started"))]), actions: { ...actions, undo: (id) => undone.push(id) } });
  const buttons = [];
  const walk = (n) => {
    if (n === null || typeof n !== "object") return;
    if (Array.isArray(n)) return n.forEach(walk);
    if (n.type && typeof n.type === "function" && n.props && typeof n.props.onClick === "function") buttons.push(n.props);
    walk(n.props?.children);
    if (typeof n.type === "function" && n.type.name === "Card") walk(n.type(n.props));
  };
  walk(el);
  buttons.find((b) => b.children === "Остановить").onClick();
  assert.deepEqual(undone, ["act1"]);
});

test("recording_started: while stopping and after, the line says so", () => {
  const busy = render([line(result("recording_started"))], { undoNote: () => ({ busy: true, done: false, text: "" }) });
  assert.match(busy, /<button[^>]*disabled[^>]*>Остановка…</);
  const done = render([line(result("recording_started"))], { undoNote: () => ({ busy: false, done: true, text: "" }) });
  assert.ok(done.includes("Запись остановлена"));
  assert.doesNotMatch(done, /<button|Запись началась/);
});

test("recording_started: after «Остановить» the line offers «Начать запись» when the panel wires offers", () => {
  const stopped = { undoNote: () => ({ busy: false, done: true, text: "" }), offer: noop };
  const out = render([line(result("recording_started"))], stopped);
  assert.ok(out.includes("Запись остановлена"));
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Начать запись</);
  assert.doesNotMatch(out, />Остановить<|>Отменить<|Запись началась/);
  // Before the stop there is no such button, and a line without that offer wiring shows none.
  assert.doesNotMatch(render([line(result("recording_started"))], { offer: noop }), /Начать запись/);
  assert.deepEqual(modelOf([line(result("recording_started"))]).cards[0].afterUndoOffer, { label: "Начать запись", offer: { action: "start_recording", target: null } });
  assert.equal(modelOf([line(result("marked_done", { count: 1 }))]).cards[0].afterUndoOffer, null);
});

test("a refusal to stop is shown as text under the line", () => {
  const out = render([line(result("recording_started"))], { undoNote: () => ({ busy: false, done: false, text: "Запись уже остановлена." }) });
  assert.ok(out.includes("Запись уже остановлена."));
  assert.match(out, />Остановить</);
});

test("recording_stopped: «Запись остановлена» and the offer «Начать запись», no «Отменить»", () => {
  const cards = [line(result("recording_stopped"), { offer: { action: "start_recording", target: null } })];
  const out = render(cards, { offer: noop });
  assert.ok(out.includes("Запись остановлена"));
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Начать запись</);
  assert.doesNotMatch(out, />Отменить</);
  assert.deepEqual(undoableActions(modelOf(cards).cards), []);
});

test("the offer button calls actions.offer with the offer; without the prop nothing is drawn at all", () => {
  const model = offerModel({ action: "start_recording", target: null });
  assert.deepEqual(model, { label: "Начать запись", offer: { action: "start_recording", target: null } });
  const seen = [];
  const on = OfferButton({ offer: model, actions: { ...actions, offer: (o) => seen.push(o) } });
  assert.equal(on.props.disabled, false);
  on.props.onClick();
  assert.deepEqual(seen, [{ action: "start_recording", target: null }]);
  assert.equal(OfferButton({ offer: model, actions }), null);
  const busy = OfferButton({ offer: model, actions: { ...actions, offer: noop, busy: true } });
  assert.equal(busy.props.disabled, true);
  const out = render([line(result("recording_stopped"), { offer: { action: "start_recording", target: null } })]);
  assert.doesNotMatch(out, /<button/);
  assert.ok(out.includes("Запись остановлена"));
});

test("recording_stopped without an offer is the sentence alone, still no «Отменить»", () => {
  const out = render([line(result("recording_stopped"))]);
  assert.ok(out.includes("Запись остановлена"));
  assert.doesNotMatch(out, /<button/);
});

test("an offer the panel has no label for draws no button and the line keeps «Отменить»", () => {
  assert.equal(offerModel({ action: "open_summary", target: null }), null);
  assert.equal(offerModel(null), null);
  const out = render([line(result("marked_done", { count: 1 }), { offer: { action: "open_summary", target: null } })]);
  assert.match(out, />Отменить</);
});

test("setting lines: recording_prompts and ac_power_only, on and off", () => {
  const t = (setting, value) => textOf(result("setting", { setting, value }));
  assert.equal(t("recording_prompts", "on"), "Предложение записи в начале пары включено");
  assert.equal(t("recording_prompts", "off"), "Предложение записи в начале пары выключено");
  assert.equal(t("ac_power_only", "on"), "Обработка только от розетки включена");
  assert.equal(t("ac_power_only", "off"), "Обработка только от розетки выключена");
  const out = render([line(result("setting", { setting: "recording_prompts", value: "off" }))]);
  assert.ok(out.includes("Предложение записи в начале пары выключено"));
  assert.match(out, />Отменить</);
});

test("setting line: the recognition load in the words of the settings page", () => {
  const t = (value) => textOf(result("setting", { setting: "recognition_load", value }));
  assert.equal(t("gentle"), "Нагрузка при расшифровке: бережная");
  assert.equal(t("normal"), "Нагрузка при расшифровке: обычная");
  assert.equal(t("max"), "Нагрузка при расшифровке: максимальная");
  assert.match(render([line(result("setting", { setting: "recognition_load", value: "max" }))]), />Отменить</);
});

test("an unknown kind, setting or value falls back to the generic line, which keeps «Отменить»", () => {
  const unknowns = [
    result("hologram"),
    result("setting", { setting: "brightness", value: "on" }),
    result("setting", { setting: "recording_prompts", value: "maybe" }),
    result("setting", { setting: "recognition_load", value: "turbo" }),
    result("setting", { setting: "toString", value: "on" }),
  ];
  for (const r of unknowns) {
    const out = render([line(r)]);
    assert.ok(out.includes("Действие выполнено"), JSON.stringify(r));
    assert.match(out, />Отменить</, JSON.stringify(r));
  }
});

test("«Отменить» of a result line calls undo(actionId); after it the line says «Отменено»", () => {
  const cards = [line(result("marked_done", { count: 2 }))];
  const done = render(cards, { undoNote: () => ({ busy: false, done: true, text: "" }) });
  assert.ok(done.includes("Отменено"));
  assert.doesNotMatch(done, /Отмечено выполненным|<button/);
  const m = modelOf(cards).cards[0];
  assert.equal(m.undo.enabled, true);
  assert.equal(m.undo.label, "Отменить");
  assert.deepEqual(undoableActions(modelOf(cards).cards), ["act1"]);
  // After the undo the saved text is gone from the line as well.
  const note = render([line(result("note_text", { excerpt: ["секрет"] }))], { undoNote: () => ({ busy: false, done: true, text: "" }) });
  assert.doesNotMatch(note, /секрет/);
});

test("a plain line is as before: «Действие выполнено», «Отменить», the assignment text of create_assignment", () => {
  const out = render([raw({})]);
  assert.ok(out.includes("Действие выполнено"));
  assert.match(out, />Отменить</);
  assert.deepEqual(modelOf([raw({})]).cards[0].detail, []);
});

// ---- delete_assignment: binding ----

/** The card as agentconfirm_assignment.go sends it. */
function asg(over = {}) {
  return {
    kind: "delete_assignment", targetKind: "assignment", targetId: "a1", state: "waiting", message: "", assignmentId: "a1",
    title: "Задачи 1–7", subject: "Матанализ", due: new Date(2026, 9, 24, 18, 30).toISOString(), recipient: "", recipientName: "",
    role: "", reason: "", subjectId: "", options: null, emailSubject: "", attachmentName: "", attachmentSize: 0,
    attachmentFormat: "", draftVersion: 0, notice: "Вместе с заданием удалится и событие в Google Календаре.", deletion: null,
    schedule: null, ...over,
  };
}

const done = () => asg({ state: "done", title: "", subject: "", due: "", notice: "" });
const declined = () => asg({ state: "declined", title: "", subject: "", due: "", notice: "" });

const view = (c, extra = {}) =>
  renderToStaticMarkup(h(ConfirmCardView, { model: confirmCardModel(c), busy: "", error: "", onYes: noop, onNo: noop, ...extra }));

test("delete_assignment is a confirm card kind and parses as the Go card", () => {
  assert.ok(confirmKinds.includes("delete_assignment"));
  const c = parseConfirmCard(asg());
  assert.equal(c.kind, "delete_assignment");
  assert.equal(c.targetKind, "assignment");
  assert.equal(c.assignmentId, "a1");
  assert.equal(c.title, "Задачи 1–7");
  assert.throws(() => parseConfirmCard(asg({ state: "bogus" })));
});

test("the known refusal of the delete is shown, any other text is generic", () => {
  assert.ok(errorTexts.includes(assignmentInUseText));
  assert.equal(confirmErrorText(new Error(assignmentInUseText)), assignmentInUseText);
  assert.match(assignmentInUseText, /Проверьте «Письма» и нажмите «Да» ещё раз/);
  assert.equal(confirmErrorText("sql: no rows"), genericError);
});

// ---- delete_assignment: the card ----

test("waiting: the heading is the question in «» with the title; subject and deadline rows; «Не удалять» then «Да, удалить»", () => {
  const out = view(asg());
  assert.ok(out.includes("Удалить «Задачи 1–7» и событие в Google Календаре?"));
  assert.doesNotMatch(out, /[„“]/);
  assert.match(out, /Предмет/);
  assert.ok(out.includes("Матанализ"));
  assert.match(out, /Срок/);
  assert.ok(out.includes("сб, 24 окт., 18:30"));
  assert.ok(out.indexOf(">Не удалять<") > 0 && out.indexOf(">Не удалять<") < out.indexOf("Да, удалить"));
  assert.doesNotMatch(out, />Нет<|>Да<|<span>Да</);
  // The confirming button is the danger one; the refusing one is quiet.
  assert.match(out, /<button[^>]*class="btn danger sm"[^>]*><span class="btn-labels"><span>Да, удалить</);
  // One heading, no repeated caption, no second note with the same words, nothing focused.
  assert.equal((out.match(/<h3/g) ?? []).length, 1);
  assert.doesNotMatch(out, /Нужно подтверждение|Вместе с заданием|autofocus|autoFocus/i);
  assert.equal(deleteAssignmentQuestion("Задачи 1–7"), "Удалить «Задачи 1–7» и событие в Google Календаре?");
  assert.equal(confirmTexts.deleteYes, "Да, удалить");
  assert.equal(confirmTexts.deleteNo, "Не удалять");
});

test("waiting: a missing subject, a missing deadline and an empty title are left out, not shown empty", () => {
  const out = view(asg({ subject: "", due: "" }));
  assert.doesNotMatch(out, /Предмет|Срок|<dl/);
  const m = confirmCardModel(asg({ subject: "", due: "" }));
  assert.deepEqual(m.rows, []);
  for (const title of ["", "   "]) {
    assert.ok(view(asg({ title })).includes("Удалить задание и событие в Google Календаре?"), JSON.stringify(title));
  }
  assert.equal(confirmTexts.deleteAssignment, "Удалить задание и событие в Google Календаре?");
  assert.deepEqual(confirmCardModel(asg({ subject: "", due: "" })).rows.map((r) => r.label), []);
  assert.deepEqual(confirmCardModel(asg({ due: "" })).rows.map((r) => r.label), ["Предмет"]);
  assert.deepEqual(confirmCardModel(asg({ subject: "" })).rows.map((r) => r.label), ["Срок"]);
});

test("waiting: the title and subject are text, never markup", () => {
  const out = view(asg({ title: evil, subject: evil }));
  assert.ok(!out.includes("<img"));
  assert.ok(out.includes("&lt;img src=x onerror=alert(1)&gt;"));
});

test("done: «Задание удалено вместе с событием…» and «Открыть «Задания»» (the student can restore it there)", () => {
  const seen = [];
  const nav = { openAssignment: (id, opts) => seen.push([id, opts]), openTeacher: noop, openSubjects: noop };
  const out = view(done(), { nav });
  assert.ok(out.includes("Задание удалено вместе с событием в Google Календаре."));
  assert.doesNotMatch(out, /Домашние задания/);
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Открыть «Задания»</);
  assert.doesNotMatch(out, />Не удалять<|Да, удалить/);
  // The press opens the assignment page by the card's target id.
  const tree = ConfirmCardView({ model: confirmCardModel(done()), busy: "", error: "", onYes: noop, onNo: noop, nav });
  const find = (n, out = []) => {
    if (n === null || typeof n !== "object") return out;
    if (Array.isArray(n)) return n.forEach((x) => find(x, out)), out;
    if (typeof n.props?.onClick === "function") out.push(n.props);
    return find(n.props?.children, out);
  };
  find(tree).find((p) => p.children === "Открыть «Задания»").onClick();
  assert.deepEqual(seen, [["a1", undefined]]);
  // Without the window's navigation, and while waiting or after «Не удалять», there is no such button.
  assert.doesNotMatch(view(done()), /<button/);
  assert.doesNotMatch(view(asg(), { nav }), /Открыть «Задания»/);
  assert.doesNotMatch(view(declined(), { nav }), /<button/);
});

test("declined: «Оставлено без изменений», no buttons", () => {
  const out = view(declined());
  assert.ok(out.includes("Оставлено без изменений"));
  assert.doesNotMatch(out, /<button/);
});

test("busy: the buttons are off and «Да, удалить» says it works", () => {
  const out = view(asg(), { busy: "confirm" });
  assert.match(out, /<button[^>]*disabled[^>]*>Не удалять</);
  assert.match(out, /<button[^>]*disabled/);
  assert.ok(out.includes("Выполняется…"));
});

test("loading: a small «Загрузка…» line with no heading; the error has the reason and «Повторить»", () => {
  const loading = renderToStaticMarkup(h(ConfirmCardPending, { error: "" }));
  assert.ok(loading.includes("Загрузка…"));
  assert.doesNotMatch(loading, /<button|role="alert"|<h[1-6]|<section|Нужно подтверждение/);
  const failed = renderToStaticMarkup(h(ConfirmCardPending, { error: genericError, onReload: noop }));
  assert.ok(failed.includes(genericError));
  assert.match(failed, /role="alert"/);
  assert.match(failed, />Повторить</);
  assert.doesNotMatch(failed, /Обновить карточку/);
  const stateless = renderToStaticMarkup(h(ConfirmCardPending, { error: genericError }));
  assert.doesNotMatch(stateless, /<button/);
  // The card of the panel starts in the loading state.
  const api = { card: async () => asg(), confirm: async () => done(), decline: async () => declined() };
  const real = renderToStaticMarkup(h(ConfirmCard, { api, deleteApi: {}, target: { kind: "assignment", id: "a1" } }));
  assert.ok(real.includes("Загрузка…"));
  assert.doesNotMatch(real, /Нужно подтверждение/);
});

test("«Да» confirms with the assignment target; «Нет» declines it; both end the card", async () => {
  const seen = [];
  const api = {
    card: async (t) => (seen.push(["card", t]), asg()),
    confirm: async (c) => (seen.push(["confirm", c.targetKind, c.targetId]), done()),
    decline: async (t) => (seen.push(["decline", t]), declined()),
  };
  const yes = new ConfirmFlow(api, noop);
  await yes.load({ kind: "assignment", id: "a1" });
  await yes.yes();
  assert.equal(yes.state.card.state, "done");
  const no = new ConfirmFlow(api, noop);
  await no.load({ kind: "assignment", id: "a1" });
  await no.no();
  assert.equal(no.state.card.state, "declined");
  assert.deepEqual(seen, [
    ["card", { kind: "assignment", id: "a1" }],
    ["confirm", "assignment", "a1"],
    ["card", { kind: "assignment", id: "a1" }],
    ["decline", { kind: "assignment", id: "a1" }],
  ]);
});

test("a refused «Да, удалить» (a letter may still go out): the reason in words, the card re-read, both buttons stay, «Открыть задание» is the way out", async () => {
  const api = {
    card: async () => asg(),
    confirm: async () => {
      throw new Error(assignmentInUseText);
    },
    decline: async () => declined(),
  };
  const flow = new ConfirmFlow(api, noop);
  await flow.load({ kind: "assignment", id: "a1" });
  await flow.yes();
  assert.equal(flow.state.error, assignmentInUseText);
  assert.equal(flow.state.card.state, "waiting");
  assert.equal(flow.state.busy, "");
  const seen = [];
  const nav = { openAssignment: (id) => seen.push(id), openTeacher: noop, openSubjects: noop };
  const out = view(flow.state.card, { error: flow.state.error, nav });
  assert.ok(out.includes(assignmentInUseText));
  assert.match(out, /role="alert"/);
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Не удалять</);
  assert.match(out, /<button(?![^>]*disabled)[^>]*><span class="btn-labels"><span>Да, удалить</);
  assert.match(out, /<button(?![^>]*disabled)[^>]*>Открыть задание</);
  // The way out opens the assignment by the card's id; other errors and a card without the window's navigation have none.
  const tree = ConfirmCardView({ model: confirmCardModel(flow.state.card), busy: "", error: flow.state.error, onYes: noop, onNo: noop, nav });
  const buttons = [];
  const walk = (n) => {
    if (n === null || typeof n !== "object") return;
    if (Array.isArray(n)) return n.forEach(walk);
    if (typeof n.props?.onClick === "function") buttons.push(n.props);
    walk(n.props?.children);
  };
  walk(tree);
  buttons.find((p) => p.children === "Открыть задание").onClick();
  assert.deepEqual(seen, ["a1"]);
  assert.doesNotMatch(view(flow.state.card, { error: genericError, nav }), /Открыть задание/);
  assert.doesNotMatch(view(flow.state.card, { error: flow.state.error }), /Открыть задание/);
});

test("an unknown error of the delete is the generic text", async () => {
  const api = {
    card: async () => asg(),
    confirm: async () => {
      throw new Error("sql: connection reset");
    },
    decline: async () => declined(),
  };
  const flow = new ConfirmFlow(api, noop);
  await flow.load({ kind: "assignment", id: "a1" });
  await flow.yes();
  assert.equal(flow.state.error, genericError);
});

test("a failed load keeps the error and the reload works", async () => {
  let fail = true;
  const api = {
    card: async () => {
      if (fail) throw new Error("boom");
      return asg();
    },
    confirm: async () => done(),
    decline: async () => declined(),
  };
  const flow = new ConfirmFlow(api, noop);
  await flow.load({ kind: "assignment", id: "a1" });
  assert.equal(flow.state.error, genericError);
  assert.equal(flow.state.card, null);
  fail = false;
  await flow.load({ kind: "assignment", id: "a1" });
  assert.equal(flow.state.error, "");
  assert.equal(flow.state.card.kind, "delete_assignment");
});

// ---- delete_assignment: in the panel ----

test("in the panel: a delete_assignment card reads the card of target kind assignment", () => {
  const cards = [raw({ kind: "delete_assignment", target: { kind: "assignment", id: "a1" }, actionId: "" })];
  const m = modelOf(cards).cards[0];
  assert.equal(m.type, "confirm");
  assert.deepEqual(m.target, { kind: "assignment", id: "a1" });
  const api = { card: async () => asg(), confirm: async () => done(), decline: async () => declined() };
  const out = render(cards, { confirm: { api, deleteApi: {} } });
  assert.ok(out.includes("Загрузка…"));
  assert.doesNotMatch(out, /Нужно подтверждение/);
});

test("in the panel: without the confirm calls, or with another target kind, the card says it is unavailable", () => {
  const ok = [raw({ kind: "delete_assignment", target: { kind: "assignment", id: "a1" }, actionId: "" })];
  assert.ok(render(ok).includes("Подтверждение пока недоступно."));
  for (const target of [{ kind: "proposal", id: "p1" }, { kind: "assignment", id: "" }, null]) {
    const cards = [raw({ kind: "delete_assignment", target, actionId: "" })];
    assert.equal(modelOf(cards).cards[0].target, null, JSON.stringify(target));
  }
  // A confirm card keeps its own kinds and does not take an assignment target.
  assert.equal(modelOf([raw({ kind: "confirm", target: { kind: "assignment", id: "a1" }, actionId: "" })]).cards[0].target, null);
  assert.deepEqual(modelOf([raw({ kind: "confirm", target: { kind: "preview", id: "r1" }, actionId: "" })]).cards[0].target, { kind: "preview", id: "r1" });
});

// ---- Journal and plan words ----

const jline = (over) => ({
  kind: "action", at: "2026-11-03T10:00:00Z", updatedAt: "", subjectId: "", actionId: "a1", threadId: "t", turn: 1, tool: "recording",
  jobId: "", jobType: "", status: "DONE", undoable: false, cannotUndo: true, noUndo: "irreversible", chatTitle: "", ...over,
});

test("journal: the wave-10 tools have plain labels", () => {
  const title = (tool) => lineModel(jline({ tool }), 0, new Map()).title;
  assert.equal(title("recording"), "Запись");
  assert.equal(title("change_assignment"), "Изменено задание");
  assert.equal(title("mark_assignments_done"), "Изменена отметка «Выполнено»");
  assert.equal(title("note"), "Изменён «Мой конспект»");
  assert.equal(title("set_setting"), "Изменена настройка");
  assert.equal(title("propose_subjects_from_schedule"), "Предложены предметы из расписания");
  assert.equal(title("some_future_tool"), "Действие");
});

test("journal: a done recording line the core calls irreversible says the stop is not undone, not «это предложение»", () => {
  const m = lineModel(jline({}), 0, new Map());
  assert.equal(m.reason, "Нельзя отменить: запись уже остановлена. Можно начать новую.");
  assert.equal(m.reason, recordingStopNoUndo);
  assert.doesNotMatch(m.reason, /предложение/);
  // Only «irreversible» means the stop: a recording line with no reason of the core's is not guessed to be one.
  assert.equal(lineModel(jline({ noUndo: "" }), 0, new Map()).reason, "Нельзя отменить: такие действия не возвращаются");
  assert.notEqual(lineModel(jline({ noUndo: "" }), 0, new Map()).reason, recordingStopNoUndo);
  assert.equal(lineModel(jline({ noUndo: "", cannotUndo: false }), 0, new Map()).reason, "");
});

test("plan steps: the offer_button tool reads «Предложена кнопка» and the notes tools say «Мой конспект»", () => {
  const step = (tool) => stepText({ tool, args: [], deferred: [] });
  assert.equal(step("offer_button"), "Предложена кнопка");
  assert.equal(step("note"), "Работа с «Мой конспект»");
  assert.equal(step("list_notes"), "Просмотр «Мой конспект»");
});

test("journal: other recording lines keep their own reason; other tools keep «это предложение»", () => {
  assert.equal(lineModel(jline({ noUndo: "changed" }), 0, new Map()).reason, "Нельзя отменить: после этого вносились другие изменения");
  assert.equal(lineModel(jline({ status: "UNDONE", noUndo: "undone" }), 0, new Map()).reason, "");
  assert.equal(lineModel(jline({ tool: "propose_send" }), 0, new Map()).reason, "Нельзя отменить: это предложение, решение за вами");
  // With «Отменить» offered (a started recording) nothing replaces it.
  assert.equal(lineModel(jline({ undoable: true, cannotUndo: false, noUndo: "" }), 0, new Map()).reason, "");
});

test("plan steps of the wave-10 tools have names", () => {
  for (const tool of ["change_assignment", "mark_assignments_done", "note", "set_setting", "recording", "propose_subjects_from_schedule"]) {
    assert.notEqual(stepText({ tool, args: [], deferred: [] }), "Шаг", tool);
  }
});
