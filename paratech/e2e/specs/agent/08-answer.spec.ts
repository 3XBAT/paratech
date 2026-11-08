// Agent pack, part B, file 8: «что было на паре по литературе» (TASK-366, ADR-0022). SCRIPTED: the model calls answer_question; the
// app asks the Hub for ANSWER (the `cited` rule of the fake builds the answer from the first fragment of the request); the panel
// shows the answer card with its sources, and a source opens the lesson page on the tab of the place it cites. The pack asserts
// what the app sent to the ANSWER reader (the fragments of the student's own lessons), what it told the planner (counters only,
// ADR-0022: never the claim or the lesson text) and what the card shows.
//
// The model's final `reply` is delayed (12 s) so the card can be looked at and used while the request is still running: the app
// replaces the cards of a request with the last one, the model's reply, so the card is gone once the reply arrives (the row
// «карточки инструментов остаются после ответа модели», file 13). The second row here is the text of the card: see its comment.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterAnswer, callRule, citedRule, replyRule } from "../../scripts/llm/agent/rules.js";
import { firstWords } from "../../src/fakellm/cited.js";
import { Feature as RecordingFeature } from "../recording/support.js";
import {
  act,
  answerRequestsAbout,
  ask,
  cardsOf,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  lectureOf,
  messagesText,
  note,
  plannerRequests,
  PrerequisitesB,
  prepare,
  recordingsOfApp,
  toolResultsOf,
  waitCards,
  waitThread,
} from "./support.js";

const lecture = lectureOf("основная");
const requires = `${PrerequisitesB} + ${RecordingFeature.summary}`;

/** How long the model's reply is held, so the card of the tool can be used before it. */
const replyDelayMs = 12_000;

interface Fragment {
  chunk_id?: string;
  text?: string;
}

/** The tab a place of a source belongs to: the section names of the card (knowledgeView.sectionLabels) are the names of the tabs. */
function tabOfPlace(place: string): string {
  if (place.startsWith("Текст лекции")) {
    return "Текст лекции";
  }
  if (place.startsWith("Расшифровка")) {
    return "Расшифровка";
  }
  if (place.startsWith("Конспект")) {
    return "Конспект";
  }
  return `?(${place})`;
}

test(Feature.source, details(Feature.source, { requires }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const student = "что было на паре по литературе";
  const question = "Беликов калоши зонтик погоду";
  const claim = "Беликов носит калоши и зонтик в любую погоду, об этом сказано в конспекте пары.";
  const finish = "Ответ с источниками подготовлен выше.";

  await step("Hub: модель вызывает answer_question; ANSWER отвечает по первому фрагменту; потом (через 12 с) модель заканчивает ответом", async () => {
    await llm.script([
      callRule(student, "answer_question", { question, scope: "all" }),
      citedRule(question, claim),
      replyRule(AfterAnswer, finish, { delayMs: replyDelayMs }),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  let place = "";
  let label = "";
  await step("вопрос ученика → карточка «Ответ по материалам» с источником, пока модель ещё пишет ответ", async () => {
    await ask(main, student);
    const cards = await waitCards(main, "карточка «Ответ по материалам» с источниками должна появиться", (c) => c.captions.includes("Ответ по материалам") && c.sources.length > 0, 45_000);
    label = cards.sources[0] ?? "";
    expect(label, "источник открывается кнопкой «Открыть: …»").toMatch(/^Открыть: /);
    place = label.replace(/^Открыть: /, "").split(",")[0] ?? "";
    note(`источники: ${cards.sources.join(" | ")}; текст карточки: «${cards.answerText}»`);
    await snapshot("Агент: ответ по материалам с источником");
  });

  let fragments: Fragment[] = [];
  await step("в Hub ушёл один запрос ANSWER: вопрос и непустые фрагменты записей ученика", async () => {
    const answerRequests = await answerRequestsAbout(llm, question);
    expect(answerRequests.length, "к Hub должен уйти ровно один запрос ANSWER").toBe(1);
    const body = JSON.parse(answerRequests[0]?.lastUser ?? "{}") as { question?: string; fragments?: Fragment[] };
    expect(body.question, "в запросе ANSWER вопрос ученика, как его передала модель").toBe(question);
    fragments = body.fragments ?? [];
    expect(fragments.length, "в запросе ANSWER должны быть фрагменты (непустой fragments)").toBeGreaterThan(0);
    for (const f of fragments) {
      expect((f.chunk_id ?? "") !== "" && (f.text ?? "").trim() !== "", `фрагмент без chunk_id или текста: ${JSON.stringify(f).slice(0, 120)}`).toBe(true);
    }
    const texts = fragments.map((f) => f.text ?? "").join("\n");
    note(`фрагментов: ${fragments.length}; начало первого: «${(fragments[0]?.text ?? "").slice(0, 80)}»`);
    expect(texts.includes("калоши"), "во фрагментах должен быть текст конспекта записи (слово «калоши» из конспекта)").toBe(true);
  });

  await step("модель получила только счётчики: ни текста ответа, ни цитаты, ни текста лекции", async () => {
    await expect
      .poll(async () => (await plannerRequests(llm)).length, { timeout: 15_000, intervals: [100, 200], message: "после answer_question модель должна получить второй запрос с результатом" })
      .toBe(2);
    const planner = await plannerRequests(llm);
    expectCatalogV9(planner);
    const results = toolResultsOf(planner[1] as NonNullable<(typeof planner)[1]>);
    expect(results.length).toBe(1);
    const r = results[0] as NonNullable<(typeof results)[0]>;
    expect(r.tool).toBe("answer_question");
    expect(r.isError, `answer_question не должен вернуть ошибку: ${r.text.slice(0, 200)}`).toBe(false);
    const j = r.json as Record<string, unknown>;
    note(`результат answer_question для модели: ${r.text}`);
    for (const k of ["answer", "fragments", "claims", "supported", "sources"]) {
      expect(j[k], `в результате answer_question должно быть поле ${k}`).toBeDefined();
    }
    expect(Number(j.fragments)).toBeGreaterThan(0);
    expect(Number(j.supported), "утверждение подтверждено цитатой").toBeGreaterThanOrEqual(1);
    expect(Number(j.sources)).toBeGreaterThanOrEqual(1);
    const quote = firstWords(fragments[0]?.text ?? "", 8);
    const sent = messagesText(planner[1] as NonNullable<(typeof planner)[1]>);
    expect(sent.includes("об этом сказано в конспекте"), "текст утверждения не должен уходить планировщику").toBe(false);
    expect(quote.length > 8 && sent.includes(quote), "цитата из лекции не должна уходить планировщику").toBe(false);
  });

  await step("источник открывает страницу пары записи на вкладке места, которое он цитирует", async () => {
    await main.locator("aside.ag ul.ag-src button").first().click(act);
    const heading = main.locator("h1.kb-heading");
    await expect(heading, "после нажатия на источник должна открыться страница пары").toBeVisible({ timeout: 20_000 });
    const title = collapse(await heading.innerText(act));
    note(`страница пары: «${title}»; источник «${label}»`);
    expect([lecture.title, lectureOf("перезапуск").title], `страница пары должна быть записью пака записи, а не «${title}»`).toContain(title);
    expect(label.includes(title), `в подписи источника должно быть название пары «${title}»`).toBe(true);
    const tab = main.getByRole("tab", { selected: true });
    await expect(tab, "на странице пары должна быть выбрана вкладка").toHaveCount(1, act);
    const want = tabOfPlace(place);
    await expect(tab, `источник «${place}» должен открыть вкладку «${want}»`).toHaveText(new RegExp(`^${want}`), act);
    await snapshot("Агент: источник открыл страницу пары");
    const known = (await recordingsOfApp(main)).map((r) => r.title);
    expect(known, "пара, которую открыл источник, есть среди записей").toContain(title);
    note(`проверено: ANSWER получил вопрос и ${fragments.length} фрагментов с текстом записи; планировщик — только счётчики; карточка «Ответ по материалам», источник «${place}» открыл страницу пары на вкладке «${want}»`);
  });

  await step("ответ модели приходит и показан как есть (карточка после него — в строке «карточки остаются после ответа»)", async () => {
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(finish)) && y.think === 0, { timeout: 60_000 });
    expect(x.replies).toEqual([collapse(finish)]);
  });
});

// The card must show the text of the answer, not only that it is ready. TASK-296 listed «render claims, quotes and
// "не нашлось в лекциях" on the card» as a follow-up that no task has done: the card says «Ответ с источниками подготовлен.»
// and lists the sources, and the claim is parsed but not shown (frontend/src/bindings/answers.ts, agent/AgentCards.tsx
// case "answer"). ADR-0022: «The text goes to a card; the planner gets counters», and the planner prompt forbids retelling
// the answer, so the student never reads it. This row fails until the card renders the claims.
test(Feature.answerText, details(Feature.answerText, { requires }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const student = "что говорится про зонтик на паре по литературе";
  const question = "Беликов зонтик калоши погоду коротко";
  const claim = "Беликов носит калоши и зонтик в любую погоду, об этом сказано в конспекте пары.";

  await step("Hub: модель вызывает answer_question; ANSWER отвечает по первому фрагменту; ответ модели задержан", async () => {
    await llm.script([
      callRule(student, "answer_question", { question, scope: "all" }),
      citedRule(question, claim),
      replyRule(AfterAnswer, "Ответ готов.", { delayMs: replyDelayMs }),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));
  await step("вопрос ученика → карточка «Ответ по материалам»", async () => {
    await ask(main, student);
    await waitCards(main, "карточка «Ответ по материалам» с источниками", (c) => c.captions.includes("Ответ по материалам") && c.sources.length > 0, 45_000);
    await snapshot("Агент: карточка ответа без текста");
  });
  let mainFailed = true;
  try {
    await step("карточка показывает текст утверждения из ответа, а не только «подготовлен»", async () => {
      const cards = await cardsOf(main);
      note(`текст под заголовком карточки: «${cards.answerText}»`);
      expect(
        cards.answerText.includes("калоши и зонтик в любую погоду"),
        `карточка «Ответ по материалам» не показывает текст ответа (утверждение «${claim}»): в ней только «${cards.answerText}» и источники; ученик не читает ответ (TASK-296: утверждения разобраны, но не выведены)`,
      ).toBe(true);
    });
    mainFailed = false;
  } finally {
    // the held reply of the model ends the request; the next row starts on a quiet panel. A request that never ends fails the test
    // when the main step passed; after a failed main step only that failure is reported.
    const ended = waitThread(main, "запрос должен закончиться", (x) => x.think === 0, { timeout: 40_000 });
    if (mainFailed) {
      await ended.catch(() => undefined);
    } else {
      await ended;
    }
  }
});
