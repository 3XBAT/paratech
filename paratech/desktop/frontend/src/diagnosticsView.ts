// View logic of the "Диагностика" page: texts, button states and the lines of a result. The check and the bundle are
// made in the Go core (ARCH-44); here only what the page shows.

import type { BundleResult, SoundCheck, SoundModeName } from "./bindings/diagnostics";

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
    "Приложение проигрывает короткий сигнал через устройство вывода по умолчанию и проверяет, что его удаётся " +
    "записать. Тихая громкость не мешает проверке.",
  soundScope:
    "Проверяется, что запись работает на этом компьютере, а не что слышно выбранное приложение для уроков.",
  checkSound: "Проверить звук",
  checking: "Проверяем…",
  checkTakes: "Проверка занимает до 15 секунд.",
  checkFailed: "Не удалось проверить звук.",
  devicePrefix: "Устройство вывода: ",
  modesTitle: "Способы записи",
  modeWorks: "Работает",
  modeFailed: "Не работает",
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

export const modeLabels: Readonly<Record<SoundModeName, string>> = {
  include: "Захват приложения",
  system: "Весь звук компьютера (классический)",
};

/** Why a mode failed, shown under its label; an unknown cause reads as a failed check rather than as raw text. */
const causeTexts: Readonly<Record<string, string>> = {
  no_device: "Нет устройства вывода.",
  silence: "Сигнал не слышен.",
  unsupported: "Не поддерживается в этой системе.",
  hands_free: "Гарнитура в режиме разговора.",
  busy: "Недоступно во время записи.",
  failed: "Не удалось проверить.",
};

export function causeText(cause: string): string {
  return Object.hasOwn(causeTexts, cause) ? causeTexts[cause] : causeTexts.failed;
}

export interface ButtonState {
  label: string;
  disabled: boolean;
}

export function checkButton(checking: boolean): ButtonState {
  return { label: checking ? texts.checking : texts.checkSound, disabled: checking };
}

export function bundleButton(saving: boolean): ButtonState {
  return { label: saving ? texts.saving : texts.saveBundle, disabled: saving };
}

export interface ModeLine {
  label: string;
  text: string;
  ok: boolean;
}

export interface SoundDisplay {
  lines: Line[];
  modes: ModeLine[];
}

/** The lines of a check: the verdict, the device, the hint, then one line per capture mode. */
export function soundDisplay(r: SoundCheck): SoundDisplay {
  const tone: Tone = r.verdict === "ok" ? "ok" : r.verdict === "busy" ? "warning" : "error";
  const lines: Line[] = [{ text: r.message, tone }];
  if (r.device !== "") {
    lines.push({ text: texts.devicePrefix + r.device, tone: "muted" });
  }
  if (r.hint !== "") {
    lines.push({ text: r.hint, tone: "warning" });
  }
  const modes = r.modes.map((m) => ({
    label: modeLabels[m.mode],
    text: m.ok ? texts.modeWorks : causeText(m.cause),
    ok: m.ok,
  }));
  return { lines, modes };
}

/** What a rejected check call shows: a fixed text, never the error. */
export function soundFailedDisplay(): SoundDisplay {
  return { lines: [{ text: texts.checkFailed, tone: "error" }], modes: [] };
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
