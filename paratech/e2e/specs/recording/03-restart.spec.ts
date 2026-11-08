// Pack «Запись → конспект → задание», part 3: AC-09, a job survives a hard stop of the app (TASK-378). A third import (other
// bytes); while its recognition runs the app is stopped the way a crash does it (taskkill /T /F of the whole tree, whisper-cli
// included), started again on the SAME sandbox, data dir and debug port (src/app/restart.ts), the page is reconnected, and the
// job goes on: the row reaches «Конспект готов» and «Расшифровка» holds words of the speech. The Hub and the fake stay up.
// The new process is registered in state.json, so the teardown stops it; the app stays up for the packs after this one.
import { readFileSync } from "node:fs";
import path from "node:path";

import { restartApp } from "../../src/app/restart.js";
import { EnvCtlDir, EnvRunRoot } from "../../src/contract.js";
import { ClipRel, expectedWords, foundWords } from "../../src/fixtures/audio.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { lectureRules } from "../../scripts/llm/recording/rules.js";
import { importRecording, openLesson, writeVariant } from "./driver.js";
import {
  chipOf,
  details,
  Feature,
  keepRequests,
  lectureOf,
  logLines,
  note,
  openSection,
  showRow,
  statusWord,
  textOf,
  waitWhisper,
  watchWhisper,
  whisperNow,
} from "./support.js";

const Label = "e2e-restart";

test(Feature.restart, details(Feature.restart, { requires: Feature.import }), async ({ main, pick, llm, app }) => {
  test.setTimeout(6 * 60_000);
  const ctl = requireEnv(EnvCtlDir);
  const lecture = lectureOf("перезапуск");
  const clip = readFileSync(path.join(ctl, "fixtures", ...ClipRel.split("/")));
  const pickRel = writeVariant(clip, "restart", Label, 3);

  await step("Hub: три ответа на запросы записи", async () => {
    await llm.script(lectureRules(lecture));
  });

  await importRecording(main, pick, lecture, pickRel, "restart.mp3");

  const before = await step("расшифровка идёт: whisper-cli.exe запущен, строка «Расшифровка»", async () => {
    const proc = await waitWhisper(90_000, "whisper-cli.exe из папки приложения не появился после импорта");
    await expect
      .poll(async () => statusWord(await chipOf(await showRow(main, lecture.title))), { timeout: 30_000, intervals: [500], message: "строка записи должна показать «Расшифровка»" })
      .toBe("Расшифровка");
    await snapshot("Перезапуск: расшифровка идёт");
    return proc;
  });
  const logFrom = logLines(/./).length;

  // From before the stop: the new whisper-cli may start while the new app is still being waited for.
  const watch = watchWhisper();
  const seen: string[] = [];
  let page = main;
  try {
    await step("приложение останавливается жёстко, как при сбое (taskkill /T /F дерева)", async () => {
      const next = await restartApp({ runRoot: requireEnv(EnvRunRoot) });
      note(`приложение запущено заново: pid ${next.pid}`);
      // the tree is gone with its recognition: the whisper-cli of the first start does not live on
      expect((await whisperNow()).map((p) => p.pid), "whisper-cli.exe первого запуска должен был умереть вместе с приложением").not.toContain(before.pid);
    });

    await step("страница переподключена к новому приложению", async () => {
      await app.reconnect();
      page = await app.main();
      await openSection(page, "Записи");
      await snapshot("Перезапуск: приложение запущено заново", page);
    });

    await step("задание продолжается: строка доходит до «Конспект готов»", async () => {
      await expect
        .poll(
          async () => {
            let word = "";
            try {
              word = statusWord(await chipOf(await showRow(page, lecture.title)));
            } catch {
              // the list is being redrawn: look again
            }
            if (word !== "" && seen.at(-1) !== word) {
              seen.push(word);
            }
            if (word === "Ошибка") {
              throw new Error(`после перезапуска цепочка закончилась ошибкой; статусы: ${seen.join(" → ")}`);
            }
            return word;
          },
          { timeout: 4 * 60_000, intervals: [500], message: "после перезапуска строка записи должна дойти до «Конспект готов»" },
        )
        .toBe("Конспект готов");
      note(`статусы строки после перезапуска: ${seen.join(" → ")}`);
      const recovery = logLines(/./)
        .slice(logFrom)
        .filter((l) => /recover|resum|requeue|stt job|transcription job/i.test(l));
      note(`журнал после перезапуска (расшифровка): ${recovery.map((l) => l.slice(0, 160)).join(" | ").slice(0, 700)}`);
      await snapshot("Перезапуск: конспект готов", page);
    });
  } finally {
    const problem = await watch.stop();
    if (problem) {
      note(`наблюдение за whisper-cli.exe прервалось: ${problem}`);
    }
  }

  await step("расшифровка записи есть: слова из речи", async () => {
    const words = expectedWords(readFileSync(path.join(ctl, "fixtures", "audio", "README.md"), "utf8"));
    await openLesson(page, lecture, "Расшифровка");
    const box = page.getByLabel("Текст расшифровки");
    await expect(box).toBeVisible({ timeout: 30_000 });
    const found = foundWords(await textOf(box), words);
    note(`слова из README после перезапуска: ${found.join(", ") || "ничего"}`);
    expect(found.length, "после перезапуска расшифровка должна содержать слова из речи").toBeGreaterThanOrEqual(2);
    const again = [...watch.seen.keys()].filter((pid) => pid !== before.pid);
    expect(again.length, "после перезапуска задание должно было запустить whisper-cli.exe заново").toBeGreaterThan(0);
  });
});

test.afterEach(async ({ llm }) => {
  await keepRequests(llm);
});
