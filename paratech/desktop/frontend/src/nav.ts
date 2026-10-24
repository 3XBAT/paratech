// Sections of the main window (TASK-124, docs/design/concept-agent.html). The side rail has six icons: Сегодня, Предметы,
// Задания, Расписание, Журнал, Настройки. Every screen is a page; a rail item covers one or more pages (Настройки holds
// the general settings, the speech models, the connections, diagnostics, the profile and the appearance; Предметы holds
// the knowledge base).

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .tsx file.
import type { IconName } from "./ui/Icon.js";

export type PageId =
  | "today"
  | "subjects"
  | "knowledge"
  | "tickets"
  | "homework"
  | "schedule"
  | "journal"
  | "settings"
  | "models"
  | "connections"
  | "diagnostics"
  | "profile"
  | "appearance";

export interface Page {
  id: PageId;
  label: string;
}

export const pages: readonly Page[] = [
  { id: "today", label: "Сегодня" },
  { id: "subjects", label: "Предметы" },
  { id: "knowledge", label: "База знаний" },
  { id: "tickets", label: "Билеты" },
  { id: "homework", label: "Задания" },
  { id: "schedule", label: "Расписание" },
  { id: "journal", label: "Журнал" },
  { id: "settings", label: "Настройки" },
  { id: "models", label: "Модели речи" },
  { id: "connections", label: "Подключения" },
  { id: "diagnostics", label: "Диагностика" },
  { id: "profile", label: "Профиль" },
  { id: "appearance", label: "Оформление" },
];

export interface RailItem {
  id: PageId;
  label: string;
  icon: IconName;
  /** The pages this item stands for: it is the current one on any of them. */
  covers: readonly PageId[];
}

/** The six icons of the rail, in order. */
export const rail: readonly RailItem[] = [
  { id: "today", label: "Сегодня", icon: "home", covers: ["today"] },
  { id: "subjects", label: "Предметы", icon: "folder", covers: ["subjects", "knowledge", "tickets"] },
  { id: "homework", label: "Задания", icon: "tasks", covers: ["homework"] },
  { id: "schedule", label: "Расписание", icon: "cal", covers: ["schedule"] },
  { id: "journal", label: "Журнал", icon: "journal", covers: ["journal"] },
  {
    id: "settings",
    label: "Настройки",
    icon: "set",
    covers: ["settings", "models", "connections", "diagnostics", "profile", "appearance"],
  },
];

/** The first item of the group pinned to the bottom of the rail. */
export const bottomGroupStart: PageId = "settings";

export const defaultPage: PageId = "today";

/** The rail item that stands for a page. */
export function railItemOf(page: PageId): RailItem {
  return rail.find((r) => r.covers.includes(page)) ?? rail[0];
}

/** The pages of «Настройки» in the order of its tabs. */
export const settingsPages: readonly PageId[] = ["settings", "models", "connections", "diagnostics", "profile", "appearance"];

/** The tab labels of «Настройки»; the first page is the general settings. */
export const settingsTabLabels: Readonly<Record<string, string>> = {
  settings: "Основные",
  models: "Модели речи",
  connections: "Подключения",
  diagnostics: "Диагностика",
  profile: "Профиль",
  appearance: "Оформление",
};

/** Returns the page with the id, or the default page for an unknown id. */
export function pageById(id: string): Page {
  return pages.find((p) => p.id === id) ?? pages.find((p) => p.id === defaultPage)!;
}

/**
 * Where the window is: a page and, over it, the transcript (TASK-079) or the summary (TASK-087) of a recording.
 * Neither is a page: they open from a recording ("" when closed), one at a time, and the page under them
 * stays as the user left it. assignment: the assignment «Задания» opens with ("" for the list).
 */
export interface Route {
  page: PageId;
  transcript: string;
  summary: string;
  assignment: string;
}

export const initialRoute: Route = { page: defaultPage, transcript: "", summary: "", assignment: "" };

/** A rail click: the page, with the transcript and the summary closed. */
export function routeToPage(id: PageId): Route {
  return { page: id, transcript: "", summary: "", assignment: "" };
}

/** Opens the transcript of a recording over the current section (in place of an open summary). */
export function routeToTranscript(r: Route, recordingId: string): Route {
  return { ...r, transcript: recordingId, summary: "" };
}

/** Opens the summary of a recording over the current section (in place of an open transcript). */
export function routeToSummary(r: Route, recordingId: string): Route {
  return { ...r, transcript: "", summary: recordingId };
}

/** Back from the transcript or the summary to the section it was opened from. */
export function closeTranscript(r: Route): Route {
  return { ...r, transcript: "", summary: "" };
}

/** Opens an assignment in «Задания» (from the summary). */
export function routeToAssignment(assignmentId: string): Route {
  return { page: "homework", transcript: "", summary: "", assignment: assignmentId };
}
