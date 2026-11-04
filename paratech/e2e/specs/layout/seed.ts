// Synthetic data of the layout pack, entered through the UI (TASK-376): a schedule from a generated ICS, the subjects it
// proposes, two subjects with long names and two assignments with long titles. Every stage is best effort: a screen
// without data is still checked in its empty state. Nothing here asserts a feature; it only needs the data to exist.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

import type { Page } from "@playwright/test";

import { buildLayoutIcs } from "../../src/checks/ics.js";
import { layoutViolations, type LayoutViolation } from "../../src/checks/layout.js";
import { consumedPicks, expect, type PickFn, snapshot, step } from "../../src/steps/index.js";
import { firstLine, note, openSection } from "./support.js";

export const ownSubjects = [
  "Сопротивление материалов и основы строительной механики для заочного отделения",
  "Организация и планирование научно-исследовательской работы студента магистратуры",
];

export const assignmentTitles = [
  "Проверка раскладки: подготовить развёрнутый конспект по разделам первой главы и оформить список литературы",
  "Проверка раскладки: решить расчётно-графическую работу и приложить пояснительную записку к расчётам",
];

/** Violations found while seeding (the schedule preview, the subjects review), handed to the test of that screen. */
const carryFile = (ctl: string, name: string) => path.join(ctl, `layout-carry-${name}.json`);

export function carry(ctl: string, name: string, violations: LayoutViolation[]): void {
  writeFileSync(carryFile(ctl, name), JSON.stringify(violations), "utf8");
}

/** The violations carried from the seeding, or undefined when that state was not reached (nothing was checked). */
export function takeCarried(ctl: string, name: string): LayoutViolation[] | undefined {
  const file = carryFile(ctl, name);
  if (!existsSync(file)) {
    return undefined;
  }
  try {
    return JSON.parse(readFileSync(file, "utf8")) as LayoutViolation[];
  } catch {
    return undefined;
  }
}

/** A seeding stage: it never throws; a failure is a note and a screenshot, and the pack goes on with what exists. */
export async function stage(what: string, fn: () => Promise<void>, main: Page): Promise<boolean> {
  return step(`посев: ${what}`, async () => {
    try {
      await fn();
      return true;
    } catch (err) {
      note(`посев не удался (${what}): ${firstLine(err)}`);
      await snapshot(`посев не удался: ${what}`, main).catch(() => undefined);
      return false;
    }
  });
}

const forPick = (ctl: string) => (picked: number) => consumedPicks(ctl).slice(picked);

/** Writes the ICS, picks it in «Расписание», checks the layout of the preview, applies it. */
export async function seedSchedule(main: Page, pick: PickFn, ctl: string): Promise<void> {
  const dir = path.join(ctl, "fixtures");
  mkdirSync(dir, { recursive: true });
  writeFileSync(path.join(dir, "layout-schedule.ics"), buildLayoutIcs(new Date()), "utf8");

  const before = consumedPicks(ctl).length;
  await pick("schedule", ["fixtures/layout-schedule.ics"]);
  await openSection(main, "Расписание");
  await main.getByRole("button", { name: "Выбрать файл…" }).click();
  await expect.poll(() => consumedPicks(ctl).length, { timeout: 15_000, message: "the app should take the schedule pick" }).toBeGreaterThan(before);
  const refused = forPick(ctl)(before).filter((p) => p.result !== "picked");
  if (refused.length > 0) {
    throw new Error(`диалог расписания отменён: ${refused.map((p) => p.reason ?? "?").join("; ")}`);
  }

  const apply = main.getByRole("button", { name: /^Применить/ });
  await expect(apply).toBeVisible({ timeout: 15_000 });
  // The preview is a screen of its own (long titles, week and day headers); its findings go to the test of «Расписание».
  carry(ctl, "schedule-preview", await layoutViolations(main));
  await apply.click();
  await expect(apply).toBeHidden({ timeout: 15_000 });
}

/** «Завести предметы из расписания» -> the review -> «Создать». The layout of the review goes to the test of «Предметы». */
export async function seedScheduleSubjects(main: Page, ctl: string): Promise<void> {
  const open = main.getByRole("button", { name: /Завести предметы из расписания/ });
  try {
    await expect(open.first()).toBeVisible({ timeout: 20_000 });
  } catch {
    // the hint after the import did not come: the same button is on «Предметы»
    await openSection(main, "Предметы");
    await expect(open.first()).toBeVisible({ timeout: 15_000 });
  }
  await open.first().click();
  const review = main.getByRole("region", { name: "Предметы из расписания" });
  await expect(review).toBeVisible({ timeout: 15_000 });
  carry(ctl, "schedule-review", await layoutViolations(main));
  await review.getByRole("button", { name: /^Создать/ }).click();
  await expect(main.locator(".sched-done")).toBeVisible({ timeout: 20_000 });
  await review.getByRole("button", { name: "Закрыть", exact: true }).click();
  await expect(review).toBeHidden({ timeout: 10_000 });
}

/** «Добавить предмет» in the tree of «Предметы», once per name; the new subject opens. */
export async function seedOwnSubjects(main: Page): Promise<void> {
  await openSection(main, "Предметы");
  for (const name of ownSubjects) {
    await main.getByRole("button", { name: "Добавить предмет" }).first().click();
    await main.getByRole("textbox", { name: "Название предмета", exact: true }).fill(name);
    await main.getByRole("button", { name: "Создать", exact: true }).click();
    await expect(main.locator("nav.tree .nm", { hasText: name }).first()).toBeVisible({ timeout: 15_000 });
  }
}

const isoDay = (days: number): string => new Date(Date.now() + days * 86_400_000).toISOString().slice(0, 10);

/** Two assignments with long titles on the page of the subject that is open (the last one created). */
export async function seedAssignments(main: Page): Promise<void> {
  await main.getByRole("tab", { name: /^Задания/ }).click();
  let n = 0;
  for (const text of assignmentTitles) {
    await main.getByRole("button", { name: "Добавить задание" }).click();
    const dialog = main.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("Что сделать").first().fill(text);
    // the due date is optional for the layout; a form that refuses the date still gets the assignment without it
    await dialog.getByLabel("Срок").first().fill(isoDay(3 + n)).catch(() => undefined);
    await dialog.getByRole("button", { name: "Добавить", exact: true }).click();
    await expect(dialog).toBeHidden({ timeout: 15_000 });
    n++;
  }
  await expect(main.getByText(/^Проверка раскладки/).first()).toBeVisible({ timeout: 15_000 });
}
