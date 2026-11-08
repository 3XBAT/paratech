// Agent pack, part B, file 13: the cards of a tool stay after the model's reply. A tool that shows a card (the answer with its
// sources, the action line with «Отменить», the confirm card with «Да»/«Нет») hands it to the panel in an update of its own; the
// model's last step, the `reply`, is another update that carries only the reply card, and the panel REPLACES the cards of a request
// with the cards of the latest update (frontend agentState.ts applyUpdateEntries; Driver.update, agentturn stepOf). The scenarios
// of files 8, 10 and 11 therefore use the cards while the reply of the model is held (delayMs): here the reply is held a few
// seconds only to SEE the cards, and the row asserts that they are still on screen after the reply.
//
// SCRIPTED. Turn 1: find_lessons, then answer_question and propose_delete_recording in ONE step, then the reply. Turn 2:
// create_subject (its action line is undone inside the window, so no subject is left), then the reply.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterLessons, AfterProposal, AfterWrite, callRule, citedRule, replyRule } from "../../scripts/llm/agent/rules.js";
import type { Rule } from "../../src/steps/index.js";
import {
  type AppRecording,
  act,
  ask,
  cardsOf,
  collapse,
  details,
  Feature,
  lectureOf,
  note,
  PrerequisitesB,
  prepare,
  recordingByTitle,
  waitCards,
  waitThread,
} from "./support.js";
import { Feature as RecordingFeature } from "../recording/support.js";

const holdMs = 5_000;

test(Feature.cardsStay, details(Feature.cardsStay, { requires: `${PrerequisitesB} + ${RecordingFeature.summary}` }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const student = "покажи, что было про Беликова, и предложи удалить запись про перезапуск";
  const question = "Беликов футляр калоши зонтик погоду два";
  const claim = "Беликов носит калоши и зонтик в любую погоду, об этом сказано в конспекте пары.";
  const answer = "Ответ по лекциям показан, удаление записи предложено и ждёт подтверждения.";
  const target = lectureOf("перезапуск");
  let recording: AppRecording = { id: "", title: "", subject: "" };

  const subjectStudent = "добавь предмет Теория вычислений";
  const subjectName = "Теория вычислений";
  const subjectAnswer = "Предмет добавлен.";

  await step("«Записи»: запись про перезапуск есть", async () => {
    recording = await recordingByTitle(main, target.title);
  });

  await step("Hub: два хода; в первом find_lessons, потом answer_question и propose_delete_recording в одном шаге, ответ через 5 с; во втором create_subject и ответ через 20 с", async () => {
    const bothTools: Rule = {
      kind: "tools",
      match: AfterLessons,
      reply: [
        { name: "answer_question", input: { question, scope: "all" } },
        { name: "propose_delete_recording", input: { recording: recording.id } },
      ],
    };
    await llm.script([
      callRule(student, "find_lessons", {}),
      bothTools,
      citedRule(question, claim),
      replyRule(AfterProposal, answer, { delayMs: holdMs }),
      callRule(subjectStudent, "create_subject", { name: subjectName }),
      replyRule(AfterWrite, subjectAnswer, { delayMs: 20_000 }),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  const problems: string[] = [];

  await step("ход 1: карточка «Ответ по материалам» с источником и карточка подтверждения видны, пока модель пишет ответ", async () => {
    await ask(main, student);
    const cards = await waitCards(
      main,
      "обе карточки должны появиться",
      (c) => c.captions.includes("Ответ по материалам") && c.sources.length > 0 && c.confirmCards > 0 && c.confirmButtons.length > 0,
      60_000,
    );
    note(`пока модель пишет ответ: карточки ${cards.captions.join(" | ")}; источников ${cards.sources.length}; подтверждение: ${cards.confirmButtons.join(" | ")}`);
    await snapshot("Агент: карточки до ответа модели");
  });

  await step("ход 1: после ответа модели обе карточки остались", async () => {
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(answer)) && y.think === 0, { timeout: 40_000 });
    const cards = await cardsOf(main);
    await snapshot("Агент: карточки после ответа модели");
    note(`после ответа модели: карточки ${cards.captions.join(" | ") || "нет"}; источников ${cards.sources.length}; карточек подтверждения ${cards.confirmCards}; ответ «${x.replies.join(" | ")}»`);
    if (!cards.captions.includes("Ответ по материалам") || cards.sources.length === 0) {
      problems.push("карточка «Ответ по материалам» с источниками исчезла после ответа модели: ученик не откроет источник");
    }
    if (cards.confirmCards !== 1) {
      problems.push("карточка подтверждения исчезла после ответа модели: удаление записи нельзя подтвердить или отклонить в панели");
    }
    // the proposal is left as the student found it: declined, if its card is still there
    if (cards.confirmCards === 1) {
      await main.locator("aside.ag section.ag-confirm").getByRole("button", { name: "Нет", exact: true }).click(act);
    }
  });

  await step("ход 2: строка действия создания предмета видна, пока модель пишет ответ; её «Отменить» убирает предмет", async () => {
    await prepare(main);
    await ask(main, subjectStudent);
    const cards = await waitCards(main, "строка действия должна появиться", (c) => c.actionLines.length > 0 && c.actionButtons.includes("Отменить"), 45_000);
    note(`пока модель пишет ответ: строки действия ${cards.actionLines.join(" | ")}`);
    await snapshot("Агент: строка действия до ответа модели");
    await main.locator("aside.ag .ag-act").getByRole("button", { name: "Отменить", exact: true }).click(act);
    await waitCards(main, "строка действия должна стать «отменено»", (c) => !c.actionButtons.includes("Отменить"), 20_000);
  });

  await step("ход 2: после ответа модели строка действия осталась", async () => {
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(subjectAnswer)) && y.think === 0, { timeout: 60_000 });
    const cards = await cardsOf(main);
    await snapshot("Агент: строка действия после ответа модели");
    note(`после ответа модели: строки действия ${cards.actionLines.join(" | ") || "нет"}; ответ «${x.replies.join(" | ")}»`);
    if (cards.actionLines.length !== 1) {
      problems.push("строка действия («Действие выполнено» / «Отменено») исчезла после ответа модели: ученик не видит, что сделано");
    }
  });

  await step("карточки инструментов не пропадают, когда приходит ответ модели", () => {
    expect(
      problems,
      "приложение заменяет все карточки запроса последним обновлением, ответом модели (agentState.ts: replace(entries, key, {update: u}); Driver.update): карточки answer_question, propose_delete_recording и create_subject видны, пока модель пишет, и пропадают с ответом",
    ).toEqual([]);
  });
});
