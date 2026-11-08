// Pack «Задания», part 2 (TASK-381; ADR-0029 addendum 2026-10-27, TASK-232). The student changes the deadline of an assignment:
// «⋯» → «Изменить» on the assignment page shows the stored deadline with its time, the new date moves the assignment to
// another group (and out of «Сроки на неделе» when it is more than 7 days away). «Журнал» is part 7.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { addAssignment, burningRows, editDeadline, expectInGroup, moreButton, openList, openFromList, pageDue, pageTitle, removeQuietly } from "./driver.js";
import { act, comingText, dayFromNow, details, Feature, note, OnlySubjects, Subject } from "./support.js";

const Moving = "Перенести доклад по истории";

test(Feature.edit, details(Feature.edit, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  const before = { date: dayFromNow(3), time: "09:30" };
  const after = { date: dayFromNow(10), time: "09:30" };
  try {
    await step(`добавить «${Moving}» со сроком через 3 дня в 09:30`, async () => {
      await addAssignment(main, { title: Moving, subject: Subject, deadline: before });
      expect(await pageDue(main)).toMatch(comingText(before));
      await openList(main);
      await expectInGroup(main, Moving, "На этой неделе");
      const rows = await burningRows(main, Moving);
      await expect(rows, "пока срок близко, задание горит в «Сроки на неделе»").toHaveCount(1, { timeout: 30_000 });
    });

    await step("«⋯» → «Изменить»: в форме сохранённые дата и время", async () => {
      await openList(main);
      await openFromList(main, Moving);
      await moreButton(main.locator(".page.hw .head"), Moving).click(act);
      await main.getByRole("menuitem", { name: "Изменить", exact: true }).click(act);
      const dialog = main.getByRole("dialog", { name: "Изменить задание" });
      await expect(dialog).toBeVisible(act);
      await expect(dialog.getByLabel("Срок", { exact: true }), "в форме правки дата срока").toHaveValue(before.date, act);
      await expect(dialog.getByLabel("Время", { exact: true }), "в форме правки время срока").toHaveValue(before.time, act);
      await snapshot("Изменить задание: форма");
      await dialog.getByRole("button", { name: "Отмена", exact: true }).click(act);
      await expect(dialog).toBeHidden(act);
    });

    await step("новая дата через 10 дней: страница показывает новый срок", async () => {
      await editDeadline(main, Moving, { date: after.date });
      await expect.poll(() => pageDue(main), { timeout: 15_000, message: "страница должна показать новый срок" }).toMatch(comingText(after));
      await expect(pageTitle(main)).toHaveText(Moving, act);
    });

    await step("«Задания»: задание перешло в группу «Позже»", async () => {
      await openList(main);
      const text = await expectInGroup(main, Moving, "Позже");
      note(`строка списка: ${text}`);
      expect(text).toMatch(comingText(after));
      await snapshot("Задания: группа «Позже»");
    });

    await step("«Сегодня»: через 10 дней задание уже не горит", async () => {
      const rows = await burningRows(main, Moving);
      await expect(rows, "срок дальше недели: задания нет в «Сроки на неделе»").toHaveCount(0, { timeout: 30_000 });
    });
  } finally {
    const left = await removeQuietly(main, [Moving]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
