// Pack «Настройки и подключения», part 2: the speech model (TASK-377; AC-07, AC-23). ggml-base-q5_1.bin is fetched once
// into paratech/dist/cache/e2e (src/fixtures/model.ts: size and SHA-256 from catalog.json), placed under <ctl>/fixtures
// (the app takes dialog picks only from there) and imported through «Настройки → Модели речи».
//
// What the app really offers (frontend ModelsPage, input/wails/models.go):
//   - the catalog row «Base Q5» -> ⋯ -> «Импортировать скачанный файл…»: the file is compared with the CATALOG (size, then
//     SHA-256); a copy that differs is refused with the fixed text «Контрольная сумма SHA-256 не совпадает…» (AC-23);
//   - «Импортировать файл модели…» (a model outside the catalog): the app shows the file's SHA-256 and the student compares
//     it with the page of the model before «SHA-256 совпадает, установить». Used here only to read what it shows.
// Order: the corrupted copy first (nothing is installed yet, so a wrongly accepted copy would show up), then the good file.
import path from "node:path";

import type { Page } from "@playwright/test";

import { EnvCtlDir, EnvDataDir } from "../../src/contract.js";
import { findFiles } from "../../src/fixtures/files.js";
import { type CatalogModel, ensureModel, makeCorruptedCopy, ModelFetchError, placeInFixtures, sha256OfFile } from "../../src/fixtures/model.js";
import { envError, requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { act, assertReadable, details, note, openSettingsItem, textOf, withPick } from "./support.js";

const Speech = "Модуль расшифровки готов";
const Corrupted = "Модель: испорченная копия отклоняется";
const Imported = "Модель: импорт с проверкой SHA-256";
const Default = "Модель: по умолчанию";

const GoodRel = "models/ggml-base-q5_1.bin";
const CorruptRel = "models-corrupt/ggml-base-q5_1.bin";

interface Prepared {
  model: CatalogModel;
  goodPick: string;
  corruptPick: string;
  corruptSha: string;
  flippedAt: number;
}
let prepared: Prepared | undefined;

const need = (): Prepared => {
  if (!prepared) {
    throw envError("модель не подготовлена (beforeAll)");
  }
  return prepared;
};

test.beforeAll(async () => {
  test.setTimeout(10 * 60_000); // the first run downloads 57 MB
  const ctl = requireEnv(EnvCtlDir);
  let fetched;
  try {
    fetched = await ensureModel();
  } catch (err) {
    // no network / a download that does not match the catalog: the machine is not ready, the app was not even asked
    throw envError(err instanceof ModelFetchError ? err.message : `загрузка модели: ${err instanceof Error ? err.message : String(err)}`);
  }
  const fixtures = path.join(ctl, "fixtures");
  const goodPick = placeInFixtures(fetched.file, fixtures, GoodRel);
  const bad = makeCorruptedCopy(fetched.file, fixtures, CorruptRel);
  const corruptSha = await sha256OfFile(path.join(fixtures, ...CorruptRel.split("/")));
  expect(corruptSha, "испорченная копия должна отличаться от модели по SHA-256").not.toBe(fetched.model.sha256);
  prepared = { model: fetched.model, goodPick, corruptPick: bad.pick, corruptSha, flippedAt: bad.offset };
});

// --- locators of «Модели речи» ---

const catalogRow = (main: Page) => main.locator('.mrow[role="listitem"]').filter({ has: main.locator('p.mname[title="base-q5_1"]') });
// an installed row's tooltip is «base-q5_1\nSHA-256: …»; the catalog row's is the bare id
const installedBase = (main: Page) =>
  main
    .locator('.mrow[role="listitem"]')
    .filter({ has: main.locator('p.mname[title^="base-q5_1"]') })
    .filter({ hasNot: main.locator('p.mname[title="base-q5_1"]') });
const moreButton = /^Ещё действия: Base Q5$/;
const importFileButton = (main: Page) => main.getByRole("button", { name: /Импортировать файл модели/ });
const importCard = (main: Page) => main.getByRole("group", { name: "Импорт файла модели" });
const refusal = (main: Page) => main.getByRole("alert").filter({ hasText: /Контрольная сумма|не совпадает|повреждён/ });

async function openModels(main: Page): Promise<void> {
  await openSettingsItem(main, "Модели речи");
  await expect(main.getByRole("heading", { name: "Модели речи", level: 2 }).first()).toBeVisible(act);
  // the catalog is loaded when the Base Q5 row is there
  await expect(catalogRow(main), "в каталоге моделей должна быть строка «Base Q5»").toHaveCount(1, { timeout: 20_000 });
}

async function openImportFromRow(main: Page): Promise<void> {
  await catalogRow(main).getByRole("button", { name: moreButton }).click(act);
  await main.getByRole("menuitem", { name: "Импортировать скачанный файл…", exact: true }).click(act);
}

test(Corrupted, details(Corrupted), async ({ main, pick }) => {
  const p = need();
  const data = requireEnv(EnvDataDir);
  let binsBefore: string[] = [];

  await step("Настройки → Модели речи: в каталоге «Base Q5» не установлена", async () => {
    await openModels(main);
    await expect(catalogRow(main).getByText("Не установлена")).toBeVisible(act);
    await expect(installedBase(main), "до импорта Base Q5 в списке установленных быть не должно").toHaveCount(0);
    binsBefore = findFiles(data, /\.bin$/i);
    await snapshot("Модели: до импорта");
  });

  await step(`копия с одним изменённым байтом (смещение ${p.flippedAt}) отклоняется при импорте из каталога`, async () => {
    await withPick(pick, "model", p.corruptPick, () => openImportFromRow(main));
    const banner = refusal(main).first();
    await expect(banner, "приложение должно показать отказ, а не установить испорченную модель").toBeVisible({ timeout: 60_000 });
    const text = await textOf(banner);
    note(`текст отказа: ${text}`);
    assertReadable("текст отказа", text);
    expect(text, "отказ должен назвать причину: контрольная сумма не совпадает").toMatch(/SHA-256.*не совпадает|не совпадает.*SHA-256/);
    expect(text, "отказ должен сказать, что модель не установлена").toMatch(/не установлена/i);
    await snapshot("Модели: испорченная копия отклонена");
  });

  await step("список моделей не пополнился, копия в папке приложения не осталась", async () => {
    await expect(installedBase(main)).toHaveCount(0);
    await expect(catalogRow(main).getByText("Не установлена")).toBeVisible(act);
    const added = findFiles(data, /\.bin$/i).filter((f) => !binsBefore.includes(f));
    expect(added, `после отказа в папке данных не должно появиться файлов модели: ${added.join(", ")}`).toEqual([]);
  });

  await step("«Импортировать файл модели…» показывает SHA-256 испорченного файла, он не равен каталожному; «Отмена»", async () => {
    await withPick(pick, "model", p.corruptPick, () => importFileButton(main).click(act));
    const card = importCard(main);
    await expect(card).toBeVisible({ timeout: 60_000 });
    const shown = (await textOf(card.locator("code.sha"))).toLowerCase();
    expect(shown, "приложение должно показать настоящий SHA-256 выбранного файла").toBe(p.corruptSha);
    expect(shown, "у испорченной копии SHA-256 не может равняться каталожному").not.toBe(p.model.sha256);
    await snapshot("Модели: SHA-256 испорченной копии");
    await card.getByRole("button", { name: "Отмена", exact: true }).click(act);
    await expect(card).toBeHidden(act);
    await expect(installedBase(main)).toHaveCount(0);
  });
});

test(Imported, details(Imported, { requires: Corrupted }), async ({ main, pick }) => {
  const p = need();

  await step("«Импортировать файл модели…» с настоящим файлом: SHA-256 на экране равен каталожному; «Отмена»", async () => {
    await openModels(main);
    await withPick(pick, "model", p.goodPick, () => importFileButton(main).click(act));
    const card = importCard(main);
    await expect(card).toBeVisible({ timeout: 60_000 });
    const shown = (await textOf(card.locator("code.sha"))).toLowerCase();
    expect(shown, "SHA-256 на экране должен равняться SHA-256 из каталога").toBe(p.model.sha256);
    await expect(card.getByText(/Сравните SHA-256/)).toBeVisible(act);
    await snapshot("Модели: SHA-256 настоящего файла");
    await card.getByRole("button", { name: "Отмена", exact: true }).click(act);
    await expect(card).toBeHidden(act);
    await expect(installedBase(main), "отмена не должна ничего устанавливать").toHaveCount(0);
  });

  await step("импорт из каталога: файл сверяется с каталогом и устанавливается как «Base Q5»", async () => {
    await withPick(pick, "model", p.goodPick, () => openImportFromRow(main));
    const done = main.getByRole("status").filter({ hasText: /Модель установлена/ });
    await expect(done, "после проверки приложение должно сказать, что модель установлена").toBeVisible({ timeout: 90_000 });
    const text = await textOf(done);
    note(`ответ приложения: ${text}`);
    expect(text).toMatch(/Модель установлена: Base Q5\./);
    await snapshot("Модели: Base Q5 установлена");
  });

  await step("модель в списке установленных; контрольная сумма в подсказке строки равна каталожной", async () => {
    const row = installedBase(main);
    await expect(row, "«Base Q5» должна появиться среди установленных").toHaveCount(1, { timeout: 20_000 });
    await expect(main.getByRole("list", { name: "Установленные модели" })).toBeVisible(act);
    const tip = (await row.locator("p.mname").getAttribute("title", act)) ?? "";
    expect(tip.toLowerCase(), "подсказка строки должна содержать SHA-256 установленного файла").toContain(`sha-256: ${p.model.sha256}`);
    await expect(row.locator(".msize")).toHaveText(`${Math.round(p.model.sizeBytes / 1048576)} МБ`, act);
    await expect(catalogRow(main).getByText("Установлена")).toBeVisible(act);
    const rowText = await textOf(row);
    note(`строка модели: ${rowText}`);
    assertReadable("строка установленной модели", rowText);
  });
});

test(Default, details(Default, { requires: Imported }), async ({ main }) => {
  await step("Модели речи: «Base Q5» — модель по умолчанию", async () => {
    await openModels(main);
    const row = installedBase(main);
    await expect(row).toHaveCount(1, act);
    const badge = row.locator("[data-status]").filter({ hasText: "По умолчанию" });
    if ((await badge.count()) === 0) {
      note("после установки «По умолчанию» не стоит: выбираю через «Сделать по умолчанию»");
      await row.getByRole("button", { name: moreButton }).click(act);
      await main.getByRole("menuitem", { name: "Сделать по умолчанию", exact: true }).click(act);
    } else {
      note("«По умолчанию» стоит сразу после первой установки");
    }
    await expect(badge, "у «Base Q5» должна стоять метка «По умолчанию»").toBeVisible({ timeout: 20_000 });
    await expect(main.getByText(/ни одна не выбрана по умолчанию/), "предупреждения о выборе модели быть не должно").toHaveCount(0);
    await snapshot("Модели: по умолчанию");
  });

  await step("меню модели больше не предлагает «Сделать по умолчанию»", async () => {
    const row = installedBase(main);
    await row.getByRole("button", { name: moreButton }).click(act);
    await expect(main.getByRole("menuitem", { name: "Сделать по умолчанию", exact: true })).toHaveCount(0);
    await main.keyboard.press("Escape");
  });
});

test(Speech, details(Speech), async ({ main }) => {
  await step("Модели речи: модуль расшифровки готов (нет уведомления об установке или сбое)", async () => {
    await openModels(main);
    // The notice of the speech component loads on its own and shows nothing while recognition works, so the state is read
    // after the list has loaded and the model is there (in a full run, seconds after the page was first opened).
    await expect(main.getByRole("heading", { name: "Установленные", exact: true })).toBeVisible(act);
    await expect(main.locator('.note[role="status"]'), "уведомление о модуле расшифровки показывается только когда он не готов").toHaveCount(0);
    await expect(main.getByRole("button", { name: "Установить модуль расшифровки" })).toHaveCount(0);
    await expect(main.getByText(/Расшифровка недоступна|Устанавливается модуль расшифровки/)).toHaveCount(0);
    await snapshot("Модели: модуль расшифровки готов");
  });
});
