// Driving «Записи» → «Импортировать запись» like a student (TASK-162): the form «Дата и время» (the lecture), then the native
// file dialog, which the E2E hooks answer from picks.jsonl. Not a spec.
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

import type { Page } from "@playwright/test";

import { EnvCtlDir } from "../../src/contract.js";
import { audioVariant } from "../../src/fixtures/audio.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, type PickFn, snapshot, step } from "../../src/steps/index.js";
import { act, type Lecture, openSection, showRow, Subject, textOf, withPick } from "./support.js";

export const VariantDir = "audio/variants";

/**
 * Writes the clip behind a tag with a label (and `times` times in a row) to <ctl>/fixtures/audio/variants/<name>.mp3 and
 * returns the pick path. The app answers a file it has seen before with the existing recording (recordingimport, step 2), so
 * every import after the first needs bytes of its own; nothing of this is committed.
 */
export function writeVariant(clip: Buffer, name: string, label: string, times = 1): string {
  const dir = path.join(requireEnv(EnvCtlDir), "fixtures", ...VariantDir.split("/"));
  mkdirSync(dir, { recursive: true });
  writeFileSync(path.join(dir, `${name}.mp3`), audioVariant(clip, label, times));
  return `fixtures/${VariantDir}/${name}.mp3`;
}

/** «Открыть» on the row of the lecture: the lesson screen, switched to the tab of the name. */
export async function openLesson(main: Page, lecture: Lecture, tab: string): Promise<void> {
  await openSection(main, "Записи");
  const row = await showRow(main, lecture.title);
  await expect(row, `на «Записи» должна быть строка «${lecture.title}»`).toHaveCount(1, { timeout: 20_000 });
  await row.getByRole("button", { name: /^Открыть: / }).click(act);
  const tabButton = main.getByRole("tab", { name: tab, exact: true });
  await expect(tabButton, `на экране пары должна быть вкладка «${tab}»`).toBeVisible({ timeout: 20_000 });
  await tabButton.click(act);
  await expect(tabButton).toHaveAttribute("aria-selected", "true", act);
}

export interface ImportResult {
  /** The line the panel shows: «Запись «…» добавлена, длительность …». */
  text: string;
}

/**
 * Opens «Записи» and the import panel, fills the lecture, picks `pickRel` and waits for the answer. `fileName` is the base name
 * of the picked file: the answer names it, which tells this answer from the one of an earlier import.
 */
export async function importRecording(main: Page, pick: PickFn, lecture: Lecture, pickRel: string, fileName: string): Promise<ImportResult> {
  return step(`«Записи» → «Импортировать запись»: ${lecture.title}`, async () => {
    await openSection(main, "Записи");
    const panel = main.locator(".rec-import-panel");
    if ((await panel.count()) === 0) {
      await main.getByRole("button", { name: "Импортировать запись", exact: true }).first().click(act);
    }
    await expect(panel, "после «Импортировать запись» должна открыться форма").toBeVisible(act);

    // «Пара из расписания» offers only today's lessons that have begun; the lecture is given by date and time.
    await panel.getByRole("group", { name: "Лекция", exact: true }).getByRole("button", { name: "Дата и время", exact: true }).click(act);
    await panel.getByLabel("Дата", { exact: true }).fill(lecture.date, act);
    await panel.getByLabel("Время начала", { exact: true }).fill(lecture.time, act);
    await panel.getByLabel("Предмет", { exact: true }).fill(Subject, act);
    await panel.getByLabel("Тип пары", { exact: true }).selectOption({ label: "Лекция" }, act);
    await panel.getByLabel("Название (необязательно)", { exact: true }).fill(lecture.title, act);
    await snapshot("Импорт записи: форма");

    await withPick(pick, "recording", pickRel, () => panel.getByRole("button", { name: "Выбрать файл и импортировать", exact: true }).click(act));
    const done = panel.getByRole("status").filter({ hasText: fileName });
    await expect(done, `после выбора файла ${fileName} панель должна сообщить, что запись добавлена`).toBeVisible({ timeout: 60_000 });
    const text = await textOf(done);
    expect(text, "ответ импорта").toMatch(/добавлена/);
    expect(text, "импорт не должен принять файл за уже добавленный").not.toMatch(/уже добавлена/);
    return { text };
  });
}
