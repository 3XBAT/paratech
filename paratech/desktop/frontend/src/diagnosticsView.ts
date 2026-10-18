// View logic of the "Диагностика" page: texts, button states and the lines of a result. The check and the bundle are
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
  stopCheck: "Остановить проверку",
  checkFailed: "Не удалось проверить звук.",
  sourcePrefix: "Источник: ",
  levelLabel: "Уровень звука",
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

export interface SoundDisplay {
  running: boolean;
  /** 0..1 for the level bar. */
  level: number;
  /** "" when there is no source to show. */
  source: string;
  lines: Line[];
}

/** What a check shows: the source, the level and the message; once done the verdict and its hints. */
export function soundDisplay(r: SoundCheck): SoundDisplay {
  const source = r.source === "" ? "" : texts.sourcePrefix + r.source;
  if (r.state === "idle") {
    return { running: false, level: 0, source: "", lines: [] };
  }
  if (r.state === "running") {
    return { running: true, level: r.level, source, lines: [{ text: r.message, tone: r.heard ? "ok" : "muted" }] };
  }
  const tone: Tone = r.verdict === "ok" ? "ok" : r.verdict === "busy" ? "warning" : "error";
  const lines: Line[] = [{ text: r.message, tone }, ...r.hints.map((h) => ({ text: h, tone: "muted" as Tone }))];
  return { running: false, level: 0, source, lines };
}

/** What a rejected check call shows: a fixed text, never the error. */
export function soundFailedDisplay(): SoundDisplay {
  return { running: false, level: 0, source: "", lines: [{ text: texts.checkFailed, tone: "error" }] };
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
