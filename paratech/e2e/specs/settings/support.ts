// Helpers of the settings pack (TASK-377): report annotations, navigation like a user, small readers of the screens.
// Not a spec. Every click, fill, read and expect has an explicit timeout (act): the Playwright config has none for
// actions, so a renamed locator would hang until the test timeout instead of failing in 10 s with its name.
// No waits on fixed time: everything waits on UI state. No string page.evaluate: the app CSP blocks it (evalInPage).
import type { Locator, Page } from "@playwright/test";

import { AnnotationFeature, AnnotationMode, AnnotationRequires, type Dialog, EnvCtlDir } from "../../src/contract.js";
import { pendingPicks, usedPicks } from "../../src/fixtures/picks.js";
import { collapse, readableRussian } from "../../src/fixtures/text.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, type PickFn, test } from "../../src/steps/index.js";
import { consumedPicks } from "../../src/steps/picks.js";

export { amountOf, collapse, readableRussian } from "../../src/fixtures/text.js";

export const actionMs = 10_000;
export const act = { timeout: actionMs } as const;

export const Tag = "@settings";

/** Test options: the tag and the annotations the report reads (one row per feature). */
export function details(feature: string, opts: { requires?: string } = {}) {
  return {
    tag: Tag,
    annotation: [
      { type: AnnotationFeature, description: feature },
      { type: AnnotationMode, description: "SCRIPTED" },
      ...(opts.requires ? [{ type: AnnotationRequires, description: opts.requires }] : []),
    ],
  };
}

export const firstLine = (err: unknown): string => (err instanceof Error ? err.message : String(err)).split("\n")[0] ?? "";

/** A fact worth keeping in the report: an annotation of the test and a line in the run output. Never put a secret here. */
export function note(text: string): void {
  test.info().annotations.push({ type: "note", description: text });
  console.log(`[settings] ${test.info().title}: ${text}`);
}

const railOf = (main: Page) => main.getByRole("navigation", { name: "Разделы", exact: true });

/** A click on a rail button (icons only: the name is the aria-label). */
export async function openSection(main: Page, name: string): Promise<void> {
  const button = railOf(main).getByRole("button", { name, exact: true });
  await button.click(act);
  await expect(button).toHaveAttribute("aria-current", "page", act);
  await main.mouse.move(1, 1); // park the pointer, so no tooltip stays open under it
}

/** «Настройки» -> an item of its left menu («Подключения», «Модели речи»...). */
export async function openSettingsItem(main: Page, item: string): Promise<void> {
  await openSection(main, "Настройки");
  const button = main.getByRole("navigation", { name: "Разделы настроек" }).getByRole("button", { name: item, exact: true });
  await button.click(act);
  await expect(button).toHaveAttribute("aria-current", "page", act);
  await main.mouse.move(1, 1);
}

/** The visible text of an element, whitespace collapsed (thin and no-break spaces become plain ones). */
export async function textOf(locator: Locator): Promise<string> {
  return collapse((await locator.innerText(act)) ?? "");
}

/**
 * Queues the answer of the next native dialog, runs the click that opens it and checks the app used exactly that line and
 * did not cancel it. The queue must be empty before: a pick left by a dialog that never opened would be taken by this one.
 */
export async function withPick(pick: PickFn, dialog: Dialog, rel: string, openDialog: () => Promise<void>): Promise<void> {
  const ctl = requireEnv(EnvCtlDir);
  expect(pendingPicks(ctl), "в picks.jsonl остался неиспользованный выбор файла от предыдущего шага").toBe(0);
  const before = usedPicks(ctl);
  await pick(dialog, [rel]);
  await openDialog();
  await expect
    .poll(() => usedPicks(ctl), { timeout: 20_000, message: `приложение должно было открыть диалог «${dialog}» и взять ${rel}` })
    .toBe(before + 1);
  const last = consumedPicks(ctl).at(-1);
  expect(last?.result, `диалог «${dialog}» отменён приложением: ${last?.reason ?? "причина не указана"}`).toBe("picked");
}

export const assertReadable = (what: string, text: string): void => {
  expect(readableRussian(text), `${what} должен быть читаемым русским текстом: «${text.slice(0, 160)}»`).toEqual([]);
};
