// Sections of the main window, in the order of the side rail (docs/design/concept-final.html). Homework is shown in
// «Записи и задачи», not in a section of its own.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .tsx file.
import type { IconName } from "./ui/Icon.js";

export type PageId = "recordings" | "schedule" | "connections" | "models" | "diagnostics" | "settings";

export interface Page {
  id: PageId;
  label: string;
  icon: IconName;
}

export const pages: readonly Page[] = [
  { id: "recordings", label: "Записи и задачи", icon: "rec" },
  { id: "schedule", label: "Расписание", icon: "cal" },
  { id: "connections", label: "Подключения", icon: "link" },
  { id: "models", label: "Модели речи", icon: "model" },
  { id: "diagnostics", label: "Диагностика", icon: "diag" },
  { id: "settings", label: "Настройки", icon: "set" },
];

/** The first section of the group pinned to the bottom of the rail. */
export const bottomGroupStart: PageId = "diagnostics";

export const defaultPage: PageId = "recordings";

/** Returns the page with the id, or the default page for an unknown id. */
export function pageById(id: string): Page {
  return pages.find((p) => p.id === id) ?? pages.find((p) => p.id === defaultPage)!;
}
