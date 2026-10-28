// «ИИ распознаёт фото конспектов» in the settings (TASK-294): texts and the switch logic only. Whether the Russian
// recognizer exists is the core's call (ARCH-44); the page shows what it says.

import type { NoteRecognition, Settings } from "./bindings/settings.js";

export const noteRecognitionTexts = {
  caption: "Конспекты",
  label: "ИИ распознаёт фото конспектов",
  hint: "Распознаёт Windows на этом компьютере, фото никуда не отправляются. Рукописный текст распознаётся хуже печатного.",
  languageMissing:
    "В Windows нет русского языка распознавания текста. Добавьте его: Параметры → Время и язык → Язык → Русский → Распознавание текста, затем вернитесь сюда.",
} as const;

/** Whether the switch is on: any engine but "off". */
export function noteRecognitionOn(s: Pick<Settings, "noteRecognition">): boolean {
  return s.noteRecognition !== "off";
}

/** The mode a switch click asks the core for. */
export function noteRecognitionModeFor(checked: boolean): NoteRecognition {
  return checked ? "local" : "off";
}

/** How to add the Russian recognizer: only with the switch on and the core saying it is missing. */
export function noteRecognitionLanguageHint(s: Pick<Settings, "noteRecognition" | "noteRecognitionLanguage">): string | null {
  return noteRecognitionOn(s) && s.noteRecognitionLanguage === "missing" ? noteRecognitionTexts.languageMissing : null;
}
