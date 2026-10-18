// Runs on the modules compiled by `npm run test` (tsconfig.test.json) with the Node test runner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  bundleStatuses,
  diagnosticsApi,
  methods,
  parseBundleResult,
  parseSoundCheck,
  soundStates,
  soundVerdicts,
} from "../.test-build/bindings/diagnostics.js";
import {
  bundleButton,
  bundleFailedLines,
  bundleLines,
  checkButton,
  heardMarkPercent,
  levelFloorDb,
  levelPercent,
  pollMs,
  pollSoundCheck,
  remainingText,
  soundDisplay,
  soundFailedDisplay,
  texts,
} from "../.test-build/diagnosticsView.js";

// Mirrors the JSON of internal/input/wails/diagnostics.go (diagnostics_test.go).
const running = {
  state: "running", level: 0.123, peak: 0.5, heard: true, elapsedMs: 4200, remainingMs: 10800, durationMs: 15000,
  verdict: "", message: "Звук слышен.", hints: [], source: "zoom.exe", wholeComputer: false,
};
const silence = {
  ...running, state: "done", level: 0, peak: 0.001, heard: false, elapsedMs: 15000, remainingMs: 0, verdict: "silence",
  message: "Звук не слышен.", hints: ["Проверьте микшер."], source: "Весь звук компьютера", wholeComputer: true,
};
const idle = { ...running, state: "idle", level: 0, peak: 0, heard: false, elapsedMs: 0, remainingMs: 15000, message: "",
  source: "" };
const saved = { status: "saved", message: "Пакет для поддержки сохранён.", name: "la-support-20261016.zip" };

function fakeCall(reply) {
  const calls = [];
  const call = async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  };
  return { call, calls };
}

const src = (file) => readFileSync(new URL(file, import.meta.url), "utf8");

test("the api calls the bound Go methods without arguments", async () => {
  const p = "paratech/desktop/internal/input/wails.DiagnosticsService.";
  assert.deepEqual(Object.values(methods).sort(),
    ["SaveSupportBundle", "SoundCheckStatus", "StartSoundCheck", "StopSoundCheck"].map((m) => p + m));

  const sound = fakeCall(running);
  const api = diagnosticsApi(sound.call);
  assert.deepEqual(await api.startSoundCheck(), running);
  assert.deepEqual(await api.soundCheckStatus(), running);
  assert.deepEqual(await api.stopSoundCheck(), running);
  const bundle = fakeCall(saved);
  assert.deepEqual(await diagnosticsApi(bundle.call).saveSupportBundle(), saved);

  // The page never passes a path: the core asks for it in its own dialog.
  assert.deepEqual(sound.calls, [[p + "StartSoundCheck"], [p + "SoundCheckStatus"], [p + "StopSoundCheck"]]);
  assert.deepEqual(bundle.calls, [[p + "SaveSupportBundle"]]);
});

test("a failing call rejects", async () => {
  const api = diagnosticsApi(async () => {
    throw new Error("boom");
  });
  await assert.rejects(api.startSoundCheck(), /boom/);
  await assert.rejects(api.soundCheckStatus(), /boom/);
  await assert.rejects(api.stopSoundCheck(), /boom/);
  await assert.rejects(api.saveSupportBundle(), /boom/);
});

test("sound check replies are parsed strictly; extra fields are dropped", () => {
  assert.deepEqual(soundStates, ["idle", "running", "done"]);
  assert.deepEqual(soundVerdicts, ["", "ok", "silence", "not_running", "no_device", "unsupported", "busy", "failed"]);
  for (const verdict of soundVerdicts.filter((v) => v !== "")) {
    assert.equal(parseSoundCheck({ ...silence, verdict }).verdict, verdict);
  }
  assert.deepEqual(parseSoundCheck({ ...running, path: "C:\\x" }), running);
  assert.deepEqual(parseSoundCheck({ ...silence, hints: null }).hints, []);
  assert.deepEqual(parseSoundCheck(idle), idle);
  for (const raw of [
    null, "ok", {},
    { ...running, state: "paused" },
    { ...running, verdict: "ok" },
    { ...silence, verdict: "" },
    { ...silence, verdict: "toString" },
    { ...running, level: 1.5 },
    { ...running, level: -0.1 },
    { ...running, peak: "0.5" },
    { ...running, level: Number.NaN },
    { ...running, elapsedMs: -1 },
    { ...running, remainingMs: 1.5 },
    { ...running, durationMs: undefined },
    { ...running, heard: "yes" },
    { ...running, message: null },
    { ...running, hints: "x" },
    { ...running, hints: [1] },
    { ...running, source: undefined },
    { ...running, wholeComputer: 0 },
  ]) {
    assert.throws(() => parseSoundCheck(raw), /sound check/);
  }
});

test("bundle replies are parsed strictly: only a saved bundle has a name", () => {
  assert.deepEqual(bundleStatuses, ["saved", "cancelled", "failed"]);
  assert.deepEqual(parseBundleResult({ ...saved, path: "C:\\Users\\x\\la.zip" }), saved);
  const cancelled = { status: "cancelled", message: "Сохранение отменено.", name: "" };
  assert.deepEqual(parseBundleResult(cancelled), cancelled);
  for (const raw of [
    null, {},
    { ...saved, status: "ok" },
    { ...saved, name: "" },
    { ...cancelled, name: "la.zip" },
    { status: "failed", message: "m", name: "la.zip" },
    { ...saved, message: undefined },
    { ...saved, name: null },
  ]) {
    assert.throws(() => parseBundleResult(raw), /bundle result/);
  }
});

test("a running check shows the source, the hint, the level, the seconds left and «Звук слышен» once heard", () => {
  assert.deepEqual(soundDisplay(running), {
    phase: "running", running: true, percent: levelPercent(0.123), heard: true, source: "Источник: zoom.exe",
    prompt: "Включите любой звук на компьютере (видео, музыку, звонок)", remaining: "Осталось 11 с",
    lines: [{ text: "Звук слышен.", tone: "ok" }],
  });
  const quiet = soundDisplay({ ...running, heard: false, level: 0, message: "Включите звук." });
  assert.equal(quiet.percent, 0);
  assert.equal(quiet.heard, false);
  assert.deepEqual(quiet.lines, [], "nothing but the hint until sound is heard");
  const whole = soundDisplay({ ...running, source: "Весь звук компьютера", wholeComputer: true });
  assert.equal(whole.source, "Источник: Весь звук компьютера");
});

test("a finished check shows the verdict and the hints of the core; other verdicts show its message", () => {
  assert.deepEqual(soundDisplay(silence), {
    phase: "done", running: false, percent: 0, heard: false, source: "Источник: Весь звук компьютера", prompt: "",
    remaining: "", lines: [{ text: "Звук не слышен.", tone: "error" }, { text: "Проверьте микшер.", tone: "muted" }],
  });
  assert.deepEqual(soundDisplay({ ...silence, verdict: "ok", message: "Звук слышен.", hints: [] }).lines,
    [{ text: "Звук слышен.", tone: "ok" }]);
  assert.equal(soundDisplay({ ...silence, verdict: "busy" }).lines[0].tone, "warning");
  for (const verdict of ["not_running", "no_device", "unsupported", "failed"]) {
    const d = soundDisplay({ ...silence, verdict, message: "m-" + verdict, hints: ["h"] });
    assert.deepEqual(d.lines, [{ text: "m-" + verdict, tone: "error" }, { text: "h", tone: "muted" }]);
  }
  assert.deepEqual(soundDisplay(idle),
    { phase: "idle", running: false, percent: 0, heard: false, source: "", prompt: "", remaining: "", lines: [] });
  assert.deepEqual(soundFailedDisplay(), {
    phase: "failed", running: false, percent: 0, heard: false, source: "", prompt: "", remaining: "",
    lines: [{ text: "Не удалось проверить звук.", tone: "error" }],
  });
});

test("the level is shown on a dB scale from -60 dBFS, so quiet sound is visible", () => {
  assert.equal(levelFloorDb, -60);
  assert.equal(levelPercent(0), 0);
  assert.equal(levelPercent(0.001), 0); // -60 dBFS, the smallest level the core sends
  assert.equal(levelPercent(0.002), 10);
  assert.equal(levelPercent(0.01), 33); // -40 dBFS, the threshold of the core
  assert.equal(levelPercent(0.1), 67);
  assert.equal(levelPercent(0.5), 90);
  assert.equal(levelPercent(1), 100);
  assert.equal(levelPercent(2), 100);
  assert.equal(levelPercent(-1), 0);
  assert.equal(levelPercent(Number.NaN), 0);
  assert.ok(levelPercent(0.02) > 2 * 0.02 * 100, "a quiet level is wider than its linear value");
  for (let l = 0.001; l < 1; l *= 1.5) {
    assert.ok(levelPercent(l * 1.5) >= levelPercent(l), "the bar grows with the level");
  }
  assert.equal(heardMarkPercent, 33);
});

test("the seconds left are whole and rounded up", () => {
  assert.equal(remainingText(15000), "Осталось 15 с");
  assert.equal(remainingText(10800), "Осталось 11 с");
  assert.equal(remainingText(1), "Осталось 1 с");
  assert.equal(remainingText(0), "Осталось 0 с");
});

// Fake timers: run() fires the one pending timer. Reads are answered by the test, so an overlap would be visible.
function fakeTimers() {
  const pending = new Map();
  let next = 1;
  return {
    timers: {
      set(fn, ms) {
        assert.equal(ms, pollMs);
        pending.set(next, fn);
        return next++;
      },
      clear(h) {
        pending.delete(h);
      },
    },
    pending,
    run() {
      assert.equal(pending.size, 1, "exactly one timer is pending");
      const [h, fn] = [...pending][0];
      pending.delete(h);
      fn();
    },
  };
}

function deferredReads() {
  const waiting = [];
  const read = () => new Promise((resolve, reject) => waiting.push({ resolve, reject }));
  return { read, waiting };
}

const flush = () => new Promise((r) => setImmediate(r));

test("polling reads one at a time and ends when the check is done", async () => {
  const t = fakeTimers();
  const r = deferredReads();
  const states = [];
  let fails = 0;
  pollSoundCheck(r.read, (s) => states.push(s.state), () => fails++, t.timers);

  t.run();
  assert.equal(r.waiting.length, 1);
  assert.equal(t.pending.size, 0, "no next read before the answer");
  r.waiting[0].resolve(running);
  await flush();
  assert.equal(t.pending.size, 1);

  t.run();
  r.waiting[1].resolve(silence);
  await flush();
  assert.deepEqual(states, ["running", "done"]);
  assert.equal(t.pending.size, 0, "done ends the polling");
  assert.equal(fails, 0);
});

test("a failed read shows the failure once and ends the polling", async () => {
  const t = fakeTimers();
  const r = deferredReads();
  const states = [];
  let fails = 0;
  pollSoundCheck(r.read, (s) => states.push(s.state), () => fails++, t.timers);
  t.run();
  r.waiting[0].reject(new Error("boom"));
  await flush();
  assert.equal(fails, 1);
  assert.deepEqual(states, []);
  assert.equal(t.pending.size, 0);
});

test("stop clears the timer and drops an answer that is on the way", async () => {
  const t = fakeTimers();
  const stop = pollSoundCheck(async () => running, () => assert.fail("no state after stop"), () => assert.fail(),
    t.timers);
  stop();
  assert.equal(t.pending.size, 0);

  const t2 = fakeTimers();
  const r = deferredReads();
  let calls = 0;
  const stop2 = pollSoundCheck(r.read, () => calls++, () => calls++, t2.timers);
  t2.run();
  stop2();
  r.waiting[0].resolve(running);
  await flush();
  assert.equal(calls, 0);
  assert.equal(t2.pending.size, 0, "nothing is scheduled after stop");
});

test("the bundle result: the name when saved, a neutral line when cancelled", () => {
  assert.deepEqual(bundleLines(saved), [
    { text: "Сохранено: la-support-20261016.zip", tone: "ok" },
    { text: "Пакет для поддержки сохранён.", tone: "muted" },
  ]);
  assert.deepEqual(bundleLines({ status: "cancelled", message: "Сохранение отменено.", name: "" }), [
    { text: "Сохранение отменено.", tone: "muted" },
  ]);
  assert.deepEqual(bundleLines({ status: "failed", message: "Не удалось.", name: "" }), [
    { text: "Не удалось.", tone: "error" },
  ]);
  assert.deepEqual(bundleFailedLines(), [{ text: "Не удалось сохранить пакет для поддержки.", tone: "error" }]);
});

test("the sound button starts or stops; buttons are disabled while their call runs", () => {
  assert.deepEqual(checkButton(false, false), { label: "Проверить звук", disabled: false });
  assert.deepEqual(checkButton(true, false), { label: "Остановить", disabled: false });
  assert.deepEqual(checkButton(false, true), { label: "Проверить звук", disabled: true });
  assert.deepEqual(bundleButton(false), { label: "Сохранить пакет для поддержки…", disabled: false });
  assert.deepEqual(bundleButton(true), { label: "Сохраняем…", disabled: true });
  // 10 reads a second, as fast as the core reports levels at most (20 a second).
  assert.equal(pollMs, 100);
});

test("the explanations say what is checked and what the bundle holds; nothing speaks of a tone", () => {
  assert.match(texts.soundExplain, /Ничего не проигрывается/);
  const all = Object.values(texts).join(" ");
  assert.ok(!/тон|сигнал|режим/i.test(all), "a text still speaks of the old tone check");
  assert.match(texts.bundleExplain, /без паролей, ключей, аудио, расшифровок и текстов уроков/);
  assert.match(texts.bundleLocal, /никуда не отправляется/);
});

test("the page renders text only and never handles a path", () => {
  for (const file of ["../src/pages/DiagnosticsPage.tsx", "../src/diagnosticsView.ts", "../src/bindings/diagnostics.ts"]) {
    const s = src(file);
    for (const banned of ["dangerouslySetInnerHTML", "innerHTML", "console.", "localStorage", "sessionStorage", "href="]) {
      assert.ok(!s.includes(banned), `${file} uses ${banned}`);
    }
  }
  const page = src("../src/pages/DiagnosticsPage.tsx");
  assert.match(page, /api\.startSoundCheck\(\)/);
  assert.match(page, /api\.soundCheckStatus\(\)/);
  assert.match(page, /api\.stopSoundCheck\(\)/);
  assert.match(page, /api\.saveSupportBundle\(\)/);
  assert.ok(!/<input\b/.test(page), "the page asks for a path itself");
});

test("the level bar is an accessible meter and leaving the page stops a running check", () => {
  const page = src("../src/pages/DiagnosticsPage.tsx");
  assert.match(page, /role="meter"/);
  assert.match(page, /aria-valuenow=\{s\.percent\}/);
  assert.match(page, /aria-label=\{texts\.levelLabel\}/);
  assert.match(page, /pollSoundCheck\(/);
  assert.ok(!/setInterval/.test(page), "polling with an interval can overlap calls");
  // The cleanup of the mount effect stops the poller and a running check.
  assert.match(page, /return \(\) => \{\s*mounted\.current = false;\s*halt\(\);[\s\S]*?api\.stopSoundCheck\(\)/);

  const css = src("../src/styles.css");
  for (const gone of [".diag-wait", ".mode-cause", ".modes"]) {
    assert.ok(!css.includes(gone), `styles.css still has ${gone}`);
  }
  assert.match(css, /\.level b \{[^}]*transition: width 80ms/);
});
