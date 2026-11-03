// Opening the lesson screen of a recording from any page (TASK-352): `const open = useOpenLesson(); open(recording.id)`.
// App provides the function; the lesson opens over the current section and «Назад» returns to it. This is the one
// replacement of the old summary and transcript viewers: the tab and the place are named as the old links named them.
import { createContext, useContext } from "react";
import type { LessonTab } from "./knowledgeView.js";

/**
 * tab undefined lets the lesson screen choose (its opening tab); index is the block of the lecture text, the segment of
 * the transcript or the summary section/point of the tab (-1 or undefined: the top).
 */
export type OpenLesson = (recordingId: string, tab?: LessonTab, index?: number) => void;

export const OpenLessonContext = createContext<OpenLesson>(() => undefined);

/** The function that opens the lesson of a recording by its id. */
export function useOpenLesson(): OpenLesson {
  return useContext(OpenLessonContext);
}
