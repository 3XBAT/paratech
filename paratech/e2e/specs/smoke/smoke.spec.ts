import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

import { AnnotationFeature, AnnotationMode, EnvCtlDir } from "../../src/contract.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";

// Checked in a real run (2026-11-04): the app creates both WebView2 pages at start, index.html at "/" and popup.html,
// the document title is «paratech» (the window title with « · E2E» is the native one, not visible to CDP).
test(
  "приложение запускается и показывает главное окно",
  {
    tag: "@smoke",
    annotation: [
      { type: AnnotationFeature, description: "Запуск приложения" },
      { type: AnnotationMode, description: "SCRIPTED" },
    ],
  },
  async ({ main, popup, opened }) => {
    await step("главное окно загрузилось", async () => {
      await expect(main).toHaveTitle("paratech");
      await expect(main.locator("#root")).not.toBeEmpty();
    });

    await step("страница всплывающего окна существует", async () => {
      expect(popup, "popup.html не найден среди страниц приложения").toBeDefined();
    });

    await step("снимок главного окна", async () => {
      await snapshot("главное окно");
    });

    await step("ничего не открыто и ни один диалог не использован", async () => {
      expect(await opened()).toEqual([]);
      const consumed = path.join(process.env[EnvCtlDir] ?? "", "consumed.jsonl");
      expect(!existsSync(consumed) || readFileSync(consumed, "utf8").trim() === "").toBe(true);
    });
  },
);
