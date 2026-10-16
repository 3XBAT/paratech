// How a status code of the core looks: chip kind, icon, label and a one-sentence hint (docs/design/brief.md section 7).
// Display only: whether a recording can be stopped or a job run now is still decided by the core (ARCH-44).

// The .js extensions let the Node test runner load the compiled module; TypeScript and Vite map them to the .ts files.
import { jobStatusLabel, recordingDurationText, recordingStatusLabel } from "./activityView.js";
import type { IconName } from "./ui/Icon.js";
import type { ChipKind } from "./ui/StatusChip.js";

export interface StatusView {
  kind: ChipKind;
  icon: IconName | null;
  label: string;
  /** Shown after the label in tabular digits ("0:42:17", "64 %"); "" for none. */
  value: string;
  /** Why it waits and what happens next; "" for none. */
  hint: string;
}

interface Look {
  kind: ChipKind;
  icon: IconName | null;
  hint: string;
}

const recordingLooks: Readonly<Record<string, Look>> = {
  STARTING: { kind: "run", icon: "clock", hint: "Подключение к звуку…" },
  RECORDING: { kind: "rec", icon: null, hint: "Окно можно закрыть — запись продолжится." },
  STOPPING: { kind: "run", icon: "clock", hint: "Несколько секунд." },
  FINALIZING: { kind: "run", icon: "clock", hint: "Несколько секунд." },
  COMPLETED: { kind: "ok", icon: "ok", hint: "" },
  FAILED: { kind: "fail", icon: "warn", hint: "" },
  RECOVERY_REQUIRED: {
    kind: "wait",
    icon: "warn",
    hint: "Запись прервалась. Сохранённая часть восстановится при следующем запуске.",
  },
};

const jobLooks: Readonly<Record<string, Look>> = {
  QUEUED: { kind: "neutral", icon: "clock", hint: "Начнётся после текущей задачи." },
  RUNNING: { kind: "run", icon: "wave", hint: "" },
  COMPLETED: { kind: "ok", icon: "ok", hint: "" },
  RETRY_WAIT: { kind: "wait", icon: "clock", hint: "Продолжится само." },
  CANCELLED: { kind: "neutral", icon: "x", hint: "" },
  FAILED: { kind: "fail", icon: "warn", hint: "" },
  WAITING_FOR_NETWORK: { kind: "net", icon: "off", hint: "Продолжится само, как только появится сеть." },
  WAITING_FOR_QUOTA: {
    kind: "wait",
    icon: "clock",
    hint: "Обработка продолжится, когда минуты пополнятся. Запись сохранена.",
  },
  WAITING_FOR_POWER: {
    kind: "wait",
    icon: "plug",
    hint: "Ноутбук на батарее, задача отложена. Начнётся сама после подключения питания.",
  },
};

// A running job is named by what it does.
const runningLabels: Readonly<Record<string, string>> = {
  stt: "Расшифровка",
  summarize: "Пишется конспект",
  extract_homework: "Поиск домашнего задания",
};

const runningHints: Readonly<Record<string, string>> = {
  summarize: "Обычно до минуты.",
};

const completedLabels: Readonly<Record<string, string>> = {
  summarize: "Конспект готов",
};

function own(map: Readonly<Record<string, string>>, key: string): string | undefined {
  return Object.hasOwn(map, key) ? map[key] : undefined;
}

const unknownLook: Look = { kind: "neutral", icon: null, hint: "" };

/** The chip of a recording; the running one shows its elapsed time by the client clock `now`. */
export function recordingStatusView(
  r: { status: string; startedAt: string; durationSeconds: number; hasDuration: boolean },
  now: number,
): StatusView {
  const look = Object.hasOwn(recordingLooks, r.status) ? recordingLooks[r.status] : unknownLook;
  return {
    ...look,
    label: recordingStatusLabel(r.status),
    value: r.status === "RECORDING" ? recordingDurationText(r, now) : "",
  };
}

/** The chip of a job; an unknown status is shown as is, as waiting when the core says so. */
export function jobStatusView(j: { type: string; status: string; waiting: boolean; progressPercent: number }): StatusView {
  const known = Object.hasOwn(jobLooks, j.status);
  const look = known ? jobLooks[j.status] : j.waiting ? { ...unknownLook, kind: "wait" as const, icon: "clock" as const } : unknownLook;
  let label = jobStatusLabel(j.status);
  let hint = look.hint;
  let value = "";
  if (j.status === "RUNNING") {
    label = own(runningLabels, j.type) ?? label;
    hint = own(runningHints, j.type) ?? hint;
    value = showsProgress(j) ? `${j.progressPercent} %` : "";
  } else if (j.status === "COMPLETED") {
    label = own(completedLabels, j.type) ?? label;
  }
  return { kind: look.kind, icon: look.icon, label, value, hint };
}

/** A running job with a known share done shows a progress bar. */
export function showsProgress(j: { status: string; progressPercent: number }): boolean {
  return j.status === "RUNNING" && j.progressPercent > 0;
}
