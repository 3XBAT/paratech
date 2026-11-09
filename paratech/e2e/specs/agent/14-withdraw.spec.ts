// Agent pack, part B, file 14: a waiting question is taken back (TASK-399, TASK-401; ADR-0026 addendum 2026-11-08, point 7). A second
// question typed while the first request runs a tool is QUEUED under a ticket: the row says «ждёт предыдущий запрос» and has the
// icon «Отменить этот запрос». Pressing it with an EMPTY field removes the row, puts the text back into the field (with focus) and the
// question never runs: the model must not get it, now or later. SCRIPTED like file 12: the model calls answer_question and the ANSWER
// reader is held for 8 s (the fake's delayMs), so the first request is inside a tool while the second question is typed, shown and
// withdrawn. The script holds NO rule for the second question: if it ever reached the model, the run fails as an unscripted call.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterAnswer, callRule, citedRule, replyRule } from "../../scripts/llm/agent/rules.js";
import { Feature as RecordingFeature } from "../recording/support.js";
import {
  act,
  answerRequestsAbout,
  ask,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  fieldOf,
  note,
  plannerRequests,
  PrerequisitesB,
  prepare,
  sample,
  type Sample,
  settle,
  waitThread,
} from "./support.js";

const refusal = "Предыдущий запрос ещё выполняется";
const queuedLabel = "ждёт предыдущий запрос";
const withdrawnLine = "Запрос отменён";

test(Feature.withdraw, details(Feature.withdraw, { requires: `${PrerequisitesB} + ${RecordingFeature.summary}` }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const first = "что было на паре по литературе, вопрос перед отменой";
  const second = "этот второй вопрос ученик заберёт назад";
  const readQuestion = "Беликов калоши зонтик погоду пока второй вопрос ждёт";
  const a1 = "Первый ответ готов.";
  const field = fieldOf(main);

  await step("Hub: answer_question, ANSWER задержан на 8 с, потом ответ; для второго вопроса правила нет", async () => {
    await llm.script([
      callRule(first, "answer_question", { question: readQuestion, scope: "all" }),
      citedRule(readQuestion, "Беликов носит калоши и зонтик в любую погоду, об этом сказано в конспекте пары.", { delayMs: 8000 }),
      replyRule(AfterAnswer, a1),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("первый вопрос дошёл до инструмента: запрос ANSWER в пути", async () => {
    await ask(main, first);
    await expect
      .poll(async () => (await answerRequestsAbout(llm, readQuestion)).length, { timeout: 40_000, intervals: [100, 200], message: "запрос ANSWER должен дойти до Hub: инструмент работает" })
      .toBeGreaterThan(0);
  });

  await step("второй вопрос во время инструмента: «ждёт предыдущий запрос» и иконка «Отменить этот запрос», к модели не уходит", async () => {
    await ask(main, second);
    const x = await waitThread(main, "второй вопрос должен показать «ждёт предыдущий запрос» и иконку отмены", (y) => y.queued.length > 0 && y.withdrawButtons > 0, { timeout: 20_000 });
    note(`пока идёт инструмент: ждёт «${x.queued.join(" | ")}», иконок отмены: ${x.withdrawButtons}; запросов в ленте: ${x.requests}`);
    expect(x.users, "оба вопроса в ленте").toEqual([first, second]);
    expect(x.queued, "у второго вопроса ровно одна метка ожидания").toEqual([queuedLabel]);
    expect(x.withdrawButtons, "у ждущего вопроса ровно одна иконка «Отменить этот запрос»").toBe(1);
    expect(x.fieldDisabled, "поле запроса включено: ученик может писать дальше").toBe(false);
    expect(x.field, "после отправки поле пустое (иначе отмена оставит черновик, а не вернёт вопрос)").toBe("");
    const planner = await plannerRequests(llm);
    expect(planner.filter((r) => r.lastUser.includes(second)), "второй вопрос не уходит к модели, пока работает инструмент").toEqual([]);
    await snapshot("Агент: второй вопрос ждёт, есть «Отменить этот запрос»");
  });

  await step("«Отменить этот запрос»: строка уходит, текст вопроса возвращается в пустое поле, фокус в поле", async () => {
    await main.locator("aside.ag .ag-thread").getByRole("button", { name: "Отменить этот запрос", exact: true }).click(act);
    const x = await waitThread(main, "после отмены в ленте остаётся один вопрос, метки ожидания нет", (y) => y.users.length === 1 && y.queued.length === 0, { timeout: 20_000 });
    note(`после отмены: вопросы ${JSON.stringify(x.users)}; запросов в ленте ${x.requests}; поле «${x.field}»`);
    expect(x.users, "в ленте только первый вопрос: ждущая строка ушла").toEqual([first]);
    expect(x.requests, "в ленте один запрос").toBe(1);
    expect(x.withdrawButtons + x.stopPrevious, "иконок «Отменить этот запрос» и «Остановить предыдущий запрос» не осталось: ждущего вопроса нет").toBe(0);
    expect(x.text.includes(second), "текст второго вопроса не остался в ленте: он вернулся в поле").toBe(false);
    expect(x.text.includes(withdrawnLine), "строки «Запрос отменён…» нет: поле было пустым, строка просто уходит").toBe(false);
    expect(x.stopLines.concat(x.callLines).filter((l) => l.includes(refusal)), "отказа «Предыдущий запрос ещё выполняется…» нет").toEqual([]);
    await expect(field, "текст отменённого вопроса возвращается в поле запроса").toHaveValue(second, act);
    await expect(field, "после отмены фокус в поле запроса: ученик продолжает печатать").toBeFocused(act);
    await expect(field, "поле остаётся включённым").toBeEnabled(act);
    await snapshot("Агент: вопрос отменён, текст в поле");
  });

  const stale: string[] = [];
  const refused: string[] = [];
  const watch = (y: Sample): void => {
    if (y.queued.length > 0 || y.stopPrevious > 0 || y.withdrawButtons > 0) {
      stale.push(`метка «${y.queued.join(", ")}», «Остановить предыдущий»: ${y.stopPrevious}, «Отменить этот запрос»: ${y.withdrawButtons}`);
    }
    refused.push(...y.stopLines.concat(y.callLines).filter((l) => l.includes(refusal)));
  };

  await step("первый запрос доходит до конца: ответ показан, второй вопрос сам не стартует", async () => {
    const done = await waitThread(main, "ответ на первый вопрос должен появиться", (y) => y.replies.includes(collapse(a1)) && y.think === 0, { timeout: 40_000, each: watch });
    expect(done.replies, "в ленте один ответ — на первый вопрос").toEqual([collapse(a1)]);
    // the second question would start right after the first is final: look for a while after that
    const later = await settle(main, 2_500, watch);
    expect(later.users, "после ответа в ленте по-прежнему один вопрос: отменённый не появился снова").toEqual([first]);
    expect(later.replies, "ответов не прибавилось").toEqual([collapse(a1)]);
    expect(later.field, "текст отменённого вопроса остался в поле, он не отправлен").toBe(second);
    expect(later.fieldDisabled, "поле включено").toBe(false);
    expect([...new Set(refused)], "ни до, ни после ответа не было строки «Предыдущий запрос ещё выполняется…»").toEqual([]);
    expect(stale, "после отмены метка «ждёт предыдущий запрос», «Остановить предыдущий» и «Отменить этот запрос» не возвращаются").toEqual([]);
  });

  await step("у модели: текст второго вопроса не появился ни в одном запросе; запросов планировщика только два (первый вопрос и итог инструмента)", async () => {
    const all = await llm.requests();
    const withSecond = all.filter((r) => JSON.stringify(r.body).includes(second));
    expect(withSecond.map((r) => r.seq), "текст отменённого вопроса не должен попадать к модели вообще").toEqual([]);
    const planner = await plannerRequests(llm);
    note(`запросов к модели: всего ${all.length}, планировщика ${planner.length}`);
    expect(planner.length, "планировщик получил два запроса: вызов инструмента и итог первого вопроса — для второго вопроса запроса нет").toBe(2);
    expectCatalogV9(planner);
    expect(planner[0]?.lastUser.includes(first), "первый запрос к модели — первый вопрос").toBe(true);
    expect(planner[1]?.lastUser.includes(AfterAnswer), "второй запрос — итог answer_question, а не второй вопрос").toBe(true);
    expect((await answerRequestsAbout(llm, readQuestion)).length, "ANSWER спрошен один раз").toBe(1);
    // a call no rule answered (the second question, if it had gone out) fails the test through the unscripted-call guard of step()
  });

  await step("в ленте нет остатка; поле очищено для следующих рядов", async () => {
    const x = await sample(main);
    expect(x.think + x.stopButtons + x.stopPrevious + x.withdrawButtons + x.elapsed.length + x.queued.length, "нет ни «Стоп», ни таймера, ни меток, ни иконок отмены").toBe(0);
    expect(x.stopLines, "после нормального ответа строк «не завершено»/«ошибка» нет").toEqual([]);
    expect(x.users, "каждый вопрос один раз: в ленте только первый").toEqual([first]);
    await field.fill("", act);
    note("проверено: ждущий вопрос отменён иконкой «Отменить этот запрос»: строка ушла, текст вернулся в пустое поле с фокусом, первый запрос закончился своим ответом, второй вопрос не дошёл до модели");
  });
});
