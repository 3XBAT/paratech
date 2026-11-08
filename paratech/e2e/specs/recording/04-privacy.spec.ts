// Pack «Запись → конспект → задание», part 4: AC-06, raw audio never leaves the machine (TASK-378). Every request the Hub sent
// to the provider during the pack (the fake keeps them until the next script(), the pack keeps them in a file) is searched for
// audio: container markers (RIFF, OggS, ID3, fLaC), a base64 run that starts like one or is longer than 4 KiB, an MPEG sync.
// A search that finds nothing proves nothing if it looked at nothing or cannot see audio, so the row also checks that
// requests were examined, that they carry the transcript, and that the same search does flag the real clip as base64.
import { readFileSync } from "node:fs";
import path from "node:path";

import { EnvCtlDir } from "../../src/contract.js";
import { audioFindings, audioInRequests, ClipRel, id3Tag } from "../../src/fixtures/audio.js";
import { requireEnv } from "../../src/steps/env.js";
import { expect, step, test } from "../../src/steps/index.js";
import { details, Feature, keepRequests, loadRequests, note } from "./support.js";

test(Feature.privacy, details(Feature.privacy, { requires: Feature.import }), async ({ llm }) => {
  await keepRequests(llm);
  const requests = loadRequests();

  await step("запросы Hub к провайдеру собраны и в них есть расшифровка (а не пустота)", async () => {
    note(`запросов к провайдеру за пак: ${requests.length}`);
    expect(requests.length, "за пак должно быть минимум три запроса (конспект, задание, текст лекции первой записи)").toBeGreaterThanOrEqual(3);
    const carrying = requests.filter((r) => JSON.stringify(r.body).includes("transcript"));
    expect(carrying.length, "в запросах должна быть расшифровка (текст), иначе проверять нечего").toBeGreaterThan(0);
  });

  await step("в запросах нет звука: ни RIFF, ни OggS, ни ID3, ни base64 длиннее 4 КиБ", async () => {
    const findings = audioInRequests(requests);
    expect(findings, "в запросах к провайдеру найден звук").toEqual([]);
  });

  await step("контроль: тот же поиск замечает настоящий клип в base64 и сырой MP3 в тексте", async () => {
    const clip = readFileSync(path.join(requireEnv(EnvCtlDir), "fixtures", ...ClipRel.split("/")));
    const asBase64 = { messages: [{ role: "user", content: clip.subarray(0, 6000).toString("base64") }] };
    expect(audioFindings(asBase64), "base64 клипа должен быть замечен").not.toEqual([]);
    const raw = (b: Buffer) => ({ messages: [{ role: "user", content: b.toString("latin1") }] });
    expect(audioFindings(raw(id3Tag("control"))).join(), "заголовок ID3 должен быть замечен").toMatch(/ID3/);
    expect(audioFindings(raw(clip.subarray(0, 4))).join(), "синхрослово MPEG должно быть замечено").toMatch(/MPEG/);
  });
});
