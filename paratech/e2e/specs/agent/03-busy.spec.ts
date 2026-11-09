// Agent pack, part A, file 3: a second question while the first still runs (BUG-20261104, TASK-380, TASK-398/399). A second question
// while the chat's current request is PLANNING or EXECUTING is never refused (ADR-0026 addendum 2026-11-08, point 7): it is queued
// (FIFO, at most 5), the panel shows «ждёт предыдущий запрос» with a stop for the running one, and it is sent to the model by
// itself, with the first one in its history, when the first request is final. This file is the Hub-step case; file 12 is the same
// rule while a TOOL runs (it used to be the busy refusal «Предыдущий запрос ещё выполняется…», now gone).
// Checked: while the first runs the second is not sent to the model, the order is kept, and nothing stale is left on the way.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { filler, replyRule } from "../../scripts/llm/agent/rules.js";
import { ask, collapse, details, expectCatalogV9, Feature, note, plannerRequests, prepare, rawLeftovers, sample, type Sample, waitThread } from "./support.js";

test(Feature.busy, details(Feature.busy), async ({ main, llm }) => {
  test.setTimeout(75_000);
  const q1 = "первый вопрос: что такое производная";
  const q2 = "второй вопрос: что такое интеграл";
  const q3 = "третий вопрос: что такое числовой ряд";
  const a1 = filler(260, 1);
  const a2 = filler(250, 2);
  const a3 = filler(240, 3);

  await step("Hub: первый ответ через 5 с, второй через 2 с, третий сразу", async () => {
    await llm.script([replyRule(q1, a1, { delayMs: 5000 }), replyRule(q2, a2, { delayMs: 2000 }), replyRule(q3, a3)]);
  });
  await step("панель: новый пустой чат", () => prepare(main));

  await step("первый вопрос идёт: видны «Остановить» и таймер", async () => {
    await ask(main, q1);
    await waitThread(main, "у первого вопроса должна быть строка «Агент отвечает…» с кнопкой «Остановить»", (x) => x.think > 0 && x.stopButtons > 0 && x.elapsed.length > 0, { timeout: 20_000 });
  });

  await step("второй вопрос в том же чате: «ждёт предыдущий запрос», остановить можно предыдущий; к модели он не уходит", async () => {
    await ask(main, q2);
    const x = await waitThread(main, "второй вопрос должен показать «ждёт предыдущий запрос»", (y) => y.queued.length > 0, { timeout: 20_000 });
    note(`пока идёт первый: вопросы ${x.users.length}; ждёт: «${x.queued.join(" | ")}»; строки «Стоп»: ${x.think}; таймеры: ${x.elapsed.join(", ")}; строки: «${x.stopLines.join(" | ")}»`);
    expect(x.users, "оба вопроса ученика должны быть в ленте").toEqual([q1, q2]);
    expect(x.queued, "у второго вопроса ровно одна метка ожидания").toEqual(["ждёт предыдущий запрос"]);
    expect(x.stopPrevious, "у ждущего вопроса должна быть кнопка «Остановить предыдущий запрос»").toBe(1);
    expect(x.elapsed.length, "таймер идёт только у запущенного первого вопроса, у ждущего его нет").toBe(1);
    expect(x.stopLines, "пока второй вопрос ждёт, лента не говорит «не завершено»/«ошибка»").toEqual([]);
    expect(x.callLines, "и не показывает отказ ядра: вопрос не отклонён, а ждёт").toEqual([]);
    const planner = await plannerRequests(llm);
    expect(planner.filter((r) => r.lastUser.includes("второй вопрос")), "второй вопрос не должен уходить к модели, пока идёт первый").toEqual([]);
    expect(planner.length, "пока идёт первый вопрос, модель получила только его запрос").toBe(1);
    await snapshot("Агент: второй вопрос ждёт первый");
  });

  await step("первый ответ пришёл: показан, метка «ждёт» и «Остановить предыдущий» ушли сразу, второй вопрос пошёл сам", async () => {
    const stale: string[] = [];
    const watch = (x: Sample): void => {
      if (x.replies.includes(collapse(a1)) && (x.queued.length > 0 || x.stopPrevious > 0)) {
        stale.push(`метка «${x.queued.join(", ")}», кнопок «Остановить предыдущий»: ${x.stopPrevious}`);
      }
    };
    await waitThread(main, "ответ на первый вопрос должен появиться", (y) => y.replies.includes(collapse(a1)), { timeout: 40_000, each: watch });
    const second = await waitThread(main, "ответ на второй вопрос должен появиться сам, без нажатий", (y) => y.replies.includes(collapse(a2)), { timeout: 40_000, each: watch });
    expect(stale, "после ответа на первый вопрос метка «ждёт предыдущий запрос» и «Остановить предыдущий» не должны оставаться").toEqual([]);
    expect(second.replies, "в ленте ответы на оба вопроса по порядку").toEqual([collapse(a1), collapse(a2)]);
    await snapshot("Агент: оба ответа");
  });

  await step("порядок у модели: сначала запрос первого вопроса, потом второго; в ленте нет «Стоп», таймера, меток", async () => {
    const x = await waitThread(main, "после ответов не должно остаться бегущих строк", (y) => y.think === 0 && y.queued.length === 0 && y.elapsed.length === 0, { timeout: 10_000 });
    expect(x.stopButtons, "кнопки «Остановить» не должно остаться").toBe(0);
    expect(x.stopPrevious, "кнопки «Остановить предыдущий запрос» не должно остаться: предыдущего запроса уже нет").toBe(0);
    expect(x.stopLines, "после двух нормальных ответов строк «не завершено»/«ошибка» быть не должно").toEqual([]);
    expect(rawLeftovers(x.text), "в ленте не должно быть «undefined»/«NaN»/сырых ошибок").toEqual([]);
    const planner = await plannerRequests(llm);
    expect(planner.map((r) => (r.lastUser.includes("первый вопрос") ? 1 : r.lastUser.includes("второй вопрос") ? 2 : 0)), "модель получила запросы в порядке вопросов").toEqual([1, 2]);
    expect(planner[1]?.lastUser.includes("первый вопрос"), "запрос второго вопроса — это вопрос ученика, а не повтор первого").toBe(false);
  });

  await step("панель принимает новый вопрос: третий вопрос получает свой ответ", async () => {
    await ask(main, q3);
    const x = await waitThread(main, "ответ на третий вопрос должен появиться", (y) => y.replies.includes(collapse(a3)) && y.think === 0, { timeout: 30_000 });
    expect(x.users, "в ленте три вопроса по порядку").toEqual([q1, q2, q3]);
    const planner = await plannerRequests(llm);
    expect(planner.length, "модель получила три запроса: по одному на вопрос").toBe(3);
    expectCatalogV9(planner);
    const last = await sample(main);
    expect(last.queued.length + last.stopPrevious + last.elapsed.length, "после третьего ответа в ленте нет ни меток, ни таймеров").toBe(0);
    note("проверено: второй вопрос ждёт первый (метка, «Остановить предыдущий», без таймера, к модели не уходит), потом идёт сам; порядок запросов 1→2→3; ничего устаревшего");
  });
});
