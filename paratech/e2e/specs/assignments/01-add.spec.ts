// Pack «Задания», part 1 (TASK-381; ADR-0029, TASK-304, TASK-343, TASK-344). The student adds assignments by hand through
// «Добавить задание» and sees them in the right group of «Задания» and in «Сегодня» → «Сроки на неделе», always with the time:
//   1. a date alone gets 20:00 (TASK-304); a typed time is kept; a deadline that is still to come reads «до <день>, ЧЧ:ММ»;
//   2. a deadline that has PASSED never reads «сегодня»/«завтра»: it is «просрочено, …», in «Просрочено», and it does not
//      burn in «Сроки на неделе». The app takes a past deadline up to 31 days back (assignment.NewByStudent).
// Extraction from a lesson summary has no UI path besides the recording import (the recording pack covers it): see the README.
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { addAssignment, burningRows, expectInGroup, openList, pageDue, removeQuietly } from "./driver.js";
import { act, comingText, dayFromNow, details, Feature, note, OnlySubjects, overdueText, Subject, textOf, todayWords } from "./support.js";

const DateOnly = "Прочитать главу про Смутное время";
const Timed = "Подготовить доклад про Петра Первого";

test(Feature.add, details(Feature.add, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  // three days ahead: «На этой неделе» (the next 7 days), and never «завтра», even if the run crosses midnight
  const dateOnly = { date: dayFromNow(3), time: "20:00" };
  const timed = { date: dayFromNow(4), time: "09:30" };
  try {
    await step("форма: время по умолчанию 20:00, срок без времени сохраняется как 20:00", async () => {
      await openList(main);
      await main.locator(".page.hw .head").getByRole("button", { name: "Добавить задание", exact: true }).click(act);
      const dialog = main.getByRole("dialog", { name: "Добавить задание" });
      await expect(dialog).toBeVisible(act);
      await expect(dialog.getByLabel("Время", { exact: true }), "время в форме начинается с 20:00").toHaveValue("20:00", act);
      await expect(dialog.getByText("По умолчанию — 20:00."), "подсказка к времени").toBeVisible(act);
      await dialog.getByRole("button", { name: "Отмена", exact: true }).click(act);
      await expect(dialog).toBeHidden(act);
    });

    await step(`«Добавить задание»: «${DateOnly}», История, только дата (время очищено)`, async () => {
      await addAssignment(main, { title: DateOnly, subject: Subject, deadline: { date: dateOnly.date, time: "" }, note: "Параграфы 5 и 6, без пересказа." });
      const due = await pageDue(main);
      note(`страница задания: ${due}`);
      expect(due, `срок без времени должен читаться как «до …, 20:00»: ${due}`).toMatch(comingText(dateOnly));
      await snapshot("Задание: страница после добавления");
    });

    await step(`«Добавить задание»: «${Timed}», История, срок с временем 09:30`, async () => {
      await addAssignment(main, { title: Timed, subject: Subject, deadline: { date: timed.date, time: timed.time } });
      const due = await pageDue(main);
      expect(due, `введённое время должно остаться: ${due}`).toMatch(comingText(timed));
    });

    await step("«Задания»: оба задания в группе «На этой неделе» со сроком и временем", async () => {
      await openList(main);
      for (const [title, deadline] of [
        [DateOnly, dateOnly],
        [Timed, timed],
      ] as const) {
        const text = await expectInGroup(main, title, "На этой неделе");
        note(`строка списка: ${text}`);
        expect(text, `у «${title}» срок со временем «до …, ${deadline.time}»`).toMatch(comingText(deadline));
        expect(text, `у «${title}» виден предмет`).toContain(Subject);
      }
      await snapshot("Задания: группа «На этой неделе»");
    });

    await step("«Сегодня» → «Сроки на неделе»: оба задания, срок со временем, предмет", async () => {
      for (const [title, deadline] of [
        [DateOnly, dateOnly],
        [Timed, timed],
      ] as const) {
        const rows = await burningRows(main, title);
        await expect(rows, `в «Сроки на неделе» должно быть задание «${title}»`).toHaveCount(1, { timeout: 30_000 });
        const text = await textOf(rows);
        note(`строка «Сроки на неделе»: ${text}`);
        expect(text, `в «Сроки на неделе» у «${title}» срок «до …, ${deadline.time}»`).toMatch(comingText(deadline));
        expect(text).toContain(Subject);
      }
      await snapshot("Сегодня: сроки на неделе");
    });
  } finally {
    const left = await removeQuietly(main, [DateOnly, Timed]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});

const Yesterday = "Сдать конспект по истории вчера";
const ThisNight = "Сдать таблицу дат с ночи";
const Control = "Контроль: срок через три дня";

test(Feature.overdue, details(Feature.overdue, { requires: OnlySubjects }), async ({ main }) => {
  test.setTimeout(3 * 60_000);
  const yesterday = { date: dayFromNow(-1), time: "20:00" };
  // today at 00:00: it has passed whatever the hour is now, and it is the day the app would call «сегодня» if it were still ahead
  const tonight = { date: dayFromNow(0), time: "00:00" };
  const control = { date: dayFromNow(3), time: "20:00" };
  try {
    await step("добавить: вчерашний срок (без времени), сегодняшний срок в 00:00 и контрольное задание через три дня", async () => {
      await addAssignment(main, { title: Yesterday, subject: Subject, deadline: { date: yesterday.date, time: "" } });
      const due = await pageDue(main);
      note(`страница, срок вчера: ${due}`);
      expect(due, `вчерашний срок читается «просрочено, …, 20:00»: ${due}`).toMatch(overdueText(yesterday));
      expect(due, `вчерашний срок не должен читаться как «сегодня»/«завтра»: ${due}`).not.toMatch(todayWords);

      await addAssignment(main, { title: ThisNight, subject: Subject, deadline: { date: tonight.date, time: tonight.time } });
      const dueNight = await pageDue(main);
      note(`страница, срок сегодня в 00:00: ${dueNight}`);
      expect(dueNight, `срок сегодня в 00:00 уже прошёл: «просрочено, …, 00:00»: ${dueNight}`).toMatch(overdueText(tonight));
      expect(dueNight, `прошедший срок сегодняшнего дня не должен читаться как «сегодня до …»: ${dueNight}`).not.toMatch(todayWords);

      await addAssignment(main, { title: Control, subject: Subject, deadline: { date: control.date, time: "" } });
    });

    await step("«Задания»: прошедшие сроки в группе «Просрочено», контрольное — в «На этой неделе»", async () => {
      await openList(main);
      for (const [title, deadline] of [
        [Yesterday, yesterday],
        [ThisNight, tonight],
      ] as const) {
        const text = await expectInGroup(main, title, "Просрочено");
        note(`строка списка: ${text}`);
        expect(text, `у «${title}» срок «просрочено, …»`).toMatch(overdueText(deadline));
        expect(text, `в списке прошедший срок «${title}» не читается как «сегодня»/«завтра»`).not.toMatch(todayWords);
      }
      await expectInGroup(main, Control, "На этой неделе");
      // «Просрочено» is the first group: it stands above «На этой неделе»
      const order = await main.locator("section.hw-sec h2.hw-group").allInnerTexts();
      const names = order.map((t) => t.replace(/\s*\d+$/, "").trim());
      expect(names.indexOf("Просрочено"), `порядок групп: ${names.join(" → ")}`).toBeLessThan(names.indexOf("На этой неделе"));
      await snapshot("Задания: просроченные");
    });

    await step("«Сегодня» → «Сроки на неделе»: контрольное задание горит, прошедшие — нет", async () => {
      const control$ = await burningRows(main, Control);
      await expect(control$, "свежие данные «Сегодня»: контрольное задание в «Сроки на неделе»").toHaveCount(1, { timeout: 30_000 });
      for (const title of [Yesterday, ThisNight]) {
        const rows = await burningRows(main, title);
        await expect(rows, `прошедший срок «${title}» не должен быть в «Сроки на неделе»`).toHaveCount(0, act);
      }
      // nothing on «Сегодня» calls them «сегодня»
      for (const title of [Yesterday, ThisNight]) {
        await expect(main.locator("main.sheet").getByText(title), `«${title}» не должно быть на «Сегодня»`).toHaveCount(0, act);
      }
      await snapshot("Сегодня: сроки на неделе без просроченных");
    });
  } finally {
    const left = await removeQuietly(main, [Yesterday, ThisNight, Control]);
    if (left.length > 0) {
      note(`не удалось убрать после проверки: ${left.join("; ")}`);
    }
  }
});
