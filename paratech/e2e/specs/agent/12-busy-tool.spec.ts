// Agent pack, part B, file 12: a second question while the first request runs a TOOL (BUG-20261104, TASK-380, TASK-398/399). A second
// question while the chat's current request is PLANNING or EXECUTING (a Hub step or a tool run alike) is never refused (ADR-0026
// addendum 2026-11-08, point 7): it is queued and sent by itself when the first request is final. SCRIPTED: the model calls
// answer_question and the ANSWER reader is held for 8 s (the fake's delayMs), so the first request is inside a tool while the second
// question is typed. The second question must NOT reach the model while the tool runs, the panel shows «ждёт предыдущий запрос», no
// refusal «Предыдущий запрос ещё выполняется…» and no «Отправить ещё раз» ever appear; after the first answer the second starts
// itself (no click), is answered, stands in the thread exactly once, and the model got it once, after the first, with the first in
// its history. (Before TASK-398 this row expected the refusal and a click on «Отправить ещё раз».)
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterAnswer, callRule, citedRule, replyRule } from "../../scripts/llm/agent/rules.js";
import {
  answerRequestsAbout,
  ask,
  cardsOf,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  messagesText,
  note,
  plannerRequests,
  PrerequisitesB,
  prepare,
  sample,
  type Sample,
  waitThread,
} from "./support.js";
import { Feature as RecordingFeature } from "../recording/support.js";

const refusal = "Предыдущий запрос ещё выполняется";
const queuedLabel = "ждёт предыдущий запрос";
const sendAgain = "Отправить ещё раз";

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

  await step("второй вопрос во время инструмента: «ждёт предыдущий запрос», отказа нет, к модели не уходит", async () => {
    await ask(main, second);
    const x = await waitThread(main, "второй вопрос должен показать «ждёт предыдущий запрос»", (y) => y.queued.length > 0, { timeout: 20_000 });
    note(`пока идёт инструмент: ждёт: «${x.queued.join(" | ")}»; строки: «${x.stopLines.join(" | ")}»; запросов в ленте: ${x.requests}`);
    expect(x.replies, "первый ответ ещё не пришёл: второй вопрос ждёт, пока инструмент работает").toEqual([]);
    expect(x.users, "оба вопроса в ленте").toEqual([first, second]);
    expect(x.queued, "у второго вопроса ровно одна метка ожидания").toEqual([queuedLabel]);
    expect(x.stopLines.concat(x.callLines).filter((l) => l.includes(refusal)), "второй вопрос не отклоняется: строки «Предыдущий запрос ещё выполняется…» нет").toEqual([]);
    const planner = await plannerRequests(llm);
    expect(planner.filter((r) => r.lastUser.includes(second)), "второй вопрос не должен уходить к модели, пока работает инструмент").toEqual([]);
    const cards = await cardsOf(main);
    note(`кнопки ленты: ${cards.controls.join(" | ")}`);
    expect(cards.controls, "«Отправить ещё раз» не нужна: вопрос не отклонён, а ждёт").not.toContain(sendAgain);
    await snapshot("Агент: второй вопрос ждёт, пока идёт инструмент");
  });

  await step("первый ответ приходит, второй вопрос идёт сам (без нажатий) и получает ответ; метка ожидания уходит", async () => {
    const stale: string[] = [];
    const refused: string[] = [];
    let labelSince = 0;
    const watch = (y: Sample): void => {
      // the label may outlive the first answer for the moment the core takes to start the second request, not longer
      if (y.replies.includes(collapse(a1)) && (y.queued.length > 0 || y.stopPrevious > 0)) {
        labelSince = labelSince === 0 ? Date.now() : labelSince;
        if (Date.now() - labelSince > 3_000) {
          stale.push(`метка «${y.queued.join(", ")}», кнопок «Остановить предыдущий»: ${y.stopPrevious}`);
        }
      } else {
        labelSince = 0;
      }
      refused.push(...y.stopLines.concat(y.callLines).filter((l) => l.includes(refusal)));
    };
    await waitThread(main, "ответ на первый вопрос", (y) => y.replies.includes(collapse(a1)), { timeout: 40_000, each: watch });
    const done = await waitThread(main, "ответ на второй вопрос должен появиться сам, без нажатий", (y) => y.replies.includes(collapse(a2)), { timeout: 40_000, each: watch });
    expect([...new Set(refused)], "ни до, ни после первого ответа в ленте не было строки «Предыдущий запрос ещё выполняется…»").toEqual([]);
    expect(stale, "после ответа на первый вопрос метка ожидания не должна оставаться").toEqual([]);
    expect(done.replies, "в ленте оба ответа по порядку").toEqual([collapse(a1), collapse(a2)]);
    await snapshot("Агент: оба ответа, второй пошёл сам");
  });

  await step("порядок у модели: второй вопрос ушёл ровно один раз, после первого, с первым в истории", async () => {
    const planner = await plannerRequests(llm);
    expect(planner.length, "модель получила три запроса: вызов инструмента, итог первого, второй вопрос").toBe(3);
    expectCatalogV9(planner);
    expect(planner[0]?.lastUser.includes(first), "первый запрос к модели — первый вопрос").toBe(true);
    expect(planner.filter((r) => r.lastUser.includes(second)).length, "второй вопрос ушёл к модели ровно один раз").toBe(1);
    expect(planner[2]?.lastUser.includes(second), "второй вопрос ушёл последним, после итога первого запроса").toBe(true);
    const history = planner[2] === undefined ? "" : messagesText(planner[2]);
    expect(history.includes(first), "в истории второго запроса есть первый вопрос").toBe(true);
    expect(history.includes(a1), "в истории второго запроса есть ответ на первый вопрос").toBe(true);
  });

  await step("в ленте нет остатка: каждый вопрос по разу, ни отказа, ни «Отправить ещё раз», ни «Стоп», таймера, меток", async () => {
    const x = await waitThread(main, "после двух ответов не должно остаться бегущих строк", (y) => y.think === 0 && y.queued.length === 0 && y.elapsed.length === 0, { timeout: 10_000 });
    note(`лента в конце: вопросы ${JSON.stringify(x.users)}; строки «${x.stopLines.join(" | ")}»`);
    expect(x.users, "каждый вопрос показан один раз").toEqual([first, second]);
    expect(x.requests, "в ленте два запроса: второй не продублирован").toBe(2);
    expect(x.stopLines, "после двух нормальных ответов строк «не завершено»/«ошибка»/«Предыдущий запрос…» быть не должно").toEqual([]);
    expect(x.text.includes(refusal), "в тексте ленты нет «Предыдущий запрос ещё выполняется»").toBe(false);
    expect(x.stopButtons + x.stopPrevious, "кнопок «Остановить» и «Остановить предыдущий запрос» не должно остаться").toBe(0);
    expect((await cardsOf(main)).controls, "кнопки «Отправить ещё раз» в ленте нет").not.toContain(sendAgain);
    note("проверено: при работающем инструменте второй вопрос ждёт («ждёт предыдущий запрос»), не отклоняется и к модели не уходит; после первого ответа идёт сам, ровно один раз и после первого, и получает ответ; в ленте нет остатка");
  });
});
