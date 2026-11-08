// The draft card of the assignment page («Быстрый черновик», «Новый черновик» / «Доработать черновик», «Версии»), driven like a
// student (TASK-381; frontend/src/pages/HomeworkPage.tsx). Not a spec. Every wait is on UI state with an explicit timeout.
import type { Locator, Page } from "@playwright/test";

import type { DraftAnswer } from "../../scripts/llm/assignments/rules.js";
import { expect } from "../../src/steps/index.js";
import { act, textOf } from "./support.js";

/** A generation goes through the job queue of the app and the Hub, and the page asks for it every 2-3 s. */
export const generationMs = 90_000;

export const titleField = (main: Page): Locator => main.getByLabel("Заголовок", { exact: true });
export const bodyField = (main: Page): Locator => main.getByLabel("Текст", { exact: true });
export const instructionField = (main: Page): Locator => main.getByLabel("Дополнительные указания", { exact: true });

/** «Версия 2 · исправлена вручную · 14:05 · последняя»: the line above the fields. */
export const versionLine = (main: Page): Locator => main.locator(".box-desc.num");

/** The rows of «Версии»: «Версия 3 · сгенерирована · последняя». */
export const versionRows = (main: Page): Locator => main.locator("[role='listitem']").filter({ hasText: /Версия \d+ · / });

/** «Новый черновик» → «Написать черновик»: waits until the draft is in the fields. */
export async function writeFirstDraft(main: Page, answer: DraftAnswer): Promise<void> {
  await expect(main.getByText("Черновика пока нет."), "у нового задания ещё нет черновика").toBeVisible(act);
  await main.getByRole("button", { name: /^Написать черновик/ }).click(act);
  await expectDraft(main, answer);
}

/** The fields hold the title and the text of the answer (the card shows the draft once the generation is done). */
export async function expectDraft(main: Page, answer: DraftAnswer): Promise<void> {
  await expect(titleField(main), "черновик должен появиться в поле «Заголовок»").toHaveValue(answer.title, { timeout: generationMs });
  await expect(bodyField(main), "черновик должен появиться в поле «Текст»").toHaveValue(answer.body, act);
}

/** The text of the version line, whitespace collapsed. */
export async function versionText(main: Page): Promise<string> {
  return textOf(versionLine(main));
}
