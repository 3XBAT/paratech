// Sections of the main window, in the order of the side rail (docs/design/concept-final.html). Homework has its own
// section right after «Записи и задачи»: the draft editor, its versions and sending by email (TASK-045); «База знаний»
// (TASK-091) follows it.

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .tsx file.
import type { IconName } from "./ui/Icon.js";

export type PageId =
  | "recordings"
  | "homework"
  | "knowledge"
  | "schedule"
  | "connections"
  | "models"
  | "diagnostics"
  | "settings";

export interface Page {
  id: PageId;
  label: string;
  icon: IconName;
}

export const pages: readonly Page[] = [
  { id: "recordings", label: "Записи и задачи", icon: "rec" },
  { id: "homework", label: "Домашние задания", icon: "doc" },
  { id: "knowledge", label: "База знаний", icon: "book" },
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
 * Where the window is: a rail section and, over it, the transcript (TASK-079) or the summary (TASK-087) of a recording.
 * Neither is a rail section: they open from a recording ("" when closed), one at a time, and the section under them
 * stays as the user left it. assignment: the assignment «Домашние задания» opens with ("" for the list).
 */
export interface Route {
  page: PageId;
  transcript: string;
  summary: string;
  assignment: string;
}

export const initialRoute: Route = { page: defaultPage, transcript: "", summary: "", assignment: "" };

/** A rail click: the section, with the transcript and the summary closed. */
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

/** Opens an assignment in «Домашние задания» (from the summary). */
export function routeToAssignment(assignmentId: string): Route {
  return { page: "homework", transcript: "", summary: "", assignment: assignmentId };
}
