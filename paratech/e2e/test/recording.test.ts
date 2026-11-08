// Unit tests of what the recording pack (TASK-378) stands on: the audio fixture and its variants, the scan for audio in the
// requests to the provider, the scripted rules, the lookup of the sandbox's processes, the restart of the app, the STT speed
// line in the report and the order of the packs. No app, no Docker.
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { after, describe, test } from "node:test";
import { fileURLToPath } from "node:url";

import { findRule } from "../src/fakellm/index.js";
import { desktopLogPath, freshLog, killProcessTree, launchApp, type LaunchOptions, type RunningApp, spawnOptions } from "../src/app/launch.js";
import { isInFolder, parseProcessJson, pathOfPid, processesNamed, processesNamedAsync, sandboxProcesses } from "../src/app/procs.js";
import { killRestartedApp, restartApp, RestartError } from "../src/app/restart.js";
import { AnnotationFeature, AnnotationRequires, AnnotationSttSpeed, type RunState } from "../src/contract.js";
import {
  audioFindings,
  audioInRequests,
  audioVariant,
  ClipBytesPerSecond,
  clipSeconds,
  expectedWords,
  foundWords,
  id3Tag,
  sttSpeedLine,
} from "../src/fixtures/audio.js";
import { dateInput, lectureOf, localOffset } from "../src/fixtures/lecture.js";
import { freeLoopbackPort } from "../src/hub/ports.js";
import { toRecord } from "../src/report/collect.js";
import type { ReportInput, TestRecord } from "../src/report/model.js";
import { renderReport } from "../src/report/render.js";
import { clearRegisteredSecrets, registerSecrets } from "../src/report/scrub.js";
import { evaluate, splitRequires } from "../src/report/rows.js";
import { readState, writeState } from "../src/sandbox/state.js";
import { ExtractMatch, fillerParagraphs, LectureMatch, LectureRunes, lectureRules, SummaryMatch } from "../scripts/llm/recording/rules.js";

const here = path.dirname(fileURLToPath(import.meta.url));
const e2eDir = path.resolve(here, "..", "..");
const clipFile = path.join(e2eDir, "fixtures", "audio", "chekhov-chelovek-v-futliare-s1-20-70s.mp3");

const tmp = mkdtempSync(path.join(os.tmpdir(), "e2e-recording-"));
after(() => rmSync(tmp, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }));

// ---------------------------------------------------------------- the clip and its variants

describe("the committed clip", () => {
  const clip = readFileSync(clipFile);

  test("is the documented cut: size, SHA-256, MPEG frame at the start, no ID3 tag, within the budget of the repo", () => {
    assert.equal(clip.length, 399_987);
    assert.equal(createHash("sha256").update(clip).digest("hex"), "8ac7d18f818a282b7734cbeb0d1b8b771243c88d1e8da679a3001cd936cd4ad9");
    assert.ok(clip.length <= 2 * 1024 * 1024, "at most 2 MB");
    assert.equal(clip[0], 0xff);
    assert.equal((clip[1] ?? 0) & 0xe0, 0xe0, "an MPEG sync, not ID3");
    assert.notEqual(clip.subarray(0, 3).toString("latin1"), "ID3");
  });

  test("about fifty seconds at 64 kbps", () => {
    assert.ok(Math.abs(clipSeconds(clip.length) - 50) < 0.5, String(clipSeconds(clip.length)));
    assert.equal(ClipBytesPerSecond, 8000);
  });
});

describe("variants of the clip", () => {
  const clip = Buffer.from([0xff, 0xfb, 0x90, 0x64, 1, 2, 3, 4, 5, 6, 7, 8]);

  test("an ID3v2.3 tag with a title frame and a syncsafe size; the audio follows it untouched", () => {
    const tag = id3Tag("e2e-cancel");
    assert.equal(tag.subarray(0, 3).toString("latin1"), "ID3");
    assert.deepEqual([...tag.subarray(3, 6)], [3, 0, 0], "version 2.3, no flags");
    const size = [...tag.subarray(6, 10)].reduce((n, b) => {
      assert.ok(b < 0x80, "syncsafe: no byte has the high bit");
      return (n << 7) | b;
    }, 0);
    assert.equal(size, tag.length - 10);
    assert.equal(tag.subarray(10, 14).toString("latin1"), "TIT2");
    assert.equal(tag.subarray(tag.length - "e2e-cancel".length).toString("latin1"), "e2e-cancel");

    const v = audioVariant(clip, "e2e-cancel");
    assert.deepEqual(v.subarray(tag.length), clip);
  });

  test("another label is another file; the number of repeats lengthens the audio", () => {
    assert.notDeepEqual(audioVariant(clip, "a"), audioVariant(clip, "b"));
    assert.notDeepEqual(audioVariant(clip, "a"), clip);
    const three = audioVariant(clip, "a", 3);
    assert.equal(three.length, id3Tag("a").length + 3 * clip.length);
    assert.throws(() => audioVariant(clip, "a", 0));
    assert.throws(() => audioVariant(clip, "a", 1.5));
  });

  test("the label is plain ASCII: it goes into a tag, not into a path or a script", () => {
    for (const bad of ["", "ё", "a/b", "a\u0000b", "x".repeat(65), "a;b"]) {
      assert.throws(() => id3Tag(bad), /label/, JSON.stringify(bad));
    }
  });
});

// ---------------------------------------------------------------- words and the speed line

describe("expected words", () => {
  test("the «Expected words:» line of the README, lower-cased, trimmed, punctuation dropped", () => {
    const readme = "# Audio\n\nSome text: with colon\nExpected words: Буркин, Иваныч; `староста`, «охотники».\nOther: line\n";
    assert.deepEqual(expectedWords(readme), ["буркин", "иваныч", "староста", "охотники"]);
    assert.deepEqual(expectedWords("**Expected words:** а, б\n"), ["а", "б"]);
    assert.deepEqual(expectedWords("- Expected words: а, б"), ["а", "б"]);
    assert.deepEqual(expectedWords("nothing here"), []);
  });

  test("found in a text case-insensitively, ё and е alike", () => {
    assert.deepEqual(foundWords("Ветеринарный врач ИВАН Иваныч и учитель", ["иваныч", "учитель", "староста"]), ["иваныч", "учитель"]);
    assert.deepEqual(foundWords("Всё было тихо", ["все", "тихо"]), ["все", "тихо"]);
    assert.deepEqual(foundWords("", ["а"]), []);
  });
});

describe("the speed line", () => {
  test("seconds of audio, seconds of work, the factor, the model and the processor", () => {
    assert.equal(
      sttSpeedLine({ audioSeconds: 50, wallSeconds: 12.5, model: "base-q5_1", cpu: "Test CPU" }),
      "50 с звука расшифровано за 12,5 с (×4,0 от реального времени); модель base-q5_1; процессор Test CPU",
    );
    assert.match(sttSpeedLine({ audioSeconds: 50, wallSeconds: 0, model: "m", cpu: "c" }), /×\?/);
  });
});

// ---------------------------------------------------------------- AC-06: audio in the requests to the provider

describe("the scan for audio in what the Hub sent to the provider", () => {
  const request = (content: string, extra: Record<string, unknown> = {}) => ({ model: "m", system: "You are a study assistant.", messages: [{ role: "user", content }], ...extra });

  test("a normal request is clean: instructions, JSON of a lesson, a Russian transcript", () => {
    const body = request('<untrusted_data>\n{"lesson_title":"Литература","transcript_minutes":1,"transcript":"На краю села Мироносицкого, в сарае старосты Прокофия"}\n</untrusted_data>');
    assert.deepEqual(audioFindings(body), []);
    assert.deepEqual(audioFindings("plain text"), []);
    assert.deepEqual(audioInRequests([{ seq: 1, body }]), []);
  });

  test("container markers are found: RIFF, OggS, ID3, fLaC", () => {
    for (const marker of ["RIFF", "OggS", "ID3", "fLaC"]) {
      assert.deepEqual(audioFindings(request(`xx ${marker}\u0000\u0001`)), [`marker ${marker}`], marker);
    }
  });

  test("base64 audio is found: a run over 4 KiB, and a shorter one that starts like a container", () => {
    const bytes = Buffer.alloc(6000, 7);
    assert.ok(audioFindings(request(bytes.toString("base64"))).some((f) => f.includes("4 KiB")));
    const riff = Buffer.concat([Buffer.from("RIFF....WAVEfmt "), Buffer.alloc(200, 1)]);
    assert.ok(audioFindings(request(riff.toString("base64"))).some((f) => f.includes("starts like audio")));
    const ogg = Buffer.concat([Buffer.from("OggS\u0000\u0002"), Buffer.alloc(200, 1)]);
    assert.ok(audioFindings(request(ogg.toString("base64"))).some((f) => f.includes("starts like audio")));
    const mpeg = Buffer.concat([Buffer.from([0xff, 0xfb, 0x90, 0x64]), Buffer.alloc(200, 1)]);
    assert.ok(audioFindings(request(mpeg.toString("base64"))).some((f) => f.includes("starts like audio")));
  });

  test("an MPEG frame sync as text is found", () => {
    assert.ok(audioFindings(request(Buffer.from([0xff, 0xfb, 0x90, 0x64]).toString("latin1"))).some((f) => f.includes("MPEG")));
  });

  test("the real clip is found in every common encoding of it", () => {
    const clip = readFileSync(clipFile);
    assert.ok(audioFindings(request(clip.subarray(0, 8000).toString("base64"))).length > 0);
    assert.ok(audioFindings(request(clip.subarray(1000, 9000).toString("base64"))).length > 0, "base64 from the middle of the file");
    assert.ok(audioFindings(request(id3Tag("x").toString("latin1"))).length > 0);
  });

  test("findings name the request by its number", () => {
    assert.deepEqual(audioInRequests([{ seq: 4, body: request("a") }, { seq: 9, body: request("OggS") }]), ["запрос 9: marker OggS"]);
  });
});

// ---------------------------------------------------------------- the lecture and the scripted rules

describe("the lecture of an import", () => {
  test("yesterday 10:00 local, a deadline in two days with the offset of the lecture start", () => {
    const now = new Date(2026, 10, 8, 15, 30);
    const l = lectureOf("основная", now);
    assert.equal(l.date, "2026-11-07");
    assert.equal(l.time, "10:00");
    assert.equal(l.due, `2026-11-10T20:00:00${localOffset(new Date(2026, 10, 7, 10, 0))}`);
    assert.equal(l.dueDay, "10.11");
    assert.match(l.due, /^\d{4}-\d{2}-\d{2}T20:00:00[+-]\d{2}:\d{2}$/);
    assert.equal(dateInput(new Date(2026, 0, 5)), "2026-01-05");
  });

  test("the label tells the lectures apart, in the title and in the assignment", () => {
    const a = lectureOf("отмена");
    const b = lectureOf("перезапуск");
    assert.notEqual(a.title, b.title);
    assert.notEqual(a.assignmentTitle, b.assignmentTitle);
    assert.ok(a.title.includes("отмена") && a.assignmentTitle.includes("отмена"));
  });

  test("month and year borders", () => {
    const l = lectureOf("x", new Date(2026, 11, 31, 8, 0));
    assert.equal(l.date, "2026-12-30");
    assert.equal(l.due.slice(0, 10), "2027-01-02");
    assert.equal(l.dueDay, "02.01");
  });
});

describe("the scripted rules of one lecture", () => {
  const lecture = lectureOf("основная", new Date(2026, 10, 8, 15, 30));
  const rules = lectureRules({
    summaryHeading: lecture.summaryHeading,
    summaryText: lecture.summaryText,
    summaryPoint: lecture.summaryPoint,
    keyPoint: lecture.keyPoint,
    assignmentTitle: lecture.assignmentTitle,
    assignmentDescription: lecture.assignmentDescription,
    due: lecture.due,
  });

  // The user messages of the three operations, as prompts.Build / BuildLectureText make them (the JSON of the payload).
  const lessonJson = '"lesson_title":"Человек в футляре · основная","lesson_subject":"Литература","lesson_kind":"lecture","lesson_start":"2026-11-07T10:00:00+03:00","lesson_weekday":"Saturday"';
  const wrap = (json: string) => `<untrusted_data>\n{${json}}\n</untrusted_data>`;
  const summaryAsk = wrap(`${lessonJson},"transcript_minutes":1,"transcript":"Он жил около города"`);
  const extractAsk = wrap(`${lessonJson},"transcript":"Он жил около города"`);
  const lectureAsk = wrap(`${lessonJson},"part":1,"parts":1,"segments":[{"i":0,"at":"00:00","text":"Он жил около города"}]`);

  test("three rules in the order lecture text, summary, extraction, each a plain answer with a distinct match", () => {
    assert.deepEqual(
      rules.map((r) => [r.kind, r.match]),
      [
        ["plain", LectureMatch],
        ["plain", SummaryMatch],
        ["plain", ExtractMatch],
      ],
    );
  });

  test("each request is answered by its own rule, whatever order the three jobs reach the Hub in", () => {
    const orders: string[][] = [
      [lectureAsk, summaryAsk, extractAsk],
      [extractAsk, summaryAsk, lectureAsk],
      [summaryAsk, lectureAsk, extractAsk],
      [lectureAsk, extractAsk, summaryAsk],
    ];
    for (const order of orders) {
      const left = [...rules];
      const want = new Map([
        [lectureAsk, LectureMatch],
        [summaryAsk, SummaryMatch],
        [extractAsk, ExtractMatch],
      ]);
      for (const ask of order) {
        const at = findRule(left, ask);
        assert.ok(at >= 0, "a rule answers");
        assert.equal(left[at]?.match, want.get(ask), `order ${order.map((o) => o.slice(0, 0) + (o === lectureAsk ? "L" : o === summaryAsk ? "S" : "E")).join("")}`);
        left.splice(at, 1);
      }
      assert.equal(left.length, 0);
    }
  });

  test("the summary and the extraction are what ParseSummary and ParseExtraction accept", () => {
    const summary = JSON.parse((rules[1] as { reply: string }).reply) as { sections: Array<{ heading: string; text: string; points: string[] }>; key_points: string[] };
    assert.deepEqual(Object.keys(summary).sort(), ["key_points", "sections"]);
    assert.equal(summary.sections.length, 1);
    assert.deepEqual(Object.keys(summary.sections[0] ?? {}).sort(), ["heading", "points", "text"]);
    assert.equal(summary.sections[0]?.heading, lecture.summaryHeading);
    assert.deepEqual(summary.key_points, [lecture.keyPoint]);

    const extract = JSON.parse((rules[2] as { reply: string }).reply) as { assignments: Array<{ title: string; description: string; due: string }>; subject_hint: string; teacher_hint: string };
    assert.deepEqual(Object.keys(extract).sort(), ["assignments", "subject_hint", "teacher_hint"]);
    assert.deepEqual(Object.keys(extract.assignments[0] ?? {}).sort(), ["description", "due", "title"]);
    assert.equal(extract.assignments[0]?.due, lecture.due);
    assert.ok(!Number.isNaN(Date.parse(extract.assignments[0]?.due ?? "")), "RFC 3339");
  });

  test("the lecture text is long enough not to be called condensed, in valid blocks", () => {
    const text = JSON.parse((rules[0] as { reply: string }).reply) as { blocks: Array<{ heading: string; from: number; paragraphs: string[] }> };
    assert.equal(text.blocks.length, 1);
    const block = text.blocks[0];
    assert.ok(block && block.from === 0 && block.heading !== "");
    const runes = block.paragraphs.reduce((n, p) => n + [...p].length, 0);
    assert.ok(runes >= LectureRunes, `${runes} runes`);
    // 45 % of a transcript of up to ~5000 characters (a 60 s clip says at most ~1000 words)
    assert.ok(runes >= 0.45 * 5000);
    for (const p of block.paragraphs) {
      assert.ok([...p].length >= 1 && [...p].length <= 4000);
      assert.ok(!/[\u0000-\u0008\u000b-\u001f]/.test(p), "no control characters");
    }
    assert.deepEqual(fillerParagraphs(10).length, 1);
  });
});

// ---------------------------------------------------------------- freshLog, processes

describe("the part of the log written by the new start", () => {
  test("only the text after the old length; a shorter (rotated) log is read whole; 0 is everything", () => {
    assert.equal(freshLog("old line\nnew line\n", 9), "new line\n");
    assert.equal(freshLog("new\n", 100), "new\n");
    assert.equal(freshLog("abc", 0), "abc");
    assert.equal(freshLog("abc", 3), "");
  });
});

describe("the processes of the sandbox", () => {
  const json = JSON.stringify([
    { ProcessId: 10, ExecutablePath: "C:\\Repo\\paratech\\dist\\e2e\\whisper-cli.exe", CommandLine: "whisper-cli.exe -m model.bin -f in.wav" },
    { ProcessId: 11, ExecutablePath: "C:\\Users\\owner\\app\\whisper-cli.exe", CommandLine: "whisper-cli.exe" },
    { ProcessId: 12, ExecutablePath: null, CommandLine: null },
    { ProcessId: -1, ExecutablePath: "x" },
    { ProcessId: "13" },
  ]);

  test("one object, a list and nothing; rows without a valid pid are dropped; missing text is empty", () => {
    assert.deepEqual(parseProcessJson(""), []);
    assert.deepEqual(parseProcessJson("  \n"), []);
    assert.deepEqual(parseProcessJson('{"ProcessId":5,"ExecutablePath":"C:\\\\a.exe","CommandLine":"a.exe -x"}'), [{ pid: 5, path: "C:\\a.exe", commandLine: "a.exe -x" }]);
    assert.deepEqual(
      parseProcessJson(json).map((p) => p.pid),
      [10, 11, 12],
    );
    assert.deepEqual(parseProcessJson(json)[2], { pid: 12, path: "", commandLine: "" });
    assert.throws(() => parseProcessJson("not json"));
  });

  test("the folder test is exact and case-insensitive: a file in it, not in a sub-folder or a sibling", () => {
    assert.ok(isInFolder("C:\\Repo\\paratech\\dist\\e2e\\whisper-cli.exe", "c:\\repo\\paratech\\DIST\\e2e"));
    assert.ok(!isInFolder("C:\\Repo\\paratech\\dist\\e2e\\sub\\whisper-cli.exe", "C:\\Repo\\paratech\\dist\\e2e"));
    assert.ok(!isInFolder("C:\\Repo\\paratech\\dist\\e2e2\\whisper-cli.exe", "C:\\Repo\\paratech\\dist\\e2e"));
    assert.ok(!isInFolder("", "C:\\Repo"));
  });

  test("the sandbox's own are the ones of its exe folder; a lookup that cannot run is an error, not «none»", async () => {
    const run = () => json;
    assert.deepEqual(
      sandboxProcesses("whisper-cli.exe", "C:\\Repo\\paratech\\dist\\e2e", run).map((p) => p.pid),
      [10],
    );
    assert.throws(() => processesNamed("whisper-cli.exe", () => undefined), /cannot list/);
    await assert.rejects(processesNamedAsync("whisper-cli.exe", async () => undefined), /cannot list/);
    assert.deepEqual((await processesNamedAsync("whisper-cli.exe", async () => json)).map((p) => p.pid), [10, 11, 12]);
  });

  test("an image name is a plain file name (it goes into a WQL filter)", () => {
    for (const bad of ["a'; drop", "a b", "", "x".repeat(65), "a\\b", 'a"b']) {
      assert.throws(() => processesNamed(bad, () => ""), /bad image name/, JSON.stringify(bad));
    }
  });

  test("a pid: its path, or empty when none runs; a bad pid is none", () => {
    assert.equal(pathOfPid(10, () => json), "C:\\Repo\\paratech\\dist\\e2e\\whisper-cli.exe");
    assert.equal(pathOfPid(10, () => ""), "");
    assert.equal(pathOfPid(0, () => json), "");
    assert.equal(pathOfPid(1.5, () => json), "");
    assert.throws(() => pathOfPid(10, () => undefined), /cannot look up/);
  });

  test("the real lookup works on this machine: this very node process is listed with its path", { skip: process.platform === "win32" ? false : "Windows only" }, () => {
    const mine = processesNamed(path.basename(process.execPath)).find((p) => p.pid === process.pid);
    assert.ok(mine, "the test runner is among the node.exe processes");
    assert.equal(path.resolve(mine.path).toLowerCase(), path.resolve(process.execPath).toLowerCase());
    assert.equal(path.resolve(pathOfPid(process.pid)).toLowerCase(), path.resolve(process.execPath).toLowerCase());
    assert.equal(pathOfPid(2_000_000_000), "", "no such pid");
  });
});

// ---------------------------------------------------------------- the restart of the app

describe("restartApp", () => {
  const exe = "C:\\Repo\\paratech\\dist\\e2e\\paratech.exe";

  function sandbox(over: Partial<RunState> = {}): { runRoot: string; state: RunState; log: string } {
    const runRoot = mkdtempSync(path.join(tmp, "run-"));
    const ctlDir = path.join(runRoot, "ctl");
    const dataDir = path.join(ctlDir, "data");
    mkdirSync(path.dirname(desktopLogPath(dataDir)), { recursive: true });
    const log = desktopLogPath(dataDir);
    writeFileSync(log, "old line 1\nold line 2\n");
    const state: RunState = {
      mode: "scripted",
      startedAt: "2026-11-08T10:00:00.000Z",
      runRoot,
      ctlDir,
      dataDir,
      fixturesDir: path.join(ctlDir, "fixtures"),
      exePath: exe,
      cdpUrl: "http://127.0.0.1:45678",
      hubUrl: "http://127.0.0.1:2",
      fakeUrl: "http://127.0.0.1:3",
      appPid: 111,
      ...over,
    };
    writeState(state);
    return { runRoot, state, log };
  }

  /** A launcher that records its options and answers with a fake running app. */
  function launcher(pid: number) {
    const calls: LaunchOptions[] = [];
    const launch = async (o: LaunchOptions): Promise<RunningApp> => {
      calls.push(o);
      o.onSpawn?.({ pid, cdpUrl: "x", child: undefined as never, stop() {} });
      return { pid, cdpUrl: "http://127.0.0.1:45678", child: undefined as never, stop() {} };
    };
    return { calls, launch };
  }

  test("kills the old tree, waits until it and its port are gone, starts the same exe on the same data dir and port, records the new pid", async () => {
    const { runRoot, log } = sandbox();
    const killed: number[] = [];
    let alive = true;
    let looks = 0;
    const { calls, launch } = launcher(222);
    const app = await restartApp({
      runRoot,
      env: { Path: "C:\\bin" },
      deps: {
        launch,
        pathOfPid: () => {
          // alive until killed and for two more looks (the process is going away)
          if (killed.length > 0 && ++looks > 2) alive = false;
          return alive ? exe.toUpperCase() : "";
        },
        kill: (pid) => killed.push(pid),
        portAnswers: async () => alive,
        sleep: async () => {},
      },
    });
    assert.equal(app.pid, 222);
    assert.deepEqual(killed, [111]);
    assert.equal(calls.length, 1);
    const o = calls[0] as LaunchOptions;
    assert.equal(o.exePath, exe);
    assert.equal(o.cdpPort, 45678, "the same debug port");
    assert.equal(o.dataDir, path.join(runRoot, "ctl", "data"));
    assert.equal(o.ctlDir, path.join(runRoot, "ctl"));
    assert.equal(o.detached, true, "the restarted app must outlive its Playwright worker (no kill-on-close job object)");
    assert.equal(o.logFrom, readFileSync(log, "utf8").length, "the lines of the first start are not the new start's");
    assert.deepEqual(o.env, { Path: "C:\\bin" });
    const state = readState(runRoot);
    assert.equal(state?.appPid, 222);
    assert.equal(state?.appRestarted, true);
  });

  test("the new pid is recorded as soon as the process exists, even if its start then fails", async () => {
    const { runRoot } = sandbox();
    const { launch } = launcher(333);
    const failing = async (o: LaunchOptions): Promise<RunningApp> => {
      await launch(o);
      assert.equal(readState(runRoot)?.appPid, 333, "recorded by onSpawn");
      throw new Error("start failed");
    };
    await assert.rejects(
      restartApp({ runRoot, deps: { launch: failing, pathOfPid: () => "", kill: () => {}, portAnswers: async () => false, sleep: async () => {} } }),
      /start failed/,
    );
    assert.equal(readState(runRoot)?.appPid, 333);
  });

  test("an app that is already gone is not killed; the restart goes on", async () => {
    const { runRoot } = sandbox();
    const killed: number[] = [];
    const { launch } = launcher(5);
    await restartApp({ runRoot, deps: { launch, pathOfPid: () => "", kill: (p) => killed.push(p), portAnswers: async () => false, sleep: async () => {} } });
    assert.deepEqual(killed, []);
  });

  test("a pid that now belongs to another exe is never killed", async () => {
    const { runRoot } = sandbox();
    const killed: number[] = [];
    const { calls, launch } = launcher(5);
    await assert.rejects(
      restartApp({ runRoot, deps: { launch, pathOfPid: () => "C:\\Users\\owner\\paratech.exe", kill: (p) => killed.push(p), portAnswers: async () => false, sleep: async () => {} } }),
      (err: unknown) => err instanceof RestartError && /чужой процесс/.test(err.message),
    );
    assert.deepEqual(killed, []);
    assert.equal(calls.length, 0, "nothing is started either");
  });

  test("an old app that does not go away is an error, not a second app on the same port", async () => {
    const { runRoot } = sandbox();
    const { calls, launch } = launcher(5);
    await assert.rejects(
      restartApp({ runRoot, goneTimeoutMs: 20, deps: { launch, pathOfPid: () => exe, kill: () => {}, portAnswers: async () => true, sleep: () => new Promise<void>((r) => setTimeout(r, 15)) } }),
      (err: unknown) => err instanceof RestartError && /не остановилось/.test(err.message),
    );
    assert.equal(calls.length, 0);
  });

  test("a port that still answers after the exe is gone is waited for too", async () => {
    const { runRoot } = sandbox();
    let answers = 3;
    const { launch } = launcher(5);
    let sleeps = 0;
    await restartApp({ runRoot, deps: { launch, pathOfPid: () => "", kill: () => {}, portAnswers: async () => answers-- > 0, sleep: async () => void sleeps++ } });
    assert.equal(sleeps, 3);
  });

  test("without a pid in state.json (or without state.json) there is nothing to restart", async () => {
    const { runRoot } = sandbox({ appPid: undefined as never });
    await assert.rejects(restartApp({ runRoot }), (err: unknown) => err instanceof RestartError && /pid/.test(err.message));
    await assert.rejects(restartApp({ runRoot: path.join(tmp, "no-such-run") }), RestartError);
  });

  test("the window size of the run is passed on", async () => {
    const { runRoot } = sandbox();
    const { calls, launch } = launcher(5);
    await restartApp({ runRoot, window: "1024x640", deps: { launch, pathOfPid: () => "", kill: () => {}, portAnswers: async () => false, sleep: async () => {} } });
    assert.equal(calls[0]?.window, "1024x640");
    const { calls: calls2, launch: launch2 } = launcher(6);
    await restartApp({ runRoot, window: "bad", deps: { launch: launch2, pathOfPid: () => "", kill: () => {}, portAnswers: async () => false, sleep: async () => {} } });
    assert.equal(calls2[0]?.window, undefined, "a malformed size is not passed");
  });
});

describe("killRestartedApp (the teardown and the exit guard)", () => {
  const exe = "C:\\Repo\\paratech\\dist\\e2e\\paratech.exe";

  function stateWith(over: Partial<RunState>): string {
    const runRoot = mkdtempSync(path.join(tmp, "kill-"));
    writeState({
      mode: "scripted",
      startedAt: "x",
      runRoot,
      ctlDir: "c",
      dataDir: "d",
      fixturesDir: "f",
      exePath: exe,
      cdpUrl: "http://127.0.0.1:1",
      hubUrl: "",
      fakeUrl: "",
      ...over,
    });
    return runRoot;
  }

  test("stops the app a spec started again, while it still runs from the sandbox's exe", () => {
    const runRoot = stateWith({ appPid: 222, appRestarted: true });
    const killed: number[] = [];
    assert.equal(killRestartedApp(runRoot, exe, 111, { pathOfPid: () => exe, kill: (p) => killed.push(p) }), true);
    assert.deepEqual(killed, [222]);
  });

  test("does nothing when the app was never restarted, when it is the one the setup started, when it is gone or is another exe", () => {
    const killed: number[] = [];
    const deps = { pathOfPid: () => exe, kill: (p: number) => void killed.push(p) };
    assert.equal(killRestartedApp(stateWith({ appPid: 222 }), exe, 111, deps), false, "not restarted: no look-up at all");
    assert.equal(killRestartedApp(stateWith({ appPid: 111, appRestarted: true }), exe, 111, deps), false);
    assert.equal(killRestartedApp(stateWith({ appPid: 222, appRestarted: true }), exe, 111, { ...deps, pathOfPid: () => "" }), false);
    assert.equal(killRestartedApp(stateWith({ appPid: 222, appRestarted: true }), exe, 111, { ...deps, pathOfPid: () => "C:\\Users\\owner\\paratech.exe" }), false);
    assert.equal(killRestartedApp(path.join(tmp, "missing"), exe, 111, deps), false);
    assert.deepEqual(killed, []);
  });

  test("a restart without a known started pid (a second pass of the teardown) still stops the recorded app", () => {
    const killed: number[] = [];
    assert.equal(killRestartedApp(stateWith({ appPid: 222, appRestarted: true }), exe, undefined, { pathOfPid: () => exe, kill: (p) => void killed.push(p) }), true);
    assert.deepEqual(killed, [222]);
  });
});

describe("restartApp with a stub app and the real taskkill (Windows)", { skip: process.platform === "win32" ? false : "Windows only (taskkill, PowerShell)" }, () => {
  const stubApp = path.join(here, "fixtures", "stub-app.js");

  test("the old process dies, the new one answers on the same port, only the new start's log lines count, and the teardown stops it", async () => {
    const runRoot = mkdtempSync(path.join(tmp, "stub-"));
    const ctlDir = path.join(runRoot, "ctl");
    const dataDir = path.join(ctlDir, "data");
    const port = await freeLoopbackPort();
    const first = await launchApp({
      exePath: process.execPath,
      exeArgs: [stubApp, "ok"],
      dataDir,
      ctlDir,
      logsDir: path.join(ctlDir, "logs"),
      cdpPort: port,
      startTimeoutMs: 10_000,
    });
    writeState({
      mode: "scripted",
      startedAt: "x",
      runRoot,
      ctlDir,
      dataDir,
      fixturesDir: path.join(ctlDir, "fixtures"),
      exePath: process.execPath,
      cdpUrl: first.cdpUrl,
      hubUrl: "",
      fakeUrl: "",
      appPid: first.pid,
    });
    let second: RunningApp | undefined;
    try {
      second = await restartApp({ runRoot, exeArgs: [stubApp, "ok"], exePath: process.execPath, startTimeoutMs: 10_000, env: process.env });
      assert.notEqual(second.pid, first.pid);
      assert.equal(pathOfPid(first.pid), "", "the first process is gone");
      const res = await fetch(`${second.cdpUrl}/json/version`);
      assert.equal(res.status, 200);
      assert.equal(readFileSync(desktopLogPath(dataDir), "utf8").split("\n").filter((l) => l.includes("e2e mode on")).length, 2, "two starts in one log");
      assert.equal(readState(runRoot)?.appPid, second.pid);

      // what the teardown of the main process does with it
      assert.equal(killRestartedApp(runRoot, process.execPath, first.pid), true);
      for (let i = 0; i < 50 && pathOfPid(second.pid) !== ""; i++) {
        await new Promise((r) => setTimeout(r, 100));
      }
      assert.equal(pathOfPid(second.pid), "", "the restarted process is stopped by the teardown");
    } finally {
      first.stop();
      second?.stop();
    }
  });
});

describe("an app detached from its spawning process", () => {
  test("launchApp spawns attached unless asked; detached only on request", () => {
    assert.equal(spawnOptions({ ctlDir: "c" }, 3).detached, false);
    assert.equal(spawnOptions({ ctlDir: "c", detached: false }, 3).detached, false);
    assert.equal(spawnOptions({ ctlDir: "c", detached: true }, 3).detached, true);
    const o = spawnOptions({ ctlDir: "c", detached: true }, 3);
    assert.deepEqual(o.stdio, ["ignore", 3, 3], "stdio stays file descriptors: nothing ties the child to the parent's pipes");
    assert.equal(o.shell, false);
  });

  test(
    "a detached app survives the exit of the process that started it, and the teardown's kill by pid still stops it",
    { skip: process.platform === "win32" ? false : "Windows only (job objects, taskkill, PowerShell)" },
    async () => {
      const root = mkdtempSync(path.join(tmp, "detach-"));
      const ctlDir = path.join(root, "ctl");
      const dataDir = path.join(ctlDir, "data");
      const port = await freeLoopbackPort();
      const parent = path.join(here, "fixtures", "detach-parent.js");
      const run = spawnSync(process.execPath, [parent, path.join(here, "fixtures", "stub-app.js"), dataDir, ctlDir, String(port), "detached"], {
        encoding: "utf8",
        timeout: 60_000,
        windowsHide: true,
      });
      const pid = Number(/pid=(\d+)/.exec(run.stdout)?.[1]);
      try {
        assert.equal(run.status, 0, `${run.stderr}\n${run.stdout}`);
        assert.ok(Number.isInteger(pid) && pid > 0, run.stdout);
        // the parent has exited by now; a kill-on-close job would have taken the app with it
        await new Promise((r) => setTimeout(r, 1_500));
        assert.equal(path.resolve(pathOfPid(pid)).toLowerCase(), path.resolve(process.execPath).toLowerCase(), "the app still runs");
        const res = await fetch(`http://127.0.0.1:${port}/json/version`);
        assert.equal(res.status, 200, "and still answers on its debug port");

        // what the teardown of the main process does: by pid, whole tree, only while it runs the sandbox's exe
        const runRoot = path.join(root, "run");
        mkdirSync(runRoot, { recursive: true });
        writeState({
          mode: "scripted",
          startedAt: "x",
          runRoot,
          ctlDir,
          dataDir,
          fixturesDir: path.join(ctlDir, "fixtures"),
          exePath: process.execPath,
          cdpUrl: `http://127.0.0.1:${port}`,
          hubUrl: "",
          fakeUrl: "",
          appPid: pid,
          appRestarted: true,
        });
        assert.equal(killRestartedApp(runRoot, process.execPath, undefined), true);
        for (let i = 0; i < 50 && pathOfPid(pid) !== ""; i++) {
          await new Promise((r) => setTimeout(r, 100));
        }
        assert.equal(pathOfPid(pid), "", "stopped by pid");
      } finally {
        if (Number.isInteger(pid) && pid > 0 && pathOfPid(pid) !== "") {
          killProcessTree(pid);
        }
      }
    },
  );
});

// ---------------------------------------------------------------- the report

describe("the STT speed in the report", () => {
  const rec = (over: Partial<TestRecord> = {}): TestRecord => ({
    title: "t",
    feature: "Запись: импорт",
    mode: "SCRIPTED",
    pack: "recording",
    outcome: "passed",
    screenshots: [],
    durationMs: 1000,
    ...over,
  });
  const input = (tests: TestRecord[]): ReportInput => ({ tests, mode: "SCRIPTED", runnerErrors: [], elapsedMs: 60_000 });

  test("the annotation reaches the record, the report prints it in its own section, once", () => {
    const like = {
      title: "t",
      annotations: [{ type: AnnotationFeature, description: "Запись: импорт" }],
      results: [
        {
          status: "passed" as const,
          duration: 5,
          errors: [],
          steps: [],
          attachments: [],
          annotations: [{ type: AnnotationSttSpeed, description: "50 с звука расшифровано за 10 с" }],
        },
      ],
      outcome: () => "expected" as const,
      location: { file: "C:\\r\\paratech\\e2e\\specs\\recording\\01-import.spec.ts" },
    };
    const r = toRecord(like, []);
    assert.equal(r.sttSpeed, "50 с звука расшифровано за 10 с");
    assert.equal(r.pack, "recording");

    const text = renderReport(input([r, rec({ sttSpeed: "50 с звука расшифровано за 10 с" })]), {});
    assert.ok(text.includes("## Скорость расшифровки\n\n- 50 с звука расшифровано за 10 с\n"), text);
    assert.equal(text.split("50 с звука расшифровано за 10 с").length, 2, "the same line once");
    assert.ok(text.indexOf("## Скорость расшифровки") < text.indexOf("## Итог прогона"));
  });

  test("a run that measured nothing has no such section; the line is scrubbed like everything else", () => {
    assert.ok(!renderReport(input([rec()]), {}).includes("Скорость расшифровки"));
    registerSecrets(["very-secret-value"]);
    try {
      const text = renderReport(input([rec({ sttSpeed: "процессор very-secret-value" })]), {});
      assert.ok(!text.includes("very-secret-value"));
    } finally {
      clearRegisteredSecrets();
    }
  });
});

describe("requires with several features", () => {
  const rec = (feature: string, over: Partial<TestRecord> = {}): TestRecord => ({
    title: feature,
    feature,
    mode: "SCRIPTED",
    pack: "p",
    outcome: "passed",
    screenshots: [],
    durationMs: 1,
    ...over,
  });
  const input = (tests: TestRecord[]): ReportInput => ({ tests, mode: "SCRIPTED", runnerErrors: [], elapsedMs: 1 });

  test("split on « + », blanks dropped", () => {
    assert.deepEqual(splitRequires("A + B+C +  D "), ["A", "B+C", "D"]);
    assert.deepEqual(splitRequires("Только одно"), ["Только одно"]);
    assert.deepEqual(splitRequires(""), []);
  });

  test("all ready: the row is judged by its own result; the first not ready one is named", () => {
    const ok = evaluate(input([rec("A"), rec("B"), rec("C", { requires: "A + B" })]));
    assert.deepEqual(ok.rows.map((r) => r.outcome), ["готово", "готово", "готово"]);

    const broken = evaluate(input([rec("A"), rec("B", { outcome: "failed", error: "boom" }), rec("C", { requires: "A + B", outcome: "skipped" })]));
    assert.equal(broken.rows[2]?.outcome, "не проверено (нужно: B)");

    const missing = evaluate(input([rec("A"), rec("C", { requires: "A + Z + B", outcome: "failed", error: "red" })]));
    assert.equal(missing.rows[1]?.outcome, "не проверено (нужно: Z (не запускалась))", "a failed row whose prerequisite did not run is not «сломано»");

    const single = evaluate(input([rec("A", { outcome: "failed", error: "x" }), rec("C", { requires: "A", outcome: "skipped" })]));
    assert.equal(single.rows[1]?.outcome, "не проверено (нужно: A)");
  });

  test("the annotation text is read as it is, with the separator", () => {
    const like = {
      title: "t",
      annotations: [{ type: AnnotationRequires, description: "A + B" }],
      results: [{ status: "passed" as const, duration: 1, errors: [], steps: [], attachments: [] }],
      outcome: () => "expected" as const,
      location: { file: "x" },
    };
    assert.equal(toRecord(like, []).requires, "A + B");
  });
});

// ---------------------------------------------------------------- the order of the packs

describe("the packs are projects in the order they depend on each other", () => {
  const config = readFileSync(path.join(e2eDir, "playwright.config.ts"), "utf8");
  const projects = [...(/projects:\s*\[([^\]]*)\]/.exec(config)?.[1] ?? "").matchAll(/pack\("(\w+)"\)/g)].map((m) => m[1]);

  test("every folder of specs/ is a project, so a new pack cannot be forgotten", () => {
    const folders = readdirSync(path.join(e2eDir, "specs"), { withFileTypes: true })
      .filter((d) => d.isDirectory())
      .map((d) => d.name)
      .sort();
    assert.deepEqual([...projects].sort(), folders);
  });

  test("settings before recording (it needs the Hub key, the model and the schedule), smoke last, layout first", () => {
    assert.ok(projects.indexOf("settings") < projects.indexOf("recording"));
    assert.equal(projects.at(-1), "smoke");
    assert.equal(projects[0], "layout");
  });

  test("no `dependencies`: a red pack must not switch the following ones off", () => {
    assert.ok(!/dependencies/.test(config.replace(/\/\/.*$/gm, "")), "a dependency would skip the dependent packs when one fails");
  });

  test("the files of the recording pack run as import, cancel, restart, privacy; each `requires` names a row of the pack or the settings pack", () => {
    const dir = path.join(e2eDir, "specs", "recording");
    const files = readdirSync(dir).filter((f) => f.endsWith(".spec.ts")).sort();
    assert.deepEqual(files, ["01-import.spec.ts", "02-cancel.spec.ts", "03-restart.spec.ts", "04-privacy.spec.ts"]);
    const support = readFileSync(path.join(dir, "support.ts"), "utf8");
    const features = [...support.matchAll(/^\s+(\w+): "(Запись: [^"]+)",$/gm)].map((m) => m[2]);
    assert.equal(features.length, 7);
    for (const f of files) {
      for (const m of readFileSync(path.join(dir, f), "utf8").matchAll(/details\(Feature\.(\w+),/g)) {
        assert.ok(support.includes(`${m[1]}: "`), `${f}: Feature.${m[1]} exists`);
      }
    }
    // the prerequisites are rows of the settings pack, under their real titles
    const settings = ["hub.spec.ts", "model.spec.ts", "schedule.spec.ts"].map((f) => readFileSync(path.join(e2eDir, "specs", "settings", f), "utf8")).join("\n");
    const titles = [...settings.matchAll(/^const \w+ = "([^"]+)";$/gm)].map((m) => m[1]);
    const needs = [...(/export const Prerequisites = \[([^\]]*)\]/.exec(support)?.[1] ?? "").matchAll(/"([^"]+)"/g)].map((m) => m[1]);
    assert.ok(needs.length >= 3, "the prerequisites are listed");
    for (const need of needs) {
      assert.ok(titles.includes(need), `«${need}» is a row of the settings pack`);
    }
  });
});
