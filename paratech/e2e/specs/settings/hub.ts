// Driving «Настройки → Подключения → Конспекты и задания» (the Hub block): the key slot, the folded «Адрес сервера», the
// quota. The key is typed into the password field and goes nowhere else: not into a title, a note, an annotation or a log.
import type { Locator, Page } from "@playwright/test";

import { amountOf } from "../../src/fixtures/text.js";
import { expect } from "../../src/steps/index.js";
import { act, openSettingsItem, textOf } from "./support.js";

export const hubCardName = /Конспекты и задания/;

/** The Hub card of the overview. */
export const hubCard = (main: Page): Locator => main.getByRole("button", { name: hubCardName });

/** «Настройки → Подключения → Конспекты и задания»: the section of the Hub is open. */
export async function openHubSection(main: Page): Promise<void> {
  await openSettingsItem(main, "Подключения");
  // a rail visit brings the overview back; if a section is still open (the same visit), its crumb leads back
  const crumb = main.getByRole("navigation", { name: "Путь" }).getByRole("button", { name: "Подключения", exact: true });
  if ((await crumb.count()) > 0) {
    await crumb.click(act);
  }
  await hubCard(main).click(act);
  await expect(main.getByRole("heading", { name: "Лимит обработки" })).toBeVisible(act);
}

/** Back to the overview of «Подключения» (the card shows the state of the Hub). */
export async function backToOverview(main: Page): Promise<void> {
  await main.getByRole("navigation", { name: "Путь" }).getByRole("button", { name: "Подключения", exact: true }).click(act);
  await expect(hubCard(main)).toBeVisible(act);
}

const keyField = (main: Page) => main.getByLabel("Новый ключ", { exact: true });

/** The chip of the key slot: «Ключ задан» / «Ключ не задан». */
export const keyChip = (main: Page, text: "Ключ задан" | "Ключ не задан"): Locator => main.locator("[data-status]").filter({ hasText: text });

/**
 * Types the key and presses «Сохранить ключ». Returns when the app has finished the call (the field is cleared by the app
 * after the answer, and with it the old quota message): the refresh that follows is awaited by quotaResult().
 */
export async function enterKey(main: Page, key: string): Promise<void> {
  const field = keyField(main);
  await field.fill(key, act);
  await main.getByRole("button", { name: "Сохранить ключ", exact: true }).click(act);
  await expect(field).toHaveValue("", act);
  await expect(field).toBeEnabled(act);
}

/** Opens the folded «Адрес сервера» if it is folded and returns its text field. */
export async function addressField(main: Page): Promise<Locator> {
  const block = main.locator("details.conn-adv");
  if ((await block.getAttribute("open", act)) === null) {
    await block.locator("summary").click(act);
    await expect(block).toHaveAttribute("open", "", act);
  }
  return main.getByRole("textbox", { name: "Адрес сервера", exact: true });
}

/** Types the address and presses «Сохранить адрес»; returns when «Сохранено.» is shown. */
export async function enterAddress(main: Page, url: string): Promise<void> {
  const field = await addressField(main);
  await field.fill(url, act);
  await main.getByRole("button", { name: "Сохранить адрес", exact: true }).click(act);
  await expect(main.locator("details.conn-adv").getByRole("status").filter({ hasText: "Сохранено." })).toBeVisible(act);
}

/** The result line under «Лимит обработки» (the answer of the last «Обновить»). Its class is s-ok / s-fail / s-wait. */
export const quotaResult = (main: Page): Locator =>
  main.locator('xpath=//h2[normalize-space()="Лимит обработки"]/following-sibling::p[contains(@class,"result")]');

/** The value of a row of the amounts («Осталось», «Лимит», «Зарезервировано») as a number; NaN when there is no such row. */
export async function amount(main: Page, label: "Осталось" | "Лимит" | "Зарезервировано"): Promise<number> {
  const row = main.locator("dl.amounts > div").filter({ has: main.locator("dt", { hasText: label }) });
  if ((await row.count()) === 0) {
    return Number.NaN;
  }
  return amountOf(await textOf(row.locator("dd")));
}

/** «Обновить» next to the quota. */
export const refreshButton = (main: Page): Locator => main.getByRole("button", { name: "Обновить", exact: true });

/** Presses «Обновить» and waits until the app is done with it (the button is busy while it asks the Hub). */
export async function refreshQuota(main: Page): Promise<void> {
  const button = refreshButton(main);
  await expect(button).toBeEnabled(act);
  await button.click(act);
  await expect(button).toBeEnabled({ timeout: 30_000 });
}
