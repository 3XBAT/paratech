// Agent pack, part B, file 13: the cards of a tool stay after the model's reply. A tool that shows a card (the answer with its
// sources, the action line with «Отменить», the confirm card with «Да»/«Нет») hands it to the panel in an update of its own; the
// model's last step, the `reply`, is another update that carries only the reply card. The panel used to REPLACE the cards of a request
// with the cards of the latest update, so they vanished when the reply arrived (D1); since TASK-399 they stay. This row is the
// regression guard: no hold of the reply (no `delayMs`), the cards are judged AFTER the final reply, and again after a short quiet
// window (a card that goes a moment later is also a defect). The other tool rows (8, 10, 11) use their cards after the reply too.
//
// SCRIPTED. Turn 1: find_lessons, then answer_question and propose_delete_recording in ONE step, then the reply. Turn 2:
// create_subject, then the reply; its action line is undone AFTER the reply, so no subject is left.
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
  settle,
  waitCards,
  waitThread,
} from "./support.js";
import { Feature as RecordingFeature } from "../recording/support.js";

/** How long the cards are watched after the reply: a card that vanishes a moment later is also a defect. */
const quietMs = 1_500;

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

  await step("Hub: два хода; в первом find_lessons, потом answer_question и propose_delete_recording в одном шаге, ответ; во втором create_subject и ответ", async () => {
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
      replyRule(AfterProposal, answer),
      callRule(subjectStudent, "create_subject", { name: subjectName }),
      replyRule(AfterWrite, subjectAnswer),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  const problems: string[] = [];

  await step("ход 1: ответ модели показан; карточка «Ответ по материалам» с источником и карточка подтверждения остались, и не пропадают позже", async () => {
    await ask(main, student);
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(answer)) && y.think === 0, { timeout: 60_000 });
    const cards = await cardsOf(main);
    await snapshot("Агент: карточки после ответа модели");
    note(`после ответа модели: карточки ${cards.captions.join(" | ") || "нет"}; источников ${cards.sources.length}; карточек подтверждения ${cards.confirmCards}; ответ «${x.replies.join(" | ")}»`);
    if (!cards.captions.includes("Ответ по материалам") || cards.sources.length === 0) {
      problems.push("карточка «Ответ по материалам» с источниками исчезла после ответа модели: ученик не откроет источник");
    }
    if (cards.confirmCards !== 1) {
      problems.push("карточка подтверждения исчезла после ответа модели: удаление записи нельзя подтвердить или отклонить в панели");
    }
    await settle(main, quietMs);
    const later = await cardsOf(main);
    if (cards.captions.includes("Ответ по материалам") && (!later.captions.includes("Ответ по материалам") || later.sources.length === 0)) {
      problems.push(`карточка «Ответ по материалам» пропала через ${quietMs} мс после ответа модели`);
    }
    if (cards.confirmCards === 1 && later.confirmCards !== 1) {
      problems.push(`карточка подтверждения пропала через ${quietMs} мс после ответа модели`);
    }
    // the proposal is left as the student found it: declined, if its card is still there
    if (later.confirmCards === 1) {
      await main.locator("aside.ag section.ag-confirm").getByRole("button", { name: "Нет", exact: true }).click(act);
    }
  });

  await step("ход 2: после ответа модели строка действия создания предмета осталась; её «Отменить» убирает предмет", async () => {
    await prepare(main);
    await ask(main, subjectStudent);
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(subjectAnswer)) && y.think === 0, { timeout: 60_000 });
    const cards = await cardsOf(main);
    await snapshot("Агент: строка действия после ответа модели");
    note(`после ответа модели: строки действия ${cards.actionLines.join(" | ") || "нет"}; кнопки ${cards.actionButtons.join(" | ") || "нет"}; ответ «${x.replies.join(" | ")}»`);
    if (cards.actionLines.length !== 1) {
      problems.push("строка действия («Действие выполнено» / «Отменено») исчезла после ответа модели: ученик не видит, что сделано");
    }
    await settle(main, quietMs);
    const later = await cardsOf(main);
    if (cards.actionLines.length === 1 && later.actionLines.length !== 1) {
      problems.push(`строка действия пропала через ${quietMs} мс после ответа модели`);
    }
    // undone after the reply, so no subject is left; a missing «Отменить» is also a defect of the stay
    if (later.actionButtons.includes("Отменить")) {
      await main.locator("aside.ag .ag-act").getByRole("button", { name: "Отменить", exact: true }).click(act);
      await waitCards(main, "строка действия должна стать «отменено»", (c) => !c.actionButtons.includes("Отменить"), 20_000);
    } else if (later.actionLines.length === 1) {
      problems.push("у строки действия после ответа модели нет «Отменить»: сделанное нельзя отменить");
    }
  });

  await step("карточки инструментов не пропадают, когда приходит ответ модели", () => {
    expect(
      problems,
      "карточки answer_question, propose_delete_recording и create_subject должны оставаться в ленте после ответа модели (D1: приложение заменяло все карточки запроса последним обновлением — ответом)",
    ).toEqual([]);
  });
});
