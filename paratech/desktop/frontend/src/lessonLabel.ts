// The lesson of an assignment as one line (TASK-092): «Предмет · тип · преподаватель» and «занятие ДД.ММ». The core
// picks the values and their source; here they are only arranged. A hint from the recording is model output: it stays
// a plain string, marked «по записи».

// The .js extension lets the Node test runner load the compiled module; TypeScript and Vite map it to the .ts file.
import { kindLabel } from "./scheduleView.js";

/** Where a subject or a teacher came from; "" when there is none. */
export type LessonSource = "schedule" | "transcript" | "user" | "";

/** The lesson fields of an assignment as the core sends them (homework and summary bindings). */
export interface LessonFields {
  subject: string;
  subjectSource: LessonSource;
  /** "lecture", "seminar", "practice", "lab" or "". */
  kind: string;
  teacher: string;
  teacherSource: LessonSource;
  /** RFC 3339 in UTC, "" when unknown. */
  lessonDate: string;
}

export const lessonSources: readonly LessonSource[] = ["schedule", "transcript", "user", ""];

/** A source code this page does not know is shown as no source rather than failing the reply. */
export function lessonSource(v: string): LessonSource {
  return lessonSources.includes(v as LessonSource) ? (v as LessonSource) : "";
}

export const fromRecording = "по записи";

function named(text: string, source: LessonSource): string {
  const t = text.trim();
  if (t === "") {
    return "";
  }
  return source === "transcript" ? `${t} (${fromRecording})` : t;
}

/** "Физика (по записи) · лекция · Иванов И. И."; empty parts are skipped, "" when all are empty. */
export function lessonLine(l: LessonFields): string {
  return [named(l.subject, l.subjectSource), kindLabel(l.kind), named(l.teacher, l.teacherSource)]
    .filter((p) => p !== "")
    .join(" · ");
}

/** "занятие 19.10" in local time; "" when the date is unknown or malformed. */
export function lessonDateText(lessonDate: string): string {
  if (lessonDate === "") {
    return "";
  }
  const d = new Date(lessonDate);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  const two = (n: number) => String(n).padStart(2, "0");
  return `занятие ${two(d.getDate())}.${two(d.getMonth() + 1)}`;
}

/** Both parts joined: "Физика · лекция · Иванов И. И. · занятие 19.10"; "" when there is nothing to show. */
export function lessonText(l: LessonFields): string {
  return [lessonLine(l), lessonDateText(l.lessonDate)].filter((p) => p !== "").join(" · ");
}
