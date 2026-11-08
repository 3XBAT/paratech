// Pack «Запись → конспект → задание», part 2: the cancel of a running recognition (TASK-378, PRD 7.2).
//
// What the app offers: a job of a recording has no «Отменить» of its own. «Остановить» on a row is only for a recording that is
// being made, and «Фоновая обработка» lists the jobs WITHOUT a recording (drafts, mail). The one way a student cancels a running
// STT job is «Удалить» in the «⋯» menu of the recording: recordingdelete cancels its jobs (jobrunner.Cancel, the handler's
// context ends, whisper-cli is killed) and then removes the recording. The row therefore has no «cancelled» state: it is gone.
// This row checks what that cancel promises: the recognition stops, no whisper-cli of the sandbox stays, nothing went to the Hub,
// no transcript or row is left, and the log says the job was cancelled. The missing cancel on the row is a finding (README).
import { readFileSync } from "node:fs";
import path from "node:path";

import { EnvCtlDir } from "../../src/contract.js";
import { ClipRel } from "../../src/fixtures/audio.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, snapshot, step, test } from "../../src/steps/index.js";
import { importRecording, writeVariant } from "./driver.js";
import {
  act,
  chipOf,
  details,
  Feature,
  keepRequests,
  lectureOf,
  logLines,
  note,
  readPack,
  rowOf,
  showRow,
  statusWord,
  textOf,
  waitWhisper,
  whisperNow,
} from "./support.js";

const Label = "e2e-cancel";

test(Feature.cancel, details(Feature.cancel, { requires: Feature.import }), async ({ main, pick, llm }) => {
  test.setTimeout(4 * 60_000);
  const ctl = requireEnv(EnvCtlDir);
  const lecture = lectureOf("отмена");
  const clip = readFileSync(path.join(ctl, "fixtures", ...ClipRel.split("/")));
  // The speech three times in a row: ~150 s of audio keeps the recognition running long enough to be cancelled in the middle.
  const pickRel = writeVariant(clip, "cancel", Label, 3);

  await step("Hub: ничего не ожидается (запрос к провайдеру здесь был бы незапланированным)", async () => {
    await llm.script([]);
  });

  await importRecording(main, pick, lecture, pickRel, "cancel.mp3");

  const started = await step("расшифровка идёт: строка «Расшифровка» и whisper-cli.exe из папки приложения", async () => {
    const proc = await waitWhisper(90_000, "whisper-cli.exe из папки приложения не появился после импорта");
    await expect
      .poll(async () => statusWord(await chipOf(rowOf(main, lecture.title))), { timeout: 30_000, intervals: [500], message: "строка записи должна показать «Расшифровка»" })
      .toBe("Расшифровка");
    await snapshot("Отмена: расшифровка идёт");
    return proc;
  });
  const logBefore = logLines(/./).length;

  await step("«⋯» → «Удалить» → подтверждение: расшифровка отменяется", async () => {
    const row = rowOf(main, lecture.title);
    await row.getByRole("button", { name: `Ещё действия: ${lecture.title}`, exact: true }).click(act);
    await main.getByRole("menuitem", { name: "Удалить", exact: true }).click(act);
    const dialog = main.getByRole("dialog");
    await expect(dialog, "после «Удалить» должно открыться подтверждение").toBeVisible(act);
    const confirm = dialog.getByRole("button", { name: "Удалить", exact: true });
    await expect(confirm).toBeEnabled({ timeout: 20_000 });
    note(`подтверждение удаления: ${await textOf(dialog)}`);
    await snapshot("Отмена: подтверждение");
    // A recognition that already finished is not cancelled by this click: the row would then prove nothing.
    const running = (await whisperNow()).some((p) => p.pid === started.pid);
    expect(running, `расшифровка (whisper-cli.exe pid ${started.pid}) успела закончиться до «Удалить»: отмену проверять не на чем`).toBe(true);
    await confirm.click(act);
    await expect(dialog).toBeHidden({ timeout: 60_000 });
  });

  await step("whisper-cli.exe остановлен, строки нет, расшифровки нет", async () => {
    await expect
      .poll(async () => (await whisperNow()).map((p) => p.pid), { timeout: 60_000, intervals: [500], message: "после отмены в песочнице не должно остаться whisper-cli.exe" })
      .toEqual([]);
    note(`whisper-cli.exe (pid ${started.pid}) исчез после «Удалить»`);

    await expect(rowOf(main, lecture.title), "строки удалённой записи быть не должно").toHaveCount(0, { timeout: 30_000 });
    await expect(await showRow(main, lecture.title)).toHaveCount(0);
    await snapshot("Отмена: записи нет");

    const after = logLines(/./).slice(logBefore);
    const cancelled = after.filter((l) => /job attempt finished/.test(l) && /outcome=cancelled/.test(l) && /\btype="?stt"?(\s|$)/.test(l));
    note(`строки журнала об отмене: ${cancelled.length > 0 ? cancelled.join(" | ").slice(0, 400) : "нет"}`);
    expect(cancelled.length, "в desktop.log должна быть строка «job attempt finished … type=stt … outcome=cancelled»").toBeGreaterThan(0);
  });

  await step("в Hub ничего не ушло, первая запись цела", async () => {
    expect(await llm.requests(), "отменённая расшифровка не должна ничего отправлять провайдеру").toEqual([]);
    const main1 = readPack().main;
    expect(main1, "первая запись пака").toBeDefined();
    const row = await showRow(main, main1?.title ?? "");
    expect(await chipOf(row), "первая запись осталась «Конспект готов»").toContain("Конспект готов");
  });
});

test.afterEach(async ({ llm }) => {
  await keepRequests(llm);
});
