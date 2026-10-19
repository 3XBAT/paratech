// Texts of the recognition time estimates (TASK-084): «Запись 1 ч ≈ 3 ч 20 мин в режиме «Бережная»», the tooltip with
// every mode and «≈ N мин осталось». The numbers, their source and the heavy flag are the core's (ARCH-44).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import type { LoadEstimate, ModelLoadEstimates, RecognitionLoad } from "./bindings/load.js";

export const loadLabels: Readonly<Record<RecognitionLoad, string>> = {
  gentle: "Бережная",
  normal: "Обычная",
  max: "Максимальная",
};

/** One line under each mode on the settings page. */
export const loadDescriptions: Readonly<Record<RecognitionLoad, string>> = {
  gentle: "низкий приоритет, не больше ~40 % процессора",
  normal: "низкий приоритет, без потолка",
  max: "без ограничений, быстрее всего",
};

export const estimateTexts = {
  setting: "Нагрузка при распознавании",
  nextRun: "Применится к следующему распознаванию.",
  approximate: " (примерно)",
  heavy: " — дольше самой записи",
  unknown: "скорость неизвестна до первого распознавания",
} as const;

/**
 * A duration in whole minutes, rounded up, at least 1 мин: «45 мин», «3 ч 20 мин», «2 ч». "" for a negative or
 * non-finite number.
 */
export function durationText(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "";
  }
  const minutes = Math.max(1, Math.ceil(seconds / 60));
  if (minutes < 60) {
    return `${minutes} мин`;
  }
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return m === 0 ? `${h} ч` : `${h} ч ${m} мин`;
}

/** Whether the estimate has a number to show. */
export function knownEstimate(e: LoadEstimate | null | undefined): e is LoadEstimate {
  return e !== null && e !== undefined && e.source !== "unknown" && e.secondsPerHour > 0;
}

/** «≈ 3 ч 20 мин (примерно)»; "" when unknown. */
export function estimateValue(e: LoadEstimate | null | undefined): string {
  if (!knownEstimate(e)) {
    return "";
  }
  const approx = e.source === "table" ? estimateTexts.approximate : "";
  return `≈ ${durationText(e.secondsPerHour)}${approx}`;
}

/** The line of a model on «Модели речи»; text "" when there is no number. */
export interface EstimateLine {
  text: string;
  /** 1 h of audio takes longer than 1 h: the line is a warning. */
  heavy: boolean;
  /** Every mode, one per line: the tooltip. */
  tip: string;
}

const noLine: EstimateLine = { text: "", heavy: false, tip: "" };

/** «Бережная: ≈ 3 ч 20 мин (примерно)» per mode, one per line. */
export function estimatesTip(est: ModelLoadEstimates | null | undefined): string {
  if (est === null || est === undefined) {
    return "";
  }
  return est.modes
    .map((e) => `${loadLabels[e.mode]}: ${knownEstimate(e) ? estimateValue(e) : estimateTexts.unknown}`)
    .join("\n");
}

/** «Запись 1 ч ≈ 3 ч 20 мин в режиме «Бережная» (примерно)», for the current mode; nothing when unknown. */
export function modelEstimateLine(est: ModelLoadEstimates | null | undefined): EstimateLine {
  if (est === null || est === undefined || !knownEstimate(est.current)) {
    return noLine;
  }
  const c = est.current;
  const approx = c.source === "table" ? estimateTexts.approximate : "";
  return {
    text: `Запись 1 ч ≈ ${durationText(c.secondsPerHour)} в режиме «${loadLabels[c.mode]}»${approx}${c.heavy ? estimateTexts.heavy : ""}`,
    heavy: c.heavy,
    tip: estimatesTip(est),
  };
}

/**
 * «Модель «large-v3-f16»: запись 1 ч ≈ 3 ч 20 мин (примерно)» next to the setting, in the selected mode; null when
 * there is no model in use or no number.
 */
export function settingEstimateLine(
  est: ModelLoadEstimates | null,
  model: string,
  load: RecognitionLoad,
): { text: string; heavy: boolean } | null {
  if (est === null || model === "") {
    return null;
  }
  const e = est.modes.find((m) => m.mode === load) ?? (est.current.mode === load ? est.current : null);
  if (!knownEstimate(e)) {
    return null;
  }
  return {
    text: `Модель «${model}»: запись 1 ч ${estimateValue(e)}${e.heavy ? estimateTexts.heavy : ""}`,
    heavy: e.heavy,
  };
}

/** «≈ 25 мин осталось» of a queued or running transcription; "" when the core has no estimate. */
export function remainingText(r: { remainingSeconds?: number; hasRemaining?: boolean }): string {
  if (r.hasRemaining !== true || typeof r.remainingSeconds !== "number") {
    return "";
  }
  const d = durationText(r.remainingSeconds);
  return d === "" ? "" : `≈ ${d} осталось`;
}
