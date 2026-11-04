// Pack «Настройки и подключения», part 1: the Hub (TASK-377; AC-12, AC-25). The sandbox starts with no key and no address;
// the pack sets them through the real UI. Three rows, in this order (workers: 1, one file: the order of the file):
//   1. address, key and the remaining quota (the E2E limit of 400 000 tokens);
//   2. a revoked key shows the rejection. The key of the RUN must stay valid for the later packs, so a SECOND key is issued
//      with `hub keys create` (src/hub/keys.ts) and that one is revoked; the key of the run is typed back at the end;
//   3. the key of the run is typed once more (a fresh visit) and works, so later packs are not affected whatever happened in
//      row 2 (the `finally` of row 2 is only a first attempt).
// The key is read from process.env only; it is never written to a title, a note, an annotation or a log.
import type { Page } from "@playwright/test";

import { EnvHubKey, EnvHubUrl } from "../../src/contract.js";
import { readableRussian } from "../../src/fixtures/text.js";
import { DefaultQuotaLimit } from "../../src/hub/index.js";
import { keyTools } from "../../src/hub/keys.js";
import { registerSecrets } from "../../src/report/scrub.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { amount, backToOverview, enterAddress, enterKey, hubCard, keyChip, openHubSection, quotaResult, refreshQuota } from "./hub.js";
import { act, details, firstLine, note, textOf } from "./support.js";

const Connect = "Hub: адрес сервера, ключ и остаток лимита";
const Revoked = "Hub: отозванный ключ показывает отказ";
const Restored = "Hub: ключ прогона принят после проверки отзыва";

/** The quota block says the key works: a green line, the E2E limit, a remaining amount within it. */
async function expectQuotaOk(main: Page, what: string): Promise<void> {
  await expect(quotaResult(main).first(), `${what}: после ответа Hub под «Лимит обработки» должна быть строка результата`).toBeVisible({
    timeout: 30_000,
  });
  await expect(quotaResult(main).first(), `${what}: строка остатка должна быть зелёной, а не ошибкой`).toHaveClass(/s-ok/, act);
  await expect.poll(() => amount(main, "Лимит"), { timeout: 15_000, message: `${what}: «Лимит» должен быть ${DefaultQuotaLimit}` }).toBe(DefaultQuotaLimit);
  const remaining = await amount(main, "Осталось");
  expect(remaining, `${what}: «Осталось» должно быть числом от 1 до лимита, а на экране ${remaining}`).toBeGreaterThan(0);
  expect(remaining).toBeLessThanOrEqual(DefaultQuotaLimit);
  expect(await amount(main, "Зарезервировано"), `${what}: «Зарезервировано» должно быть числом`).toBeGreaterThanOrEqual(0);
}

test(Connect, details(Connect), async ({ main }) => {
  const url = requireEnv(EnvHubUrl);
  const key = requireEnv(EnvHubKey);

  await step("Настройки → Подключения → «Конспекты и задания»", async () => {
    await openHubSection(main);
    note(`до настройки: ${await textOf(main.locator("section.conn-page .box").first())}`.slice(0, 300));
    await snapshot("Hub: раздел до настройки");
  });

  await step("ключ вводится в поле «Новый ключ» и сохраняется", async () => {
    await enterKey(main, key);
    await expect(keyChip(main, "Ключ задан")).toBeVisible(act);
    await snapshot("Hub: ключ сохранён");
  });

  await step("«Адрес сервера» (Дополнительно): адрес харнесса сохраняется", async () => {
    await enterAddress(main, url);
    await snapshot("Hub: адрес сохранён");
  });

  await step(`остаток лимита: «Лимит» ${DefaultQuotaLimit}`, async () => {
    // saving the address asks the Hub by itself; «Обновить» is the user's own way to ask again
    await expectQuotaOk(main, "после сохранения адреса");
    await refreshQuota(main);
    await expectQuotaOk(main, "после «Обновить»");
    note(`строка остатка: ${await textOf(quotaResult(main).first())}`);
    await snapshot("Hub: остаток лимита");
  });

  await step("карточка «Конспекты и задания» говорит «Подключено»", async () => {
    await backToOverview(main);
    await expect(hubCard(main).locator("[data-status]")).toContainText("Подключено", act);
    await snapshot("Hub: карточка в «Подключениях»");
  });
});

test(Revoked, details(Revoked, { requires: Connect }), async ({ main }) => {
  const runKey = requireEnv(EnvHubKey);
  const tools = keyTools();
  const label = `e2e-rev-${Date.now().toString(36)}`;
  const second = await tools.issueKey(label);
  registerSecrets([second.key]);
  note(`второй ключ выдан (метка ${label}); ключ прогона не отзывается`);

  let restored = false;
  try {
    await step("второй ключ вводится в UI, остаток лимита виден", async () => {
      await openHubSection(main);
      await enterKey(main, second.key);
      await expectQuotaOk(main, "со вторым ключом");
      await snapshot("Отзыв: второй ключ принят");
    });

    await step("второй ключ отзывается командой hub keys revoke (ключ прогона остаётся действующим)", async () => {
      await tools.revokeKey(second.id);
      const rows = await tools.listKeys();
      const mine = rows.find((r) => r.id === second.id);
      expect(mine?.status, "второй ключ в `hub keys list` должен быть отозван").toBe("revoked");
      expect(rows.filter((r) => r.id !== second.id && r.status === "active").length, "ключ прогона должен остаться действующим").toBeGreaterThanOrEqual(1);
    });

    await step("«Обновить» показывает, что ключ не принят, и дорогу дальше", async () => {
      await refreshQuota(main);
      const line = quotaResult(main).first();
      await expect(line, "под «Лимит обработки» должна быть строка отказа").toBeVisible(act);
      await expect(line, "отказ должен быть красной строкой").toHaveClass(/s-fail/, act);
      const text = await textOf(line);
      note(`текст отказа: ${text}`);
      expect(readableRussian(text), `текст отказа должен быть читаемым русским: «${text}»`).toEqual([]);
      expect(text, "отказ должен назвать причину (ключ не принят или отозван)").toMatch(/не принял.*ключ|ключ.*(неверн|отозван)/i);
      expect(text, "отказ должен сказать, что делать (ввести действующий ключ)").toMatch(/действующий ключ/i);
      // the way out is on the screen: the key field is usable and «Сохранить ключ» is there for a typed value
      await expect(main.getByLabel("Новый ключ", { exact: true })).toBeEnabled(act);
      await expect(main.getByRole("button", { name: "Сохранить ключ", exact: true })).toBeVisible(act);
      await snapshot("Отзыв: отказ в строке остатка");
      await backToOverview(main);
      await expect(hubCard(main).locator("[data-status]"), "карточка в «Подключениях» должна показать отказ").toContainText("Ключ не принят", act);
      await snapshot("Отзыв: карточка «Ключ не принят»");
    });

    await step("ключ прогона вводится обратно, остаток лимита снова виден", async () => {
      await hubCard(main).click(act);
      await enterKey(main, runKey);
      await expectQuotaOk(main, "с ключом прогона");
      restored = true;
      await snapshot("Отзыв: ключ прогона возвращён");
    });
  } finally {
    if (!restored) {
      // keep the later packs alive: put the key of the run back even though this row failed
      try {
        await openHubSection(main);
        await enterKey(main, runKey);
        note("ключ прогона возвращён в finally (строка упала раньше)");
      } catch (err) {
        note(`ключ прогона вернуть не удалось: ${firstLine(err)}`);
      }
    }
  }
});

test(Restored, details(Restored, { requires: Connect }), async ({ main }) => {
  await step("новый заход в «Подключения → Конспекты и задания»: ключ прогона вводится сам, остаток лимита приходит", async () => {
    // Whatever happened in the revoke row, this row types the key of the run itself: the later packs need it valid.
    await openHubSection(main);
    await enterKey(main, requireEnv(EnvHubKey));
    await refreshQuota(main);
    await expectQuotaOk(main, "с ключом прогона");
    await expect(keyChip(main, "Ключ задан")).toBeVisible(act);
    await snapshot("Ключ прогона принят");
  });
});
