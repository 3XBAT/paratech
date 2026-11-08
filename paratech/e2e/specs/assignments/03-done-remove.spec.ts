// Pack «Задания», part 3 (TASK-381; TASK-316, TASK-232, TASK-338). The student closes and removes assignments:
//   1. the round check «Отметить выполненным» takes the row out of «Открытые» (and out of «Сроки на неделе»), the toast
//      «Отменить» (and Ctrl+Z) puts it back, «Выполненные» lists it, «Вернуть в работу» brings it back; the same on the page;
//   2. «⋯» → «Удалить» removes it at once (the question «Работа остановится» exists only for a running draft), the line
//      «Задание удалено» offers «Вернуть», which brings the row back; a final delete leaves no row in any list.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { addAssignment, burningRows, doneCheck, expectInGroup, hasStateSwitch, listRow, moreButton, openFromList, openList, pageTitle, reopen, removeQuietly, showState, toast } from "./driver.js";
import { act, dayFromNow, details, Feature, note, OnlySubjects, Subject, textOf } from "./support.js";

const Closing = "Закрыть тему про Ивана Грозного";
const Removing = "Убрать лишнее задание по истории";

test(Feature.done, details(Feature.done, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  try {
    await step(`добавить «${Closing}» со сроком через 3 дня; пока открыто, оно горит на «Сегодня»`, async () => {
      await addAssignment(main, { title: Closing, subject: Subject, deadline: { date: dayFromNow(3) } });
      await openList(main);
      await expectInGroup(main, Closing, "На этой неделе");
      const rows = await burningRows(main, Closing);
      await expect(rows, "открытое задание горит в «Сроки на неделе»").toHaveCount(1, { timeout: 30_000 });
    });

    await step("кружок «Отметить выполненным»: строка уходит из «Открытые», в тосте «Отменить»", async () => {
      await openList(main);
      await doneCheck(main, Closing).click(act);
      await expect(listRow(main, Closing), "выполненное задание уходит из списка «Открытые»").toHaveCount(0, { timeout: 15_000 });
      await expect(toast(main), "после отметки показывается тост").toBeVisible(act);
      const text = await textOf(toast(main));
      note(`тост: ${text}`);
      expect(text).toContain(`Выполнено: «${Closing}»`);
      expect(text).toContain("Отменить");
      await snapshot("Задания: тост после «Выполнено»");
    });

    await step("«Отменить» в тосте возвращает задание в «На этой неделе»", async () => {
      await toast(main).getByRole("button", { name: "Отменить", exact: true }).click(act);
      await expectInGroup(main, Closing, "На этой неделе");
    });

    await step("«Выполненные»: после повторной отметки задание в этом списке с датой «Выполнено …»", async () => {
      await doneCheck(main, Closing).click(act);
      await expect(listRow(main, Closing)).toHaveCount(0, { timeout: 15_000 });
      await showState(main, "Выполненные");
      const row = listRow(main, Closing);
      await expect(row, "в «Выполненные» должно быть это задание").toHaveCount(1, { timeout: 15_000 });
      const text = await textOf(row);
      note(`строка «Выполненные»: ${text}`);
      expect(text, "у выполненного есть дата «Выполнено …»").toMatch(/Выполнено \d{1,2} /);
      await expect(doneCheck(main, Closing, true), "кружок выполненного предлагает «Вернуть в работу»").toBeVisible(act);
      await snapshot("Задания: выполненные");
    });

    await step("«Сегодня»: выполненное задание больше не горит", async () => {
      const rows = await burningRows(main, Closing);
      await expect(rows).toHaveCount(0, { timeout: 30_000 });
    });

    await step("«Вернуть в работу»: задание снова в «Открытые»", async () => {
      await openList(main);
      await showState(main, "Выполненные");
      await doneCheck(main, Closing, true).click(act);
      await expect(listRow(main, Closing), "возвращённое задание уходит из «Выполненные»").toHaveCount(0, { timeout: 15_000 });
      await showState(main, "Открытые");
      await expectInGroup(main, Closing, "На этой неделе");
    });

    await step("страница задания: «Отметить выполненным» → «Выполнено …» → «Вернуть в работу»", async () => {
      await openFromList(main, Closing);
      const mark = main.locator(".page.hw .head").getByRole("button", { name: "Отметить выполненным", exact: true });
      await mark.click(act);
      await expect(main.locator(".hw-donedate").first(), "под заголовком появляется «Выполнено …»").toContainText("Выполнено", act);
      const back = main.locator(".page.hw .head").getByRole("button", { name: "Вернуть в работу", exact: true });
      await expect(back, "кнопка страницы теперь «Вернуть в работу»").toBeVisible(act);
      await back.click(act);
      await expect(mark, "после возврата снова «Отметить выполненным»").toBeVisible(act);
      await expect(main.locator(".page.hw .head + p.hw-donedate")).toHaveCount(0, act);
      await snapshot("Страница задания: отметка выполнено и возврат");
    });
  } finally {
    const left = await removeQuietly(main, [Closing]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});

test(Feature.remove, details(Feature.remove, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  try {
    await step(`добавить «${Removing}»; пока оно есть, оно горит на «Сегодня»`, async () => {
      await addAssignment(main, { title: Removing, subject: Subject, deadline: { date: dayFromNow(3) } });
      await openList(main);
      await expectInGroup(main, Removing, "На этой неделе");
      const rows = await burningRows(main, Removing);
      await expect(rows).toHaveCount(1, { timeout: 30_000 });
    });

    await step("«⋯» → «Удалить»: «Задание удалено» и кнопка «Вернуть»", async () => {
      await openList(main);
      await moreButton(listRow(main, Removing), Removing).click(act);
      await main.getByRole("menuitem", { name: "Удалить", exact: true }).click(act);
      const removed = main.locator(".hw-removed");
      await expect(removed, "на месте строки должно быть «Задание удалено»").toContainText("Задание удалено", { timeout: 15_000 });
      await expect(removed.getByRole("button", { name: "Вернуть", exact: true }), "у удаления есть «Вернуть»").toBeVisible(act);
      await snapshot("Задания: задание удалено, «Вернуть»");
    });

    await step("«Вернуть»: задание снова в списке, в своей группе", async () => {
      await main.locator(".hw-removed").getByRole("button", { name: "Вернуть", exact: true }).click(act);
      await expect(main.locator(".hw-removed"), "строка «Задание удалено» уходит").toHaveCount(0, { timeout: 15_000 });
      await expectInGroup(main, Removing, "На этой неделе");
      await reopen(main, Removing);
      await expect(pageTitle(main)).toHaveText(Removing, act);
    });

    await step("удалить ещё раз: после выхода из «Задания» и возврата строки нет ни в одном списке", async () => {
      await openList(main);
      await moreButton(listRow(main, Removing), Removing).click(act);
      await main.getByRole("menuitem", { name: "Удалить", exact: true }).click(act);
      await expect(main.locator(".hw-removed")).toContainText("Задание удалено", { timeout: 15_000 });
      await openList(main);
      await expect(listRow(main, Removing), "удалённого задания нет в «Открытые»").toHaveCount(0, act);
      // with no other assignment the lists give way to the first-run screen, which has no switch: nothing exists then
      for (const state of ["Все", "Выполненные"] as const) {
        if (!(await hasStateSwitch(main))) {
          break;
        }
        const listed = await showState(main, state);
        if (!listed) {
          note(`в «${state}» ничего нет: вместо списка экран «Заданий пока нет»`);
          await expect(main.getByText("Заданий пока нет")).toBeVisible(act);
        }
        await expect(listRow(main, Removing), `удалённого задания нет и в «${state}»`).toHaveCount(0, act);
      }
    });

    await step("«Сегодня»: удалённое задание не горит", async () => {
      const rows = await burningRows(main, Removing);
      await expect(rows).toHaveCount(0, { timeout: 30_000 });
    });
  } finally {
    const left = await removeQuietly(main, [Removing]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
