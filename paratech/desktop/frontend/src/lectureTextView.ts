// The lecture text on the summary page without React (TASK-090): its state line, the start button and the blocks to
// show. The core owns the text and every state text. Headings and paragraphs are model output: they stay plain strings
// for React text nodes, never HTML, markdown or links.

// The .js extensions let the Node test runner load the compiled modules; TypeScript and Vite map them to the .ts files.
import { formatTime } from "./activityView.js";
import type { SummaryView } from "./bindings/summary.js";
import { type StepLine, stepLine } from "./summaryView.js";

/** The lecture text's state line, or null when the ready text says it all. */
export function lectureLine(v: Pick<SummaryView, "lectureText">): StepLine | null {
  const l = v.lectureText;
  return l.state === "done" && l.ready ? null : stepLine(l);
}

/** Under a ready lecture text while a new run waits, runs or failed. */
export const lectureEarlierNote = "Показан текст лекции, составленный раньше.";

export function showsEarlierLecture(v: Pick<SummaryView, "lectureText">): boolean {
  return v.lectureText.ready && v.lectureText.state !== "done";
}

/** «Составлен <дата, время>» of a ready lecture text; "" otherwise. */
export function lectureUpdatedText(v: Pick<SummaryView, "lectureText">): string {
  const l = v.lectureText;
  if (!l.ready || l.updatedAt === "") {
    return "";
  }
  const at = formatTime(l.updatedAt);
  return at === "—" ? "" : `Составлен ${at}`;
}

/** The label of the button that starts the lecture text, or null when the core offers none. */
export function lectureStartLabel(v: Pick<SummaryView, "lectureText">): string | null {
  const l = v.lectureText;
  if (l.retryWork !== "lectureText") {
    return null;
  }
  if (l.canStart) {
    return "Сделать текст лекции";
  }
  return l.state === "failed" ? "Повторить" : null;
}

/** One block as shown: the heading ("" for none), its start time and its paragraphs. */
export interface LectureBlockView {
  heading: string;
  time: string;
  paragraphs: string[];
}

/** The blocks of a ready lecture text without empty paragraphs and empty blocks. */
export function lectureBlocks(v: Pick<SummaryView, "lectureText">): LectureBlockView[] {
  if (!v.lectureText.ready) {
    return [];
  }
  return v.lectureText.blocks
    .map((b) => ({
      heading: b.heading.trim(),
      time: b.time,
      paragraphs: b.paragraphs.map((p) => p.trim()).filter((p) => p !== ""),
    }))
    .filter((b) => b.heading !== "" || b.paragraphs.length > 0);
}

export const lectureCopiedText = "Текст лекции скопирован в буфер обмена.";
