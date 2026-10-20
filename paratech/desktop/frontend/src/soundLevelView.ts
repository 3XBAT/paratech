// View logic of the sound level capsule (TASK-099): texts, the fill and the polling of the live sound. Whether sound is
// heard and when the quiet hint shows are the core's decisions (ARCH-44); here only the drawing.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import type { RecordingSound } from "./bindings/activity.js";
import { heardMarkPercent, levelPercent } from "./diagnosticsView.js";
import { plural } from "./plural.js";

/** About 4 reads a second, as the core measures (TASK-099). */
export const soundPollMs = 250;

/** The «слышно» mark: the same place as on the «Диагностика» bar. */
export const soundMarkPercent = heardMarkPercent;

export const soundTexts = {
  heard: "Звук идёт",
  quiet: "Тихо",
  labelPrefix: "Уровень звука: ",
  heardLabel: "звук идёт",
  quietLabel: "тихо",
  hint: "Если занятие идёт, а звука нет, возможно, выбран не тот источник.",
} as const;

export type SoundState = "heard" | "quiet" | "hint";

export interface SoundLevelModel {
  state: SoundState;
  /** Fill of the capsule, 0..100 (dB scale, as in «Диагностика»); 0 unless heard. */
  percent: number;
  /** The word next to the capsule: colour is never the only signal. */
  word: string;
  /** Accessible name of the capsule (role="img"). */
  label: string;
  /** The calm line under it; "" unless the core asks for the hint. */
  hint: string;
}

/** What the capsule shows; null when no recording captures (nothing to show). */
export function soundLevelModel(s: RecordingSound | null): SoundLevelModel | null {
  if (s === null || !s.recording) {
    return null;
  }
  if (s.heard) {
    return {
      state: "heard",
      percent: levelPercent(s.level),
      word: soundTexts.heard,
      label: soundTexts.labelPrefix + soundTexts.heardLabel,
      hint: "",
    };
  }
  if (!s.quietHint) {
    return { state: "quiet", percent: 0, word: soundTexts.quiet, label: soundTexts.labelPrefix + soundTexts.quietLabel, hint: "" };
  }
  const min = Math.max(1, Math.floor(s.quietMs / 60000));
  return {
    state: "hint",
    percent: 0,
    word: `${soundTexts.quiet} ${min} мин`,
    label: `${soundTexts.labelPrefix}звука нет ${min} ${plural(min, "минуту", "минуты", "минут")}`,
    hint: soundTexts.hint,
  };
}

export interface SoundTimers {
  set(fn: () => void, ms: number): unknown;
  clear(handle: unknown): void;
}

/** After a failed read the next one comes later: the core may be busy. */
export const soundRetryMs = 1000;

/** How many failed reads in a row keep the last good reading on screen. */
export const soundKeepOnFailures = 2;

/**
 * SoundPoller reads the live sound every soundPollMs while active. Reads never overlap: the next one is planned only
 * after the previous one has settled. A failed read keeps the last good reading for soundKeepOnFailures reads, then
 * shows nothing (null); after a failure it reads again in soundRetryMs. With untilIdle the polling pauses only after a
 * successful reply without a recording, until refreshNow: the popup asks only when it is shown.
 */
export class SoundPoller {
  private active = false;
  private busy = false;
  private again = false;
  private handle: unknown = undefined;
  /** Replies of an earlier activation are dropped. */
  private gen = 0;
  private failures = 0;

  constructor(
    private readonly read: () => Promise<RecordingSound>,
    private readonly apply: (s: RecordingSound | null) => void,
    private readonly timers: SoundTimers,
    private readonly untilIdle = false,
    private readonly ms = soundPollMs,
  ) {}

  /** Turns polling on (reading at once) or off (showing nothing). */
  setActive(on: boolean): void {
    if (on === this.active) {
      return;
    }
    this.active = on;
    this.gen++;
    this.again = false;
    this.failures = 0;
    this.cancel();
    if (on) {
      this.tick();
    } else {
      this.apply(null);
    }
  }

  /** Reads now when active; a read in flight is not doubled. */
  refreshNow(): void {
    if (this.active) {
      this.tick();
    }
  }

  private cancel(): void {
    if (this.handle !== undefined) {
      this.timers.clear(this.handle);
      this.handle = undefined;
    }
  }

  private plan(ms: number): void {
    this.handle = this.timers.set(() => {
      this.handle = undefined;
      this.tick();
    }, ms);
  }

  private tick(): void {
    this.cancel();
    if (this.busy) {
      this.again = true;
      return;
    }
    this.busy = true;
    const gen = this.gen;
    let next: RecordingSound | null = null;
    this.read()
      .then(
        (s) => {
          next = s;
        },
        () => undefined,
      )
      .finally(() => {
        this.busy = false;
        if (gen !== this.gen || !this.active) {
          // A read of an earlier activation: a new one may be waiting for it.
          if (this.active) {
            this.again = false;
            this.tick();
          }
          return;
        }
        const failed = next === null;
        if (failed) {
          this.failures++;
          if (this.failures > soundKeepOnFailures) {
            this.apply(null);
          }
        } else {
          this.failures = 0;
          this.apply(next);
        }
        const again = this.again;
        this.again = false;
        if (again) {
          this.tick();
        } else if (failed) {
          this.plan(soundRetryMs);
        } else if (!this.untilIdle || next?.recording === true) {
          this.plan(this.ms);
        }
      });
  }
}
