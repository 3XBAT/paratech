// Driving «Задания» and «Сегодня» like a student (TASK-381): the add and edit dialogs, the rows of the list, the «⋯» menu, the
// check «Выполнено», the toast, the assignment page. Labels are read from the real UI (frontend/src/pages/Homework*.tsx,
// AssignmentDoneParts.tsx, TodayParts.tsx). Not a spec. Every wait is on UI state with an explicit timeout.
import type { Locator, Page } from "@playwright/test";

import { collapse } from "../../src/fixtures/text.js";
import { expect } from "../../src/steps/index.js";
import { act, openSection, textOf } from "./support.js";

const loadMs = 20_000;

// ---- the list ----

export const listHeading = (main: Page): Locator => main.locator("#hw-list-title");
export const pageTitle = (main: Page): Locator => main.locator("#hw-detail-title");

/** «Задания» from the rail: the list (the rail always leaves an open assignment), loaded. */
export async function openList(main: Page): Promise<void> {
  await openSection(main, "Задания");
  await expect(listHeading(main), "после «Задания» на рейке должен быть список заданий").toBeVisible({ timeout: loadMs });
  await expect(main.locator(".page.hw").getByText("Загрузка…", { exact: true }), "список заданий должен загрузиться").toHaveCount(0, { timeout: loadMs });
}

/** The row of an assignment in the list by its title (the title is the `p.t` of the row). */
export const listRow = (main: Page, title: string): Locator =>
  main.locator(".page.hw .row").filter({ has: main.locator("p.t", { hasText: title }) });

/** Unfolds the groups that hide rows behind «Показать ещё N» until the row is there (or nothing more can be unfolded). */
export async function revealRow(main: Page, title: string): Promise<Locator> {
  const row = listRow(main, title);
  for (let i = 0; i < 6 && (await row.count()) === 0; i++) {
    const more = main.locator(".page.hw").getByRole("button", { name: /^Показать ещё/ });
    if ((await more.count()) === 0) {
      break;
    }
    await more.first().click(act);
  }
  return row;
}

/** The heading of the group a row stands in («Просрочено», «На этой неделе», «Позже»...), "" for the flat list of «Выполненные». */
export async function groupOf(main: Page, title: string): Promise<string> {
  const row = await revealRow(main, title);
  await expect(row, `в списке должно быть задание «${title}»`).toHaveCount(1, { timeout: loadMs });
  const section = main.locator("section.hw-sec").filter({ has: main.locator(".row p.t", { hasText: title }) });
  const heading = section.locator("h2.hw-group");
  if ((await heading.count()) === 0) {
    return "";
  }
  return collapse(await heading.first().innerText(act)).replace(/\s*\d+$/, "");
}

/** Waits until the row stands in the group and returns its text (meta included). */
export async function expectInGroup(main: Page, title: string, group: string): Promise<string> {
  await expect
    .poll(() => groupOf(main, title), { timeout: loadMs, message: `задание «${title}» должно стоять в группе «${group}»` })
    .toBe(group);
  return textOf(listRow(main, title));
}

const stateSwitch = (main: Page): Locator => main.getByRole("group", { name: "Какие задания показывать: по отметке" });

/** The switch is not drawn while the student has no assignment at all (the first-run screen). */
export async function hasStateSwitch(main: Page): Promise<boolean> {
  return (await stateSwitch(main).count()) > 0;
}

/**
 * «Открытые» / «Выполненные» / «Все» above the list. An empty «Все» is the first-run screen, which has no switch: then
 * the result is false (nothing exists), otherwise true with the state selected and the list loaded.
 */
export async function showState(main: Page, state: "Открытые" | "Выполненные" | "Все"): Promise<boolean> {
  const button = stateSwitch(main).getByRole("button", { name: state, exact: true });
  await button.click(act);
  await expect
    .poll(
      async () => !(await hasStateSwitch(main)) || (await stateSwitch(main).locator("button[aria-pressed='true']").allInnerTexts()).some((t) => t.trim() === state),
      { timeout: loadMs, message: `«${state}» должно стать выбранным` },
    )
    .toBe(true);
  await expect(main.locator(".page.hw").getByText("Загрузка…", { exact: true })).toHaveCount(0, { timeout: loadMs });
  return hasStateSwitch(main);
}

// ---- the add and the edit dialogs ----

export interface NewAssignment {
  title: string;
  /** The title of a subject of the library; none: «Без предмета». */
  subject?: string;
  deadline: { date: string; time?: string };
  note?: string;
}

/**
 * The time field starts with «20:00» (the default). `time` undefined leaves it, "" clears it (a date alone: the app puts
 * 20:00), another value is typed.
 */
export async function addAssignment(main: Page, a: NewAssignment): Promise<void> {
  await openList(main);
  await main.locator(".page.hw .head").getByRole("button", { name: "Добавить задание", exact: true }).click(act);
  const dialog = main.getByRole("dialog", { name: "Добавить задание" });
  await expect(dialog, "после «Добавить задание» должна открыться форма").toBeVisible(act);
  await dialog.getByLabel("Что сделать", { exact: true }).fill(a.title, act);
  if (a.subject !== undefined) {
    await dialog.getByLabel("Предмет", { exact: true }).selectOption({ label: a.subject }, act);
  }
  await dialog.getByLabel("Срок", { exact: true }).fill(a.deadline.date, act);
  if (a.deadline.time !== undefined) {
    await dialog.getByLabel("Время", { exact: true }).fill(a.deadline.time, act);
  }
  if (a.note !== undefined) {
    await dialog.getByLabel("Заметка", { exact: true }).fill(a.note, act);
  }
  await dialog.getByRole("button", { name: "Добавить", exact: true }).click(act);
  await expect(dialog, "форма должна закрыться, когда задание добавлено").toBeHidden({ timeout: loadMs });
  await expect(pageTitle(main), "после добавления открывается страница задания").toHaveText(a.title, { timeout: loadMs });
}

/** On the assignment page: «⋯» → «Изменить» → a new date (and time when given) → «Сохранить». */
export async function editDeadline(main: Page, title: string, deadline: { date: string; time?: string }): Promise<void> {
  await pickHeadMenu(main, title, "Изменить");
  const dialog = main.getByRole("dialog", { name: "Изменить задание" });
  await expect(dialog, "после «Изменить» должна открыться форма").toBeVisible(act);
  await dialog.getByLabel("Срок", { exact: true }).fill(deadline.date, act);
  if (deadline.time !== undefined) {
    await dialog.getByLabel("Время", { exact: true }).fill(deadline.time, act);
  }
  await dialog.getByRole("button", { name: "Сохранить", exact: true }).click(act);
  await expect(dialog, "форма должна закрыться, когда правка сохранена").toBeHidden({ timeout: loadMs });
}

/** The «⋯» button of a row or of the page head: its accessible name is «Ещё действия: <title>». */
export const moreButton = (scope: Locator | Page, title: string): Locator => scope.getByRole("button", { name: `Ещё действия: ${title}`, exact: true });

/** «⋯» of the page head → an item (the menu opens under the button). */
export async function pickHeadMenu(main: Page, title: string, item: string): Promise<void> {
  await moreButton(main.locator(".page.hw .head"), title).click(act);
  await main.getByRole("menuitem", { name: item, exact: true }).click(act);
}

// ---- the assignment page ----

/** The deadline line of the page head («до вт, 10 нояб., 20:00», «просрочено, …»). */
export async function pageDue(main: Page): Promise<string> {
  return textOf(main.locator(".page.hw .head > span.num").first());
}

/** From the list: «Открыть» on the row, then the page of the assignment is there. */
export async function openFromList(main: Page, title: string): Promise<void> {
  const row = await revealRow(main, title);
  await row.getByRole("button", { name: "Открыть", exact: true }).click(act);
  await expect(pageTitle(main), "страница задания должна открыться").toHaveText(title, { timeout: loadMs });
}

/** The list, then the page of the assignment (a way to leave an assignment page and come back to it). */
export async function reopen(main: Page, title: string): Promise<void> {
  await openList(main);
  await openFromList(main, title);
}

// ---- «Выполнено», the toast ----

export const doneCheck = (main: Page, title: string, done = false): Locator =>
  main.getByRole("button", { name: `${done ? "Вернуть в работу" : "Отметить выполненным"}: ${title}`, exact: true });

export const toast = (main: Page): Locator => main.locator(".toast-host .toast");

// ---- «Сегодня» → «Сроки на неделе» ----

/** The rows of the block «Сроки на неделе» of «Сегодня» whose text holds `title`; the block itself is waited for. */
export async function burningRows(main: Page, title: string): Promise<Locator> {
  await openSection(main, "Сегодня");
  const section = main.locator("section.today-sec").filter({ has: main.getByRole("heading", { name: /^Сроки на неделе/ }) });
  await expect(section, "на «Сегодня» должен быть блок «Сроки на неделе»").toBeVisible({ timeout: loadMs });
  return section.locator("[role='listitem']").filter({ hasText: title });
}

// ---- teachers of a subject ----

/** A fresh visit of the page of a subject («Сегодня» first, so the page is mounted anew): the first block «Преподаватели». */
async function openTeachers(main: Page, subject: string): Promise<Locator> {
  await openSection(main, "Сегодня");
  await openSection(main, "Предметы");
  const item = main.locator(`nav.tree button.ti[data-subject-id][title="${subject}"]`);
  await item.click(act);
  await expect(item, `предмет «${subject}» должен открыться в дереве`).toHaveAttribute("aria-current", "page", { timeout: loadMs });
  const block = main.locator("section.teachers").first();
  await expect(block.getByRole("listitem").first(), "блок «Преподаватели» должен загрузиться").toBeVisible({ timeout: loadMs });
  return block;
}

/**
 * «Предметы» → the subject → «Преподаватели»: «Изменить: …» → «Убрать» for every teacher that is set. Returns how many were
 * removed. After each removal the page is visited anew: the copies of the block that the finding of 06-send leaves on the
 * page are not trusted to show the state.
 */
export async function removeTeachers(main: Page, subject: string): Promise<number> {
  let removed = 0;
  for (let i = 0; i <= 3; i++) {
    const block = await openTeachers(main, subject);
    const change = block.getByRole("button", { name: /^Изменить: / });
    if ((await change.count()) === 0) {
      return removed;
    }
    if (i === 3) {
      break;
    }
    await change.first().click(act);
    const form = main.locator("form.teacher-form").first();
    await expect(form, "после «Изменить» открыта форма преподавателя").toBeVisible({ timeout: loadMs });
    await form.getByRole("button", { name: "Убрать", exact: true }).click(act);
    // the note is printed by the copies of the block; any one of them proves the removal
    await expect(main.getByText("Преподаватель убран.").first(), "после «Убрать» страница сообщает об этом").toBeVisible({ timeout: loadMs });
    removed++;
  }
  throw new Error(`у предмета «${subject}» остались преподаватели после ${removed} удалений`);
}

// ---- cleanup ----

/** Deletes an assignment from the list with its «⋯» → «Удалить» (best effort: a cleanup must not hide the real failure). */
export async function removeQuietly(main: Page, titles: readonly string[]): Promise<string[]> {
  const left: string[] = [];
  for (const title of titles) {
    try {
      // a failed step can leave a dialog open, and a modal dialog blocks the rail
      if ((await main.locator("dialog[open]").count()) > 0) {
        await main.keyboard.press("Escape");
        await expect(main.locator("dialog[open]")).toHaveCount(0, { timeout: 5_000 });
      }
      await openList(main);
      if (!(await hasStateSwitch(main))) {
        continue; // nothing is left at all
      }
      await showState(main, "Все");
      const row = await revealRow(main, title);
      if ((await row.count()) === 0) {
        continue;
      }
      await moreButton(row, title).click(act);
      await main.getByRole("menuitem", { name: "Удалить", exact: true }).click(act);
      await expect(main.locator(".hw-removed").first()).toBeVisible({ timeout: 10_000 });
    } catch {
      left.push(title);
    }
  }
  return left;
}

