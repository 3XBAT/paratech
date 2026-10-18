// Sections of the main window, in the order of the side rail (docs/design/concept-final.html). Homework has its own
// section right after «Записи и задачи»: the draft editor, its versions and sending by email (TASK-045).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .tsx file.
import type { IconName } from "./ui/Icon.js";

export type PageId = "recordings" | "homework" | "schedule" | "connections" | "models" | "diagnostics" | "settings";

export interface Page {
  id: PageId;
  label: string;
  icon: IconName;
}

export const pages: readonly Page[] = [
  { id: "recordings", label: "Записи и задачи", icon: "rec" },
  { id: "homework", label: "Домашние задания", icon: "doc" },
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

/**
 * Where the window is: a rail section and, over it, the transcript of a recording (TASK-079). The transcript is not a
 * rail section: it opens from a recording ("" when closed) and the section under it stays as the user left it.
 */
export interface Route {
  page: PageId;
  transcript: string;
}

export const initialRoute: Route = { page: defaultPage, transcript: "" };

/** A rail click: the section, with the transcript closed. */
export function routeToPage(id: PageId): Route {
  return { page: id, transcript: "" };
}

/** Opens the transcript of a recording over the current section. */
export function routeToTranscript(r: Route, recordingId: string): Route {
  return { page: r.page, transcript: recordingId };
}

/** Back from the transcript to the section it was opened from. */
export function closeTranscript(r: Route): Route {
  return { page: r.page, transcript: "" };
}
