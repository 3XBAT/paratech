// Agent pack, part B, file 12: a second question while the first request runs a TOOL (BUG-20261104, TASK-380). Part A found that a
// second question during the Hub step waits for the first; the busy refusal «Предыдущий запрос ещё выполняется…» belongs to the
// time the first request is outside the Hub step. SCRIPTED: the model calls answer_question and the ANSWER reader is held for 8 s
// (the fake's delayMs), so the first request is inside a tool while the second question is typed. The second question must NOT reach
// the model, the panel says the previous request is still running, and after the first answer the second can be sent again.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterAnswer, callRule, citedRule, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  act,
  answerRequestsAbout,
  ask,
  cardsOf,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  note,
  panelOf,
  plannerRequests,
  PrerequisitesB,
  prepare,
  sample,
  waitThread,
} from "./support.js";
import { Feature as RecordingFeature } from "../recording/support.js";

test(Feature.busyTool, details(Feature.busyTool, { requires: `${PrerequisitesB} + ${RecordingFeature.summary}` }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const first = "что было на паре по литературе, первый вопрос";
  const second = "второй вопрос пока идёт поиск по лекциям";
  const readQuestion = "Беликов калоши зонтик погоду долгий поиск";
  const a1 = "Первый ответ готов.";
  const a2 = "Второй ответ готов.";

  await step("Hub: answer_question, ANSWER задержан на 8 с; после него ответ; потом ответ на второй вопрос", async () => {
    await llm.script([
      callRule(first, "answer_question", { question: readQuestion, scope: "all" }),
      citedRule(readQuestion, "Беликов носит калоши и зонтик в любую погоду, об этом сказано в конспекте пары.", { delayMs: 8000 }),
      replyRule(AfterAnswer, a1),
      replyRule(second, a2),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("первый вопрос дошёл до инструмента: запрос ANSWER в пути, ответа ещё нет", async () => {
    await ask(main, first);
    await expect
      .poll(async () => (await answerRequestsAbout(llm, readQuestion)).length, { timeout: 40_000, intervals: [100, 200], message: "запрос ANSWER должен дойти до Hub: инструмент работает" })
      .toBeGreaterThan(0);
    const x = await sample(main);
    expect(x.replies, "пока инструмент работает, ответа на первый вопрос ещё нет").toEqual([]);
  });

  await step("второй вопрос во время инструмента: «Предыдущий запрос ещё выполняется», к модели не уходит", async () => {
    await ask(main, second);
    const x = await waitThread(main, "панель должна сказать, что предыдущий запрос ещё выполняется", (y) => y.stopLines.some((l) => l.startsWith("Предыдущий запрос ещё выполняется")), { timeout: 20_000 });
    note(`пока идёт инструмент: строки отказа: «${x.stopLines.join(" | ")}»; ждёт: «${x.queued.join(" | ")}»; запросов в ленте: ${x.requests}`);
    expect(x.replies, "первый ответ ещё не пришёл: отказ показан, пока инструмент работает").toEqual([]);
    const planner = await plannerRequests(llm);
    expect(planner.filter((r) => r.lastUser.includes(second)), "второй вопрос не должен уходить к модели, пока работает инструмент").toEqual([]);
    expect(x.users, "оба вопроса в ленте").toEqual([first, second]);
    const cards = await cardsOf(main);
    note(`кнопки ленты: ${cards.controls.join(" | ")}`);
    expect(cards.controls, "у отказа есть путь дальше: отправить второй вопрос ещё раз").toContain("Отправить ещё раз");
    await snapshot("Агент: второй вопрос при работающем инструменте");
  });

  await step("первый ответ приходит сам", async () => {
    await waitThread(main, "ответ на первый вопрос", (y) => y.replies.includes(collapse(a1)), { timeout: 40_000 });
    const planner = await plannerRequests(llm);
    expect(planner.length, "до повторной отправки модель получила два запроса первого вопроса").toBe(2);
    expectCatalogV9(planner);
  });

  await step("«Отправить ещё раз»: второй вопрос уходит и получает ответ", async () => {
    await panelOf(main).locator(".ag-thread").getByRole("button", { name: "Отправить ещё раз", exact: true }).click(act);
    const x = await waitThread(main, "ответ на второй вопрос", (y) => y.replies.includes(collapse(a2)), { timeout: 40_000 });
    expect(x.replies, "в ленте оба ответа по порядку").toEqual([collapse(a1), collapse(a2)]);
    const planner = await plannerRequests(llm);
    expect(planner.filter((r) => r.lastUser.includes(second)).length, "второй вопрос ушёл к модели ровно один раз, после первого").toBe(1);
    note("проверено: при работающем инструменте второй вопрос отклонён строкой «Предыдущий запрос ещё выполняется…», к модели не ушёл; после первого ответа «Отправить ещё раз» отправил его и пришёл ответ");
    await snapshot("Агент: после «Отправить ещё раз»");
  });

  // The refused request stays in the thread beside the new one: the student sees the question twice, and the old line says the
  // previous request is still running although both are answered. A finding of the pack, kept apart from the checks above.
  await step("после повторной отправки в ленте нет остатка отказа: вопрос один раз, строки «Предыдущий запрос ещё выполняется» нет", async () => {
    const x = await sample(main);
    note(`лента после повторной отправки: вопросы ${JSON.stringify(x.users)}; строки отказа «${x.stopLines.join(" | ")}»`);
    expect(x.stopLines.filter((l) => l.startsWith("Предыдущий запрос")), "после повторной отправки в ленте остаётся строка отказа «Предыдущий запрос ещё выполняется…» при уже полученном ответе").toEqual([]);
    expect(x.users, "после повторной отправки в ленте остаётся отклонённый запрос: вопрос показан дважды").toEqual([first, second]);
  });
});
