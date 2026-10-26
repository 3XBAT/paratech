// Questions, «Как понято задание» and the helper of a turnkey work (TASK-130): binding, view logic, cards, plain text.
// Runs on the modules compiled by `npm run test` (tsconfig.test.json).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement as h } from "react";
import server from "react-dom/server";
import { genericError, methods, parseWork, turnkeyApi, turnkeyErrorText } from "../.test-build/bindings/turnkey.js";
import {
  answeredLine,
  askStepOutcome,
  cardRows,
  cardValue,
  checkCard,
  cleanCard,
  confirmCard,
  initialDrafts,
  isCardEdited,
  isLastRound,
  lineOf,
  stepQuestionCheck,
  toAnswers,
  turnkeyCardModel,
  turnkeyLimits,
  turnkeyTexts,
} from "../.test-build/turnkeyView.js";
import { acceptWork, pollDelay, workCardModel } from "../.test-build/workView.js";
import { WorkBody } from "../.test-build/agent/WorkCard.js";
import { HelperCard, QuestionsCard, UnderstandingCard } from "../.test-build/agent/TurnkeyCards.js";

const { renderToStaticMarkup } = server;
const noop = () => undefined;

const evil = '<img src=x onerror=alert(1)> [x](http://evil.example/a) <a href="http://evil.example">z</a> mail@evil.example C:\\Windows\\win.ini';
const evilEscaped = "&lt;img src=x onerror=alert(1)&gt; [x](http://evil.example/a) &lt;a href=&quot;http://evil.example&quot;&gt;z&lt;/a&gt; mail@evil.example C:\\Windows\\win.ini";

const card = { workType: "referat", volume: "10 страниц", fileFormat: "docx", formatting: "ГОСТ", variant: "", deadline: "1 декабря", sources: "не менее 5" };

function work(over) {
  return {
    id: "w1", status: "triage", version: 1, writing: null, workType: "referat", problem: "", message: "", questionRound: 0,
    questions: [], hasCard: false, card, edited: [], feasibility: "", feasibilityReason: "", partial: false, assumptions: [],
    helper: null, answered: [], ...over,
  };
}

const choice = { kind: "choice", text: "Какой объём?", options: ["5 страниц", "10 страниц"], default: 1, defaultText: "", why: "Влияет на план" };
const textQ = { kind: "text", text: "Какая тема?", options: [], default: 0, defaultText: "свободная", why: "" };

function fakeApi(log = [], over = {}) {
  const reply = (name) => async (...args) => {
    log.push([name, ...args]);
    return work({ status: "review", hasCard: true });
  };
  return {
    work: reply("work"), startWriting: reply("startWriting"), approveOutline: reply("approveOutline"), editOutline: reply("editOutline"),
    editOutlineAt: reply("editOutlineAt"), stopWriting: reply("stopWriting"),
    regenerate: reply("regenerate"), workOfAssignment: reply("workOfAssignment"), startForAssignment: reply("startForAssignment"), answerQuestions: reply("answerQuestions"),
    skipQuestions: reply("skipQuestions"), editUnderstanding: reply("editUnderstanding"), confirmRequirements: reply("confirmRequirements"),
    retry: reply("retry"), askStep: async (...args) => (log.push(["askStep", ...args]), "Ответ"), ...over,
  };
}

function render(w, api = fakeApi(), extra = {}) {
  return renderToStaticMarkup(h(WorkBody, { work: w, api, onWork: noop, ...extra }));
}

// ---- Binding ----

test("bound method names match the Go TurnkeyService", () => {
  const src = readFileSync(new URL("../../internal/input/wails/turnkey.go", import.meta.url), "utf8");
  for (const name of Object.values(methods)) {
    const method = name.slice(name.lastIndexOf(".") + 1);
    assert.match(src, new RegExp(`func \\(s \\*TurnkeyService\\) ${method}\\(`), method);
  }
});

test("parseWork reads the questions, the card, the helper and the assumptions", () => {
  const w = parseWork({
    id: "w1", status: "questions", version: 3, problem: "", message: "", round: 1, questionRound: 1, hasCard: true,
    questions: [{ kind: "choice", text: "Q", options: ["a", "b"], default: 1, defaultText: "", why: "w" }],
    card: { ...card }, edited: ["volume"], feasibility: "partial", feasibilityReason: "причина", partial: true,
    assumptions: ["допущение"], helper: { steps: ["s"], submit: "x", pitfalls: ["p"], readyParts: [], placeholders: null },
    answered: [{ round: 1, question: "Q0", answer: "A", default: true }], writing: null,
  });
  assert.equal(w.questions[0].default, 1);
  assert.equal(w.card.volume, "10 страниц");
  assert.equal(w.workType, "referat");
  assert.deepEqual(w.edited, ["volume"]);
  assert.deepEqual(w.helper.placeholders, []);
  assert.equal(w.answered[0].default, true);
  assert.equal(w.feasibility, "partial");
});

test("parseWork tolerates a reply without the new fields and rejects wrong ones", () => {
  const w = parseWork({ id: "w1", status: "confirmed", version: 1, problem: "", message: "" });
  assert.deepEqual(w.questions, []);
  assert.equal(w.helper, null);
  assert.equal(w.card.volume, "");
  assert.throws(() => parseWork({ id: "w1", status: "review", version: 1, problem: "", message: "", questions: [{ kind: "radio", text: "x" }] }));
  assert.throws(() => parseWork({ id: "w1", status: "review", version: 1, problem: "", message: "", assumptions: [1] }));
  assert.throws(() => parseWork({ id: "w1", status: "review", version: 1, problem: "", message: "", feasibility: "maybe" }));
  assert.throws(() => parseWork({ id: "w1", status: "helper", version: 1, problem: "", message: "", helper: { steps: "x" } }));
});

test("the API sends the answers, the card and the step question as the Go binding takes them", async () => {
  const calls = [];
  const api = turnkeyApi(async (name, ...args) => {
    calls.push([name, ...args]);
    return name === methods.askStep ? { text: "Ответ" } : { id: "w1", status: "review", version: 1, problem: "", message: "" };
  });
  await api.answerQuestions("w1", [{ question: 0, option: 1, text: "" }]);
  await api.skipQuestions("w1");
  await api.editUnderstanding("w1", card);
  await api.confirmRequirements("w1");
  await api.retry("w1");
  assert.equal(await api.askStep("w1", 2, "почему?"), "Ответ");
  assert.deepEqual(calls[0], [methods.answerQuestions, "w1", [{ question: 0, option: 1, text: "" }]]);
  assert.deepEqual(calls[2], [methods.editUnderstanding, "w1", card]);
  assert.deepEqual(calls[5], [methods.askStep, "w1", 2, "почему?"]);
  const bad = turnkeyApi(async () => ({ nope: 1 }));
  await assert.rejects(bad.askStep("w1", 0, "q"));
});

test("startForAssignment starts the triage by the assignment id and parses the work or its problem", async () => {
  assert.equal(methods.startForAssignment, "paratech/desktop/internal/input/wails.TurnkeyService.StartForAssignment");
  const calls = [];
  let reply = { id: "w1", assignmentId: "a7", status: "questions", version: 2, problem: "", message: "", questionRound: 1, questions: [choice] };
  const api = turnkeyApi(async (name, ...args) => {
    calls.push([name, ...args]);
    return reply;
  });
  const w = await api.startForAssignment("a7");
  assert.deepEqual(calls, [[methods.startForAssignment, "a7"]]);
  assert.equal(w.status, "questions");
  assert.equal(w.questions[0].text, choice.text);

  reply = { id: "w1", status: "triage", version: 1, problem: "offline", message: "Нет связи с paratech: разбор не завершён. Повторите позже." };
  const offline = await api.startForAssignment("a7");
  assert.equal(offline.problem, "offline");
  assert.equal(offline.status, "triage");

  reply = { id: 1 };
  await assert.rejects(api.startForAssignment("a7"));
  const missing = turnkeyApi(async () => {
    throw new Error("Задание не найдено: возможно, его удалили.");
  });
  await missing.startForAssignment("gone").then(
    () => assert.fail("no error"),
    (err) => assert.equal(turnkeyErrorText(err), "Задание не найдено: возможно, его удалили."),
  );
});

const writingRaw = { step: "section", generation: 1, topic: "T", sections: [], done: 1, total: 3, subjectId: "", materialId: "", result: "", job: "j1" };

test("parseWork reads the writing's active and jobState, defaults for an older build, rejects unknown", () => {
  const base = { id: "w1", status: "writing", version: 2, problem: "", message: "" };
  const on = parseWork({ ...base, writing: { ...writingRaw, active: true, jobState: "waiting_quota" } });
  assert.equal(on.writing.active, true);
  assert.equal(on.writing.jobState, "waiting_quota");
  const old = parseWork({ ...base, writing: writingRaw });
  assert.equal(old.writing.active, false);
  assert.equal(old.writing.jobState, "");
  assert.equal(parseWork({ ...base, writing: { ...writingRaw, active: false, jobState: "" } }).writing.jobState, "");
  assert.throws(() => parseWork({ ...base, writing: { ...writingRaw, jobState: "paused" } }));
  assert.throws(() => parseWork({ ...base, writing: { ...writingRaw, active: "yes" } }));
});

test("stopWriting and editOutlineAt call the Go methods with the arguments the binding takes", async () => {
  const calls = [];
  const api = turnkeyApi(async (name, ...args) => {
    calls.push([name, ...args]);
    return { id: "w1", status: "writing", version: 3, problem: "", message: "" };
  });
  await api.stopWriting("w1");
  await api.editOutlineAt("w1", 2, [{ number: 1, title: "Введение", words: 300, extra: "x" }]);
  assert.deepEqual(calls[0], [methods.stopWriting, "w1"]);
  assert.deepEqual(calls[1], [methods.editOutlineAt, "w1", 2, [{ number: 1, title: "Введение", words: 300 }]]);
  assert.match(methods.stopWriting, /TurnkeyService\.StopWriting$/);
  assert.match(methods.editOutlineAt, /TurnkeyService\.EditOutlineAt$/);
});

test("the new refusal texts are known, others become the generic one", () => {
  assert.equal(turnkeyErrorText("Работа изменилась: обновите карточку."), "Работа изменилась: обновите карточку.");
  assert.equal(turnkeyErrorText(new Error("Не удалось остановить написание. Попробуйте ещё раз.")), "Не удалось остановить написание. Попробуйте ещё раз.");
  assert.equal(turnkeyErrorText("boom"), genericError);
});

// ---- Which card ----

test("turnkeyCardModel maps every status without a writing", () => {
  assert.equal(turnkeyCardModel(work({ status: "triage" })).type, "triage");
  assert.equal(turnkeyCardModel(work({ status: "questions", questions: [choice] })).type, "questions");
  assert.equal(turnkeyCardModel(work({ status: "questions" })), null);
  assert.equal(turnkeyCardModel(work({ status: "review", hasCard: true })).type, "understanding");
  assert.equal(turnkeyCardModel(work({ status: "review" })), null);
  assert.equal(turnkeyCardModel(work({ status: "helper" })).type, "helper");
  assert.equal(turnkeyCardModel(work({ status: "failed" })).type, "failed");
  assert.equal(turnkeyCardModel(work({ status: "confirmed" })), null);
  assert.equal(turnkeyCardModel(work({ status: "done" })), null);
  // The writing cards own a work with a writing.
  assert.equal(turnkeyCardModel(work({ status: "writing", writing: { step: "outline" } })), null);
  assert.equal(workCardModel(work({ status: "confirmed" })).type, "start");
});

test("a stored triage is not polled indefinitely", () => {
  assert.equal(pollDelay(work({ status: "triage" }), false), null);
  assert.equal(pollDelay(work({ status: "triage", problem: "offline", message: "Нет связи" }), false), null);
  assert.equal(pollDelay(work({ status: "questions", questions: [choice] }), false), null);
});

test("a reply with a problem keeps it and «Повторить» over a same-version read without one", () => {
  const reply = work({ status: "triage", version: 3, problem: "offline", message: "Нет связи с paratech" });
  const read = work({ status: "triage", version: 3 });
  const kept = acceptWork(reply, read);
  assert.equal(kept.problem, "offline");
  assert.equal(kept.message, "Нет связи с paratech");
  const out = render(kept);
  assert.ok(out.includes("Нет связи с paratech") && out.includes(turnkeyTexts.retry));
  // A newer version or another status drops the old problem.
  assert.equal(acceptWork(reply, work({ status: "triage", version: 4 })).problem, "");
  assert.equal(acceptWork(reply, work({ status: "questions", version: 3 })).problem, "");
});

// ---- Questions ----

test("the questions card shows each question with its input, the round and both buttons", () => {
  const out = render(work({ status: "questions", questionRound: 1, questions: [choice, textQ] }));
  assert.ok(out.includes("Какой объём?") && out.includes("Какая тема?"));
  assert.ok(out.includes("Уточнение 1 из 2"));
  assert.equal((out.match(/type="radio"/g) ?? []).length, 2);
  assert.ok(out.includes("Влияет на план"));
  assert.ok(/placeholder="по умолчанию: свободная"/.test(out));
  assert.ok(out.includes(turnkeyTexts.answer) && out.includes(turnkeyTexts.skip));
  // The default option is chosen at first.
  assert.ok(/<input type="radio"[^>]*checked=""[^>]*\/><span[^>]*>10 страниц/.test(out), out);
  assert.ok(!out.includes(turnkeyTexts.lastRoundNote));
});

test("the second round is the last: the card says so and shows the earlier answers", () => {
  const w = work({
    status: "questions", questionRound: 2, questions: [textQ],
    answered: [{ round: 1, question: "Какой объём?", answer: "10 страниц", default: true }, { round: 1, question: "Тема?", answer: "", default: false }],
  });
  assert.equal(isLastRound(w), true);
  const out = render(w);
  assert.ok(out.includes("Уточнение 2 из 2") && out.includes(turnkeyTexts.lastRoundNote));
  assert.ok(out.includes("Какой объём? — 10 страниц (по умолчанию)") && out.includes("Тема? — пропущено"));
  assert.equal(answeredLine({ round: 1, question: "q", answer: "x", default: false }), "x");
});

test("answers: the default option, the typed text on one line, the question number", () => {
  const qs = [choice, textQ, { ...choice, default: 7 }];
  const drafts = initialDrafts(qs);
  assert.deepEqual(drafts, [{ option: 1, text: "" }, { option: 0, text: "" }, { option: 0, text: "" }]);
  const sent = toAnswers(qs, [{ option: 0, text: "" }, { option: 5, text: "  тема\nработы\t " }, drafts[2]]);
  assert.deepEqual(sent, [
    { question: 0, option: 0, text: "" },
    { question: 1, option: 0, text: "тема работы" },
    { question: 2, option: 0, text: "" },
  ]);
  assert.equal(toAnswers([textQ], [{ option: 0, text: "я".repeat(1500) }])[0].text.length, turnkeyLimits.answerMax);
  assert.equal(lineOf("a\u0000b", 10), "a b");
});

test("a failed answer keeps the card and shows the problem of the reader call", () => {
  const failed = work({ status: "triage", problem: "offline", message: "Нет связи с paratech: разбор не завершён. Повторите позже." });
  const out = render(failed);
  assert.ok(out.includes("Нет связи с paratech") && out.includes(turnkeyTexts.retry));
  // A stored triage without a problem means a reader call is due: «Повторить» is offered.
  const stored = render(work({ status: "triage" }));
  assert.ok(stored.includes(turnkeyTexts.triageLine) && stored.includes(turnkeyTexts.retry));
  const err = render(work({ status: "questions", questionRound: 1, questions: [choice] }), fakeApi(), { error: "Не удалось выполнить. Попробуйте ещё раз.", onError: noop });
  assert.ok(err.includes('role="alert"') && err.includes("Не удалось выполнить"));
  assert.ok(render(work({ status: "failed" })).includes(turnkeyTexts.failedText));
});

// ---- «Как понято задание» ----

test("the requirements card shows the fields, marks the student's edits and names the unknown", () => {
  const out = render(work({ status: "review", hasCard: true, edited: ["volume"] }));
  assert.ok(out.includes(turnkeyTexts.cardCaption));
  for (const row of cardRows) {
    assert.ok(out.includes(row.label), row.label);
  }
  assert.ok(out.includes("Реферат") && out.includes("10 страниц<span class=\"ag-wk-verify\"> · изменено</span>"));
  assert.equal((out.match(/изменено/g) ?? []).length, 1);
  assert.ok(out.includes("не указано"));
  assert.ok(out.includes(turnkeyTexts.confirm) && out.includes(turnkeyTexts.edit));
  assert.equal(cardValue({ ...card, workType: "unknown_x" }, "workType"), "unknown_x");
});

test("the third-round assumptions are shown as plain text", () => {
  const out = render(work({ status: "review", hasCard: true, assumptions: ["Объём — 10 страниц", evil] }));
  assert.ok(out.includes(turnkeyTexts.assumptions));
  assert.ok(out.includes("Объём — 10 страниц"));
  assert.ok(out.includes(evilEscaped));
  const none = render(work({ status: "review", hasCard: true }));
  assert.ok(!none.includes(turnkeyTexts.assumptions));
});

test("a partial work names the placeholders to fill", () => {
  const out = render(work({
    status: "review", hasCard: true, partial: true, feasibility: "partial", feasibilityReason: "Нужны данные опыта",
    helper: { steps: [], submit: "", pitfalls: [], readyParts: [], placeholders: ["Таблица измерений"] },
  }));
  assert.ok(out.includes(turnkeyTexts.partialCaption) && out.includes("Нужны данные опыта") && out.includes("Таблица измерений"));
});

test("card editing: the check, the change test and the clean lines", () => {
  assert.equal(isCardEdited(card, card), false);
  assert.equal(isCardEdited({ ...card, volume: "  10 страниц " }, card), false);
  assert.equal(isCardEdited({ ...card, volume: "12 страниц" }, card), true);
  assert.equal(checkCard(card).ok, true);
  const long = checkCard({ ...card, formatting: "я".repeat(turnkeyLimits.requirementMax + 1) });
  assert.equal(long.ok, false);
  assert.equal(long.bad.formatting, true);
  assert.equal(long.problem, turnkeyTexts.lineTooLong);
  assert.equal(checkCard({ ...card, workType: "novel" }).ok, false);
  assert.equal(cleanCard({ ...card, volume: " 5\nстраниц " }).volume, "5 страниц");
});

test("«Подтвердить» saves an edit first, then confirms; an unedited card only confirms", async () => {
  const log = [];
  const api = fakeApi(log);
  const r = await confirmCard(api, "w1", { ...card, volume: " 12 страниц " }, true);
  assert.equal(r.ok, true);
  assert.deepEqual(log.map((c) => c[0]), ["editUnderstanding", "confirmRequirements"]);
  assert.equal(log[0][2].volume, "12 страниц");
  log.length = 0;
  await confirmCard(api, "w1", card, false);
  assert.deepEqual(log.map((c) => c[0]), ["confirmRequirements"]);
});

test("a failed save does not confirm; a failed confirm re-reads the saved work", async () => {
  const log = [];
  const saveFails = fakeApi(log, { editUnderstanding: async () => { throw "Это действие сейчас недоступно: обновите карточку."; } });
  const r1 = await confirmCard(saveFails, "w1", { ...card, volume: "x" }, true);
  assert.equal(r1.ok, false);
  assert.equal(r1.fresh, null);
  assert.ok(!log.some((c) => c[0] === "confirmRequirements"));

  const confirmFails = fakeApi([], { confirmRequirements: async () => { throw new Error("boom"); } });
  const r2 = await confirmCard(confirmFails, "w1", { ...card, volume: "x" }, true);
  assert.equal(r2.ok, false);
  assert.equal(r2.fresh.status, "review");
  const r3 = await confirmCard(confirmFails, "w1", card, false);
  assert.equal(r3.fresh, null);
});

test("the view mode of the card has no inputs", () => {
  const ctx = { work: work({ status: "review", hasCard: true }), api: fakeApi(), busy: false, run: async () => undefined, step: async () => undefined, onWork: noop, errorLine: false };
  const out = renderToStaticMarkup(h(UnderstandingCard, { ctx }));
  assert.ok(!out.includes("<select") && !out.includes("<input"));
});

test("the editor renders an input per requirement, a work type list with the unknown type, aria-invalid and a disabled Save", () => {
  const ctxOf = (c) => ({ work: work({ status: "review", hasCard: true, card: c }), api: fakeApi(), busy: false, run: async () => undefined, step: async () => undefined, onWork: noop, errorLine: false });
  const out = renderToStaticMarkup(h(UnderstandingCard, { ctx: ctxOf(card), startEditing: true }));
  assert.equal((out.match(/<input/g) ?? []).length, 6);
  assert.equal((out.match(/<select/g) ?? []).length, 1);
  assert.ok(out.includes('value="referat" selected=""') || /<option[^>]*value="referat"[^>]*selected/.test(out) || out.includes(">Реферат<"));
  assert.ok(!out.includes("aria-invalid"));
  assert.ok(/<button[^>]*disabled=""[^>]*>[^<]*(<[^>]*>)*[^<]*Сохранить/.test(out), out);

  const odd = renderToStaticMarkup(h(UnderstandingCard, { ctx: ctxOf({ ...card, workType: "mystery", volume: "я".repeat(300) }), startEditing: true }));
  assert.ok(odd.includes('value="mystery"') && odd.includes(turnkeyTexts.notSet));
  assert.equal((odd.match(/aria-invalid="true"/g) ?? []).length, 2);
  assert.ok(odd.includes(turnkeyTexts.lineTooLong));
});

// ---- Helper ----

const helperWork = (over) =>
  work({
    status: "helper", feasibility: "impossible", feasibilityReason: "Нужен эксперимент в лаборатории",
    helper: {
      steps: ["Собрать установку", "Снять показания"], submit: "Отчёт в формате docx через почту", pitfalls: ["Забыть единицы измерения"],
      readyParts: ["Введение"], placeholders: ["Таблица измерений"],
    }, ...over,
  });

test("the helper page shows the steps, what to hand in, the typical mistakes and «Спросить по шагу»", () => {
  const out = render(helperWork());
  assert.ok(out.includes(turnkeyTexts.helperCaption) && out.includes("Нужен эксперимент в лаборатории"));
  assert.ok(out.includes("Собрать установку") && out.includes("Снять показания"));
  assert.ok(out.includes(turnkeyTexts.submit) && out.includes("Отчёт в формате docx через почту"));
  assert.ok(out.includes(turnkeyTexts.pitfalls) && out.includes("Забыть единицы измерения"));
  assert.ok(out.includes("Введение") && out.includes("Таблица измерений"));
  assert.equal((out.match(new RegExp(turnkeyTexts.askStep, "g")) ?? []).length, 2);
  assert.ok(/<ol/.test(out));
});

test("an empty helper says so instead of showing empty blocks", () => {
  const out = render(helperWork({ helper: null, feasibilityReason: "" }));
  assert.ok(out.includes(turnkeyTexts.helperEmpty));
  assert.ok(!out.includes(turnkeyTexts.steps));
  const blank = render(helperWork({ helper: { steps: [], submit: "", pitfalls: [], readyParts: [], placeholders: [] } }));
  assert.ok(blank.includes(turnkeyTexts.helperEmpty));
});

test("«Спросить по шагу»: the answer is shown, «пока недоступны» is a calm note, other errors fail", async () => {
  const log = [];
  const ok = await askStepOutcome(fakeApi(log), "w1", 1, "почему?");
  assert.deepEqual(ok, { kind: "answer", text: "Ответ" });
  assert.deepEqual(log[0], ["askStep", "w1", 1, "почему?"]);

  const none = await askStepOutcome(fakeApi([], { askStep: async () => { throw "Вопросы по шагу пока недоступны."; } }), "w1", 0, "q");
  assert.deepEqual(none, { kind: "unavailable", text: "Вопросы по шагу пока недоступны." });

  const wails = await askStepOutcome(fakeApi([], { askStep: async () => { throw new Error("Вопросы по шагу пока недоступны."); } }), "w1", 0, "q");
  assert.equal(wails.kind, "unavailable");

  const bad = await askStepOutcome(fakeApi([], { askStep: async () => { throw new Error("secret internal detail"); } }), "w1", 0, "q");
  assert.deepEqual(bad, { kind: "error", text: "Не удалось выполнить. Попробуйте ещё раз." });
});

test("the step question must be a non-empty single line within the limit", () => {
  assert.equal(stepQuestionCheck("   ").ok, false);
  assert.equal(stepQuestionCheck("   ").problem, turnkeyTexts.askEmpty);
  assert.deepEqual(stepQuestionCheck(" как\nэто? "), { ok: true, text: "как это?", problem: "" });
  const long = stepQuestionCheck("я".repeat(turnkeyLimits.stepQuestionMax + 1));
  assert.equal(long.ok, false);
  assert.equal(long.problem, turnkeyTexts.askTooLong);
});

// ---- Plain text ----

test("reader text never becomes markup, a link or an address", () => {
  const out = [
    render(work({ status: "questions", questionRound: 1, questions: [{ ...choice, text: evil, options: [evil, "b"], why: evil }, { ...textQ, text: evil, defaultText: evil }], answered: [{ round: 1, question: evil, answer: evil, default: false }] })),
    render(work({ status: "review", hasCard: true, card: { ...card, volume: evil, workType: evil }, assumptions: [evil], feasibilityReason: evil })),
    render(helperWork({ feasibilityReason: evil, helper: { steps: [evil], submit: evil, pitfalls: [evil], readyParts: [evil], placeholders: [evil] } })),
  ];
  for (const html of out) {
    assert.ok(!html.includes("<img") && !html.includes("<a "), html);
    // An attribute value (a placeholder) holds the escaped text; only an element with a real href attribute is wrong.
    assert.ok(!/<[a-z]+[^>]* href=/.test(html.replace(/="[^"]*"/g, '=""')), html);
    assert.ok(html.includes(evilEscaped) || html.includes(evilEscaped.replace(/"/g, "&quot;")), html.slice(0, 400));
  }
});

test("the sources of the new cards use no HTML sink, link, storage or console", () => {
  for (const file of ["../src/agent/TurnkeyCards.tsx", "../src/turnkeyView.ts"]) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "outerHTML", "insertAdjacentHTML", "href=", "window.open", "localStorage", "sessionStorage", "console.", "marked", "<a "]) {
      assert.ok(!src.includes(banned), `${file}: ${banned}`);
    }
  }
});

test("the cards render nothing for a work that has no outcome to show", () => {
  const ctx = { work: work({ status: "helper" }), api: fakeApi(), busy: false, run: async () => undefined, step: async () => undefined, onWork: noop, errorLine: false };
  assert.ok(renderToStaticMarkup(h(HelperCard, { ctx })).includes(turnkeyTexts.helperEmpty));
  assert.ok(renderToStaticMarkup(h(QuestionsCard, { ctx: { ...ctx, work: work({ status: "questions", questionRound: 1, questions: [choice] }) } })).includes("Какой объём?"));
  assert.equal(render(work({ status: "confirmed", workType: "presentation" })), "");
});
