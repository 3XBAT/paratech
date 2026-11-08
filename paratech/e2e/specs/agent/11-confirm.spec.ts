// Agent pack, part B, file 11: a class C tool never acts on its own (AC-18, hard rule 3, ADR-0021/0022). SCRIPTED: the model finds the
// recording with find_lessons and proposes its deletion with propose_delete_recording (the id comes from the tool result of the
// same turn, as the provenance check demands). The pack asserts that the panel shows the confirm card, that the recording is
// still in «Записи» while nobody pressed anything, that the model was told it is only a proposal, and that «Нет» keeps the
// recording. It never presses the destructive «Да, удалить»: the recording is needed by the rows after it and by the other packs.
//
// The model's final `reply` is delayed (12 s) so the card can be used while the request is still running: the app replaces the cards
// of a request with the last one, the model's reply (the row «карточки остаются после ответа модели», file 13).
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { AfterLessons, AfterProposal, callRule, replyRule } from "../../scripts/llm/agent/rules.js";
import { showRow } from "../recording/support.js";
import {
  type AppRecording,
  act,
  ask,
  cardsOf,
  collapse,
  details,
  expectCatalogV9,
  Feature,
  lectureOf,
  note,
  openSection,
  plannerRequests,
  PrerequisitesB,
  prepare,
  recordingByTitle,
  recordingsOfApp,
  toolResultsOf,
  waitCards,
  waitThread,
} from "./support.js";

const replyDelayMs = 12_000;

test(Feature.confirm, details(Feature.confirm, { requires: PrerequisitesB }), async ({ main, llm }) => {
  test.setTimeout(120_000);
  const question = "удали запись про перезапуск";
  const target = lectureOf("перезапуск");
  const answer = "Удаление записи предложено: оно выполнится только после подтверждения в карточке.";

  let recording: AppRecording = { id: "", title: "", subject: "" };
  let countBefore = 0;
  await step("«Записи»: запись для удаления есть", async () => {
    recording = await recordingByTitle(main, target.title);
    countBefore = (await recordingsOfApp(main)).length;
    note(`запись для удаления: «${target.title}», всего записей: ${countBefore}`);
  });

  await step("Hub: find_lessons, потом propose_delete_recording с id из результата, потом (через 12 с) ответ", async () => {
    await llm.script([
      callRule(question, "find_lessons", {}),
      callRule(AfterLessons, "propose_delete_recording", { recording: recording.id }),
      replyRule(AfterProposal, answer, { delayMs: replyDelayMs }),
    ]);
  });
  await step("панель: новый пустой чат на «Сегодня»", () => prepare(main));

  await step("просьба удалить запись → карточка подтверждения: «Не удалять» и «Да, удалить», строки действия нет", async () => {
    await ask(main, question);
    const cards = await waitCards(main, "карточка подтверждения должна появиться", (c) => c.confirmCards > 0 && c.confirmButtons.length > 0, 60_000);
    note(`карточка: «${cards.confirmText}»; кнопки: ${cards.confirmButtons.join(" | ")}`);
    expect(cards.confirmCards, "ровно одна карточка подтверждения").toBe(1);
    expect(cards.confirmText, "карточка называет удаление записи").toContain("Удаление записи");
    expect(cards.confirmText, `карточка называет запись «${target.title}»`).toContain(target.title);
    expect(cards.confirmButtons, "у карточки есть отказ «Нет»").toContain("Нет");
    expect(cards.confirmButtons.some((b) => b.startsWith("Да")), `и подтверждение «Да», которое ученик нажимает сам; кнопки: ${cards.confirmButtons.join(" | ")}`).toBe(true);
    expect(cards.actionLines, "ничего не выполнено: строки действия нет").toEqual([]);
    await snapshot("Агент: карточка подтверждения удаления");
  });

  await step("модели сказано, что это только предложение: proposed, waits_for student, без done", async () => {
    await expect
      .poll(async () => (await plannerRequests(llm)).length, { timeout: 15_000, intervals: [100, 200], message: "после предложения модель должна получить третий запрос" })
      .toBe(3);
    const planner = await plannerRequests(llm);
    expectCatalogV9(planner);
    const found = toolResultsOf(planner[1] as NonNullable<(typeof planner)[1]>);
    expect(found[0]?.text.includes(recording.id), "id записи модель взяла из результата find_lessons этого же запроса").toBe(true);
    const proposed = toolResultsOf(planner[2] as NonNullable<(typeof planner)[2]>);
    expect(proposed.length).toBe(1);
    const p = proposed[0] as NonNullable<(typeof proposed)[0]>;
    note(`результат propose_delete_recording для модели: is_error=${p.isError} ${p.text}`);
    expect(p.tool).toBe("propose_delete_recording");
    expect(p.isError, `предложение не должно быть отклонено: ${p.text.slice(0, 200)}`).toBe(false);
    const j = p.json as Record<string, unknown>;
    expect(j.proposed, "результат: proposed").toBe(true);
    expect(j.waits_for, "результат: ждёт ученика").toBe("student");
    expect(j.done, "модели не сказано, что запись удалена").toBeUndefined();
    expect(j.deleted, "модели не сказано, что запись удалена").toBeUndefined();
  });

  await step("без нажатия запись на месте: в «Записи» есть строка, список записей не изменился", async () => {
    await openSection(main, "Записи");
    const row = await showRow(main, target.title);
    await expect(row, `запись «${target.title}» должна остаться в «Записи»`).toHaveCount(1, { timeout: 20_000 });
    const now = await recordingsOfApp(main);
    expect(now.length, "число записей не изменилось").toBe(countBefore);
    expect(now.some((r) => r.id === recording.id), "запись с тем же id всё ещё есть").toBe(true);
    await snapshot("Записи: запись на месте до подтверждения");
  });

  await step("«Нет»: предложение отклонено, запись остаётся, «Да» больше нет", async () => {
    const decline = main.locator("aside.ag section.ag-confirm").getByRole("button", { name: "Нет", exact: true });
    await expect(decline, "в панели должна быть кнопка «Нет»").toBeVisible(act);
    await decline.click(act);
    const cards = await waitCards(main, "карточка должна сказать, что ничего не выполнено", (c) => c.confirmCards > 0 && !c.confirmButtons.some((b) => b.startsWith("Да")), 20_000);
    note(`карточка после отказа: «${cards.confirmText}»`);
    expect(cards.confirmText, "карточка говорит, что ничего не выполнено").toMatch(/ничего не выполнено|Оставлено|Отклонено/);
    const now = await recordingsOfApp(main);
    expect(now.some((r) => r.id === recording.id), "после отказа запись осталась").toBe(true);
    expect(now.length).toBe(countBefore);
    await snapshot("Агент: удаление отклонено");
    note("проверено: C-инструмент даёт только карточку подтверждения; запись на месте и до, и после «Нет»; модели сказано proposed/waits_for=student без done; «Да» не нажималась");
  });

  await step("ответ модели приходит и показан как есть (карточка после него — в строке «карточки остаются после ответа»)", async () => {
    const x = await waitThread(main, "ответ модели должен появиться", (y) => y.replies.includes(collapse(answer)) && y.think === 0, { timeout: 60_000 });
    expect(x.replies).toEqual([collapse(answer)]);
  });
});
