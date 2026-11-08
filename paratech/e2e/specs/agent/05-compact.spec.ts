// Agent pack, part A, file 5: context compaction (TASK-269, TASK-270, ADR-0028, TASK-380). The desktop compacts a chat at an Ask when
// the earlier requests fill the window: Fill >= 1, that is 20 pairs or 48 KiB of student texts and reply lines (thread.MaxHistoryBytes).
// A student text is at most 4 KiB and a reply line 2 KiB, so nine fat requests fill it; the tenth Ask first calls the Hub's
// chat_summary operation (a Complete call, answered by a scripted rule) while the panel shows «Уплотнение контекста…».
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { chatSummaryRule, ChatSummaryMatch, filler, replyRule } from "../../scripts/llm/agent/rules.js";
import { ask, collapse, details, expectCatalogV9, Feature, messagesText, note, plannerRequests, prepare, waitThread } from "./support.js";

const compactingLine = "Уплотнение контекста…";
const asks = 12;

/** Russian filler of at most `bytes` UTF-8 bytes, and not much less (cut in steps of 20 characters from a text of `bytes` characters). */
function fillerBytes(bytes: number, seed: number): string {
  let text = filler(bytes, seed);
  while (Buffer.byteLength(text, "utf8") > bytes) {
    text = text.slice(0, -20);
  }
  return text.trimEnd();
}

test(Feature.compact, details(Feature.compact), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const summary = "Краткая сводка беседы для проверки: ученик спрашивал про предел, производную и интеграл; агент отвечал по порядку.";
  const questions = Array.from({ length: asks }, (_, i) => `К${i + 1}. ${fillerBytes(3900, i)}`);
  const replies = Array.from({ length: asks }, (_, i) => fillerBytes(1900, i + 20));

  // The Hub limits one key to 30 calls a minute with a burst of 10 (HUB_RATE_LIMIT_*; the harness keeps the defaults), and this
  // scenario needs ten calls: each answer waits 2.3 s, so the questions go slower than the bucket refills (0.5 a second) and the
  // calls of the earlier scenarios cannot drain it.
  await step("Hub: ответы на вопросы К1…К12 (каждый через 2,3 с) и медленный ответ на chat_summary", async () => {
    await llm.script([
      chatSummaryRule(summary, 3000),
      ...questions.map((q, i) => replyRule(`К${i + 1}. `, replies[i] as string, { delayMs: 2300 })),
    ]);
  });
  await step("панель: новый пустой чат", () => prepare(main));

  let sawLine = false;
  let compactedAt = 0;
  for (let i = 0; i < asks && compactedAt === 0; i++) {
    await step(`вопрос К${i + 1}`, async () => {
      await ask(main, questions[i] as string);
      await waitThread(main, `ответ на К${i + 1} должен появиться`, (x) => x.replies.length === i + 1 && x.think === 0, {
        timeout: 40_000,
        each: (x) => {
          if (x.compaction.includes(compactingLine)) {
            sawLine = true;
            compactedAt = i + 1;
          }
        },
      });
    });
  }

  await step("уплотнение запустилось: строка «Уплотнение контекста…» и запрос chat_summary к Hub", async () => {
    note(`строка «${compactingLine}» ${sawLine ? `появилась на вопросе К${compactedAt}` : "не появилась за 12 вопросов"}`);
    if (!sawLine) {
      await snapshot("Агент: уплотнение не началось");
    }
    expect(sawLine, `за ${asks} больших вопросов (по ~6 KiB пары) уплотнение должно было начаться: нужна строка «${compactingLine}»`).toBe(true);
    const all = await llm.requests();
    const summaries = all.filter((r) => r.lastUser.includes(ChatSummaryMatch));
    expect(summaries.length, "к Hub должен уйти ровно один запрос chat_summary").toBe(1);
    expect(summaries[0]?.matched, "запрос chat_summary должен быть отвечен скриптом").toBe(true);
    expect(summaries[0]?.lastUser.includes(questions[0]?.slice(0, 40) ?? "?"), "в запросе chat_summary должны быть самые старые вопросы чата").toBe(true);
    await snapshot("Агент: уплотнение");
  });

  await step("строка пропала, ответ на вопрос пришёл, а следующий запрос к модели несёт сводку вместо старых пар", async () => {
    const x = await waitThread(main, "после уплотнения строка должна пропасть", (y) => y.compaction === "" && y.think === 0, { timeout: 40_000 });
    expect(x.compaction).toBe("");
    const planner = await plannerRequests(llm);
    expectCatalogV9(planner);
    const last = planner.at(-1);
    expect(last?.lastUser.includes(`К${compactedAt}. `), "последний запрос к модели — вопрос, на котором началось уплотнение").toBe(true);
    const history = last ? messagesText(last) : "";
    expect(history.includes(summary.slice(0, 40)), "после уплотнения в запросе к модели должна быть сводка беседы").toBe(true);
    expect(history.includes(collapse(questions[0] as string).slice(0, 60)), "самый старый вопрос после уплотнения не должен идти в запрос полностью").toBe(false);
    note(`запрос к модели после уплотнения: ${history.length} знаков истории; сводка есть, К1 в нём нет`);
    note(`проверено: уплотнение на вопросе К${compactedAt}: строка «${compactingLine}», один запрос chat_summary со старейшими вопросами, сводка в следующем запросе вместо старых пар`);
  });
});
