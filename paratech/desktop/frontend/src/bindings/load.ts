// Recognition load modes and time estimates (TASK-084), as the Go core returns them (internal/input/wails/load.go).
// The settings and the models bindings share these shapes; the core computes every number.

/** «Бережная», «Обычная», «Максимальная». */
export type RecognitionLoad = "gentle" | "normal" | "max";

export const recognitionLoads: readonly RecognitionLoad[] = ["gentle", "normal", "max"];

/** table: the generic speed table, never shown (TASK-111); measured: runs on this computer; unknown: no number. */
export type EstimateSource = "table" | "measured" | "unknown";

const sources: readonly EstimateSource[] = ["table", "measured", "unknown"];

/** The processing time of a 1 h recording in one load mode. */
export interface LoadEstimate {
  mode: RecognitionLoad;
  /** Seconds of processing per 1 h of audio; 0 when unknown. */
  secondsPerHour: number;
  source: EstimateSource;
  /** 1 h of audio takes longer than 1 h. */
  heavy: boolean;
}

/** The estimates of one model: in the current mode and in every mode (gentle, normal, max). */
export interface ModelLoadEstimates {
  current: LoadEstimate;
  modes: LoadEstimate[];
}

function record(raw: unknown, what: string): Record<string, unknown> {
  if (typeof raw !== "object" || raw === null) {
    throw new Error(`${what}: reply is not an object`);
  }
  return raw as Record<string, unknown>;
}

function oneOf<T extends string>(v: unknown, key: string, what: string, allowed: readonly T[]): T {
  if (typeof v !== "string" || !(allowed as readonly string[]).includes(v)) {
    throw new Error(`${what}: unknown ${key}`);
  }
  return v as T;
}

/** A load mode of a reply; anything else is an error. */
export function parseRecognitionLoad(v: unknown, what: string): RecognitionLoad {
  return oneOf(v, "recognition load", what, recognitionLoads);
}

export function parseLoadEstimate(raw: unknown): LoadEstimate {
  const w = "load estimate";
  const r = record(raw, w);
  const s = r.secondsPerHour;
  if (typeof s !== "number" || !Number.isSafeInteger(s) || s < 0) {
    throw new Error(`${w}: secondsPerHour is not a count`);
  }
  if (typeof r.heavy !== "boolean") {
    throw new Error(`${w}: heavy is not a boolean`);
  }
  return {
    mode: parseRecognitionLoad(r.mode, w),
    secondsPerHour: s,
    source: oneOf(r.source, "source", w, sources),
    heavy: r.heavy,
  };
}

/** The estimates of a model, or null when the core sent none. */
export function parseModelLoadEstimates(raw: unknown): ModelLoadEstimates | null {
  if (raw === null) {
    return null;
  }
  const r = record(raw, "load estimates");
  const modes = r.modes;
  if (!Array.isArray(modes)) {
    throw new Error("load estimates: modes is not a list");
  }
  return { current: parseLoadEstimate(r.current), modes: modes.map(parseLoadEstimate) };
}
