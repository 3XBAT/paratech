// Agent pack, part B, file 9: a tool fails and the app stays honest (TASK-366, ADR-0022). The owner met «материала нет» as the
// reason of a failed answer_question: an invented cause. The contract: the app tells the model only an error ENUM (is_error, never
// a text, never a cause), shows no success card for a failed call, and adds no words of its own about the data. SCRIPTED: the
// ANSWER reader answers garbage twice (the Hub asks once more after a refusal), so the real answer path fails; then a call with an
// id the model never saw is refused by the provenance check; the model's last step is a cannot_do card. The model's own words
// after a failure are the LIVE set (TASK-382); here the scripted sentence only has to be drawn as it is.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { afterRefusal, badAnswerRule, callRule, cannotDoRule, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  ask,
  cardsOf,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  note,
  plannerRequests,
  PrerequisitesB,
  prepare,
  rawLeftovers,
  toolResultsOf,
  waitCards,
  waitThread,
} from "./support.js";

// What the student must never be told by the app about a failure: a guessed fact about the data or a guessed cause.
const invented = /материал[аов]*\s+нет|нет\s+в\s+базе|записей\s+нет|лекций\s+нет|не\s+найден[оа]?\s+в\s+(лекци|материал)|провайдер|не\s+прошёл\s+проверку|упал|invalid_answer|reask|schema|JSON/i;

test(Feature.failure, details(Feature.failure, { requires: PrerequisitesB }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const askFail = "что было на паре про зонтик, подробно";
  const failQuestion = "Что носит Беликов в любую погоду кроме калош";
  const sentence = "Произошла ошибка, ответ не получен. Повторить?";
  const askUnknown = "сделай конспект по записи с идентификатором из головы";
  const unknownRecording = "rec_e2e_never_seen";
  const your = ["Выбрать запись из списка «Записи»"];
  const ours = ["Конспект лекции"];

  await step("Hub: ANSWER отвечает мусором дважды (ответ и повторная попытка Hub); модель отвечает сдержанно; потом вызов с чужим id и cannot_do", async () => {
    await llm.script([
      callRule(askFail, "answer_question", { question: failQuestion, scope: "all" }),
      badAnswerRule(failQuestion),
      badAnswerRule(failQuestion),
      replyRule(afterRefusal("failed"), sentence),
      callRule(askUnknown, "make_summary", { recording: unknownRecording }),
      cannotDoRule('{"error":"', your, ours),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("вопрос по лекциям, ответ которого не получен → сдержанное сообщение", async () => {
    await ask(main, askFail);
    await waitThread(main, "ответ модели должен появиться", (x) => x.replies.length === 1 && x.think === 0, { timeout: 90_000 });
    await snapshot("Агент: сбой answer_question");
  });

  await step("модель получила is_error и только enum «failed»: ни причины, ни текста ошибки", async () => {
    const planner = await plannerRequests(llm);
    expect(planner.length, "модель получила два запроса: вопрос и результат упавшего answer_question").toBe(2);
    expectCatalogV9(planner);
    const results = toolResultsOf(planner[1] as NonNullable<(typeof planner)[1]>);
    expect(results.length).toBe(1);
    const r = results[0] as NonNullable<(typeof results)[0]>;
    note(`результат упавшего answer_question для модели: is_error=${r.isError} ${r.text}`);
    expect(r.tool).toBe("answer_question");
    expect(r.isError, "результат упавшего вызова — is_error").toBe(true);
    expect(Object.keys((r.json ?? {}) as object), "в результате только поле error (ADR-0022: enum, не текст)").toEqual(["error"]);
    expect((r.json as { error?: string }).error, "ошибка — enum «failed»").toBe("failed");
    expect(r.text.match(invented), "в результате для модели не должно быть выдуманной причины").toBeNull();
  });

  await step("лента: сдержанный ответ модели, карточки успеха нет, слов о данных от приложения нет", async () => {
    const x = await waitThread(main, "лента", (y) => y.replies.length === 1, { timeout: 10_000 });
    expect(x.replies[0], "текст модели показан как есть").toBe(collapse(sentence));
    const cards = await cardsOf(main);
    note(`карточки ленты: ${cards.captions.join(" | ") || "только ответ"}; строки: ${cards.problems.join(" | ") || "нет"}`);
    expect(cards.captions, "после сбоя нет карточки «Ответ по материалам» (успеха)").not.toContain("Ответ по материалам");
    expect(cards.sources, "после сбоя нет источников").toEqual([]);
    expect(cards.askCards, "после сбоя нет карточки вопросов").toBe(0);
    expect(x.text.match(invented), `в ленте нет выдуманной причины и «материала нет»: «${x.text.slice(0, 200)}»`).toBeNull();
    expect(rawLeftovers(x.text), "в ленте нет сырых ошибок").toEqual([]);
  });

  await step("вызов с id, которого модель не видела → is_error unknown_id, и карточка cannot_do с двумя списками", async () => {
    await ask(main, askUnknown);
    const cards = await waitCards(main, "карточка «Целиком не получится» должна появиться", (c) => c.cannotDo !== undefined, 60_000);
    await waitThread(main, "запрос закончился", (y) => y.think === 0, { timeout: 20_000 });
    await snapshot("Агент: cannot_do после отказа");
    const planner = await plannerRequests(llm);
    const second = planner.filter((r) => toolResultsOf(r).some((t) => t.tool === "make_summary"));
    expect(second.length, "модель получила результат вызова make_summary").toBe(1);
    const t = toolResultsOf(second[0] as NonNullable<(typeof second)[0]>)[0] as ReturnType<typeof toolResultsOf>[0];
    note(`результат make_summary с чужим id: is_error=${t.isError} ${t.text}`);
    expect(t.isError, "вызов с id, которого не было в данных, отклонён как is_error").toBe(true);
    expect((t.json as { error?: string }).error, "отказ — enum unknown_id").toBe("unknown_id");
    expect(t.text.match(invented), "в результате нет выдуманной причины").toBeNull();
    expect(Object.keys((t.json ?? {}) as object), "в результате только поле error").toEqual(["error"]);
    expect(cards.captions, "карточка cannot_do — «Целиком не получится»").toContain("Целиком не получится");
    expect(cards.cannotDo?.subs, "две подписи: что остаётся ученику и что берёт paratech").toEqual(["Остаётся на вашей стороне", "Берёт на себя paratech"]);
    expect(cards.cannotDo?.lines, "строки обоих списков как у модели").toEqual([...your, ...ours]);
    expect(cards.askCards + cards.confirmCards, "это не карточка вопросов и не подтверждения").toBe(0);
    expect(cards.actionLines, "ничего не выполнено: строки действия нет").toEqual([]);
    note("проверено: упавший answer_question → is_error {error:failed}, без причины; в ленте нет «Ответ по материалам» и слов о данных; make_summary с чужим id → is_error unknown_id, ничего не сделано; cannot_do рисует два списка");
  });
});
