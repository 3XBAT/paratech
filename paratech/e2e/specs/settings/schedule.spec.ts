// Pack «Настройки и подключения», part 3: the schedule (TASK-377; AC-16). A synthetic week is built at run time
// (src/fixtures/schedule.ts: Матанализ, Литература, История; one lesson about two minutes after the file is written), written
// to <ctl>/fixtures/schedule/week.ics and picked in «Расписание» -> «Выбрать файл…». Rows:
//   1. the preview shows the three subjects, their times in the zone of the file and the teachers; «Применить» commits
//      (AC-16); the lesson of the next minutes is on «Сегодня»; the same file again changes nothing;
//   2. «Завести предметы из расписания»: the three subjects are created;
//   3. the synthetic call link is saved for «Матанализ» on «Сегодня» (the ICS importer ignores the link in the event text,
//      icsschedule/doc.go, so it enters the app here). It is not opened: only a later pack clicks «Подключиться».
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import type { Page } from "@playwright/test";

import { EnvCtlDir } from "../../src/contract.js";
import {
  buildWeekIcs,
  CallLinkUrl,
  isLocalToday,
  localTime,
  previewDayLabel,
  previewTime,
  ScheduleZone,
  type WeekLesson,
  weekLessons,
  WeekSubjects,
} from "../../src/fixtures/schedule.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, type PickFn, snapshot, step, test } from "../../src/steps/index.js";
import { act, details, note, openSection, textOf, withPick } from "./support.js";

const Import = "Расписание: импорт из файла с предпросмотром";
const Subjects = "Расписание: предметы из расписания";
const Link = "Расписание: ссылка на пару";

const WeekRel = "schedule/week.ics";

// The week is built once, when the first test writes the file; the next tests of the file read the same lessons.
let week: { now: Date; lessons: WeekLesson[] } | undefined;

const preview = (main: Page) => main.locator(".import-preview");
const tile = (main: Page, label: string) =>
  main.getByRole("list", { name: "Итог проверки файла" }).getByRole("listitem").filter({ hasText: label }).locator("b");

async function chooseWeek(main: Page, pick: PickFn): Promise<void> {
  await withPick(pick, "schedule", `fixtures/${WeekRel}`, () => main.getByRole("button", { name: /Выбрать файл/ }).click(act));
  await expect(preview(main), "после выбора файла должен появиться предпросмотр").toBeVisible({ timeout: 20_000 });
}

test(Import, details(Import), async ({ main, pick }) => {
  const ctl = requireEnv(EnvCtlDir);

  const lessons = await step("файл расписания строится на лету и кладётся в fixtures/schedule/week.ics", async () => {
    const now = new Date();
    const dir = path.join(ctl, "fixtures", "schedule");
    mkdirSync(dir, { recursive: true });
    writeFileSync(path.join(dir, "week.ics"), buildWeekIcs(now), "utf8");
    week = { now, lessons: weekLessons(now) };
    const soon = week.lessons.find((l) => l.soon);
    note(`пара «${WeekSubjects.analysis}» начинается в ${previewTime(week.lessons[0] as WeekLesson)} (${ScheduleZone}), локально ${soon ? localTime(soon.start) : "?"}`);
    return week.lessons;
  });
  const { now } = week as { now: Date };

  await step("«Расписание» → «Выбрать файл…»: предпросмотр", async () => {
    await openSection(main, "Расписание");
    await chooseWeek(main, pick);
    await expect(main.getByRole("heading", { name: /Файл: week\.ics/ })).toBeVisible(act);
    expect(await textOf(tile(main, "Ошибки")), "в файле не должно быть ошибок строк").toBe("0");
    expect(await textOf(tile(main, "Добавится")), "должны добавиться три пары").toBe("3");
    await snapshot("Расписание: предпросмотр");
  });

  await step("в предпросмотре три предмета, время в зоне файла, тип, преподаватель", async () => {
    for (const l of lessons) {
      const row = preview(main).locator("li.ln").filter({ hasText: l.title });
      await expect(row, `в предпросмотре должна быть пара «${l.title}»`).toHaveCount(1, act);
      await expect(row.locator(".ln-when"), `время пары «${l.title}» в зоне ${ScheduleZone}`).toHaveText(previewTime(l), act);
      const meta = await textOf(row.locator(".ln-meta"));
      expect(meta, `тип пары «${l.title}»`).toContain(l.kindLabel);
      expect(meta, `предмет пары «${l.title}»`).toContain(`предмет: ${l.subject}`);
      expect(meta, `преподаватель пары «${l.title}»`).toContain(`преподаватель: ${l.teacher}`);
      expect(await textOf(row.locator(".ln-zone")), `зона пары «${l.title}»`).toBe(ScheduleZone);
      const day = previewDayLabel(l, now);
      if (day !== "") {
        expect(await textOf(row.locator(".ln-day")), `день пары «${l.title}»`).toBe(day);
      }
    }
    await preview(main).locator(".g-added").scrollIntoViewIfNeeded(act);
    await snapshot("Расписание: добавляемые пары");
    const text = await textOf(preview(main));
    note(
      text.includes("telemost")
        ? "ссылка из текста события есть в предпросмотре"
        : "ссылка из текста события в предпросмотре не показывается (ICS: LOCATION и описание, кроме «Преподаватель:», не читаются)",
    );
  });

  await step("«Применить» записывает расписание", async () => {
    const apply = main.getByRole("button", { name: /^Применить/ });
    await expect(apply).toBeVisible(act);
    await apply.click(act);
    const done = main.getByRole("status").filter({ hasText: /Расписание обновлено/ });
    await expect(done, "после «Применить» приложение должно сообщить о результате").toBeVisible({ timeout: 20_000 });
    const text = await textOf(done);
    note(`ответ приложения: ${text}`);
    expect(text).toMatch(/добавлено 3,/);
    await expect(preview(main)).toBeHidden(act);
    // The screen of «Расписание» keeps no list of lessons after the commit (SchedulePage.tsx: the preview goes away); what it
    // shows about the imported schedule is the result line and the offer to make the subjects out of it.
    const next = main.locator(".sched-next");
    await expect(next, "после «Применить» на «Расписание» должно быть предложение завести предметы из расписания").toBeVisible({ timeout: 20_000 });
    const offer = await textOf(next);
    note(`предложение на «Расписание»: ${offer}`);
    expect(offer).toMatch(/Из расписания можно создать предметы: [1-9]\d*/);
    await expect(next.getByRole("button", { name: "Завести предметы из расписания", exact: true })).toBeVisible(act);
    await expect(main.getByText("Загрузите расписание из файла"), "после «Применить» пустого экрана быть не должно").toHaveCount(0);
    await snapshot("Расписание: применено");
  });

  const today: { gap?: string } = {}; // an object: TS would narrow a plain let, assigned in a callback, to undefined
  await step("«Сегодня»: ближайшая пара на экране", async () => {
    const soon = lessons.find((l) => l.soon) as WeekLesson;
    await openSection(main, "Сегодня");
    if (!isLocalToday(soon.start, new Date())) {
      // not asserted here; the row ends as «не проверено» after all other checks of the row (below)
      today.gap = `пара «${soon.title}» начинается после полуночи по времени компьютера: «Сегодня» её не покажет`;
      note(today.gap);
      return;
    }
    const row = main.locator(".today-lesson").filter({ hasText: soon.title });
    await expect(row, `на «Сегодня» должна быть пара «${soon.title}»`).toHaveCount(1, { timeout: 20_000 });
    await expect(row.locator(".today-tm"), "время пары на «Сегодня»").toContainText(localTime(soon.start), act);
    await snapshot("Сегодня: пара из расписания");
  });

  await step("тот же файл ещё раз: изменений нет", async () => {
    await openSection(main, "Расписание");
    await chooseWeek(main, pick);
    expect(await textOf(tile(main, "Добавится")), "повторный импорт ничего не добавляет").toBe("0");
    expect(await textOf(tile(main, "Без изменений")), "три пары уже в расписании").toBe("3");
    await expect(preview(main).getByText(/Изменений нет/)).toBeVisible(act);
    await snapshot("Расписание: повторный импорт без изменений");
    await preview(main).getByRole("button", { name: "Отмена", exact: true }).click(act);
    await expect(preview(main)).toBeHidden(act);
  });

  // Last, after every other assertion of the row: a gap in «Сегодня» makes the row «не проверено», never «готово».
  test.skip(today.gap !== undefined, `«Сегодня» не проверено: ${today.gap ?? ""}`);
});

test(Subjects, details(Subjects, { requires: Import }), async ({ main }) => {
  test.skip(week === undefined, "расписание не импортировано");
  const lessons = (week as { lessons: WeekLesson[] }).lessons;

  await step("после импорта предлагается «Завести предметы из расписания»", async () => {
    // the hint under the import result is gone after a visit; the same button is on «Предметы»
    await openSection(main, "Предметы");
    const open = main.getByRole("button", { name: /Завести предметы из расписания/ }).first();
    await expect(open, "на «Предметы» должна быть кнопка «Завести предметы из расписания»").toBeVisible({ timeout: 20_000 });
    await open.click(act);
    const review = main.getByRole("region", { name: "Предметы из расписания" });
    await expect(review).toBeVisible(act);
    const names: string[] = [];
    for (const input of await review.locator("ul.sched-rows input.input").all()) {
      names.push(await input.inputValue(act));
    }
    for (const l of lessons) {
      expect(names, `в списке предметов из расписания должен быть «${l.subject}»`).toContain(l.subject);
    }
    await snapshot("Предметы из расписания: список");
    await review.getByRole("button", { name: /^Создать/ }).click(act);
    await expect(main.locator(".sched-done"), "после «Создать» должна быть строка «Создано предметов»").toBeVisible({ timeout: 20_000 });
    note(`ответ приложения: ${await textOf(main.locator(".sched-done"))}`);
    await review.getByRole("button", { name: "Закрыть", exact: true }).click(act);
    await expect(review).toBeHidden(act);
  });

  await step("три предмета в дереве «Предметы»", async () => {
    for (const l of lessons) {
      await expect(main.locator("nav.tree .nm", { hasText: l.subject }).first(), `предмет «${l.subject}» должен быть в дереве`).toBeVisible({ timeout: 15_000 });
    }
    await snapshot("Предметы: из расписания");
  });
});

test(Link, details(Link, { requires: Subjects }), async ({ main, opened }) => {
  test.skip(week === undefined, "расписание не импортировано");
  const soon = (week as { lessons: WeekLesson[] }).lessons.find((l) => l.soon) as WeekLesson;
  test.skip(!isLocalToday(soon.start, new Date()), "пара начинается после полуночи: на «Сегодня» её нет");

  await step("«Сегодня»: у пары «Матанализ» «Добавить ссылку» и синтетическая ссылка", async () => {
    await openSection(main, "Сегодня");
    const row = main.locator(".today-lesson").filter({ hasText: soon.title });
    await expect(row).toHaveCount(1, { timeout: 20_000 });
    await row.getByRole("button", { name: /^Добавить ссылку/ }).click(act);
    const form = row.getByRole("form", { name: "Ссылка на пару" });
    await expect(form).toBeVisible(act);
    await form.getByRole("textbox").fill(CallLinkUrl, act);
    await snapshot("Ссылка на пару: поле");
    await form.getByRole("button", { name: /^Сохранить/ }).click(act);
    await expect(row.getByRole("status").filter({ hasText: /Ссылка на пару сохранена/ })).toBeVisible({ timeout: 15_000 });
  });

  await step("у пары «Подключиться» и хост ссылки, ссылка не открывалась", async () => {
    const row = main.locator(".today-lesson").filter({ hasText: soon.title });
    await expect(row.getByRole("button", { name: /^Подключиться/ })).toBeVisible(act);
    await expect(row.locator(".today-host")).toContainText(new URL(CallLinkUrl).host, act);
    expect(opened().filter((r) => r.value.includes(new URL(CallLinkUrl).host)), "ссылка не должна открываться: только запись в opened.jsonl после нажатия «Подключиться»").toEqual([]);
    await snapshot("Ссылка на пару: сохранена");
  });
});
