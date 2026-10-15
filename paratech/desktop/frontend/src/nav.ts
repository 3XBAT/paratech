// Sections of the main window. Pages other than Recordings and Settings are placeholders filled by later tasks.

export type PageId = "recordings" | "models" | "homework" | "schedule" | "settings";

export interface Page {
  id: PageId;
  label: string;
}

export const pages: readonly Page[] = [
  { id: "recordings", label: "Записи" },
  { id: "models", label: "Модели" },
  { id: "homework", label: "Домашние задания" },
  { id: "schedule", label: "Расписание" },
  { id: "settings", label: "Настройки" },
];

export const defaultPage: PageId = "recordings";

/** Returns the page with the id, or the default page for an unknown id. */
export function pageById(id: string): Page {
  return pages.find((p) => p.id === id) ?? pages.find((p) => p.id === defaultPage)!;
}
