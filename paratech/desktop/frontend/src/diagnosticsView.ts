// View logic of the "Диагностика" page: texts, button states, the level bar, the lines of a result and the polling. The check and the bundle are
// made in the Go core (ARCH-44); here only what the page shows.

import type { BundleResult, SoundCheck } from "./bindings/diagnostics";

/** The class of a line, one of the page styles. */
export type Tone = "ok" | "warning" | "error" | "muted";

export interface Line {
  text: string;
  tone: Tone;
}

export const texts = {
  title: "Диагностика",
  lead: "Проверка звука и пакет для поддержки",
  soundTitle: "Проверка звука",
  soundExplain:
    "Приложение слушает выбранный для записи источник и показывает уровень звука. Ничего не проигрывается и не " +
    "записывается.",
  checkSound: "Проверить звук",
  stopCheck: "Остановить",
  checkFailed: "Не удалось проверить звук.",
  sourcePrefix: "Источник: ",
  playSomething: "Включите любой звук на компьютере (видео, музыку, звонок)",
  heard: "Звук слышен.",
  levelLabel: "Уровень звука",
  remainingPrefix: "Осталось ",
  secondsSuffix: " с",
  bundleTitle: "Пакет для поддержки",
  bundleExplain:
    "Zip-архив с журналами, версиями Windows и приложения и настройками — без паролей, ключей, аудио, " +
    "расшифровок и текстов уроков.",
  bundleLocal: "Он сохраняется только туда, куда вы выберете, и никуда не отправляется.",
  saveBundle: "Сохранить пакет для поддержки…",
  saving: "Сохраняем…",
  savedPrefix: "Сохранено: ",
  saveFailed: "Не удалось сохранить пакет для поддержки.",
} as const;

/** How often the page reads the state of a running check. */
export const pollMs = 100;

export interface ButtonState {
  label: string;
  disabled: boolean;
}

/** The sound button starts a check, or stops the running one; busy while a call is on the way. */
export function checkButton(running: boolean, busy: boolean): ButtonState {
  return { label: running ? texts.stopCheck : texts.checkSound, disabled: busy };
}

export function bundleButton(saving: boolean): ButtonState {
  return { label: saving ? texts.saving : texts.saveBundle, disabled: saving };
}

/** The bottom of the bar: quieter levels show as empty. Go rounds levels to 0.001 (-60 dBFS). */
export const levelFloorDb = -60;

/**
 * Maps a linear peak (0..1) to the width of the bar on a dB scale, 0..100, so quiet sound is visible as it is in the
 * meters of Windows and Zoom.
 */
export function levelPercent(level: number): number {
  if (!Number.isFinite(level) || level <= 0) {
    return 0;
  }
  const db = 20 * Math.log10(Math.min(level, 1));
  return Math.round(Math.max(0, ((db - levelFloorDb) / -levelFloorDb) * 100));
}

/**
 * Where the bar marks "слышно": values.HeardLevel of the core (0.01, -40 dBFS). Only a drawing; whether sound was
 * heard is always the core's heard flag.
 */
export const heardMarkPercent = levelPercent(0.01);

/** The whole seconds left, rounded up so the count reaches 0 only at the end. */
export function remainingText(ms: number): string {
  return texts.remainingPrefix + Math.ceil(Math.max(ms, 0) / 1000) + texts.secondsSuffix;
}

export type SoundPhase = "idle" | "running" | "done" | "failed";

export interface SoundDisplay {
  phase: SoundPhase;
  running: boolean;
  /** 0..100, the width of the level bar while running. */
  percent: number;
  /** The core heard sound; the bar shows it in the ok colour. */
  heard: boolean;
  /** "" when there is no source to show. */
  source: string;
  /** The hint to play something while running, else "". */
  prompt: string;
  /** The seconds left while running, else "". */
  remaining: string;
  lines: Line[];
}

/**
 * What a check shows. While it runs: the source, the hint to play something, the level and the seconds left, and
 * "Звук слышен" once the core heard sound. Once done: the core's verdict message and its hints.
 */
export function soundDisplay(r: SoundCheck): SoundDisplay {
  const source = r.source === "" ? "" : texts.sourcePrefix + r.source;
  const none = { percent: 0, heard: false, prompt: "", remaining: "" };
  if (r.state === "idle") {
    return { phase: "idle", running: false, ...none, source: "", lines: [] };
  }
  if (r.state === "running") {
    return {
      phase: "running", running: true, percent: levelPercent(r.level), heard: r.heard, source,
      prompt: texts.playSomething, remaining: remainingText(r.remainingMs),
      lines: r.heard ? [{ text: texts.heard, tone: "ok" }] : [],
    };
  }
  const tone: Tone = r.verdict === "ok" ? "ok" : r.verdict === "busy" ? "warning" : "error";
  const lines: Line[] = [{ text: r.message, tone }, ...r.hints.map((h) => ({ text: h, tone: "muted" as Tone }))];
  return { phase: "done", running: false, ...none, source, lines };
}

/** What a rejected check call shows: a fixed text, never the error. */
export function soundFailedDisplay(): SoundDisplay {
  return {
    phase: "failed", running: false, percent: 0, heard: false, source: "", prompt: "", remaining: "",
    lines: [{ text: texts.checkFailed, tone: "error" }],
  };
}

/** Timers of the poller: window.setTimeout in the page, a fake in tests. */
export interface Timers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

/**
 * Reads the state every `every` ms while the check runs. The next read is scheduled only after the previous answer,
 * so calls never overlap. Polling ends on a state other than running, on a failed read (onFail) and on the returned
 * stop(); an answer that arrives after stop() is dropped.
 */
export function pollSoundCheck(
  read: () => Promise<SoundCheck>,
  onState: (r: SoundCheck) => void,
  onFail: () => void,
  timers: Timers,
  every: number = pollMs,
): () => void {
  let stopped = false;
  let handle: unknown;

  const schedule = () => {
    handle = timers.set(tick, every);
  };
  function tick() {
    handle = undefined;
    read().then(
      (r) => {
        if (stopped) {
          return;
        }
        onState(r);
        if (r.state === "running") {
          schedule();
        }
      },
      () => {
        if (stopped) {
          return;
        }
        stopped = true;
        onFail();
      },
    );
  }

  schedule();
  return () => {
    stopped = true;
    if (handle !== undefined) {
      timers.clear(handle);
      handle = undefined;
    }
  };
}

/** The lines of a bundle answer; a cancelled save is one neutral line. */
export function bundleLines(r: BundleResult): Line[] {
  switch (r.status) {
    case "saved":
      return [
        { text: texts.savedPrefix + r.name, tone: "ok" },
        { text: r.message, tone: "muted" },
      ];
    case "cancelled":
      return [{ text: r.message, tone: "muted" }];
    default:
      return [{ text: r.message, tone: "error" }];
  }
}

/** What a rejected save call shows. */
export function bundleFailedLines(): Line[] {
  return [{ text: texts.saveFailed, tone: "error" }];
}
