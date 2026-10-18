// Display logic of the speech recognition component notice. The Go core decides the state and which buttons are
// offered (ARCH-44); the page turns them into texts. Messages of the core are fixed Russian texts shown as is.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import type { SpeechStage, SpeechStatus } from "./bindings/speech.js";
import { sizeText } from "./modelsView.js";

export const speechTexts = {
  installing: "Устанавливается компонент распознавания речи",
  unavailable: "Распознавание речи недоступно",
  noRestart: "Записи начнут распознаваться сразу после установки, перезапуск не нужен.",
  install: "Установить компонент распознавания",
  starting: "Запуск…",
  cancel: "Отменить",
  cancelling: "Отмена…",
  genericError: "Не удалось установить компонент распознавания речи",
} as const;

const stageLabels: Readonly<Record<SpeechStage, string>> = {
  "": "Установка",
  downloading: "Скачивание",
  verifying: "Проверка",
  unpacking: "Распаковка",
  activating: "Включение",
};

/** The Russian name of an install stage. */
export function stageLabel(stage: SpeechStage): string {
  return Object.hasOwn(stageLabels, stage) ? stageLabels[stage] : stageLabels[""];
}

/** Whole percent of the download, or null when there is no known size to show a bar for. */
export function installPercent(s: SpeechStatus): number | null {
  if (s.state !== "installing" || s.stage !== "downloading" || s.totalBytes <= 0) {
    return null;
  }
  return Math.min(100, Math.max(0, Math.floor((s.doneBytes * 100) / s.totalBytes)));
}

/** "Скачивание: 42% (12 МБ из 30 МБ)" while downloading, "Проверка…" for the later stages; "" when not installing. */
export function progressText(s: SpeechStatus): string {
  if (s.state !== "installing") {
    return "";
  }
  const label = stageLabel(s.stage);
  if (s.stage !== "downloading") {
    return `${label}…`;
  }
  const p = installPercent(s);
  return p !== null
    ? `${label}: ${p}% (${sizeText(s.doneBytes)} из ${sizeText(s.totalBytes)})`
    : `${label}: ${sizeText(s.doneBytes)}`;
}

/** What the notice shows; null when speech recognition works and there is nothing to say. */
export interface SpeechNotice {
  kind: "wait" | "fail";
  text: string;
  hint: string;
  progress: string;
  percent: number | null;
  install: boolean;
  cancel: boolean;
}

export function speechNotice(s: SpeechStatus): SpeechNotice | null {
  if (s.state === "ready") {
    return null;
  }
  const installing = s.state === "installing";
  return {
    kind: s.state === "failed" ? "fail" : "wait",
    text: installing ? speechTexts.installing : s.message || speechTexts.unavailable,
    hint: s.canInstall ? speechTexts.noRestart : "",
    progress: progressText(s),
    percent: installPercent(s),
    install: s.canInstall,
    cancel: s.canCancel,
  };
}

/**
 * The text of a failed StartInstall or CancelInstall. The core's refusals are fixed Russian sentences and are shown as
 * is; anything else (a runtime error that may name internals or a path) gets a generic text.
 */
export function speechErrorText(err: unknown): string {
  let msg = "";
  if (typeof err === "string") {
    msg = err;
  } else if (typeof err === "object" && err !== null && typeof (err as { message?: unknown }).message === "string") {
    msg = (err as { message: string }).message;
  }
  return /^[А-ЯЁ]/.test(msg) ? msg : speechTexts.genericError;
}
